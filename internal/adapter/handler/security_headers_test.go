package handler

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// runRequest is a tiny helper to drive the middleware over a single
// request and return the response writer for assertion.
func runRequest(t *testing.T, mw gin.HandlerFunc, path string, withTLS, withForwardedHTTPS bool) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(mw)
	r.GET("/*any", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if withTLS {
		req.TLS = &tls.ConnectionState{}
	}
	if withForwardedHTTPS {
		req.Header.Set("X-Forwarded-Proto", "https")
	}
	r.ServeHTTP(w, req)
	return w
}

func TestSecurityHeaders_DefaultsApplied(t *testing.T) {
	w := runRequest(t, SecurityHeaders(DefaultSecurityHeaders()), "/anywhere", true, false)

	got := map[string]string{
		"X-Frame-Options":           w.Header().Get("X-Frame-Options"),
		"X-Content-Type-Options":    w.Header().Get("X-Content-Type-Options"),
		"Referrer-Policy":           w.Header().Get("Referrer-Policy"),
		"X-XSS-Protection":          w.Header().Get("X-XSS-Protection"),
		"Permissions-Policy":        w.Header().Get("Permissions-Policy"),
		"Strict-Transport-Security": w.Header().Get("Strict-Transport-Security"),
	}
	want := map[string]string{
		"X-Frame-Options":           "DENY",
		"X-Content-Type-Options":    "nosniff",
		"Referrer-Policy":           "strict-origin-when-cross-origin",
		"X-XSS-Protection":          "0",
		"Permissions-Policy":        "geolocation=(), microphone=(), camera=(), payment=(self), usb=()",
		"Strict-Transport-Security": "max-age=31536000; includeSubDomains",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

func TestSecurityHeaders_HSTS_OnlyOverHTTPS(t *testing.T) {
	cfg := DefaultSecurityHeaders()

	// Plain HTTP request → HSTS must NOT be sent (RFC 6797 §7.2: don't
	// emit HSTS over insecure transport).
	w := runRequest(t, SecurityHeaders(cfg), "/", false, false)
	if got := w.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("HSTS over HTTP: got %q, want empty", got)
	}

	// Direct TLS → HSTS sent.
	w = runRequest(t, SecurityHeaders(cfg), "/", true, false)
	if got := w.Header().Get("Strict-Transport-Security"); got == "" {
		t.Error("HSTS over direct TLS: missing")
	}

	// Behind a TLS-terminating proxy with X-Forwarded-Proto=https → HSTS sent.
	w = runRequest(t, SecurityHeaders(cfg), "/", false, true)
	if got := w.Header().Get("Strict-Transport-Security"); got == "" {
		t.Error("HSTS via X-Forwarded-Proto=https: missing")
	}
}

func TestSecurityHeaders_EmptyValueDisablesHeader(t *testing.T) {
	cfg := DefaultSecurityHeaders()
	cfg.FrameOptions = "" // explicit opt-out

	w := runRequest(t, SecurityHeaders(cfg), "/", true, false)
	if got := w.Header().Get("X-Frame-Options"); got != "" {
		t.Errorf("expected no X-Frame-Options when disabled, got %q", got)
	}
	// Sibling headers unaffected.
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options should still be set, got %q", got)
	}
}

func TestSecurityHeaders_SkipPaths(t *testing.T) {
	cfg := DefaultSecurityHeaders()
	cfg.SkipPaths = []string{"/oidc-embed/"}

	// Skipped path → no headers.
	w := runRequest(t, SecurityHeaders(cfg), "/oidc-embed/callback", true, false)
	if got := w.Header().Get("X-Frame-Options"); got != "" {
		t.Errorf("expected skipped path to omit headers, got X-Frame-Options=%q", got)
	}

	// Non-skipped path still gets them.
	w = runRequest(t, SecurityHeaders(cfg), "/normal", true, false)
	if got := w.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("non-skipped path: X-Frame-Options=%q, want DENY", got)
	}
}

func TestSecurityHeaders_SkipPathsSnapshotsCallerSlice(t *testing.T) {
	// Mutating the original slice after middleware construction must not
	// alter behaviour — defends against operator footgun.
	skip := []string{"/initial"}
	mw := SecurityHeaders(SecurityHeadersConfig{
		FrameOptions: "DENY",
		SkipPaths:    skip,
	})
	skip[0] = "/now-different"

	w := runRequest(t, mw, "/initial", true, false)
	if got := w.Header().Get("X-Frame-Options"); got != "" {
		t.Errorf("middleware should still skip /initial after caller mutation, got %q", got)
	}
}

func TestSecurityHeaders_Idempotent(t *testing.T) {
	// Wrapping the same middleware twice must be safe — headers are SET
	// (not added), so two passes produce one value, not two.
	gin.SetMode(gin.TestMode)
	r := gin.New()
	mw := SecurityHeaders(DefaultSecurityHeaders())
	r.Use(mw, mw)
	r.GET("/", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.TLS = &tls.ConnectionState{}
	r.ServeHTTP(w, req)

	values := w.Header().Values("X-Frame-Options")
	if len(values) != 1 || values[0] != "DENY" {
		t.Errorf("expected single X-Frame-Options=DENY, got %v", values)
	}
}
