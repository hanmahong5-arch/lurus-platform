package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/auth"
)

func TestInternalHandler_GetAccountByZitadelSub(t *testing.T) {
	as := newMockAccountStore()
	acct := as.seed(entity.Account{ZitadelSub: "sub-123", Email: "a@b.com", DisplayName: "Alice"})

	h := NewInternalHandler(
		makeAccountServiceWith(as),
		makeSubService(),
		makeEntitlementService(),
		makeVIPService(),
		nil,
		makeWalletService(),
		makeReferralService(),
		"",
	)

	r := testRouter()
	r.GET("/internal/v1/accounts/by-zitadel-sub/:sub", h.GetAccountByZitadelSub)

	tests := []struct {
		name   string
		sub    string
		status int
	}{
		{"found", "sub-123", http.StatusOK},
		{"not_found", "no-such-sub", http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/internal/v1/accounts/by-zitadel-sub/"+tt.sub, nil)
			r.ServeHTTP(w, req)
			if w.Code != tt.status {
				t.Errorf("status = %d, want %d", w.Code, tt.status)
			}
			if tt.status == http.StatusOK {
				var resp map[string]interface{}
				json.Unmarshal(w.Body.Bytes(), &resp)
				if resp["email"] != acct.Email {
					t.Errorf("email = %v, want %s", resp["email"], acct.Email)
				}
			}
		})
	}
}

func TestInternalHandler_UpsertAccount(t *testing.T) {
	as := newMockAccountStore()
	h := NewInternalHandler(
		makeAccountServiceWith(as),
		makeSubService(),
		makeEntitlementService(),
		makeVIPService(),
		nil,
		makeWalletService(),
		makeReferralService(),
		"",
	)

	r := testRouter()
	r.POST("/internal/v1/accounts/upsert", h.UpsertAccount)

	tests := []struct {
		name   string
		body   map[string]string
		status int
	}{
		{
			"valid_new_account",
			map[string]string{"zitadel_sub": "new-sub", "email": "new@b.com", "display_name": "Bob"},
			http.StatusOK,
		},
		{
			"missing_sub",
			map[string]string{"email": "c@b.com"},
			http.StatusBadRequest,
		},
		{
			"missing_email",
			map[string]string{"zitadel_sub": "sub-x"},
			http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.body)
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/internal/v1/accounts/upsert", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			if w.Code != tt.status {
				t.Errorf("status = %d, want %d, body: %s", w.Code, tt.status, w.Body.String())
			}
		})
	}
}

func TestInternalHandler_GetEntitlements(t *testing.T) {
	h := NewInternalHandler(
		makeAccountService(),
		makeSubService(),
		makeEntitlementService(),
		makeVIPService(),
		nil,
		makeWalletService(),
		makeReferralService(),
		"",
	)

	r := testRouter()
	r.GET("/internal/v1/accounts/:id/entitlements/:product_id", h.GetEntitlements)

	tests := []struct {
		name      string
		path      string
		status    int
		checkFree bool // expect default {"plan_code":"free"}
	}{
		{"valid_id_no_sub", "/internal/v1/accounts/1/entitlements/lurus_api", http.StatusOK, true},
		{"invalid_id", "/internal/v1/accounts/abc/entitlements/lurus_api", http.StatusBadRequest, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			r.ServeHTTP(w, req)
			if w.Code != tt.status {
				t.Errorf("status = %d, want %d", w.Code, tt.status)
			}
			if tt.checkFree {
				var resp map[string]string
				json.Unmarshal(w.Body.Bytes(), &resp)
				if resp["plan_code"] != "free" {
					t.Errorf("plan_code = %q, want \"free\"", resp["plan_code"])
				}
			}
		})
	}
}

func TestInternalHandler_GetSubscription(t *testing.T) {
	h := NewInternalHandler(
		makeAccountService(),
		makeSubService(),
		makeEntitlementService(),
		makeVIPService(),
		nil,
		makeWalletService(),
		makeReferralService(),
		"",
	)

	r := testRouter()
	r.GET("/internal/v1/accounts/:id/subscription/:product_id", h.GetSubscription)

	tests := []struct {
		name   string
		path   string
		status int
	}{
		{"no_sub", "/internal/v1/accounts/1/subscription/lurus_api", http.StatusNotFound},
		{"bad_id", "/internal/v1/accounts/abc/subscription/lurus_api", http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			r.ServeHTTP(w, req)
			if w.Code != tt.status {
				t.Errorf("status = %d, want %d", w.Code, tt.status)
			}
		})
	}
}

