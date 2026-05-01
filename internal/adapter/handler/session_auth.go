package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/auth"
)

// ── Session-token auth middleware ────────────────────────────────────────────
//
// /api/v1/whoami and /api/v1/account/me/llm-token sit OUTSIDE the standard
// `/api/v1/*` JWT middleware group because they accept BOTH the parent-
// domain `lurus_session` cookie (drop-in for *.lurus.cn products) and the
// Authorization Bearer header — the standard JWTMiddleware only knows
// Bearer.
//
// Before P1-6 these routes had no per-user rate limit because PerUser
// reads `account_id` from gin.Context, which only the JWT middleware ever
// set. RequireSession closes that gap by parsing+validating the lurus
// session token, optionally checking the P1-5 revoke list, and seeding
// `account_id` so RateLimit.PerUser() further down the chain just works.

// SessionAuthDeps is the constructor input for RequireSession. Kept as a
// struct so adding future knobs (custom 401 writer, max-age cap) doesn't
// churn every router call site.
type SessionAuthDeps struct {
	// Secret is the HS256 key used by auth.IssueSessionToken. Empty
	// secret causes the middleware to refuse traffic with 503 — that
	// matches the existing fallback in WhoamiHandler / LLMTokenHandler
	// so behaviour is identical regardless of which gate fires first.
	Secret string

	// Revoker is the optional server-side JWT revoke list (P1-5).
	// nil = revocation check is a no-op, so a missing Redis or a
	// pre-P1-5 deployment keeps working unchanged.
	Revoker *auth.SessionRevoker
}

// RequireSession returns a Gin middleware that:
//  1. extracts the lurus session token from cookie OR Bearer
//  2. validates HMAC + expiry via auth.ValidateSession
//  3. checks the revoke list (when wired) so logged-out tokens are rejected
//  4. seeds c.Set("account_id", id) for RateLimit.PerUser() and handlers
//
// 401 is returned for missing/invalid/revoked tokens — uniformly, with
// the same message envelope, so the caller can't distinguish "no token"
// from "wrong sig" from "revoked" (same reasoning as WhoamiHandler.Whoami).
//
// 503 is returned only when Secret is unset — that's a deployment misconfig,
// not a per-request condition; serving 401 there would silently lock out
// legitimate users without flagging the misconfig.
func RequireSession(deps SessionAuthDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if deps.Secret == "" {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"error": "session validation not configured",
			})
			return
		}

		token := ReadSessionToken(c)
		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "unauthenticated",
			})
			return
		}

		claims, err := auth.ValidateSession(token, deps.Secret)
		if err != nil {
			// Don't disclose whether the token shape was wrong vs expired vs
			// signature mismatch — clients don't need to branch and detail
			// helps attackers calibrate.
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "invalid or expired session",
			})
			return
		}

		// Revoke check sits AFTER signature/expiry so we never hit Redis
		// for tokens that would have been rejected anyway.
		if deps.Revoker != nil && deps.Revoker.IsRevoked(c.Request.Context(), token) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "invalid or expired session",
			})
			return
		}

		c.Set(auth.ContextKeyAccountID, claims.AccountID)
		c.Next()
	}
}
