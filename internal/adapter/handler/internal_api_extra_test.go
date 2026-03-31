package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// makeInternalHandlerExtra creates an InternalHandler with product service wired.
func makeInternalHandlerExtra(as *mockAccountStore) *InternalHandler {
	h := makeInternalHandler(as)
	h.WithProductService(makeProductService())
	return h
}

// ── PreAuthorize handler ──────────────────────────────────────────────────

func TestInternalHandlerExtra_PreAuthorize_Success(t *testing.T) {
	as := newMockAccountStore()
	h := makeInternalHandlerExtra(as)
	r := testRouter()
	r.POST("/internal/v1/accounts/:id/wallet/pre-authorize", withAllScopes(), h.PreAuthorize)

	body, _ := json.Marshal(map[string]any{
		"amount":     5.0,
		"product_id": "lucrum",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/accounts/1/wallet/pre-authorize", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != "active" {
		t.Errorf("status = %v, want active", resp["status"])
	}
}

func TestInternalHandlerExtra_PreAuthorize_InvalidID(t *testing.T) {
	h := makeInternalHandlerExtra(newMockAccountStore())
	r := testRouter()
	r.POST("/internal/v1/accounts/:id/wallet/pre-authorize", withAllScopes(), h.PreAuthorize)

	body, _ := json.Marshal(map[string]any{"amount": 5.0, "product_id": "p"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/accounts/abc/wallet/pre-authorize", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestInternalHandlerExtra_PreAuthorize_MissingFields(t *testing.T) {
	h := makeInternalHandlerExtra(newMockAccountStore())
	r := testRouter()
	r.POST("/internal/v1/accounts/:id/wallet/pre-authorize", withAllScopes(), h.PreAuthorize)

	body, _ := json.Marshal(map[string]any{"product_id": "p"}) // missing amount
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/accounts/1/wallet/pre-authorize", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// ── SettlePreAuth handler ─────────────────────────────────────────────────

func TestInternalHandlerExtra_SettlePreAuth_InvalidID(t *testing.T) {
	h := makeInternalHandlerExtra(newMockAccountStore())
	r := testRouter()
	r.POST("/internal/v1/wallet/pre-auth/:id/settle", withAllScopes(), h.SettlePreAuth)

	body, _ := json.Marshal(map[string]any{"actual_amount": 3.0})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/wallet/pre-auth/abc/settle", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestInternalHandlerExtra_SettlePreAuth_NotFound(t *testing.T) {
	h := makeInternalHandlerExtra(newMockAccountStore())
	r := testRouter()
	r.POST("/internal/v1/wallet/pre-auth/:id/settle", withAllScopes(), h.SettlePreAuth)

	body, _ := json.Marshal(map[string]any{"actual_amount": 3.0})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/wallet/pre-auth/999/settle", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (not found mapped to bad request)", w.Code)
	}
}

// ── ReleasePreAuth handler ────────────────────────────────────────────────

func TestInternalHandlerExtra_ReleasePreAuth_InvalidID(t *testing.T) {
	h := makeInternalHandlerExtra(newMockAccountStore())
	r := testRouter()
	r.POST("/internal/v1/wallet/pre-auth/:id/release", withAllScopes(), h.ReleasePreAuth)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/wallet/pre-auth/abc/release", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestInternalHandlerExtra_ReleasePreAuth_NotFound(t *testing.T) {
	h := makeInternalHandlerExtra(newMockAccountStore())
	r := testRouter()
	r.POST("/internal/v1/wallet/pre-auth/:id/release", withAllScopes(), h.ReleasePreAuth)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/wallet/pre-auth/999/release", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (not found mapped to bad request)", w.Code)
	}
}

// ── GetBillingSummary handler ─────────────────────────────────────────────