func TestInternalHandler_ReportUsage(t *testing.T) {
	h := NewInternalHandler(
		makeAccountService(),
		makeSubService(),
		makeEntitlementService(),
		makeVIPService(),
		nil,
		makeWalletService(),
		makeReferralService(),
		"",
	)

	r := testRouter()
	r.POST("/internal/v1/usage/report", h.ReportUsage)

	tests := []struct {
		name   string
		body   map[string]interface{}
		status int
	}{
		{"valid", map[string]interface{}{"account_id": 1, "amount_cny": 10.5}, http.StatusOK},
		{"missing_account", map[string]interface{}{"amount_cny": 10.5}, http.StatusBadRequest},
		{"missing_amount", map[string]interface{}{"account_id": 1}, http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.body)
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/internal/v1/usage/report", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			if w.Code != tt.status {
				t.Errorf("status = %d, want %d, body: %s", w.Code, tt.status, w.Body.String())
			}
		})
	}
}

func TestInternalHandler_DebitWallet(t *testing.T) {
	ws := newMockWalletStore()
	// Pre-fund account 1 with 100.0 LB
	_, _ = ws.Credit(context.Background(), 1, 100.0, "topup", "test fund", "", "", "")
	walletSvc := app.NewWalletService(ws, makeVIPService())

	h := NewInternalHandler(
		makeAccountService(),
		makeSubService(),
		makeEntitlementService(),
		makeVIPService(),
		nil,
		walletSvc,
		makeReferralService(),
		"",
	)

	r := testRouter()
	r.POST("/internal/v1/accounts/:id/wallet/debit", h.DebitWallet)

	tests := []struct {
		name   string
		path   string
		body   map[string]interface{}
		status int
	}{
		{
			"valid_debit",
			"/internal/v1/accounts/1/wallet/debit",
			map[string]interface{}{"amount": 10.0, "type": "ai_quota_overage", "product_id": "lurus-gushen"},
			http.StatusOK,
		},
		{
			"insufficient_balance",
			"/internal/v1/accounts/1/wallet/debit",
			map[string]interface{}{"amount": 999.0, "type": "ai_quota_overage"},
			http.StatusBadRequest,
		},
		{
			"missing_amount",
			"/internal/v1/accounts/1/wallet/debit",
			map[string]interface{}{"type": "ai_quota_overage"},
			http.StatusBadRequest,
		},
		{
			"zero_amount_rejected",
			"/internal/v1/accounts/1/wallet/debit",
			map[string]interface{}{"amount": 0.0, "type": "ai_quota_overage"},
			http.StatusBadRequest,
		},
		{
			"invalid_account_id",
			"/internal/v1/accounts/abc/wallet/debit",
			map[string]interface{}{"amount": 10.0, "type": "ai_quota_overage"},
			http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.body)
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, tt.path, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			if w.Code != tt.status {
				t.Errorf("status = %d, want %d, body: %s", w.Code, tt.status, w.Body.String())
			}
		})
	}

	t.Run("balance_after_returned", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{"amount": 5.0, "type": "ai_quota_overage"})
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/internal/v1/accounts/1/wallet/debit", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["success"] != true {
			t.Error("expected success=true")
		}
		if resp["balance_after"] == nil {
			t.Error("expected balance_after in response")
		}
	})
}

