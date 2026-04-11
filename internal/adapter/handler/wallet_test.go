package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/adapter/payment"
	"github.com/hanmahong5-arch/lurus-platform/internal/app"
)

func TestWalletHandler_GetWallet(t *testing.T) {
	h := NewWalletHandler(makeWalletService(), payment.NewRegistry())
	r := testRouter()
	r.GET("/api/v1/wallet", withAccountID(1), h.GetWallet)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/wallet", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}
}

func TestWalletHandler_ListTransactions(t *testing.T) {
	h := NewWalletHandler(makeWalletService(), payment.NewRegistry())
	r := testRouter()
	r.GET("/api/v1/wallet/transactions", withAccountID(1), h.ListTransactions)

	tests := []struct {
		name  string
		query string
	}{
		{"default", ""},
		{"custom_page", "?page=2&page_size=10"},
		{"bad_page_normalized", "?page=-5&page_size=999"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/wallet/transactions"+tt.query, nil)
			r.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
			}
		})
	}
}

func TestWalletHandler_Redeem(t *testing.T) {
	h := NewWalletHandler(makeWalletService(), payment.NewRegistry())
	r := testRouter()
	r.POST("/api/v1/wallet/redeem", withAccountID(1), h.Redeem)

	tests := []struct {
		name   string
		body   map[string]string
		status int
	}{
		{"missing_code", map[string]string{}, http.StatusBadRequest},
		{"invalid_code", map[string]string{"code": "NOTREAL"}, http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.body)
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/wallet/redeem", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			if w.Code != tt.status {
				t.Errorf("status = %d, want %d", w.Code, tt.status)
			}
		})
	}
}

func TestWalletHandler_TopupInfo(t *testing.T) {
	tests := []struct {
		name          string
		hasEpay       bool
		hasStripe     bool
		hasCreem      bool
		expectMethods int
	}{
		{"no_providers", false, false, false, 0},
		{"all_providers", true, true, true, 4}, // epay adds 2 (alipay + wxpay)
		{"only_stripe", false, true, false, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// nil providers = disabled
			h := NewWalletHandler(makeWalletService(), payment.NewRegistry())
			// We can't easily construct real providers, but nil check is the test
			r := testRouter()
			r.GET("/api/v1/wallet/topup/info", withAccountID(1), h.TopupInfo)

			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/wallet/topup/info", nil)
			r.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
			}
			var resp map[string]interface{}
			json.Unmarshal(w.Body.Bytes(), &resp)
			methods, _ := resp["payment_methods"].([]interface{})
			if tt.name == "no_providers" && len(methods) != 0 {
				t.Errorf("expected 0 methods with nil providers, got %d", len(methods))
			}
		})
	}
}

func TestWalletHandler_CreateTopup_Validation(t *testing.T) {
	h := NewWalletHandler(makeWalletService(), payment.NewRegistry())
	r := testRouter()
	r.POST("/api/v1/wallet/topup", withAccountID(1), h.CreateTopup)

	tests := []struct {
		name   string
		body   map[string]interface{}
		status int
		errMsg string
	}{
		{
			"missing_amount",
			map[string]interface{}{"payment_method": "stripe"},
			http.StatusBadRequest,
			"",
		},
		{
			"missing_method",
			map[string]interface{}{"amount_cny": 50.0},
			http.StatusBadRequest,
			"",
		},
		{
			"amount_below_min",
			map[string]interface{}{"amount_cny": 0.5, "payment_method": "stripe"},
			http.StatusBadRequest,
			"Minimum topup",
		},
		{
			"amount_above_max",
			map[string]interface{}{"amount_cny": 200000.0, "payment_method": "stripe"},
			http.StatusBadRequest,
			"Maximum topup",
		},
		{
			"invalid_payment_method",
			map[string]interface{}{"amount_cny": 50.0, "payment_method": "bitcoin"},
			http.StatusBadRequest,
			"Unsupported",
		},
		{
			"provider_disabled",
			map[string]interface{}{"amount_cny": 50.0, "payment_method": "stripe"},
			// stripe not registered in empty registry → HasMethod returns false → 400
			http.StatusBadRequest,
			"Unsupported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.body)
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/wallet/topup", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			if w.Code != tt.status {
				t.Errorf("status = %d, want %d, body: %s", w.Code, tt.status, w.Body.String())
			}
			if tt.errMsg != "" {
				var resp map[string]interface{}
				json.Unmarshal(w.Body.Bytes(), &resp)
				// Check "message" field (new unified format) or "error" field (legacy).
				msg, _ := resp["message"].(string)
				if msg == "" {
					msg, _ = resp["error"].(string)
				}
				if !containsStr(msg, tt.errMsg) {
					t.Errorf("message = %q, want containing %q (body: %s)", msg, tt.errMsg, w.Body.String())
				}
			}
		})
	}
}

