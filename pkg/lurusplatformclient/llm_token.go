package lurusplatformclient

import (
	"context"
	"net/http"
	"net/url"
)

// LLMTokenResponse is the wire shape of GET /api/v1/account/me/llm-token.
//
//   - Key: the raw "sk-xxx" bearer for NewAPI's OpenAI-compatible /v1/*
//     endpoints. Treat as a secret — log only redacted.
//   - BaseURL: the public NewAPI host the OpenAI SDK should target.
//   - Name: human-readable token name; defaults to platform's own
//     ("lurus-platform-default") when LLMTokenOptions.Name is empty.
//   - UnlimitedQuota: true ⇒ NewAPI doesn't apply per-token caps, the
//     platform is metering quota via wallet debits instead.
type LLMTokenResponse struct {
	Key            string `json:"key"`
	BaseURL        string `json:"base_url"`
	Name           string `json:"name"`
	UnlimitedQuota bool   `json:"unlimited_quota"`
}

// LLMTokenOptions tunes the request. The only currently-supported knob
// is Name (forwarded as `?name=<value>`) for products that want a scoped
// key — extension point for future per-product attribution / revocation.
type LLMTokenOptions struct {
	// Name, when non-empty, requests a token scoped to this name (e.g.
	// "lucrum"). Empty = platform-default.
	Name string
}

// GetLLMToken calls GET /api/v1/account/me/llm-token. Requires a Bearer
// or cookie auth (set via WithBearerToken / WithCookieToken).
//
// Common errors (use errors.As + sentinel checks):
//   - 503 newapi_sync_disabled: NEWAPI_* env unset on the platform.
//     Operator misconfiguration; caller should surface and not retry.
//   - 503 account_not_provisioned: NewAPI mirror not yet created for
//     this account (transient — register hook still running or back-fill
//     cron hasn't seen it). IsRetriable() returns true; back off ~5s.
//   - 502 newapi_unavailable: NewAPI is down. IsUpstreamFailed() true;
//     caller can retry with backoff.
//
// Idempotent: NewAPI's per-user-per-name upsert means repeated calls
// return the same key. Products can cache freely.
func (c *Client) GetLLMToken(ctx context.Context, opts *LLMTokenOptions) (*LLMTokenResponse, error) {
	path := "/api/v1/account/me/llm-token"
	if opts != nil && opts.Name != "" {
		// url.Values keeps escaping correct for names with reserved chars.
		q := url.Values{}
		q.Set("name", opts.Name)
		path = path + "?" + q.Encode()
	}
	var out LLMTokenResponse
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
