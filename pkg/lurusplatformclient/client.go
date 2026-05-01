// Package lurusplatformclient is a typed Go client for lurus-platform's
// /internal/v1/* and selected /api/v1/* endpoints.
//
// Downstream services (lucrum / tally / switch / kova / forge) import
// this package instead of writing their own HTTP boilerplate. Wire-stable
// on master; breaking changes will bump to pkg/lurusplatformclient/v2.
//
// Authentication is configured per Client. Exactly one of WithInternalKey,
// WithBearerToken, or WithCookieToken should be set per Client instance.
//
//	// Service-to-service (most common — INTERNAL_API_KEY):
//	c := lurusplatformclient.New("https://identity.lurus.cn").
//	    WithInternalKey(os.Getenv("LURUS_PLATFORM_INTERNAL_KEY"))
//	acc, err := c.GetAccountByID(ctx, 42)
//
//	// End-user proxy mode — forward the user's bearer token:
//	c := lurusplatformclient.New("https://identity.lurus.cn").
//	    WithBearerToken(userJWT)
//	w, err := c.Whoami(ctx)
//
// Errors are decoded into *PlatformError; use errors.As to inspect them.
package lurusplatformclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// defaultHTTPTimeout is the per-request budget when the caller hasn't
// supplied their own *http.Client. 10s is large enough for synchronous
// platform calls (which typically respond in <100ms) but short enough
// that a hung connection won't pin a goroutine forever.
const defaultHTTPTimeout = 10 * time.Second

// maxResponseBodyBytes caps how much of the response body we read,
// regardless of HTTP status. 1 MiB is well above any legitimate
// platform response and prevents a malicious or malfunctioning peer
// from exhausting client memory. Hit this limit ⇒ best-effort error.
const maxResponseBodyBytes = 1 << 20

// authMode enumerates the auth-header strategy a Client uses.
type authMode int

const (
	authNone authMode = iota
	authInternalKey
	authBearerToken
	authCookieToken
)

// Client is a typed HTTP client for lurus-platform endpoints.
//
// Construct via New + chain WithXxx setters; the zero value is not usable.
// Safe for concurrent use after construction; do NOT mutate WithXxx
// settings after the first call.
type Client struct {
	baseURL    string
	httpClient *http.Client

	// authMode + authValue carry the active credential. Exactly one of
	// the WithXxx setters should be invoked per Client; later setters
	// overwrite earlier ones rather than producing an error, so the
	// final-call-wins. The mutually-exclusive contract is documented;
	// not enforced at runtime because doing so would require either a
	// constructor explosion or a deferred error, both worse than the
	// rule.
	authMode  authMode
	authValue string
}

// New constructs a Client. baseURL should NOT include a trailing slash
// (e.g. "https://identity.lurus.cn"). The default HTTP client has a
// 10s timeout; override via WithHTTPClient.
func New(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: defaultHTTPTimeout,
		},
	}
}

// WithHTTPClient swaps the underlying *http.Client. Use this to install
// a transport with custom TLS, tracing, or a different timeout.
//
// Returns the receiver for chaining; nil h is a no-op so callers can
// pass a feature-flag-disabled override.
func (c *Client) WithHTTPClient(h *http.Client) *Client {
	if h != nil {
		c.httpClient = h
	}
	return c
}

// WithInternalKey configures the Client for service-to-service calls
// against /internal/v1/*. The key is sent as `Authorization: Bearer <key>`.
//
// Empty key disables auth (useful in tests); the server will reject
// the request with 401, but we don't pre-validate so misconfigured
// callers fail loud at the first call rather than at construction.
func (c *Client) WithInternalKey(key string) *Client {
	c.authMode = authInternalKey
	c.authValue = key
	return c
}

// WithBearerToken configures the Client for end-user proxy calls — typically
// `Authorization: Bearer <user-jwt>`. Use this when a downstream service
// is forwarding the caller's identity (lucrum frontend → backend → platform).
func (c *Client) WithBearerToken(token string) *Client {
	c.authMode = authBearerToken
	c.authValue = token
	return c
}

// WithCookieToken configures the Client to send the lurus_session cookie.
// Use this for browser-style flows where the caller has the session token
// but the path is cookie-only on the server side. The cookie is sent with
// the canonical `lurus_session` name; baseURL must match the cookie's
// scope.
func (c *Client) WithCookieToken(token string) *Client {
	c.authMode = authCookieToken
	c.authValue = token
	return c
}

