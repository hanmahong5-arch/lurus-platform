package lurusplatformclient

import (
	"context"
	"fmt"
	"net/http"
)

// WalletBalance is the wire shape returned by GET
// /internal/v1/accounts/:id/wallet/balance.
//
//	Balance — currently spendable balance in the account's wallet currency
//	          (LB / LucrumBucks; one Lurus-internal currency).
//	Frozen  — amount held by an active pre-authorization or pending refund;
//	          unavailable for new debits but counted toward the user-visible
//	          total in some surfaces.
type WalletBalance struct {
	Balance float64 `json:"balance"`
	Frozen  float64 `json:"frozen"`
}

// GetWalletBalance fetches an account's wallet balance. Requires
// InternalKey + scope `wallet:read`. Returns the zero-value balance
// (`{0, 0}`) when the account exists but has never been credited —
// the platform handler does this server-side, so no special-casing on
// the client.
func (c *Client) GetWalletBalance(ctx context.Context, accountID int64) (*WalletBalance, error) {
	if accountID <= 0 {
		return nil, fmt.Errorf("lurusplatformclient: GetWalletBalance: accountID must be > 0, got %d", accountID)
	}
	var out WalletBalance
	path := fmt.Sprintf("/internal/v1/accounts/%d/wallet/balance", accountID)
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DebitRequest is the body for POST /internal/v1/accounts/:id/wallet/debit.
//
//	Amount      — positive number; must be > 0. Server enforces.
//	Type        — short string identifying the debit reason category
//	              ("usage", "subscription", ...) for ledger filtering.
//	ProductID   — optional product attribution (e.g. "lucrum"). Used by
//	              the platform for per-product accounting / VIP tier.
//	Description — optional free-text shown on the user's transaction
//	              ledger. Visible to end users; do not log secrets here.
type DebitRequest struct {
	Amount      float64 `json:"amount"`
	Type        string  `json:"type"`
	ProductID   string  `json:"product_id,omitempty"`
	Description string  `json:"description,omitempty"`
}

// DebitResponse is the wire shape returned on a successful debit.
type DebitResponse struct {
	Success      bool    `json:"success"`
	BalanceAfter float64 `json:"balance_after"`
}

// DebitWallet deducts Amount from the account's wallet. Requires
// InternalKey + scope `wallet:debit`.
//
// Returns *PlatformError with IsInsufficient() == true when the wallet
// doesn't have enough funds; callers should guide the user to top up
// rather than retry. All other 4xx are likely caller bugs (bad type
// string, missing scope, ...).
//
// Idempotency: this endpoint is NOT idempotent on the wire. Callers
// that need at-most-once semantics should pre-authorize via the
// /wallet/pre-authorize → /pre-auth/:id/settle flow exposed on the
// platform (not currently wrapped by this SDK — see CHANGELOG for
// roadmap).
func (c *Client) DebitWallet(ctx context.Context, accountID int64, req *DebitRequest) (*DebitResponse, error) {
	if accountID <= 0 {
		return nil, fmt.Errorf("lurusplatformclient: DebitWallet: accountID must be > 0, got %d", accountID)
	}
	if req == nil {
		return nil, fmt.Errorf("lurusplatformclient: DebitWallet: req must not be nil")
	}
	if req.Amount <= 0 {
		return nil, fmt.Errorf("lurusplatformclient: DebitWallet: amount must be > 0, got %v", req.Amount)
	}
	if req.Type == "" {
		return nil, fmt.Errorf("lurusplatformclient: DebitWallet: type must not be empty")
	}
	var out DebitResponse
	path := fmt.Sprintf("/internal/v1/accounts/%d/wallet/debit", accountID)
	if err := c.do(ctx, http.MethodPost, path, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