func TestInternalHandler_CreditWallet(t *testing.T) {
	ws := newMockWalletStore()
	walletSvc := app.NewWalletService(ws, makeVIPService())

	h := NewInternalHandler(
		makeAccountService(),
		makeSubService(),
		makeEntitlementService(),
		makeVIPService(),
		nil,
		walletSvc,
		makeReferralService(),
		"",
	)

	r := testRouter()
	r.POST("/internal/v1/accounts/:id/wallet/credit", h.CreditWallet)

	tests := []struct {
		name   string
		path   string
		body   map[string]interface{}
		status int
	}{
		{
			"valid_credit",
			"/internal/v1/accounts/1/wallet/credit",
			map[string]interface{}{"amount": 3.5, "type": "marketplace_revenue", "product_id": "lurus-gushen"},
			http.StatusOK,
		},
		{
			"missing_type",
			"/internal/v1/accounts/1/wallet/credit",
			map[string]interface{}{"amount": 3.5},
			http.StatusBadRequest,
		},
		{
			"zero_amount_rejected",
			"/internal/v1/accounts/1/wallet/credit",
			map[string]interface{}{"amount": 0, "type": "marketplace_revenue"},
			http.StatusBadRequest,
		},
		{
			"invalid_id",
			"/internal/v1/accounts/xyz/wallet/credit",
			map[string]interface{}{"amount": 1.0, "type": "marketplace_revenue"},
			http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.body)
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, tt.path, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			if w.Code != tt.status {
				t.Errorf("status = %d, want %d, body: %s", w.Code, tt.status, w.Body.String())
			}
		})
	}

	t.Run("balance_increases_after_credit", func(t *testing.T) {
		// Credit 10 LB to account 2
		body, _ := json.Marshal(map[string]interface{}{"amount": 10.0, "type": "marketplace_revenue"})
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/internal/v1/accounts/2/wallet/credit", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["balance_after"].(float64) != 10.0 {
			t.Errorf("balance_after = %v, want 10.0", resp["balance_after"])
		}
	})
}

func TestInternalHandler_GetAccountByOAuth(t *testing.T) {
	h := NewInternalHandler(
		makeAccountService(),
		makeSubService(),
		makeEntitlementService(),
		makeVIPService(),
		nil,
		makeWalletService(),
		makeReferralService(),
		"",
	)

	r := testRouter()
	r.GET("/internal/v1/accounts/by-oauth/:provider/:provider_id", h.GetAccountByOAuth)

	tests := []struct {
		name   string
		path   string
		status int
	}{
		{"not_found", "/internal/v1/accounts/by-oauth/wechat/wx123", http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			r.ServeHTTP(w, req)
			if w.Code != tt.status {
				t.Errorf("status = %d, want %d", w.Code, tt.status)
			}
		})
	}
}

func TestInternalHandler_GetAccountOverview(t *testing.T) {
	as := newMockAccountStore()
	as.seed(entity.Account{ZitadelSub: "sub-ov", Email: "ov@x.com", DisplayName: "OverviewUser"})

	h := NewInternalHandler(
		makeAccountServiceWith(as),
		makeSubService(),
		makeEntitlementService(),
		makeVIPService(),
		makeOverviewServiceWithAccounts(as),
		makeWalletService(),
		makeReferralService(),
		"",
	)

	r := testRouter()
	r.GET("/internal/v1/accounts/:id/overview", h.GetAccountOverview)

	t.Run("ok", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/internal/v1/accounts/1/overview", nil)
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("invalid_id", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/internal/v1/accounts/abc/overview", nil)
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("status=%d, want 400", w.Code)
		}
	})

	t.Run("not_found", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/internal/v1/accounts/999/overview", nil)
		r.ServeHTTP(w, req)
		if w.Code != http.StatusInternalServerError {
			t.Errorf("status=%d, want 500 (account not found → overview compute error)", w.Code)
		}
	})
}

func TestInternalHandler_UpsertAccount_WithReferrer(t *testing.T) {
	as := newMockAccountStore()
	// Seed referrer with known aff_code
	referrer := as.seed(entity.Account{
		ZitadelSub:  "referrer-sub",
		Email:       "referrer@b.com",
		DisplayName: "Referrer",
		AffCode:     "abc12345",
	})

	h := NewInternalHandler(
		makeAccountServiceWith(as),
		makeSubService(),
		makeEntitlementService(),
		makeVIPService(),
		nil,
		makeWalletService(),
		makeReferralService(),
		"",
	)

	r := testRouter()
	r.POST("/internal/v1/accounts/upsert", h.UpsertAccount)

	t.Run("with_valid_referrer_aff_code", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{
			"zitadel_sub":       "new-ref-sub",
			"email":             "newref@b.com",
			"display_name":      "NewRef",
			"referrer_aff_code": "abc12345",
		})
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/internal/v1/accounts/upsert", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
		}
	})

	t.Run("with_invalid_referrer_aff_code_still_creates", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{
			"zitadel_sub":       "new-ref-sub2",
			"email":             "newref2@b.com",
			"referrer_aff_code": "nonexistent",
		})
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/internal/v1/accounts/upsert", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		// Should succeed even with invalid referrer code
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
	})

	_ = referrer
}