func TestWalletHandler_AdminAdjustWallet(t *testing.T) {
	h := NewWalletHandler(makeWalletService(), payment.NewRegistry())

	tests := []struct {
		name   string
		id     string
		body   map[string]interface{}
		status int
	}{
		{
			"valid_credit",
			"1",
			map[string]interface{}{"amount": 100.0, "description": "bonus"},
			http.StatusOK,
		},
		{
			"invalid_id",
			"abc",
			map[string]interface{}{"amount": 100.0, "description": "bonus"},
			http.StatusBadRequest,
		},
		{
			"missing_description",
			"1",
			map[string]interface{}{"amount": 100.0},
			http.StatusBadRequest,
		},
		{
			"missing_amount",
			"1",
			map[string]interface{}{"description": "test"},
			http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := testRouter()
			r.POST("/admin/v1/accounts/:id/wallet/adjust", h.AdminAdjustWallet)
			body, _ := json.Marshal(tt.body)
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/admin/v1/accounts/"+tt.id+"/wallet/adjust", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			if w.Code != tt.status {
				t.Errorf("status = %d, want %d, body: %s", w.Code, tt.status, w.Body.String())
			}
		})
	}
}

func TestWalletHandler_ListOrders(t *testing.T) {
	h := NewWalletHandler(makeWalletService(), payment.NewRegistry())
	r := testRouter()
	r.GET("/api/v1/wallet/orders", withAccountID(1), h.ListOrders)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/wallet/orders", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestWalletHandler_GetOrder_NotFound(t *testing.T) {
	h := NewWalletHandler(makeWalletService(), payment.NewRegistry())
	r := testRouter()
	r.GET("/api/v1/wallet/orders/:order_no", withAccountID(1), h.GetOrder)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/wallet/orders/NONEXISTENT", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// containsStr checks if s contains substr.
func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || indexOf(s, substr) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// ---------- resolveCheckout (wallet topup provider routing) ----------

// TestWalletHandler_CreateTopup_EpayAlipay_ProviderNil verifies 400 when epay is nil.
func TestWalletHandler_CreateTopup_EpayAlipay_ProviderNil(t *testing.T) {
	h := NewWalletHandler(makeWalletService(), payment.NewRegistry())
	r := testRouter()
	r.POST("/api/v1/wallet/topup", withAccountID(1), h.CreateTopup)

	body, _ := json.Marshal(map[string]interface{}{
		"amount_cny":     10.0,
		"payment_method": "epay_alipay",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/wallet/topup", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (epay provider not configured)", w.Code)
	}
}

// TestWalletHandler_CreateTopup_EpayWxpay_ProviderNil verifies 400 for epay_wxpay with nil provider.
func TestWalletHandler_CreateTopup_EpayWxpay_ProviderNil(t *testing.T) {
	h := NewWalletHandler(makeWalletService(), payment.NewRegistry())
	r := testRouter()
	r.POST("/api/v1/wallet/topup", withAccountID(1), h.CreateTopup)

	body, _ := json.Marshal(map[string]interface{}{
		"amount_cny":     10.0,
		"payment_method": "epay_wxpay",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/wallet/topup", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (epay_wxpay provider not configured)", w.Code)
	}
}

// TestWalletHandler_CreateTopup_Creem_ProviderNil verifies 400 for creem with nil provider.
func TestWalletHandler_CreateTopup_Creem_ProviderNil(t *testing.T) {
	h := NewWalletHandler(makeWalletService(), payment.NewRegistry())
	r := testRouter()
	r.POST("/api/v1/wallet/topup", withAccountID(1), h.CreateTopup)

	body, _ := json.Marshal(map[string]interface{}{
		"amount_cny":     10.0,
		"payment_method": "creem",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/wallet/topup", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (creem provider not configured)", w.Code)
	}
}

func TestWalletHandler_GetWallet_Error(t *testing.T) {
	store := &errWalletH{}
	svc := app.NewWalletService(store, makeVIPService())
	h := NewWalletHandler(svc, payment.NewRegistry())
	r := testRouter()
	r.GET("/api/v1/wallet", withAccountID(1), h.GetWallet)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/wallet", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500; body: %s", w.Code, w.Body.String())
	}
}