func TestInternalHandlerExtra_GetBillingSummary_InvalidID(t *testing.T) {
	h := makeInternalHandlerExtra(newMockAccountStore())
	r := testRouter()
	r.GET("/internal/v1/accounts/:id/billing-summary", withAllScopes(), h.GetBillingSummary)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/accounts/abc/billing-summary", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// ── ExchangeLucToLut handler ──────────────────────────────────────────────

func TestInternalHandlerExtra_ExchangeLucToLut_NoAPI(t *testing.T) {
	h := makeInternalHandlerExtra(newMockAccountStore())
	r := testRouter()
	r.POST("/internal/v1/accounts/:id/currency/exchange", withAllScopes(), h.ExchangeLucToLut)

	body, _ := json.Marshal(map[string]any{
		"amount": 100.0, "lurus_user_id": 1, "idempotency_key": "k1",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/accounts/1/currency/exchange", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}

func TestInternalHandlerExtra_ExchangeLucToLut_ExceedsMax(t *testing.T) {
	h := makeInternalHandlerExtra(newMockAccountStore())
	h.lurusAPI = nil // will hit nil check first anyway
	r := testRouter()
	r.POST("/internal/v1/accounts/:id/currency/exchange", withAllScopes(), h.ExchangeLucToLut)

	body, _ := json.Marshal(map[string]any{
		"amount": 200000.0, "lurus_user_id": 1, "idempotency_key": "k2",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/accounts/1/currency/exchange", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	// lurusAPI nil check comes first (503), then amount check (400).
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 (nil check before amount)", w.Code)
	}
}

// ── GetCurrencyInfo handler ───────────────────────────────────────────────

func TestInternalHandlerExtra_GetCurrencyInfo_NoAPI(t *testing.T) {
	h := makeInternalHandlerExtra(newMockAccountStore())
	r := testRouter()
	r.GET("/internal/v1/currency/info", withAllScopes(), h.GetCurrencyInfo)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/currency/info", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}

// ── InternalSubscriptionCheckout handler ──────────────────────────────────

func TestInternalHandlerExtra_SubCheckout_NoPlansSvc(t *testing.T) {
	h := makeInternalHandler(newMockAccountStore()) // no WithProductService
	r := testRouter()
	r.POST("/internal/v1/subscriptions/checkout", withAllScopes(), h.InternalSubscriptionCheckout)

	body, _ := json.Marshal(map[string]any{
		"account_id": 1, "product_id": "p", "plan_code": "free",
		"billing_cycle": "monthly", "payment_method": "wallet",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/subscriptions/checkout", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}

func TestInternalHandlerExtra_SubCheckout_MissingFields(t *testing.T) {
	h := makeInternalHandlerExtra(newMockAccountStore())
	r := testRouter()
	r.POST("/internal/v1/subscriptions/checkout", withAllScopes(), h.InternalSubscriptionCheckout)

	body, _ := json.Marshal(map[string]any{"account_id": 1})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/subscriptions/checkout", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestInternalHandlerExtra_SubCheckout_PlanNotFound(t *testing.T) {
	h := makeInternalHandlerExtra(newMockAccountStore())
	r := testRouter()
	r.POST("/internal/v1/subscriptions/checkout", withAllScopes(), h.InternalSubscriptionCheckout)

	body, _ := json.Marshal(map[string]any{
		"account_id": 1, "product_id": "nonexistent", "plan_code": "pro",
		"billing_cycle": "monthly", "payment_method": "wallet",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/subscriptions/checkout", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// ── CreateCheckout handler ────────────────────────────────────────────────

func TestInternalHandlerExtra_CreateCheckout_MissingFields(t *testing.T) {
	h := makeInternalHandlerExtra(newMockAccountStore())
	r := testRouter()
	r.POST("/internal/v1/checkout/create", withAllScopes(), h.CreateCheckout)

	body, _ := json.Marshal(map[string]any{"account_id": 1})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/checkout/create", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestInternalHandlerExtra_CreateCheckout_AmountBelowMin(t *testing.T) {
	h := makeInternalHandlerExtra(newMockAccountStore())
	r := testRouter()
	r.POST("/internal/v1/checkout/create", withAllScopes(), h.CreateCheckout)

	body, _ := json.Marshal(map[string]any{
		"account_id": 1, "amount_cny": 0.5, "payment_method": "stripe", "source_service": "test",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/checkout/create", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (below minimum)", w.Code)
	}
}

func TestInternalHandlerExtra_CreateCheckout_InvalidPaymentMethod(t *testing.T) {
	h := makeInternalHandlerExtra(newMockAccountStore())
	r := testRouter()
	r.POST("/internal/v1/checkout/create", withAllScopes(), h.CreateCheckout)

	body, _ := json.Marshal(map[string]any{
		"account_id": 1, "amount_cny": 50.0, "payment_method": "bitcoin", "source_service": "test",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/checkout/create", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (unsupported method)", w.Code)
	}
}

// ── GetAccountByID ────────────────────────────────────────────────────────

func TestInternalHandlerExtra_GetAccountByID_Success(t *testing.T) {
	as := newMockAccountStore()
	as.seed(entity.Account{Email: "found@test.com"})
	h := makeInternalHandler(as)
	r := testRouter()
	r.GET("/internal/v1/accounts/by-id/:id", withAllScopes(), h.GetAccountByID)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/accounts/by-id/1", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestInternalHandlerExtra_GetAccountByID_NotFound(t *testing.T) {
	h := makeInternalHandler(newMockAccountStore())
	r := testRouter()
	r.GET("/internal/v1/accounts/by-id/:id", withAllScopes(), h.GetAccountByID)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/accounts/by-id/999", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestInternalHandlerExtra_GetAccountByID_Error(t *testing.T) {
	errStore := &errAccountStoreH{*newMockAccountStore()}
	h := NewInternalHandler(
		app.NewAccountService(errStore, newMockWalletStore(), newMockVIPStore()),
		makeSubService(), makeEntitlementService(), makeVIPService(), nil,
		makeWalletService(), makeReferralService(), "",
	)
	r := testRouter()
	r.GET("/internal/v1/accounts/by-id/:id", withAllScopes(), h.GetAccountByID)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/accounts/by-id/1", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
}

// ── GetPaymentMethods ─────────────────────────────────────────────────────

func TestInternalHandlerExtra_GetPaymentMethods_NoProviders(t *testing.T) {
	h := makeInternalHandlerExtra(newMockAccountStore())
	r := testRouter()
	r.GET("/internal/v1/payment-methods", withAllScopes(), h.GetPaymentMethods)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/payment-methods", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	methods, _ := resp["payment_methods"].([]any)
	if len(methods) != 0 {
		t.Errorf("payment_methods count = %d, want 0", len(methods))
	}
}
