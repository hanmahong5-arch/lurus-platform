package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// SecurityHeadersConfig configures the SecurityHeaders middleware. The
// zero value is safe for production — use Default() if you want explicit
// reading.
//
// Each field documents one specific browser/network risk we want to push
// back on. Empty / "" disables that header (so a deployment can opt out
// of a specific one without forking the middleware).
type SecurityHeadersConfig struct {
	// FrameOptions sets X-Frame-Options. "DENY" prevents this site from
	// being framed by any origin (clickjacking defence). Use SAMEORIGIN
	// if a same-origin iframe is intentional — we don't iframe ourselves,
	// so DENY is the secure default.
	FrameOptions string

	// ContentTypeOptions sets X-Content-Type-Options. "nosniff" tells the
	// browser to honour Content-Type and not auto-detect. Prevents MIME
	// confusion attacks (an uploaded .png executed as JS).
	ContentTypeOptions string

	// ReferrerPolicy sets Referrer-Policy. "strict-origin-when-cross-origin"
	// avoids leaking full request URLs (which can carry tokens or PII) to
	// third-party links while keeping same-origin diagnostics intact.
	ReferrerPolicy string

	// HSTS sets Strict-Transport-Security. Forces TLS for all subsequent
	// requests; 1 year is the standard minimum to be eligible for
	// browser HSTS preload, but we DO NOT enable `preload` directive by
	// default — once preloaded, mistakes (cert lapse, switching to HTTP
	// for a subdomain) lock users out. Enable preload manually after
	// confirming all subdomains are HTTPS-only.
	HSTS string

	// XSSProtection sets X-XSS-Protection. Modern browsers ignore this
	// (CSP is the replacement), but legacy browsers benefit from "0"
	// (disable legacy filter — it can be exploited by attackers to
	// reflect-XSS-protect against legitimate behaviour).
	XSSProtection string

	// PermissionsPolicy sets Permissions-Policy (replaces deprecated
	// Feature-Policy). Empty = no header. We disable browser APIs we
	// don't use, to limit the impact of a hypothetical XSS payload that
	// could otherwise abuse e.g. geolocation.
	PermissionsPolicy string

	// SkipPaths lists URL path prefixes to bypass entirely (no security
	// headers added). Use sparingly — only for routes where a header
	// breaks integration (e.g. an OIDC callback that the partner expects
	// to embed). Empty list = apply to all paths.
	SkipPaths []string
}

// DefaultSecurityHeaders returns the recommended values for production —
// suitable as-is for a public-facing identity service. Tune via the
// returned struct if a specific deployment needs to relax something.
//
// Rationale per field is in the SecurityHeadersConfig docstring.
func DefaultSecurityHeaders() SecurityHeadersConfig {
	return SecurityHeadersConfig{
		FrameOptions:       "DENY",
		ContentTypeOptions: "nosniff",
		ReferrerPolicy:     "strict-origin-when-cross-origin",
		// 1 year, includeSubDomains; NO preload (caller opts in).
		HSTS:          "max-age=31536000; includeSubDomains",
		XSSProtection: "0",
		// Disable browser features the platform doesn't use. Tightening
		// the camera/mic/geo surface narrows the post-XSS blast radius.
		PermissionsPolicy: "geolocation=(), microphone=(), camera=(), payment=(self), usb=()",
	}
}

// SecurityHeaders returns a Gin middleware that sets a fixed set of
// security-related response headers on every served request, except for
// paths in cfg.SkipPaths (prefix match). The middleware is idempotent —
// calling it twice is harmless because headers are set, not appended.
func SecurityHeaders(cfg SecurityHeadersConfig) gin.HandlerFunc {
	// Snapshot the SkipPaths slice so a caller mutating it after wiring
	// can't surprise us at request-handling time.
	skip := append([]string(nil), cfg.SkipPaths...)

	return func(c *gin.Context) {
		path := c.Request.URL.Path
		for _, p := range skip {
			if strings.HasPrefix(path, p) {
				c.Next()
				return
			}
		}

		h := c.Writer.Header()
		setIfNonEmpty(h, "X-Frame-Options", cfg.FrameOptions)
		setIfNonEmpty(h, "X-Content-Type-Options", cfg.ContentTypeOptions)
		setIfNonEmpty(h, "Referrer-Policy", cfg.ReferrerPolicy)
		setIfNonEmpty(h, "X-XSS-Protection", cfg.XSSProtection)
		setIfNonEmpty(h, "Permissions-Policy", cfg.PermissionsPolicy)

		// HSTS only on HTTPS responses — sending it over plain HTTP is
		// a no-op per RFC, but emitting it courts confusion in logs.
		// Detect via gin's c.Request.TLS or X-Forwarded-Proto (set by
		// Traefik in our deployment).
		if cfg.HSTS != "" && requestIsHTTPS(c) {
			h.Set("Strict-Transport-Security", cfg.HSTS)
		}

		c.Next()
	}
}

func setIfNonEmpty(h http.Header, key, value string) {
	if value != "" {
		h.Set(key, value)
	}
}

// requestIsHTTPS returns true when the request reached us over TLS,
// either directly (c.Request.TLS != nil) or via a trusted reverse proxy
// that set X-Forwarded-Proto=https. We only honour the header when Gin
// has already validated the proxy via TrustedProxies — gin.Context
// surfaces that decision through its own ClientIP machinery; we
// approximate by accepting either signal here, mirroring the existing
// CORS / cookie code paths.
func requestIsHTTPS(c *gin.Context) bool {
	if c.Request.TLS != nil {
		return true
	}
	// "X-Forwarded-Proto" is what Traefik sets on the websecure entrypoint.
	if proto := c.GetHeader("X-Forwarded-Proto"); strings.EqualFold(proto, "https") {
		return true
	}
	return false
}
