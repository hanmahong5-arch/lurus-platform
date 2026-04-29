package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// 共享 cookie 行为 — 所有发 token 的端点 (login / register / login-or-register
// / wechat-callback ...) 都走同一份配置，避免子域共享与 SameSite 之类的细节
// 漂移。这是 drop-in 模式的基石——子域产品看到 cookie 才能用 /whoami。

// SessionCookieName is the canonical browser-cookie key for the lurus
// session JWT. Once set, all *.lurus.cn subdomains can read it (subject
// to the request's Domain attribute matching) and bounce the user back
// to identity.lurus.cn for refresh.
const SessionCookieName = "lurus_session"

// SessionCookieTTL mirrors the JWT TTL issued by auth.IssueSessionToken
// in DirectLogin — keep in lock-step so the cookie does not outlive the
// JWT (browser would still send it but server would 401, looking like
// silent logout from the user's perspective).
const SessionCookieTTL = 30 * 24 * time.Hour

// SetSessionCookie writes the lurus session JWT as a parent-domain cookie
// so any *.lurus.cn subdomain can read it. Idempotent — overwriting an
// existing cookie is safe.
//
// Attribute reasoning:
//   - Domain=.lurus.cn        — cross-subdomain visibility (required for drop-in model)
//   - Path=/                  — every route on the subdomain can read it
//   - Secure=true             — HTTPS only; identity.lurus.cn is always TLS
//   - HttpOnly=true           — JS can't read; prevents naive XSS-token-theft
//   - SameSite=Lax            — top-level navigations between subdomains keep the cookie;
//                                cross-site fetch must use credentials: 'include'
//
// CookieDomain is read from env `LURUS_COOKIE_DOMAIN` so dev (localhost)
// and prod (.lurus.cn) can diverge without code changes.
func SetSessionCookie(c *gin.Context, token string, cookieDomain string) {
	if token == "" {
		return
	}
	domain := cookieDomain
	if domain == "" {
		// Empty Domain attribute = host-only cookie (only the issuing host
		// can read it). Safe fallback when env not set; loses the
		// drop-in cross-subdomain ability but doesn't break anything.
		domain = ""
	}
	// Gin's SetCookie wraps net/http SetCookie. SameSite must be set on
	// the response writer separately because Gin's signature doesn't
	// expose it.
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(
		SessionCookieName,
		token,
		int(SessionCookieTTL.Seconds()),
		"/",
		domain,
		true, // Secure
		true, // HttpOnly
	)
}

// ClearSessionCookie expires the cookie on logout. Domain must match what
// SetSessionCookie wrote, otherwise the browser keeps the old cookie.
func ClearSessionCookie(c *gin.Context, cookieDomain string) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(SessionCookieName, "", -1, "/", cookieDomain, true, true)
}

// ReadSessionToken extracts the lurus session JWT from either the cookie
// or the Authorization Bearer header. Cookie wins on conflict because
// browsers reliably attach it; Bearer is the SDK / curl path.
//
// Returns "" when neither is present.
func ReadSessionToken(c *gin.Context) string {
	if cookie, err := c.Cookie(SessionCookieName); err == nil {
		if cookie = strings.TrimSpace(cookie); cookie != "" {
			return cookie
		}
	}
	header := c.GetHeader("Authorization")
	if strings.HasPrefix(header, "Bearer ") {
		return strings.TrimSpace(header[len("Bearer "):])
	}
	return ""
}
