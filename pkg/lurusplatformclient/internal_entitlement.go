package lurusplatformclient

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// Entitlements is the per-account-per-product permission map. Common keys:
//
//	"plan_code"   — "free" / "pro" / "enterprise" / ...
//	"max_seats"   — string-encoded integer
//	"feature_xx"  — "true" / "false" or a free-form scalar
//
// Values are always strings on the wire to keep the protocol stable as
// new feature flags are added (avoids the "is this an int or a bool"
// parsing question per consumer).
type Entitlements map[string]string

// GetEntitlements fetches the entitlements for an account+product pair.
// Requires InternalKey + scope `entitlement`.
//
// Important: the platform returns `{"plan_code": "free"}` for accounts
// that have NO active subscription on this product — this is NOT a 404,
// and the SDK does not synthesise one. Callers should treat the absent
// keys as defaults.
func (c *Client) GetEntitlements(ctx context.Context, accountID int64, productID string) (Entitlements, error) {
	if accountID <= 0 {
		return nil, fmt.Errorf("lurusplatformclient: GetEntitlements: accountID must be > 0, got %d", accountID)
	}
	if productID == "" {
		return nil, fmt.Errorf("lurusplatformclient: GetEntitlements: productID must not be empty")
	}
	var out Entitlements
	path := fmt.Sprintf("/internal/v1/accounts/%d/entitlements/%s",
		accountID, url.PathEscape(productID))
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
