package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// ── GetCheckoutStatus ─────────────────────────────────────────────────────

func TestInternalHandlerDeep_GetCheckoutStatus_NotFound(t *testing.T) {
	h := makeInternalHandler(newMockAccountStore())
	r := testRouter()
	r.GET("/internal/v1/checkout/:order_no/status", withAllScopes(), h.GetCheckoutStatus)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/internal/v1/checkout/NONEXISTENT/status", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestInternalHandlerDeep_GetCheckoutStatus_Found(t *testing.T) {
	ws := newMockWalletStore()
	// Seed an order directly in the mock store.
	ws.orders["ORD-TEST-001"] = &entity.PaymentOrder{
		OrderNo:       "ORD-TEST-001",
		AccountID:     1,
		AmountCNY:     50.0,
		PaymentMethod: "stripe",
		Status:        entity.OrderStatusPending,
	}
	vipSvc := makeVIPService()
	walletSvc := app.NewWalletService(ws, vipSvc)

	h := NewInternalHandler(
		makeAccountService(), makeSubService(), makeEntitlementService(),
		vipSvc, nil, walletSvc, makeReferralService(), "",
	)
	r := testRouter()
	r.GET("/internal/v1/checkout/:order_no/status", withAllScopes(), h.GetCheckoutStatus)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/internal/v1/checkout/ORD-TEST-001/status", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["order_no"] != "ORD-TEST-001" {
		t.Errorf("order_no = %v, want ORD-TEST-001", resp["order_no"])
	}
	if resp["status"] != "pending" {
		t.Errorf("status = %v, want pending", resp["status"])
	}
}

// ── GetAccountOverview ────────────────────────────────────────────────────

func TestInternalHandlerDeep_GetAccountOverview_Success(t *testing.T) {
	as := newMockAccountStore()
	as.seed(entity.Account{ZitadelSub: "sub-ov", Email: "ov@test.com", DisplayName: "Overview"})
	h := makeInternalHandler(as)
	r := testRouter()
	r.GET("/internal/v1/accounts/:id/overview", withAllScopes(), h.GetAccountOverview)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/internal/v1/accounts/1/overview", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestInternalHandlerDeep_GetAccountOverview_WithProductID(t *testing.T) {
	as := newMockAccountStore()
	as.seed(entity.Account{ZitadelSub: "sub-ov2", Email: "ov2@test.com"})
	h := makeInternalHandler(as)
	r := testRouter()
	r.GET("/internal/v1/accounts/:id/overview", withAllScopes(), h.GetAccountOverview)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/internal/v1/accounts/1/overview?product_id=lucrum", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestInternalHandlerDeep_GetAccountOverview_InvalidID(t *testing.T) {
	h := makeInternalHandler(newMockAccountStore())
	r := testRouter()
	r.GET("/internal/v1/accounts/:id/overview", withAllScopes(), h.GetAccountOverview)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/internal/v1/accounts/abc/overview", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestInternalHandlerDeep_GetAccountOverview_AccountNotFound(t *testing.T) {
	h := makeInternalHandler(newMockAccountStore()) // empty store
	r := testRouter()
	r.GET("/internal/v1/accounts/:id/overview", withAllScopes(), h.GetAccountOverview)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/internal/v1/accounts/999/overview", nil)
	r.ServeHTTP(w, req)

	// Overview service tries to compute from DB; fails because account 999 doesn't exist.
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 (account not found in DB)", w.Code)
	}
}

// ── UpsertAccount with referrer ───────────────────────────────────────────