// sessionCookieName is the canonical lurus session cookie name. Must
// match handler.SessionCookieName on the server.
const sessionCookieName = "lurus_session"

// errorEnvelope is the wire shape platform emits for every non-2xx.
// Mirrors `{error: "<code>", message: "<text>"}`.
type errorEnvelope struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// do is the central HTTP roundtrip used by every endpoint method. It:
//   - JSON-encodes body when non-nil
//   - sets the configured auth header / cookie
//   - reads up to maxResponseBodyBytes
//   - on 2xx: JSON-decodes into out (skipped if out is nil)
//   - on non-2xx: builds a *PlatformError, decoding the envelope when
//     the body is JSON and falling back to status-derived defaults
//     when it isn't (e.g. a bare 502 from a proxy)
//
// Network errors (DNS, TCP refused, mid-response disconnect) come back
// as wrapped errors; check with errors.As(err, &pe *PlatformError) to
// discriminate.
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var bodyReader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("lurusplatformclient: marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("lurusplatformclient: build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	switch c.authMode {
	case authInternalKey, authBearerToken:
		if c.authValue != "" {
			req.Header.Set("Authorization", "Bearer "+c.authValue)
		}
	case authCookieToken:
		if c.authValue != "" {
			req.AddCookie(&http.Cookie{
				Name:  sessionCookieName,
				Value: c.authValue,
			})
		}
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("lurusplatformclient: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodyBytes))
	if readErr != nil {
		return fmt.Errorf("lurusplatformclient: read response %s %s: %w",
			method, path, readErr)
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out == nil || len(respBody) == 0 {
			return nil
		}
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("lurusplatformclient: decode response %s %s: %w",
				method, path, err)
		}
		return nil
	}

	return parsePlatformError(resp.StatusCode, respBody)
}

// parsePlatformError builds a *PlatformError from a non-2xx response.
// Falls back to status-derived defaults when the body isn't a valid
// JSON envelope (e.g. an HTML 502 from a reverse proxy).
func parsePlatformError(status int, body []byte) error {
	pe := &PlatformError{
		Status:  status,
		RawBody: truncateString(string(body), rawBodyTruncateBytes),
	}

	var env errorEnvelope
	if len(body) > 0 && json.Unmarshal(body, &env) == nil && (env.Error != "" || env.Message != "") {
		pe.Code = env.Error
		pe.Message = env.Message
	}

	// Fill defaults so callers never see Code="" — that would force them
	// to also branch on Status, defeating the sentinel-check ergonomics.
	if pe.Code == "" {
		pe.Code = defaultCodeForStatus(status)
	}
	if pe.Message == "" {
		pe.Message = http.StatusText(status)
		if pe.Message == "" {
			pe.Message = fmt.Sprintf("HTTP %d", status)
		}
	}
	return pe
}

// defaultCodeForStatus is the fallback Code when the response body has
// no envelope (or isn't JSON). Keeps sentinel checks working even
// against unexpected upstream shapes (proxy 502 HTML, k8s 503, ...).
func defaultCodeForStatus(status int) string {
	switch status {
	case http.StatusBadRequest:
		return codeInvalidRequest
	case http.StatusUnauthorized:
		return codeUnauthorized
	case http.StatusForbidden:
		return codeForbidden
	case http.StatusNotFound:
		return codeNotFound
	case http.StatusConflict:
		return codeConflict
	case http.StatusTooManyRequests:
		return codeRateLimited
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return codeUpstreamFailed
	default:
		if status >= 500 {
			return codeInternal
		}
		return codeInvalidRequest
	}
}

func truncateString(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	return s[:maxBytes]
}

// AsPlatformError is a convenience helper for the common
// `errors.As(err, &pe)` pattern. Returns the typed error and true on
// match, or nil and false otherwise.
//
//	if pe, ok := lurusplatformclient.AsPlatformError(err); ok && pe.IsNotFound() {
//	    // handle missing account
//	}
func AsPlatformError(err error) (*PlatformError, bool) {
	var pe *PlatformError
	if errors.As(err, &pe) {
		return pe, true
	}
	return nil, false
}