func TestWalletHandler_ListOrders_Error(t *testing.T) {
	store := &errWalletH{}
	svc := app.NewWalletService(store, makeVIPService())
	h := NewWalletHandler(svc, payment.NewRegistry())
	r := testRouter()
	r.GET("/api/v1/wallet/orders", withAccountID(1), h.ListOrders)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/wallet/orders", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500; body: %s", w.Code, w.Body.String())
	}
}

// TestWalletHandler_AdminAdjustWallet_NegativeAmount verifies 400 when debit fails due to insufficient balance.
func TestWalletHandler_AdminAdjustWallet_NegativeAmount(t *testing.T) {
	h := NewWalletHandler(makeWalletService(), payment.NewRegistry())
	r := testRouter()
	r.POST("/admin/v1/accounts/:id/wallet/adjust", h.AdminAdjustWallet)

	// amount=-50 on a zero-balance wallet → Debit fails (insufficient balance) → 402 Payment Required.
	body, _ := json.Marshal(map[string]interface{}{"amount": -50.0, "description": "refund"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/v1/accounts/1/wallet/adjust", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusPaymentRequired {
		t.Errorf("status = %d, want 402; body: %s", w.Code, w.Body.String())
	}
}

// TestWalletHandler_AdminAdjustWallet_GetWalletError verifies 500 when GetWallet fails after credit.
func TestWalletHandler_AdminAdjustWallet_GetWalletError(t *testing.T) {
	store := &errGetWalletH{*newMockWalletStore()}
	svc := app.NewWalletService(store, makeVIPService())
	h := NewWalletHandler(svc, payment.NewRegistry())
	r := testRouter()
	r.POST("/admin/v1/accounts/:id/wallet/adjust", h.AdminAdjustWallet)

	// Credit (amount=100) succeeds, but GetByAccountID fails → 500
	body, _ := json.Marshal(map[string]interface{}{"amount": 100.0, "description": "bonus"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/v1/accounts/1/wallet/adjust", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500; body: %s", w.Code, w.Body.String())
	}
}

// TestWalletHandler_TopupInfo_AllProviders verifies payment_methods includes epay, stripe, and creem entries.
func TestWalletHandler_TopupInfo_AllProviders(t *testing.T) {
	epayProvider, err := payment.NewEpayProvider("12345", "testkey12345678", "https://pay.example.com", "https://notify.example.com")
	if err != nil {
		t.Fatalf("NewEpayProvider: %v", err)
	}
	stripeProvider := payment.NewStripeProvider("sk_test_fake", "whsec_fake", 7.1)
	creemProvider, err := payment.NewCreemProvider("creem_key", "creem_secret")
	if err != nil {
		t.Fatalf("NewCreemProvider: %v", err)
	}

	reg := payment.NewRegistry()
	reg.Register("epay", epayProvider,
		payment.MethodInfo{ID: "epay_alipay", Name: "支付宝", Provider: "epay", Type: "qr"},
		payment.MethodInfo{ID: "epay_wechat", Name: "微信支付", Provider: "epay", Type: "qr"},
	)
	reg.Register("stripe", stripeProvider,
		payment.MethodInfo{ID: "stripe", Name: "Stripe", Provider: "stripe", Type: "redirect"})
	reg.Register("creem", creemProvider,
		payment.MethodInfo{ID: "creem", Name: "Creem", Provider: "creem", Type: "redirect"})

	h := NewWalletHandler(makeWalletService(), reg)
	r := testRouter()
	r.GET("/api/v1/wallet/topup/info", withAccountID(1), h.TopupInfo)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/wallet/topup/info", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	methods, _ := resp["payment_methods"].([]interface{})
	// epay adds 2 (alipay + wxpay) + 1 stripe + 1 creem = 4
	if len(methods) != 4 {
		t.Errorf("expected 4 payment methods with all providers, got %d", len(methods))
	}
}
