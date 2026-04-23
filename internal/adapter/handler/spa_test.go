package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
)

// fakeFS provides a minimal in-memory SPA distribution for the NoRoute tests.
func fakeFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":    {Data: []byte("<!doctype html><html><body>SPA</body></html>")},
		"assets/app.js": {Data: []byte("console.log('ok');")},
	}
}

func newSPARouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	// A real API route so paths that DO match aren't caught by NoRoute.
	r.GET("/api/v1/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	r.NoRoute(NoRouteHandler(fakeFS()))
	return r
}

// TestNoRoute_UnknownAPIPath_ReturnsJSON404 is the regression test for the
// class of bug where a missing or disabled API route fell through to the
// SPA HTML shell, producing "Unexpected token '<'" at the JSON client.
func TestNoRoute_UnknownAPIPath_ReturnsJSON404(t *testing.T) {
	r := newSPARouter()

	cases := []string{
		"/api/v1/does-not-exist",
		"/internal/v1/does-not-exist",
		"/admin/v1/does-not-exist",
		"/webhooks/unknown",
		"/oauth/nope",
	}
	for _, path := range cases {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusNotFound {
				t.Errorf("%s: want 404, got %d", path, w.Code)
			}
			ct := w.Header().Get("Content-Type")
			if !strings.HasPrefix(ct, "application/json") {
				t.Errorf("%s: want JSON content-type, got %q (body=%s)", path, ct, w.Body.String())
			}
			var body map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Errorf("%s: body is not valid JSON: %v (body=%s)", path, err, w.Body.String())
			}
			if body["error"] != "route_not_found" {
				t.Errorf("%s: error field = %v, want route_not_found", path, body["error"])
			}
		})
	}
}

// TestNoRoute_KnownAPIPath_IsNotCaught — sanity check: the NoRoute handler
// must not interfere with real registered routes.
func TestNoRoute_KnownAPIPath_IsNotCaught(t *testing.T) {
	r := newSPARouter()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("registered route: want 200, got %d", w.Code)
	}
}

// TestNoRoute_StaticAsset_ServesFile — JS/CSS files in dist/ should be served
// directly, not rewritten to index.html.
func TestNoRoute_StaticAsset_ServesFile(t *testing.T) {
	r := newSPARouter()
	req := httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("asset: want 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "console.log") {
		t.Errorf("asset body unexpected: %s", w.Body.String())
	}
}

// TestNoRoute_UnknownNonAPIPath_ServesSPA — SPA client-side routes
// (/wallet, /profile, ...) must fall back to index.html so React/Vue
// routing works on first load.
func TestNoRoute_UnknownNonAPIPath_ServesSPA(t *testing.T) {
	r := newSPARouter()
	for _, path := range []string{"/wallet", "/profile/settings", "/"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Errorf("%s: want 200, got %d", path, w.Code)
			}
			ct := w.Header().Get("Content-Type")
			if !strings.HasPrefix(ct, "text/html") {
				t.Errorf("%s: want text/html, got %q", path, ct)
			}
			if !strings.Contains(w.Body.String(), "SPA") {
				t.Errorf("%s: expected SPA shell, got %q", path, w.Body.String())
			}
		})
	}
}