// makeInternalHandler builds an InternalHandler with the given account store.
func makeInternalHandler(as *mockAccountStore) *InternalHandler {
	return NewInternalHandler(
		makeAccountServiceWith(as),
		makeSubService(),
		makeEntitlementService(),
		makeVIPService(),
		makeOverviewServiceWithAccounts(as),
		makeWalletService(),
		makeReferralService(),
		"",
	)
}

// makeInternalHandlerWithSecret builds an InternalHandler with a session secret.
func makeInternalHandlerWithSecret(as *mockAccountStore, secret string) *InternalHandler {
	return NewInternalHandler(
		makeAccountServiceWith(as),
		makeSubService(),
		makeEntitlementService(),
		makeVIPService(),
		makeOverviewServiceWithAccounts(as),
		makeWalletService(),
		makeReferralService(),
		secret,
	)
}

// TestInternalHandler_GetAccountByEmail_NotFound verifies 404 when email is not found.
func TestInternalHandler_GetAccountByEmail_NotFound(t *testing.T) {
	as := newMockAccountStore()
	h := makeInternalHandler(as)

	r := testRouter()
	r.GET("/internal/v1/accounts/by-email/:email", h.GetAccountByEmail)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/accounts/by-email/unknown@example.com", nil)
	r.ServeHTTP(w, req)

	// mockAccountStore.GetByEmail always returns nil → 404
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// TestInternalHandler_GetAccountByPhone_Found verifies 200 when phone is found.
func TestInternalHandler_GetAccountByPhone_Found(t *testing.T) {
	as := newMockAccountStore()
	as.seed(entity.Account{Phone: "+8613800138000", Email: "phone@example.com"})
	h := makeInternalHandler(as)

	r := testRouter()
	r.GET("/internal/v1/accounts/by-phone/:phone", h.GetAccountByPhone)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/accounts/by-phone/+8613800138000", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// TestInternalHandler_GetAccountByPhone_NotFound verifies 404 when phone is not found.
func TestInternalHandler_GetAccountByPhone_NotFound(t *testing.T) {
	as := newMockAccountStore()
	h := makeInternalHandler(as)

	r := testRouter()
	r.GET("/internal/v1/accounts/by-phone/:phone", h.GetAccountByPhone)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/accounts/by-phone/+10000000000", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// TestInternalHandler_GetWalletBalance_Success verifies 200 with balance data.
func TestInternalHandler_GetWalletBalance_Success(t *testing.T) {
	as := newMockAccountStore()
	h := makeInternalHandler(as)

	r := testRouter()
	r.GET("/internal/v1/accounts/:id/wallet/balance", h.GetWalletBalance)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/accounts/1/wallet/balance", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response not JSON: %v", err)
	}
	if _, ok := resp["balance"]; !ok {
		t.Error("response missing 'balance'")
	}
}

// TestInternalHandler_GetWalletBalance_BadID verifies 400 for invalid account ID.
func TestInternalHandler_GetWalletBalance_BadID(t *testing.T) {
	as := newMockAccountStore()
	h := makeInternalHandler(as)

	r := testRouter()
	r.GET("/internal/v1/accounts/:id/wallet/balance", h.GetWalletBalance)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/accounts/not-a-number/wallet/balance", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// TestInternalHandler_ValidateSession_NoSecret verifies 503 when session secret is not configured.
func TestInternalHandler_ValidateSession_NoSecret(t *testing.T) {
	as := newMockAccountStore()
	h := makeInternalHandlerWithSecret(as, "") // empty secret → disabled

	r := testRouter()
	r.POST("/internal/v1/accounts/validate-session", h.ValidateSession)

	body, _ := json.Marshal(map[string]string{"token": "sometoken"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/accounts/validate-session", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 (session validation not configured)", w.Code)
	}
}

// TestInternalHandler_ValidateSession_InvalidToken verifies 401 for invalid token.
func TestInternalHandler_ValidateSession_InvalidToken(t *testing.T) {
	as := newMockAccountStore()
	const secret = "test-session-secret-at-least-32-bytes!!"
	h := makeInternalHandlerWithSecret(as, secret)

	r := testRouter()
	r.POST("/internal/v1/accounts/validate-session", h.ValidateSession)

	body, _ := json.Marshal(map[string]string{"token": "not-a-valid-token"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/accounts/validate-session", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

// TestInternalHandler_ValidateSession_MissingToken verifies 400 when token field is absent.
func TestInternalHandler_ValidateSession_MissingToken(t *testing.T) {
	as := newMockAccountStore()
	const secret = "test-session-secret-at-least-32-bytes!!"
	h := makeInternalHandlerWithSecret(as, secret)

	r := testRouter()
	r.POST("/internal/v1/accounts/validate-session", h.ValidateSession)

	body, _ := json.Marshal(map[string]string{}) // missing token field
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/accounts/validate-session", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// TestInternalHandler_GetEntitlements_Error verifies 500 when entitlement store fails.
func TestInternalHandler_GetEntitlements_Error(t *testing.T) {
	entSvc := app.NewEntitlementService(&errEntSubStore{*newMockSubStore()}, newMockPlanStore(), newMockCache())
	h := NewInternalHandler(
		makeAccountService(), makeSubService(), entSvc,
		makeVIPService(), nil, makeWalletService(), makeReferralService(), "",
	)
	r := testRouter()
	r.GET("/internal/v1/accounts/:id/entitlements/:product_id", h.GetEntitlements)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/accounts/1/entitlements/lurus_api", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500; body: %s", w.Code, w.Body.String())
	}
}

// TestInternalHandler_GetSubscription_Error verifies 500 when the subscription store returns an error.
func TestInternalHandler_GetSubscription_Error(t *testing.T) {
	subSvc := app.NewSubscriptionService(&errGetActiveSubStore{*newMockSubStore()}, newMockPlanStore(), makeEntitlementService(), 3)
	h := NewInternalHandler(
		makeAccountService(), subSvc, makeEntitlementService(),
		makeVIPService(), nil, makeWalletService(), makeReferralService(), "",
	)
	r := testRouter()
	r.GET("/internal/v1/accounts/:id/subscription/:product_id", h.GetSubscription)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/accounts/1/subscription/lurus_api", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500; body: %s", w.Code, w.Body.String())
	}
}

// TestInternalHandler_GetSubscription_Found verifies 200 when an active subscription exists.
func TestInternalHandler_GetSubscription_Found(t *testing.T) {
	store := newMockSubStore()
	store.active["1:lurus_api"] = &entity.Subscription{
		ID: 1, AccountID: 1, ProductID: "lurus_api", PlanID: 1, Status: "active",
	}
	subSvc := app.NewSubscriptionService(store, newMockPlanStore(), makeEntitlementService(), 3)
	h := NewInternalHandler(
		makeAccountService(), subSvc, makeEntitlementService(),
		makeVIPService(), nil, makeWalletService(), makeReferralService(), "",
	)
	r := testRouter()
	r.GET("/internal/v1/accounts/:id/subscription/:product_id", h.GetSubscription)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/accounts/1/subscription/lurus_api", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

// TestInternalHandler_GetAccountByEmail_Found verifies 200 when account is found by email.
func TestInternalHandler_GetAccountByEmail_Found(t *testing.T) {
	eas := newEmailAwareAccountStore()
	eas.seedEmail(entity.Account{ZitadelSub: "sub-email", Email: "found@x.com"})
	h := NewInternalHandler(
		app.NewAccountService(eas, newMockWalletStore(), newMockVIPStore()),
		makeSubService(),
		makeEntitlementService(),
		makeVIPService(),
		nil,
		makeWalletService(),
		makeReferralService(),
		"",
	)

	r := testRouter()
	r.GET("/internal/v1/accounts/by-email/:email", h.GetAccountByEmail)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/accounts/by-email/found@x.com", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

// TestInternalHandler_GetAccountByEmail_Error verifies 500 when the store returns an error.
func TestInternalHandler_GetAccountByEmail_Error(t *testing.T) {
	errStore := &errEmailAccountStore{*newMockAccountStore()}
	h := NewInternalHandler(
		app.NewAccountService(errStore, newMockWalletStore(), newMockVIPStore()),
		makeSubService(),
		makeEntitlementService(),
		makeVIPService(),
		nil,
		makeWalletService(),
		makeReferralService(),
		"",
	)

	r := testRouter()
	r.GET("/internal/v1/accounts/by-email/:email", h.GetAccountByEmail)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/accounts/by-email/err@x.com", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500; body: %s", w.Code, w.Body.String())
	}
}

// TestInternalHandler_ValidateSession_Success verifies 200 with a valid session token.
func TestInternalHandler_ValidateSession_Success(t *testing.T) {
	as := newMockAccountStore()
	acct := as.seed(entity.Account{ZitadelSub: "sub-sess", Email: "sess@x.com"})
	const secret = "test-session-secret-at-least-32-bytes!!"
	h := makeInternalHandlerWithSecret(as, secret)

	token, err := auth.IssueSessionToken(acct.ID, time.Hour, secret)
	if err != nil {
		t.Fatalf("IssueSessionToken: %v", err)
	}

	r := testRouter()
	r.POST("/internal/v1/accounts/validate-session", h.ValidateSession)

	body, _ := json.Marshal(map[string]string{"token": token})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/accounts/validate-session", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

// TestInternalHandler_ValidateSession_AccountNotFound verifies 404 when token is valid but account is absent.
func TestInternalHandler_ValidateSession_AccountNotFound(t *testing.T) {
	as := newMockAccountStore() // empty store — no account with ID 999
	const secret = "test-session-secret-at-least-32-bytes!!"
	h := makeInternalHandlerWithSecret(as, secret)

	token, err := auth.IssueSessionToken(999, time.Hour, secret)
	if err != nil {
		t.Fatalf("IssueSessionToken: %v", err)
	}

	r := testRouter()
	r.POST("/internal/v1/accounts/validate-session", h.ValidateSession)

	body, _ := json.Marshal(map[string]string{"token": token})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/accounts/validate-session", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404; body: %s", w.Code, w.Body.String())
	}
}
func TestInternalHandler_GetAccountByOAuth_Error(t *testing.T) {
	errStore := &errOAuthBindingStoreH{*newMockAccountStore()}
	acctSvc := app.NewAccountService(errStore, newMockWalletStore(), newMockVIPStore())
	h := NewInternalHandler(acctSvc, makeSubService(), makeEntitlementService(), makeVIPService(), nil, makeWalletService(), makeReferralService(), "")

	r := testRouter()
	r.GET("/internal/v1/accounts/by-oauth/:provider/:provider_id", h.GetAccountByOAuth)

	req := httptest.NewRequest(http.MethodGet, "/internal/v1/accounts/by-oauth/wechat/wx-err", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
}

func TestInternalHandler_GetAccountByOAuth_Found(t *testing.T) {
	oauthStore := &oauthAwareAccountStore{mockAccountStore: *newMockAccountStore()}
	acct := oauthStore.seed(entity.Account{ZitadelSub: "sub-oauth-found", Email: "oauth@x.com"})
	oauthStore.oauthAccount = acct
	acctSvc := app.NewAccountService(oauthStore, newMockWalletStore(), newMockVIPStore())
	h := NewInternalHandler(acctSvc, makeSubService(), makeEntitlementService(), makeVIPService(), nil, makeWalletService(), makeReferralService(), "")

	r := testRouter()
	r.GET("/internal/v1/accounts/by-oauth/:provider/:provider_id", h.GetAccountByOAuth)

	req := httptest.NewRequest(http.MethodGet, "/internal/v1/accounts/by-oauth/wechat/wx-found", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}
