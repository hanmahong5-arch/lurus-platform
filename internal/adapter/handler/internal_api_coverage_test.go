package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/adapter/payment"
	"github.com/hanmahong5-arch/lurus-platform/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// ── InternalSubscriptionCheckout: wallet payment success ──────────────────

func TestInternalCoverage_SubCheckout_WalletFree(t *testing.T) {
	as := newMockAccountStore()
	ws := newMockWalletStore()
	vs := newMockVIPStore()
	ps := newMockPlanStore()
	ss := newMockSubStore()

	// Seed a product plan with 0 price (free tier).
	ps.plans[1] = &entity.ProductPlan{
		ID:           1,
		ProductID:    "lucrum",
		Code:         "free",
		BillingCycle: "monthly",
		PriceCNY:     0,
	}

	accountSvc := app.NewAccountService(as, ws, vs)
	vipSvc := app.NewVIPService(vs, ws)
	walletSvc := app.NewWalletService(ws, vipSvc)
	subSvc := app.NewSubscriptionService(ss, ps, app.NewEntitlementService(ss, ps, newMockCache()), 3)
	entSvc := app.NewEntitlementService(ss, ps, newMockCache())
	referralSvc := app.NewReferralServiceWithCodes(as, ws, &mockRedemptionCodeStore{})

	h := NewInternalHandler(accountSvc, subSvc, entSvc, vipSvc, nil, walletSvc, referralSvc, "")
	h.WithProductService(app.NewProductService(ps))
	r := testRouter()
	r.POST("/internal/v1/subscriptions/checkout", withAllScopes(), h.InternalSubscriptionCheckout)

	body, _ := json.Marshal(map[string]any{
		"account_id":     1,
		"product_id":     "lucrum",
		"plan_code":      "free",
		"billing_cycle":  "monthly",
		"payment_method": "wallet",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/internal/v1/subscriptions/checkout", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["subscription"] == nil {
		t.Error("expected subscription in response")
	}
}

func TestInternalCoverage_SubCheckout_WalletInsufficientBalance(t *testing.T) {
	as := newMockAccountStore()
	ws := newMockWalletStore()
	vs := newMockVIPStore()
	ps := newMockPlanStore()
	ss := newMockSubStore()

	// Paid plan but wallet has 0 balance.
	ps.plans[2] = &entity.ProductPlan{
		ID:           2,
		ProductID:    "lucrum",
		Code:         "pro",
		BillingCycle: "monthly",
		PriceCNY:     29.9,
	}

	accountSvc := app.NewAccountService(as, ws, vs)
	vipSvc := app.NewVIPService(vs, ws)
	walletSvc := app.NewWalletService(ws, vipSvc)
	subSvc := app.NewSubscriptionService(ss, ps, app.NewEntitlementService(ss, ps, newMockCache()), 3)
	entSvc := app.NewEntitlementService(ss, ps, newMockCache())
	referralSvc := app.NewReferralServiceWithCodes(as, ws, &mockRedemptionCodeStore{})

	h := NewInternalHandler(accountSvc, subSvc, entSvc, vipSvc, nil, walletSvc, referralSvc, "")
	h.WithProductService(app.NewProductService(ps))
	r := testRouter()
	r.POST("/internal/v1/subscriptions/checkout", withAllScopes(), h.InternalSubscriptionCheckout)

	body, _ := json.Marshal(map[string]any{
		"account_id":     1,
		"product_id":     "lucrum",
		"plan_code":      "pro",
		"billing_cycle":  "monthly",
		"payment_method": "wallet",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/internal/v1/subscriptions/checkout", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusPaymentRequired {
		t.Errorf("status = %d, want 402 (insufficient balance)", w.Code)
	}
}

func TestInternalCoverage_SubCheckout_ExternalPayment_NoProvider(t *testing.T) {
	as := newMockAccountStore()
	ws := newMockWalletStore()
	vs := newMockVIPStore()
	ps := newMockPlanStore()
	ss := newMockSubStore()

	ps.plans[3] = &entity.ProductPlan{
		ID:           3,
		ProductID:    "lucrum",
		Code:         "pro",
		BillingCycle: "monthly",
		PriceCNY:     29.9,
	}

	accountSvc := app.NewAccountService(as, ws, vs)
	vipSvc := app.NewVIPService(vs, ws)
	walletSvc := app.NewWalletService(ws, vipSvc)
	subSvc := app.NewSubscriptionService(ss, ps, app.NewEntitlementService(ss, ps, newMockCache()), 3)
	entSvc := app.NewEntitlementService(ss, ps, newMockCache())
	referralSvc := app.NewReferralServiceWithCodes(as, ws, &mockRedemptionCodeStore{})

	h := NewInternalHandler(accountSvc, subSvc, entSvc, vipSvc, nil, walletSvc, referralSvc, "")
	h.WithProductService(app.NewProductService(ps))
	h.WithPayments(payment.NewRegistry()) // Empty registry → stripe not available.
	r := testRouter()
	r.POST("/internal/v1/subscriptions/checkout", withAllScopes(), h.InternalSubscriptionCheckout)

	body, _ := json.Marshal(map[string]any{
		"account_id":     1,
		"product_id":     "lucrum",
		"plan_code":      "pro",
		"billing_cycle":  "monthly",
		"payment_method": "stripe", // external, but stripe not in registry
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/internal/v1/subscriptions/checkout", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	// Stripe not in registry → ProviderNotAvailableError → 400.
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (provider not available); body: %s", w.Code, w.Body.String())
	}
}

// ── CreateCheckout with provider nil ──────────────────────────────────────

func TestInternalCoverage_CreateCheckout_StripeProviderNil(t *testing.T) {
	ws := newMockWalletStore()
	vipSvc := makeVIPService()
	walletSvc := app.NewWalletService(ws, vipSvc)

	h := NewInternalHandler(
		makeAccountService(), makeSubService(), makeEntitlementService(),
		vipSvc, nil, walletSvc, makeReferralService(), "",
	)
	// No providers set.
	r := testRouter()
	r.POST("/internal/v1/checkout/create", withAllScopes(), h.CreateCheckout)

	body, _ := json.Marshal(map[string]any{
		"account_id":     1,
		"amount_cny":     50.0,
		"payment_method": "stripe",
		"source_service": "lucrum",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/internal/v1/checkout/create", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	// Provider nil → 400 (providerError).
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (stripe nil); body: %s", w.Code, w.Body.String())
	}
}

func TestInternalCoverage_CreateCheckout_EpayProviderNil(t *testing.T) {
	ws := newMockWalletStore()
	vipSvc := makeVIPService()
	walletSvc := app.NewWalletService(ws, vipSvc)

	h := NewInternalHandler(
		makeAccountService(), makeSubService(), makeEntitlementService(),
		vipSvc, nil, walletSvc, makeReferralService(), "",
	)
	r := testRouter()
	r.POST("/internal/v1/checkout/create", withAllScopes(), h.CreateCheckout)

	body, _ := json.Marshal(map[string]any{
		"account_id":     1,
		"amount_cny":     50.0,
		"payment_method": "epay_alipay",
		"source_service": "lucrum",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/internal/v1/checkout/create", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (epay nil)", w.Code)
	}
}

func TestInternalCoverage_CreateCheckout_CreemProviderNil(t *testing.T) {
	ws := newMockWalletStore()
	vipSvc := makeVIPService()
	walletSvc := app.NewWalletService(ws, vipSvc)

	h := NewInternalHandler(
		makeAccountService(), makeSubService(), makeEntitlementService(),
		vipSvc, nil, walletSvc, makeReferralService(), "",
	)
	r := testRouter()
	r.POST("/internal/v1/checkout/create", withAllScopes(), h.CreateCheckout)

	body, _ := json.Marshal(map[string]any{
		"account_id":     1,
		"amount_cny":     50.0,
		"payment_method": "creem",
		"source_service": "lucrum",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/internal/v1/checkout/create", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (creem nil)", w.Code)
	}
}

// ── InternalListWalletTransactions pagination ─────────────────────────────

func TestInternalCoverage_ListTransactions_WithPagination(t *testing.T) {
	ws := newMockWalletStore()
	// Credit to create a transaction.
	ws.Credit(context.Background(), 1, 50.0, "topup", "", "", "", "")
	vipSvc := makeVIPService()
	walletSvc := app.NewWalletService(ws, vipSvc)

	h := NewInternalHandler(
		makeAccountService(), makeSubService(), makeEntitlementService(),
		vipSvc, nil, walletSvc, makeReferralService(), "",
	)
	r := testRouter()
	r.POST("/internal/v1/accounts/:id/wallet/transactions", withAllScopes(), h.InternalListWalletTransactions)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/internal/v1/accounts/1/wallet/transactions?page=1&page_size=10", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if _, ok := resp["data"]; !ok {
		t.Error("expected 'data' field in response")
	}
	if _, ok := resp["total"]; !ok {
		t.Error("expected 'total' field in response")
	}
}

// ── GetBillingSummary success ──────────────────────────────────────────────

func TestInternalCoverage_GetBillingSummary_Success(t *testing.T) {
	ws := newMockWalletStore()
	ws.Credit(context.Background(), 1, 200.0, "topup", "", "", "", "")
	vipSvc := makeVIPService()
	walletSvc := app.NewWalletService(ws, vipSvc)

	h := NewInternalHandler(
		makeAccountService(), makeSubService(), makeEntitlementService(),
		vipSvc, nil, walletSvc, makeReferralService(), "",
	)
	r := testRouter()
	r.GET("/internal/v1/accounts/:id/billing-summary", withAllScopes(), h.GetBillingSummary)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/internal/v1/accounts/1/billing-summary", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["balance"] == nil {
		t.Error("expected 'balance' in billing summary")
	}
}

// ── GetEntitlements default free ───────────────────────────────────────────

func TestInternalCoverage_GetEntitlements_DefaultFree(t *testing.T) {
	h := makeInternalHandler(newMockAccountStore())
	r := testRouter()
	r.GET("/internal/v1/accounts/:id/entitlements/:product_id", withAllScopes(), h.GetEntitlements)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/internal/v1/accounts/1/entitlements/lucrum", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["plan_code"] != "free" {
		t.Errorf("plan_code = %s, want free", resp["plan_code"])
	}
}

// ── GetSubscription not found ─────────────────────────────────────────────

func TestInternalCoverage_GetSubscription_NotFound(t *testing.T) {
	h := makeInternalHandler(newMockAccountStore())
	r := testRouter()
	r.GET("/internal/v1/accounts/:id/subscription/:product_id", withAllScopes(), h.GetSubscription)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/internal/v1/accounts/1/subscription/lucrum", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// ── DebitWallet insufficient error message ────────────────────────────────

func TestInternalCoverage_DebitWallet_InsufficientBalanceMessage(t *testing.T) {
	ws := newMockWalletStore()
	// Account has 0 balance.
	vipSvc := makeVIPService()
	walletSvc := app.NewWalletService(ws, vipSvc)

	h := NewInternalHandler(
		makeAccountService(), makeSubService(), makeEntitlementService(),
		vipSvc, nil, walletSvc, makeReferralService(), "",
	)
	r := testRouter()
	r.POST("/internal/v1/accounts/:id/wallet/debit", withAllScopes(), h.DebitWallet)

	body, _ := json.Marshal(map[string]any{
		"amount": 50.0, "type": "test",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/internal/v1/accounts/1/wallet/debit", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "insufficient_balance" {
		t.Errorf("error = %v, want insufficient_balance", resp["error"])
	}
}
