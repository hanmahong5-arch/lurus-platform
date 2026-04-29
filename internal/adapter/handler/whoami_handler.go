package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/hanmahong5-arch/lurus-platform/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/auth"
)

// WhoamiHandler implements GET /api/v1/whoami — the single endpoint a
// downstream product needs to call to learn "who is this user".
//
// Design contract (drop-in vision):
//   - Reads cookie OR Bearer token (no other auth shapes)
//   - Returns FLAT JSON with stable field names (no nested orgs/permissions)
//   - 401 on missing/invalid auth — never reveals whether the account exists
//   - Response is the canonical product-facing user shape; new fields must
//     be additive
//
// Multi-tenant policy: by default does NOT include org_id (per
// docs/多租户简化.md). Products that need org context call separate
// /api/v1/account/me/orgs.
type WhoamiHandler struct {
	accounts      *app.AccountService
	sessionSecret string
	// cookieDomain = "" means the cookie was issued host-only; we still
	// read it via c.Cookie() since the browser sends what it has.
}

// NewWhoamiHandler constructs the handler. sessionSecret must be the
// same secret used by auth.IssueSessionToken — otherwise tokens will not
// validate even when freshly issued.
func NewWhoamiHandler(accounts *app.AccountService, sessionSecret string) *WhoamiHandler {
	return &WhoamiHandler{accounts: accounts, sessionSecret: sessionSecret}
}

// whoamiResponse is the wire shape. Keep the field names short and
// stable — products will consume this as a contract.
//
// `phone` is masked so a leaked /whoami response can't be replayed for
// SMS phishing. Products that actually need the raw phone use a
// different scoped endpoint.
type whoamiResponse struct {
	AccountID   int64  `json:"account_id"`
	LurusID     string `json:"lurus_id"`
	DisplayName string `json:"display_name,omitempty"`
	Email       string `json:"email,omitempty"`
	Phone       string `json:"phone,omitempty"`
}

// Whoami serves GET /api/v1/whoami.
func (h *WhoamiHandler) Whoami(c *gin.Context) {
	if h.sessionSecret == "" {
		// Refuse to serve unauthenticated traffic when the validating
		// secret is missing — fail closed.
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "session validation not configured"})
		return
	}

	token := ReadSessionToken(c)
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}

	accountID, err := auth.ValidateSessionToken(token, h.sessionSecret)
	if err != nil {
		// Don't disclose whether the token shape was wrong vs expired vs
		// signature mismatch — clients don't need to know, and detail
		// helps attackers calibrate.
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired session"})
		return
	}

	acc, err := h.accounts.GetByID(c.Request.Context(), accountID)
	if err != nil {
		// Account-was-deleted-but-token-still-valid → return 401 so the
		// client clears the cookie and re-logins. We deliberately don't
		// distinguish "not found" vs "DB error" — clients shouldn't
		// branch on that, and 401 leaks the least info to stale-token
		// holders.
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}

	c.JSON(http.StatusOK, whoamiResponse{
		AccountID:   acc.ID,
		LurusID:     acc.LurusID,
		DisplayName: acc.DisplayName,
		Email:       acc.Email,
		Phone:       maskPhone(acc.Phone),
	})
}

// Logout clears the cookie so the user returns to a logged-out state on
// every *.lurus.cn subdomain. Idempotent — calling without a cookie is
// a no-op 200.
//
// POST so it's not GET-cacheable and not triggerable from a casual link.
func (h *WhoamiHandler) Logout(c *gin.Context, cookieDomain string) {
	ClearSessionCookie(c, cookieDomain)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// maskPhone hides middle digits of an E.164 / domestic CN phone string
// for safe display in /whoami responses.
//
//	+8613811112222 → +86138****2222
//	13811112222    → 138****2222
//	+11234567890   → +1123****7890
//	(empty)        → ""
//
// Country-code parsing is deliberately enumerated rather than auto-derived
// because E.164 country codes are 1–3 digits with no length-encoded prefix
// — auto-deriving leads to off-by-one bugs (e.g. taking 3 digits of "+86…"
// instead of 2). We only carry the markets we actually serve; falling back
// to "no prefix" for unknown shapes is harmless because the mask still
// hides the middle of the body.
func maskPhone(phone string) string {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return ""
	}
	prefix, body := splitCountryCode(phone)
	if len(body) < 7 {
		return phone
	}
	masked := body[:3] + strings.Repeat("*", len(body)-7) + body[len(body)-4:]
	return prefix + masked
}

func splitCountryCode(phone string) (prefix, body string) {
	switch {
	case strings.HasPrefix(phone, "+86"):
		return "+86", phone[3:]
	case strings.HasPrefix(phone, "+852"):
		return "+852", phone[4:]
	case strings.HasPrefix(phone, "+1"):
		return "+1", phone[2:]
	}
	return "", phone
}
