package lurusplatformclient

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// Account is the platform's canonical user record returned by the
// /internal/v1/accounts/* family.
//
// Subset of the server-side identity.accounts row: only fields downstream
// services have a legitimate reason to consume are exposed. PII like
// ZitadelSub or referrer chain is intentionally omitted; if a downstream
// needs it, that's a contract conversation, not a struct edit.
type Account struct {
	ID          int64     `json:"id"`
	LurusID     string    `json:"lurus_id"`
	Username    string    `json:"username,omitempty"`
	Email       string    `json:"email,omitempty"`
	Phone       string    `json:"phone,omitempty"`
	DisplayName string    `json:"display_name,omitempty"`
	Status      string    `json:"status,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// GetAccountByID fetches an account by its numeric platform ID. Requires
// the Client to have an InternalKey configured + scope `account:read`
// granted to that key.
//
// Returns *PlatformError with IsNotFound() == true when the ID doesn't
// resolve — distinguishable from network / permission errors via the
// sentinel check.
func (c *Client) GetAccountByID(ctx context.Context, id int64) (*Account, error) {
	if id <= 0 {
		return nil, fmt.Errorf("lurusplatformclient: GetAccountByID: id must be > 0, got %d", id)
	}
	var out Account
	path := fmt.Sprintf("/internal/v1/accounts/%d", id)
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetAccountByEmail fetches an account by email address. Same auth
// requirements as GetAccountByID.
//
// The email is path-escaped, so addresses containing `+` (gmail dots) or
// other reserved characters are passed through correctly.
func (c *Client) GetAccountByEmail(ctx context.Context, email string) (*Account, error) {
	if email == "" {
		return nil, fmt.Errorf("lurusplatformclient: GetAccountByEmail: email must not be empty")
	}
	var out Account
	path := "/internal/v1/accounts/by-email/" + url.PathEscape(email)
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
