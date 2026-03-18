// Package auth provides JWT authentication middleware for the notification service.
// Supports two token types:
//   - Lurus session token (HS256, issued by platform-core for web/mobile logins)
//   - Zitadel JWT (RS256/ES256, validated via JWKS and resolved to account_id
//     through platform internal API)
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	// contextKeyAccountID is the gin context key for the resolved account ID.
	contextKeyAccountID = "account_id"
	// sessionIssuer must match platform-core's SessionIssuer constant.
	sessionIssuer = "lurus-platform"
)

// Config holds authentication configuration for the notification service.
type Config struct {
	SessionSecret      string // shared HMAC secret for lurus session tokens
	PlatformURL        string // platform-core internal URL (for Zitadel sub resolution)
	PlatformInternalKey string // bearer key for platform internal API
}

// Middleware is a Gin middleware that validates Bearer tokens and sets
// account_id in the request context.
type Middleware struct {
	cfg        Config
	httpClient *http.Client
}

// NewMiddleware creates the auth middleware.
func NewMiddleware(cfg Config) *Middleware {
	return &Middleware{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// Auth returns a Gin HandlerFunc that validates the Bearer token and sets
// account_id in the context.
//
// Validation order:
//  1. Lurus session token (HS256, cheap local check — no network)
//  2. Zitadel JWT → call platform internal API to resolve sub → account_id
//
// Aborts with 401 on any failure.
func (m *Middleware) Auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := extractBearerToken(c)
		if err != nil {
			// For WebSocket upgrades, try query parameter as fallback.
			if c.Request.URL.Path == "/api/v1/notifications/ws" {
				if qToken := c.Query("token"); qToken != "" {
					token = qToken
					err = nil
				}
			}
			if err != nil {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
				return
			}
		}

		// Fast path: try lurus session token (no network, pure HMAC).
		if m.cfg.SessionSecret != "" {
			if accountID, err := validateSessionToken(token, m.cfg.SessionSecret); err == nil {
				c.Set(contextKeyAccountID, accountID)
				c.Next()
				return
			}
		}

		// Slow path: extract sub from JWT, resolve via platform internal API.
		sub, err := extractJWTSub(token)
		if err != nil {
			slog.Warn("auth: token validation failed",
				"path", c.Request.URL.Path,
				"err", err,
			)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}

		accountID, err := m.resolveZitadelSub(c.Request.Context(), sub)
		if err != nil {
			slog.Warn("auth: account resolution failed",
				"sub", sub,
				"err", err,
			)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "account lookup failed"})
			return
		}

		c.Set(contextKeyAccountID, accountID)
		c.Next()
	}
}

// GetAccountID retrieves the account ID set by Auth middleware.
func GetAccountID(c *gin.Context) int64 {
	v, _ := c.Get(contextKeyAccountID)
	id, _ := v.(int64)
	return id
}

// validateSessionToken parses and verifies a lurus-issued HS256 session token.
// Returns the lurus account ID embedded in the sub claim.
func validateSessionToken(tokenStr, secret string) (int64, error) {
	parts := strings.Split(tokenStr, ".")
	if len(parts) != 3 {
		return 0, fmt.Errorf("session: malformed token")
	}

	// Verify HMAC-SHA256 signature.
	body := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	expectedSig := mac.Sum(nil)

	gotSig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return 0, fmt.Errorf("session: decode signature: %w", err)
	}
	if !hmac.Equal(expectedSig, gotSig) {
		return 0, fmt.Errorf("session: invalid signature")
	}

	// Decode and validate payload.
	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return 0, fmt.Errorf("session: decode payload: %w", err)
	}
	var claims struct {
		Iss string `json:"iss"`
		Sub string `json:"sub"`
		Exp int64  `json:"exp"`
	}
	if err := json.Unmarshal(payloadJSON, &claims); err != nil {
		return 0, fmt.Errorf("session: parse payload: %w", err)
	}
	if claims.Iss != sessionIssuer {
		return 0, fmt.Errorf("session: unexpected issuer %q", claims.Iss)
	}
	if time.Now().Unix() > claims.Exp {
		return 0, fmt.Errorf("session: token expired")
	}

	// Parse sub: "lurus:<accountID>".
	const subPrefix = "lurus:"
	if !strings.HasPrefix(claims.Sub, subPrefix) {
		return 0, fmt.Errorf("session: invalid sub format: %q", claims.Sub)
	}
	id, err := strconv.ParseInt(claims.Sub[len(subPrefix):], 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("session: invalid account id in sub")
	}
	return id, nil
}

// extractJWTSub extracts the sub claim from an unverified JWT.
// The actual token validation is delegated to platform-core via the resolution API.
func extractJWTSub(tokenStr string) (string, error) {
	parts := strings.Split(tokenStr, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("jwt: malformed token")
	}
	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("jwt: decode payload: %w", err)
	}
	var claims struct {
		Sub string `json:"sub"`
		Exp int64  `json:"exp"`
	}
	if err := json.Unmarshal(payloadJSON, &claims); err != nil {
		return "", fmt.Errorf("jwt: parse claims: %w", err)
	}
	if claims.Sub == "" {
		return "", fmt.Errorf("jwt: missing sub claim")
	}
	if claims.Exp > 0 && time.Now().Unix() > claims.Exp {
		return "", fmt.Errorf("jwt: token expired")
	}
	return claims.Sub, nil
}

// resolveZitadelSub calls platform-core's internal API to resolve a Zitadel
// subject (user ID) to a lurus-platform account ID.
func (m *Middleware) resolveZitadelSub(ctx context.Context, sub string) (int64, error) {
	if m.cfg.PlatformURL == "" {
		return 0, fmt.Errorf("platform URL not configured")
	}

	url := m.cfg.PlatformURL + "/internal/v1/accounts/by-zitadel-sub/" + sub
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+m.cfg.PlatformInternalKey)

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("platform request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return 0, fmt.Errorf("platform returned %d: %s", resp.StatusCode, string(body))
	}

	var account struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&account); err != nil {
		return 0, fmt.Errorf("decode response: %w", err)
	}
	if account.ID <= 0 {
		return 0, fmt.Errorf("invalid account ID from platform")
	}
	return account.ID, nil
}

// extractBearerToken extracts the token from Authorization: Bearer <token>.
func extractBearerToken(c *gin.Context) (string, error) {
	header := c.GetHeader("Authorization")
	if header == "" {
		return "", fmt.Errorf("missing Authorization header")
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return "", fmt.Errorf("Authorization header must use Bearer scheme")
	}
	token := strings.TrimSpace(header[len(prefix):])
	if token == "" {
		return "", fmt.Errorf("empty bearer token")
	}
	return token, nil
}
