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

// NewAPIProxyHandler reverse-proxies admin requests to the NewAPI LLM gateway.
// It replaces the Zitadel admin JWT with a NewAPI access token so that
// the caller only needs a single credential (the admin JWT).
type NewAPIProxyHandler struct {
	target      *url.URL
	proxy       *httputil.ReverseProxy
	accessToken string
	userID      string
}

// NewNewAPIProxyHandler creates a handler that proxies to internalURL.
// The accessToken and userID are injected into forwarded requests so that
// NewAPI treats them as admin operations.
func NewNewAPIProxyHandler(internalURL, accessToken, userID string) (*NewAPIProxyHandler, error) {
	target, err := url.Parse(internalURL)
	if err != nil {
		return nil, fmt.Errorf("parse newapi internal url %q: %w", internalURL, err)
	}

	h := &NewAPIProxyHandler{
		target:      target,
		accessToken: accessToken,
		userID:      userID,
	}

	proxy := &httputil.ReverseProxy{
		Director: h.director,
		ModifyResponse: func(resp *http.Response) error {
			// Remove Set-Cookie headers to prevent newapi session leakage.
			resp.Header.Del("Set-Cookie")
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			slog.Error("newapi proxy error", "url", r.URL.String(), "err", err)
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":"LLM gateway unreachable"}`))
		},
	}
	proxy.Transport = &http.Transport{
		ResponseHeaderTimeout: 30 * time.Second,
	}
	h.proxy = proxy

	return h, nil
}

// director rewrites the outgoing request to point at the newapi backend.
func (h *NewAPIProxyHandler) director(req *http.Request) {
	// Strip the /proxy/newapi prefix so that /proxy/newapi/api/channel → /api/channel.
	req.URL.Path = strings.TrimPrefix(req.URL.Path, "/proxy/newapi")
	if req.URL.Path == "" {
		req.URL.Path = "/"
	}

	req.URL.Scheme = h.target.Scheme
	req.URL.Host = h.target.Host
	req.Host = h.target.Host

	// Replace authorization with newapi admin token.
	req.Header.Set("Authorization", "Bearer "+h.accessToken)
	// NewAPI uses this header to identify the operating user.
	if h.userID != "" {
		req.Header.Set("New-Api-User", h.userID)
	}
}

// Handle is the Gin handler that proxies the request.
func (h *NewAPIProxyHandler) Handle(c *gin.Context) {
	h.proxy.ServeHTTP(c.Writer, c.Request)
}
