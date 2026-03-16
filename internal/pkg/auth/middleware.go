package auth

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	// ContextKeyAccountID is the gin context key for the resolved account ID.
	ContextKeyAccountID = "account_id"
	// ContextKeyClaims is the gin context key for the parsed Zitadel JWT claims.
	ContextKeyClaims = "jwt_claims"
)

// AccountLookup resolves a Zitadel JWT claims to a lurus-platform account ID.
// On first login (DB miss), implementations should auto-create the account via
// the claims fields (sub, email, name) rather than returning an error.
type AccountLookup func(ctx context.Context, claims *Claims) (int64, error)

// JWTMiddleware is a Gin middleware factory for JWT validation.
// It supports two token types:
//   - lurus session token (HS256, issued by this service for WeChat logins)
//   - Zitadel JWT (RS256/ES256, issued by Zitadel OIDC server)
type JWTMiddleware struct {
	validator     *Validator
	lookup        AccountLookup
	sessionSecret string // for lurus-issued HS256 session tokens; empty = disabled
}

// NewJWTMiddleware creates the middleware. sessionSecret enables lurus session token
// validation in addition to Zitadel JWT. Pass "" to disable session tokens.
func NewJWTMiddleware(v *Validator, lookup AccountLookup, sessionSecret string) *JWTMiddleware {
	return &JWTMiddleware{validator: v, lookup: lookup, sessionSecret: sessionSecret}
}

// Auth returns a Gin HandlerFunc that validates the Bearer JWT and sets
// account_id (and optionally jwt_claims) in the context.
//
// Validation order:
//  1. Lurus session token (HS256, cheap local check — no network)
//  2. Zitadel JWT (RS256/ES256, requires JWKS fetch on first use)
//
// Aborts with 401 on any failure.
func (m *JWTMiddleware) Auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := extractBearerToken(c)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			return
		}

		// Fast path: try lurus session token (no network, pure HMAC).
		if m.sessionSecret != "" {
			if accountID, err := ValidateSessionToken(token, m.sessionSecret); err == nil {
				c.Set(ContextKeyAccountID, accountID)
				c.Next()
				return
			}
		}

		// Slow path: Zitadel JWT validation (may fetch JWKS on cache miss).
		claims, err := m.validator.Validate(c.Request.Context(), token)
		if err != nil {
			slog.Warn("auth: JWT validation failed",
				"path", c.Request.URL.Path,
				"err", err,
				"token_prefix", safeTokenPrefix(token),
			)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}

		accountID, err := m.lookup(c.Request.Context(), claims)
		if err != nil {
			slog.Error("auth: account lookup failed",
				"path", c.Request.URL.Path,
				"sub", claims.Sub,
				"err", err,
			)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "account lookup failed"})
			return
		}

		c.Set(ContextKeyAccountID, accountID)
		c.Set(ContextKeyClaims, claims)
		c.Next()
	}
}

// AdminAuth returns a Gin HandlerFunc that validates the JWT AND requires an
// admin role. Lurus session tokens are not accepted here — admin access requires
// a Zitadel JWT with the configured admin role.
// Returns 401 for missing/invalid tokens, 403 for missing role.
func (m *JWTMiddleware) AdminAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := extractBearerToken(c)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			return
		}

		claims, err := m.validator.Validate(c.Request.Context(), token)
		if err != nil {
			slog.Warn("auth: admin JWT validation failed",
				"path", c.Request.URL.Path,
				"err", err,
				"token_prefix", safeTokenPrefix(token),
			)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}

		if !m.validator.HasAdminRole(claims) {
			slog.Warn("auth: admin role missing",
				"path", c.Request.URL.Path,
				"sub", claims.Sub,
				"roles", claims.Roles,
			)
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin role required"})
			return
		}

		accountID, err := m.lookup(c.Request.Context(), claims)
		if err != nil {
			slog.Error("auth: admin account lookup failed",
				"path", c.Request.URL.Path,
				"sub", claims.Sub,
				"err", err,
			)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "account lookup failed"})
			return
		}

		c.Set(ContextKeyAccountID, accountID)
		c.Set(ContextKeyClaims, claims)
		c.Next()
	}
}

// extractBearerToken extracts the token from Authorization: Bearer <token>.
func extractBearerToken(c *gin.Context) (string, error) {
	header := c.GetHeader("Authorization")
	if header == "" {
		return "", &errUnauthorized{"missing Authorization header"}
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return "", &errUnauthorized{"Authorization header must use Bearer scheme"}
	}
	token := strings.TrimSpace(header[len(prefix):])
	if token == "" {
		return "", &errUnauthorized{"empty bearer token"}
	}
	return token, nil
}

type errUnauthorized struct{ msg string }

func (e *errUnauthorized) Error() string { return e.msg }

// safeTokenPrefix returns the first 16 chars of a token for debug logging.
func safeTokenPrefix(token string) string {
	if len(token) <= 16 {
		return token[:len(token)/2] + "..."
	}
	return token[:16] + "..."
}

// GetAccountID retrieves the account ID set by Auth middleware.
// Returns 0 if not set (should not happen on authenticated routes).
func GetAccountID(c *gin.Context) int64 {
	v, _ := c.Get(ContextKeyAccountID)
	id, _ := v.(int64)
	return id
}

// GetClaims retrieves the JWT claims set by Auth middleware.
// Returns nil for lurus session token authenticated requests.
func GetClaims(c *gin.Context) *Claims {
	v, _ := c.Get(ContextKeyClaims)
	claims, _ := v.(*Claims)
	return claims
}
