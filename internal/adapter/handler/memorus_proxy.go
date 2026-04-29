// Package handler provides HTTP handlers for lurus-platform.
package handler

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// MemorusProxyHandler reverse-proxies user-authenticated requests to
// the in-cluster memorus AI memory engine, injecting the shared
// X-API-Key server-side. Clients need only a Lutu JWT — they never
// see or hold the memorus key. Mirrors NewAPIProxyHandler.
//
// Path mapping: /api/v1/memorus/<rest> → <internalURL>/<rest>
// e.g. POST /api/v1/memorus/memories → POST <internal>/memories
//
// Per-user namespacing: memorus accepts a `user_id` field in payload.
// We trust the upstream gin handler to scope it via the JWT claim
// before reaching this proxy (handler in app/memorus_service.go OR
// — if accepted via raw passthrough — the client must populate
// user_id correctly. Server-side enforcement is the caller's choice).
type MemorusProxyHandler struct {
	target *url.URL
	proxy  *httputil.ReverseProxy
	apiKey string
}

// NewMemorusProxyHandler builds the proxy. internalURL is the in-cluster
// memorus service, e.g. http://memorus.lurus-system.svc:8880.
// apiKey is the X-API-Key value (env MEMORUS_API_KEY).
func NewMemorusProxyHandler(internalURL, apiKey string) (*MemorusProxyHandler, error) {
	target, err := url.Parse(internalURL)
	if err != nil {
		return nil, fmt.Errorf("parse memorus internal url %q: %w", internalURL, err)
	}
	if apiKey == "" {
		return nil, fmt.Errorf("memorus api key is empty")
	}

	h := &MemorusProxyHandler{
		target: target,
		apiKey: apiKey,
	}

	proxy := &httputil.ReverseProxy{
		Director: h.director,
		ModifyResponse: func(resp *http.Response) error {
			// Strip any cookies / auth challenges memorus might emit;
			// platform-core controls auth, downstream details stay private.
			resp.Header.Del("Set-Cookie")
			resp.Header.Del("WWW-Authenticate")
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			slog.Error("memorus proxy error", "url", r.URL.String(), "err", err)
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":"memorus unreachable"}`))
		},
	}
	proxy.Transport = &http.Transport{
		ResponseHeaderTimeout: 15 * time.Second,
	}
	h.proxy = proxy

	return h, nil
}

func (h *MemorusProxyHandler) director(req *http.Request) {
	// Strip /api/v1/memorus prefix; everything else becomes the upstream path.
	req.URL.Path = strings.TrimPrefix(req.URL.Path, "/api/v1/memorus")
	if req.URL.Path == "" {
		req.URL.Path = "/"
	}

	req.URL.Scheme = h.target.Scheme
	req.URL.Host = h.target.Host
	req.Host = h.target.Host

	// Replace whatever Authorization header the client sent (their Lutu JWT,
	// already validated by upstream auth middleware) with memorus' X-API-Key.
	req.Header.Del("Authorization")
	req.Header.Set("X-API-Key", h.apiKey)
}

// Handle is the Gin handler. Mounted under v1.Any("/memorus/*path", h.Handle)
// so all HTTP verbs and sub-paths flow through.
func (h *MemorusProxyHandler) Handle(c *gin.Context) {
	h.proxy.ServeHTTP(c.Writer, c.Request)
}
