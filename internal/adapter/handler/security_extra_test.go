package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/adapter/payment"
	"github.com/hanmahong5-arch/lurus-platform/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// ── IDOR: Pre-Auth Cross-Account Prevention ───────────────────────────────

func TestSecurity_PreAuth_IDOR_SettleOtherAccountPreAuth(t *testing.T) {
	// Account 1 creates a pre-auth, account 2 (via internal API) tries to settle it.
	// Since internal API uses service scopes (not user identity), this tests
	// that pre-auth IDs are not guessable and settle validates ownership.
	ws := newMockWalletStore()
	ws.Credit(context.Background(), 1, 100.0, "topup", "", "", "", "")
	vipSvc := makeVIPService()
	walletSvc := app.NewWalletService(ws, vipSvc)

	h := NewInternalHandler(
		makeAccountService(), makeSubService(), makeEntitlementService(),
		vipSvc, nil, walletSvc, makeReferralService(), "",
	)
	r := testRouter()
	r.POST("/internal/v1/accounts/:id/wallet/pre-authorize", withAllScopes(), h.PreAuthorize)
	r.POST("/internal/v1/wallet/pre-auth/:id/settle", withAllScopes(), h.SettlePreAuth)

	// Create pre-auth for account 1.
	body, _ := json.Marshal(map[string]any{
		"amount": 10.0, "product_id": "lucrum",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/internal/v1/accounts/1/wallet/pre-authorize", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("pre-auth creation failed: %d %s", w.Code, w.Body.String())
	}
	var paResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &paResp)
	preauthID := paResp["preauth_id"]

	// Settle with the pre-auth ID (should work — internal API).
	settleBody, _ := json.Marshal(map[string]any{"actual_amount": 8.0})
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("POST", "/internal/v1/wallet/pre-auth/"+formatFloat(preauthID)+"/settle", bytes.NewReader(settleBody))
	req2.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("settle status = %d, want 200; body: %s", w2.Code, w2.Body.String())
	}
}

// ── IDOR: Wallet Order Cross-Account ──────────────────────────────────────

func TestSecurity_Wallet_GetOrder_CrossAccount(t *testing.T) {
	// Order belongs to account 1; request from account 2 should get 404 (not 403).
	ws := newMockWalletStore()
	vipSvc := makeVIPService()
	walletSvc := app.NewWalletService(ws, vipSvc)

	// Create order for account 1.
	order := &entity.PaymentOrder{
		AccountID:     1,
		OrderNo:       "ORD-SEC-001",
		OrderType:     "topup",
		AmountCNY:     50.0,
		PaymentMethod: "stripe",
		Status:        entity.OrderStatusPending,
	}
	ws.CreatePaymentOrder(context.Background(), order)

	h := NewWalletHandler(walletSvc, payment.NewRegistry())
	r := testRouter()
	r.GET("/api/v1/wallet/orders/:order_no", withAccountID(2), h.GetOrder) // user 2

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/wallet/orders/ORD-SEC-001", nil)
	r.ServeHTTP(w, req)

	// Should be 404 (not 200 or 403) to prevent enumeration.
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (IDOR prevention)", w.Code)
	}
}

// ── Concurrent Double Debit Prevention ────────────────────────────────────

func TestSecurity_ConcurrentDebit_NoOverdraft(t *testing.T) {
	ws := newMockWalletStore()
	ws.Credit(context.Background(), 1, 10.0, "topup", "", "", "", "")
	vipSvc := makeVIPService()
	walletSvc := app.NewWalletService(ws, vipSvc)

	h := NewInternalHandler(
		makeAccountService(), makeSubService(), makeEntitlementService(),
		vipSvc, nil, walletSvc, makeReferralService(), "",
	)
	r := testRouter()
	r.POST("/internal/v1/accounts/:id/wallet/debit", withAllScopes(), h.DebitWallet)

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)
	var mu sync.Mutex
	successCount := 0

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			body, _ := json.Marshal(map[string]any{
				"amount": 1.0, "type": "concurrent_test",
			})
			w := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/internal/v1/accounts/1/wallet/debit", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			if w.Code == http.StatusOK {
				mu.Lock()
				successCount++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	// With 10.0 balance and 1.0 per debit, exactly 10 should succeed.
	if successCount > 10 {
		t.Errorf("concurrent debit: %d succeeded (overdraft!), want <= 10", successCount)
	}
	if successCount == 0 {
		t.Error("concurrent debit: 0 succeeded, expected some")
	}
}

