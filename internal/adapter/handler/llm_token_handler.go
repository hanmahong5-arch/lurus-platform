package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/hanmahong5-arch/lurus-platform/internal/module/newapi_sync"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/auth"
)

// LLMTokenHandler exposes GET /api/v1/account/me/llm-token — the
// "drop-in" endpoint that lets a *.lurus.cn product fetch its current
// user's NewAPI bearer token. The product then calls NewAPI's
// OpenAI-compatible /v1/* endpoints directly with that token; platform
// is out of the LLM hot path entirely (see C.2 step 4e in
// docs/ADR-newapi-billing-sync.md).
//
// Decoupling: this handler delegates orchestration to newapi_sync.Module;
// it doesn't know what NewAPI is. Adding a new LLM provider tomorrow
// would mean swapping the module impl, not rewriting this file.
//
// Idempotency: GET semantics + NewAPI's per-user-per-name idempotent
// upsert mean repeated calls return the same key. Products can cache
// freely.
//
// Auth: same shape as /whoami — accepts cookie or Bearer. Reusing the
// auth code path from session_cookie.go keeps the contract uniform
// across drop-in endpoints.
type LLMTokenHandler struct {
	sessionSecret string
	module        *newapi_sync.Module // nil-safe: returns 503 when sync unwired
}

// NewLLMTokenHandler builds the handler. module=nil is allowed (e.g. dev
// without NEWAPI env vars) — endpoint registers but returns 503 to make
// the misconfiguration visible at request time rather than silently
// returning 404 from the SPA fallback.
func NewLLMTokenHandler(sessionSecret string, mod *newapi_sync.Module) *LLMTokenHandler {
	return &LLMTokenHandler{sessionSecret: sessionSecret, module: mod}
}

// llmTokenResponse is the wire shape products consume. Stable fields:
//
//	key             — the raw "sk-xxx" bearer for NewAPI /v1/* endpoints
//	base_url        — where to point the OpenAI SDK (helps clients avoid
//	                  hardcoding the host)
//	name            — token name (debug/audit; default "lurus-platform-default")
//	unlimited_quota — true ⇒ NewAPI doesn't apply per-token cap (platform
//	                  meters via its own wallet)
//
// Adding fields here is fine; renaming or removing fields breaks every
// downstream product, so don't.
type llmTokenResponse struct {
	Key            string `json:"key"`
	BaseURL        string `json:"base_url"`
	Name           string `json:"name"`
	UnlimitedQuota bool   `json:"unlimited_quota"`
}

// Get serves GET /api/v1/account/me/llm-token. Status code semantics:
//
//	200 — token issued / reused, body has Key
//	401 — missing or invalid session
//	503 — module not configured (NEWAPI env unset) OR account not yet
//	      provisioned in NewAPI (transient: register-hook still running
//	      or the back-fill cron hasn't seen this account)
//	502 — NewAPI is down or returned an unexpected error
//
// 503 vs 502 distinction matters: 503 says "try again in a moment", 502
// says "platform is in a bad state, escalate". Lucrum / Tally clients
// can branch on it.
func (h *LLMTokenHandler) Get(c *gin.Context) {
	if h.sessionSecret == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "session validation not configured"})
		return
	}
	if h.module == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":   "newapi_sync_disabled",
			"message": "platform is not configured to issue LLM tokens — NEWAPI_* env not set",
		})
		return
	}

	// Reuse /whoami's auth machinery so cookies + Bearer both work.
	token := ReadSessionToken(c)
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}
	accountID, err := auth.ValidateSessionToken(token, h.sessionSecret)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired session"})
		return
	}

	// `name` query param is an extension point — products can request
	// scoped keys later (e.g. ?name=lucrum). Empty → DefaultTokenName.
	name := c.Query("name")

	tok, err := h.module.EnsureUserLLMToken(c.Request.Context(), accountID, name)
	if err != nil {
		if errors.Is(err, newapi_sync.ErrAccountNotProvisioned) {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error":   "account_not_provisioned",
				"message": "NewAPI mirror not yet created for this account; retry in a few seconds",
			})
			return
		}
		slog.WarnContext(c.Request.Context(), "llm-token: ensure failed", "account_id", accountID, "err", err)
		c.JSON(http.StatusBadGateway, gin.H{
			"error":   "newapi_unavailable",
			"message": "NewAPI did not return a usable token — platform admin should investigate",
		})
		return
	}

	c.JSON(http.StatusOK, llmTokenResponse{
		Key:            tok.Key,
		BaseURL:        h.baseURL(),
		Name:           tok.Name,
		UnlimitedQuota: tok.UnlimitedQuota,
	})
}

// baseURL is the public-facing NewAPI host that products should point
// their OpenAI clients at. Hardcoded for now (matches the prod ingress);
// when we go multi-region we'll source it from env. Centralising here
// means clients don't need to know.
func (h *LLMTokenHandler) baseURL() string {
	return "https://newapi.lurus.cn/v1"
}
