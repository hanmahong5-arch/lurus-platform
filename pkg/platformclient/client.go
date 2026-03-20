// Package platformclient provides a type-safe Go client for lurus-platform
// internal API endpoints. Import this package instead of hand-writing HTTP calls.
//
// Usage:
//
//	client := platformclient.New("http://platform-core.lurus-platform.svc:18104", "your-api-key")
//
//	// Look up user
//	account, err := client.GetAccountByZitadelSub(ctx, "zitadel-sub-123")
//
//	// Check permissions
//	entitlements, err := client.GetEntitlements(ctx, account.ID, "my-product")
//
//	// Charge user
//	tx, err := client.DebitWallet(ctx, account.ID, 10.0, "usage", "API call charge", "my-product")
//	if errors.Is(err, platformclient.ErrInsufficientBalance) {
//	    // Guide user to top up
//	}
package platformclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Well-known errors that callers can check with errors.Is().
var (
	ErrNotFound            = errors.New("platformclient: resource not found")
	ErrInsufficientBalance = errors.New("platformclient: insufficient wallet balance")
	ErrUnauthorized        = errors.New("platformclient: unauthorized (check API key)")
	ErrRateLimited         = errors.New("platformclient: rate limited")
)

// Client is a type-safe HTTP client for lurus-platform internal API.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// New creates a platform client. BaseURL should include scheme and port,
// e.g. "http://platform-core.lurus-platform.svc:18104".
func New(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// ── Account Operations ──────────────────────────────────────────────────────

// Account represents a user account returned by the platform.
type Account struct {
	ID            int64  `json:"id"`
	LurusID       string `json:"lurus_id"`
	ZitadelSub    string `json:"zitadel_sub"`
	DisplayName   string `json:"display_name"`
	Email         string `json:"email"`
	Phone         string `json:"phone"`
	Status        int16  `json:"status"`
	AffCode       string `json:"aff_code"`
}

// GetAccountByID looks up an account by its numeric ID.
func (c *Client) GetAccountByID(ctx context.Context, id int64) (*Account, error) {
	var acct Account
	if err := c.get(ctx, fmt.Sprintf("/internal/v1/accounts/by-id/%d", id), &acct); err != nil {
		return nil, err
	}
	return &acct, nil
}

// GetAccountByZitadelSub looks up an account by Zitadel OIDC subject.
func (c *Client) GetAccountByZitadelSub(ctx context.Context, sub string) (*Account, error) {
	var acct Account
	if err := c.get(ctx, "/internal/v1/accounts/by-zitadel-sub/"+sub, &acct); err != nil {
		return nil, err
	}
	return &acct, nil
}

// GetAccountByEmail looks up an account by email address.
func (c *Client) GetAccountByEmail(ctx context.Context, email string) (*Account, error) {
	var acct Account
	if err := c.get(ctx, "/internal/v1/accounts/by-email/"+email, &acct); err != nil {
		return nil, err
	}
	return &acct, nil
}

// ValidateSession validates a session token and returns the associated account.
func (c *Client) ValidateSession(ctx context.Context, token string) (*Account, error) {
	var acct Account
	if err := c.post(ctx, "/internal/v1/accounts/validate-session", map[string]string{"token": token}, &acct); err != nil {
		return nil, err
	}
	return &acct, nil
}

// ── Entitlements ────────────────────────────────────────────────────────────

// Entitlements is a map of permission key → value for a product.
// Common keys: "plan_code", "max_requests", "feature_x".
type Entitlements map[string]string

// GetEntitlements returns the entitlements for an account + product.
// Returns {"plan_code": "free"} if no entitlements are set.
func (c *Client) GetEntitlements(ctx context.Context, accountID int64, productID string) (Entitlements, error) {
	var ent Entitlements
	path := fmt.Sprintf("/internal/v1/accounts/%d/entitlements/%s", accountID, productID)
	if err := c.get(ctx, path, &ent); err != nil {
		return nil, err
	}
	return ent, nil
}

// ── Wallet Operations ───────────────────────────────────────────────────────

// WalletBalance holds the wallet balance information.
type WalletBalance struct {
	Balance       float64 `json:"balance"`
	Frozen        float64 `json:"frozen"`
	LifetimeTopup float64 `json:"lifetime_topup"`
	LifetimeSpend float64 `json:"lifetime_spend"`
}

// GetWalletBalance returns the wallet balance for an account.
func (c *Client) GetWalletBalance(ctx context.Context, accountID int64) (*WalletBalance, error) {
	var bal WalletBalance
	path := fmt.Sprintf("/internal/v1/accounts/%d/wallet/balance", accountID)
	if err := c.get(ctx, path, &bal); err != nil {
		return nil, err
	}
	return &bal, nil
}

// DebitRequest is the payload for wallet debit.
type DebitRequest struct {
	Amount      float64 `json:"amount"`
	Type        string  `json:"type"`
	Description string  `json:"description"`
	ProductID   string  `json:"product_id"`
}

// DebitResult is the response from a successful debit.
type DebitResult struct {
	TransactionID int64   `json:"transaction_id"`
	BalanceAfter  float64 `json:"balance_after"`
}

// DebitWallet charges the user's wallet. Returns ErrInsufficientBalance if
// the wallet doesn't have enough funds — the caller should guide the user
// to top up (e.g. via CreateCheckoutSession).
func (c *Client) DebitWallet(ctx context.Context, accountID int64, amount float64, txType, desc, productID string) (*DebitResult, error) {
	var result DebitResult
	path := fmt.Sprintf("/internal/v1/accounts/%d/wallet/debit", accountID)
	err := c.post(ctx, path, DebitRequest{
		Amount:      amount,
		Type:        txType,
		Description: desc,
		ProductID:   productID,
	}, &result)
	return &result, err
}

// ── Pre-Authorization (Streaming/Long-running calls) ────────────────────────

// PreAuthRequest freezes an amount on the wallet for later settlement.
type PreAuthRequest struct {
	Amount      float64 `json:"amount"`
	ProductID   string  `json:"product_id"`
	ReferenceID string  `json:"reference_id"`
	Description string  `json:"description"`
	TTLSeconds  int     `json:"ttl_seconds,omitempty"`
}

// PreAuthResult is the response from a successful pre-authorization.
type PreAuthResult struct {
	PreAuthID int64   `json:"preauth_id"`
	Amount    float64 `json:"amount"`
	ExpiresAt string  `json:"expires_at"`
}

// PreAuthorize freezes an amount on the wallet for later settlement.
// Use this for streaming API calls where the final cost is unknown upfront.
// Flow: PreAuthorize → (do work) → SettlePreAuth or ReleasePreAuth.
func (c *Client) PreAuthorize(ctx context.Context, accountID int64, req PreAuthRequest) (*PreAuthResult, error) {
	var result PreAuthResult
	path := fmt.Sprintf("/internal/v1/accounts/%d/wallet/pre-authorize", accountID)
	if err := c.post(ctx, path, req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SettlePreAuth charges the actual amount and releases the hold.
func (c *Client) SettlePreAuth(ctx context.Context, preAuthID int64, actualAmount float64) error {
	path := fmt.Sprintf("/internal/v1/wallet/pre-auth/%d/settle", preAuthID)
	return c.post(ctx, path, map[string]float64{"actual_amount": actualAmount}, nil)
}

// ReleasePreAuth cancels the hold without charging.
func (c *Client) ReleasePreAuth(ctx context.Context, preAuthID int64) error {
	path := fmt.Sprintf("/internal/v1/wallet/pre-auth/%d/release", preAuthID)
	return c.post(ctx, path, nil, nil)
}

// ── Checkout (External Payment) ─────────────────────────────────────────────

// CheckoutRequest creates a payment order for external payment.
type CheckoutRequest struct {
	AccountID      int64  `json:"account_id"`
	AmountCNY      float64 `json:"amount_cny"`
	PaymentMethod  string  `json:"payment_method"`
	SourceService  string  `json:"source_service"`
	IdempotencyKey string  `json:"idempotency_key,omitempty"`
	TTLSeconds     int     `json:"ttl_seconds,omitempty"`
}

// CheckoutResult is the response from creating a checkout session.
type CheckoutResult struct {
	OrderNo   string `json:"order_no"`
	PayURL    string `json:"pay_url"`
	Status    string `json:"status"`
	ExpiresAt string `json:"expires_at"`
}

// CreateCheckoutSession creates a payment order that the user can pay externally.
// The caller should redirect the user to PayURL.
func (c *Client) CreateCheckoutSession(ctx context.Context, req CheckoutRequest) (*CheckoutResult, error) {
	var result CheckoutResult
	if err := c.post(ctx, "/internal/v1/checkout/create", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetCheckoutStatus returns the current status of a checkout order.
func (c *Client) GetCheckoutStatus(ctx context.Context, orderNo string) (*CheckoutResult, error) {
	var result CheckoutResult
	if err := c.get(ctx, "/internal/v1/checkout/"+orderNo+"/status", &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ── HTTP Transport ──────────────────────────────────────────────────────────

func (c *Client) get(ctx context.Context, path string, out interface{}) error {
	return c.do(ctx, http.MethodGet, path, nil, out)
}

func (c *Client) post(ctx context.Context, path string, body interface{}, out interface{}) error {
	return c.do(ctx, http.MethodPost, path, body, out)
}

func (c *Client) do(ctx context.Context, method, path string, body interface{}, out interface{}) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("platformclient: marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("platformclient: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("platformclient: http request to %s: %w", path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 65536))
	if err != nil {
		return fmt.Errorf("platformclient: read response: %w", err)
	}

	// Map HTTP status codes to well-known errors.
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		// Success — unmarshal if out is provided.
		if out != nil && len(respBody) > 0 {
			if err := json.Unmarshal(respBody, out); err != nil {
				return fmt.Errorf("platformclient: unmarshal response: %w", err)
			}
		}
		return nil
	case resp.StatusCode == 401:
		return ErrUnauthorized
	case resp.StatusCode == 404:
		return ErrNotFound
	case resp.StatusCode == 402:
		return ErrInsufficientBalance
	case resp.StatusCode == 429:
		return ErrRateLimited
	default:
		// Extract error message from response body.
		var errResp struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Message != "" {
			return fmt.Errorf("platformclient: %s %s → %d: %s", method, path, resp.StatusCode, errResp.Message)
		}
		if errResp.Error != "" {
			return fmt.Errorf("platformclient: %s %s → %d: %s", method, path, resp.StatusCode, errResp.Error)
		}
		return fmt.Errorf("platformclient: %s %s → %d", method, path, resp.StatusCode)
	}
}