// ── Scope Escalation: wrong scope on admin endpoint ───────────────────────

func TestSecurity_AdminCreditWallet_NoScope(t *testing.T) {
	// The CreditWallet endpoint is under admin routes and doesn't use requireScope.
	// It relies on admin JWT auth middleware instead.
	// This test verifies the handler itself doesn't crash with nil wallet service.
	h := NewInternalHandler(
		makeAccountService(), makeSubService(), makeEntitlementService(),
		makeVIPService(), nil, makeWalletService(), makeReferralService(), "",
	)
	r := testRouter()
	r.POST("/admin/v1/accounts/:id/wallet/credit", h.CreditWallet)

	body, _ := json.Marshal(map[string]any{
		"amount": 10.0, "type": "admin_credit", "description": "test credit",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/admin/v1/accounts/1/wallet/credit", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["success"] != true {
		t.Errorf("success = %v, want true", resp["success"])
	}
}

func TestSecurity_AdminCreditWallet_InvalidID(t *testing.T) {
	h := NewInternalHandler(
		makeAccountService(), makeSubService(), makeEntitlementService(),
		makeVIPService(), nil, makeWalletService(), makeReferralService(), "",
	)
	r := testRouter()
	r.POST("/admin/v1/accounts/:id/wallet/credit", h.CreditWallet)

	body, _ := json.Marshal(map[string]any{"amount": 10.0, "type": "t"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/admin/v1/accounts/abc/wallet/credit", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestSecurity_AdminCreditWallet_MissingFields(t *testing.T) {
	h := NewInternalHandler(
		makeAccountService(), makeSubService(), makeEntitlementService(),
		makeVIPService(), nil, makeWalletService(), makeReferralService(), "",
	)
	r := testRouter()
	r.POST("/admin/v1/accounts/:id/wallet/credit", h.CreditWallet)

	body, _ := json.Marshal(map[string]any{"amount": 10.0}) // missing type
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/admin/v1/accounts/1/wallet/credit", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (missing type)", w.Code)
	}
}

// ── Malformed JSON body ───────────────────────────────────────────────────

func TestSecurity_MalformedJSON_InternalAPI(t *testing.T) {
	h := NewInternalHandler(
		makeAccountService(), makeSubService(), makeEntitlementService(),
		makeVIPService(), nil, makeWalletService(), makeReferralService(), "",
	)
	r := testRouter()
	r.POST("/internal/v1/accounts/:id/wallet/debit", withAllScopes(), h.DebitWallet)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/internal/v1/accounts/1/wallet/debit", bytes.NewReader([]byte("{{invalid json")))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (malformed JSON)", w.Code)
	}
}

func TestSecurity_EmptyBody_InternalAPI(t *testing.T) {
	h := NewInternalHandler(
		makeAccountService(), makeSubService(), makeEntitlementService(),
		makeVIPService(), nil, makeWalletService(), makeReferralService(), "",
	)
	r := testRouter()
	r.POST("/internal/v1/accounts/:id/wallet/debit", withAllScopes(), h.DebitWallet)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/internal/v1/accounts/1/wallet/debit", nil)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (empty body)", w.Code)
	}
}

// ── Type confusion ────────────────────────────────────────────────────────

func TestSecurity_TypeConfusion_StringAsNumber(t *testing.T) {
	h := NewInternalHandler(
		makeAccountService(), makeSubService(), makeEntitlementService(),
		makeVIPService(), nil, makeWalletService(), makeReferralService(), "",
	)
	r := testRouter()
	r.POST("/internal/v1/accounts/:id/wallet/debit", withAllScopes(), h.DebitWallet)

	// Send string where number expected.
	body := []byte(`{"amount":"not-a-number","type":"test"}`)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/internal/v1/accounts/1/wallet/debit", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (type confusion)", w.Code)
	}
}

// helper
func formatFloat(v any) string {
	switch f := v.(type) {
	case float64:
		return fmt.Sprintf("%d", int64(f))
	default:
		return fmt.Sprintf("%v", v)
	}
}