func TestInternalHandlerDeep_UpsertAccount_WithReferrer(t *testing.T) {
	as := newMockAccountStore()
	// Seed a referrer account with an aff_code.
	referrer := as.seed(entity.Account{
		ZitadelSub: "sub-referrer",
		Email:      "referrer@test.com",
		AffCode:    "REF123",
	})

	ws := newMockWalletStore()
	vs := newMockVIPStore()
	accountSvc := app.NewAccountService(as, ws, vs)
	vipSvc := app.NewVIPService(vs, ws)
	walletSvc := app.NewWalletService(ws, vipSvc)
	referralSvc := app.NewReferralServiceWithCodes(as, ws, &mockRedemptionCodeStore{})

	h := NewInternalHandler(
		accountSvc, makeSubService(), makeEntitlementService(),
		vipSvc, nil, walletSvc, referralSvc, "",
	)
	r := testRouter()
	r.POST("/internal/v1/accounts/upsert", withAllScopes(), h.UpsertAccount)

	body, _ := json.Marshal(map[string]string{
		"zitadel_sub":      "sub-new-user",
		"email":            "newuser@test.com",
		"display_name":     "New User",
		"referrer_aff_code": "REF123",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/internal/v1/accounts/upsert", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["email"] != "newuser@test.com" {
		t.Errorf("email = %v, want newuser@test.com", resp["email"])
	}
	_ = referrer
}

func TestInternalHandlerDeep_UpsertAccount_SelfReferralIgnored(t *testing.T) {
	as := newMockAccountStore()
	existing := as.seed(entity.Account{
		ZitadelSub: "sub-self",
		Email:      "self@test.com",
		AffCode:    "SELF001",
	})

	ws := newMockWalletStore()
	vs := newMockVIPStore()
	accountSvc := app.NewAccountService(as, ws, vs)
	vipSvc := app.NewVIPService(vs, ws)
	walletSvc := app.NewWalletService(ws, vipSvc)
	referralSvc := app.NewReferralServiceWithCodes(as, ws, &mockRedemptionCodeStore{})

	h := NewInternalHandler(
		accountSvc, makeSubService(), makeEntitlementService(),
		vipSvc, nil, walletSvc, referralSvc, "",
	)
	r := testRouter()
	r.POST("/internal/v1/accounts/upsert", withAllScopes(), h.UpsertAccount)

	// Try to refer yourself.
	body, _ := json.Marshal(map[string]string{
		"zitadel_sub":      "sub-self",
		"email":            "self@test.com",
		"referrer_aff_code": "SELF001", // own code
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/internal/v1/accounts/upsert", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	// Self-referral should be silently ignored.
	a, _ := as.GetByID(context.Background(), existing.ID)
	if a.ReferrerID != nil {
		t.Error("self-referral should be ignored (ReferrerID should remain nil)")
	}
}

// ── DebitWallet success response fields ───────────────────────────────────

func TestInternalHandlerDeep_DebitWallet_SuccessResponseFields(t *testing.T) {
	ws := newMockWalletStore()
	ws.Credit(context.Background(), 1, 100.0, "topup", "", "", "", "")
	vipSvc := makeVIPService()
	walletSvc := app.NewWalletService(ws, vipSvc)

	h := NewInternalHandler(
		makeAccountService(), makeSubService(), makeEntitlementService(),
		vipSvc, nil, walletSvc, makeReferralService(), "",
	)
	r := testRouter()
	r.POST("/internal/v1/accounts/:id/wallet/debit", withAllScopes(), h.DebitWallet)

	body, _ := json.Marshal(map[string]any{
		"amount":      25.5,
		"type":        "api_usage",
		"product_id":  "lucrum",
		"description": "API overage charge",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/internal/v1/accounts/1/wallet/debit", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["success"] != true {
		t.Errorf("success = %v, want true", resp["success"])
	}
	bal, _ := resp["balance_after"].(float64)
	expected := 100.0 - 25.5
	if bal < expected-0.01 || bal > expected+0.01 {
		t.Errorf("balance_after = %.2f, want ~%.2f", bal, expected)
	}
}

// ── CreditWallet success ─────────────────────────────────────────────────

func TestInternalHandlerDeep_CreditWallet_Success(t *testing.T) {
	h := makeInternalHandler(newMockAccountStore())
	r := testRouter()
	r.POST("/admin/v1/accounts/:id/wallet/credit", h.CreditWallet)

	body, _ := json.Marshal(map[string]any{
		"amount":      100.0,
		"type":        "marketplace_revenue",
		"description": "Author revenue share",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/admin/v1/accounts/1/wallet/credit", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["success"] != true {
		t.Errorf("success = %v, want true", resp["success"])
	}
	bal, _ := resp["balance_after"].(float64)
	if bal != 100.0 {
		t.Errorf("balance_after = %.2f, want 100.0", bal)
	}
}

// ── CreateCheckout amount exceeds max ─────────────────────────────────────

func TestInternalHandlerDeep_CreateCheckout_AmountAboveMax(t *testing.T) {
	h := makeInternalHandler(newMockAccountStore())
	r := testRouter()
	r.POST("/internal/v1/checkout/create", withAllScopes(), h.CreateCheckout)

	body, _ := json.Marshal(map[string]any{
		"account_id":     1,
		"amount_cny":     150000.0, // exceeds 100,000 max
		"payment_method": "stripe",
		"source_service": "lucrum",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/internal/v1/checkout/create", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (exceeds max)", w.Code)
	}
}

// ── ReportUsage response ──────────────────────────────────────────────────

func TestInternalHandlerDeep_ReportUsage_AcceptedResponse(t *testing.T) {
	h := makeInternalHandler(newMockAccountStore())
	r := testRouter()
	r.POST("/internal/v1/usage/report", withAllScopes(), h.ReportUsage)

	body, _ := json.Marshal(map[string]any{
		"account_id": 1,
		"amount_cny": 5.0,
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/internal/v1/usage/report", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["accepted"] != true {
		t.Errorf("accepted = %v, want true", resp["accepted"])
	}
}

// ── GetAccountByOAuth not found ───────────────────────────────────────────

func TestInternalHandlerDeep_GetAccountByOAuth_NotFound(t *testing.T) {
	h := makeInternalHandler(newMockAccountStore())
	r := testRouter()
	r.GET("/internal/v1/accounts/by-oauth/:provider/:provider_id", withAllScopes(), h.GetAccountByOAuth)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/internal/v1/accounts/by-oauth/wechat/nonexistent", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}
