package lurusplatformclient

import (
	"context"
	"net/http"
)

// WhoamiResponse is the wire shape returned by GET /api/v1/whoami.
//
// `phone` is masked server-side (e.g. +86138****2222), so it's safe to
// surface this struct to logs / response payloads without further
// redaction. New fields will only be added; never removed nor renamed.
type WhoamiResponse struct {
	AccountID   int64  `json:"account_id"`
	LurusID     string `json:"lurus_id"`
	DisplayName string `json:"display_name,omitempty"`
	Email       string `json:"email,omitempty"`
	Phone       string `json:"phone,omitempty"`
}

// Whoami calls GET /api/v1/whoami using the configured Bearer or cookie
// auth. It returns the canonical product-facing user shape and is the
// recommended drop-in for "who is the caller" lookups across all
// *.lurus.cn products.
//
// Common errors (use errors.As + sentinel checks):
//   - 401 unauthorized: token missing / invalid / expired — caller
//     should clear the cookie and re-prompt for login.
//   - 503 session_unconfigured: the platform deployment is missing its
//     session secret. Operator-level bug.
func (c *Client) Whoami(ctx context.Context) (*WhoamiResponse, error) {
	var out WhoamiResponse
	if err := c.do(ctx, http.MethodGet, "/api/v1/whoami", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
