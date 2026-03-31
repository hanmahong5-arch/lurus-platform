package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ── GetWallet ─────────────────────────────────────────────────────────────

func TestWalletHandler_GetWallet_NoAuth(t *testing.T) {
	h := NewWalletHandler(makeWalletService(), nil, nil, nil)
	r := testRouter()
	r.GET("/api/v1/wallet", h.GetWallet) // no withAccountID

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/wallet", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

// ── ListTransactions ──────────────────────────────────────────────────────

func TestWalletHandler_ListTransactions_Success(t *testing.T) {
	h := NewWalletHandler(makeWalletService(), nil, nil, nil)
	r := testRouter()
	r.GET("/api/v1/wallet/transactions", withAccountID(1), h.ListTransactions)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/wallet/transactions", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if _, ok := resp["data"]; !ok {
		t.Error("missing 'data' field")
	}
	if _, ok := resp["total"]; !ok {
		t.Error("missing 'total' field")
	}
}

func TestWalletHandler_ListTransactions_NoAuth(t *testing.T) {
	h := NewWalletHandler(makeWalletService(), nil, nil, nil)
	r := testRouter()
	r.GET("/api/v1/wallet/transactions", h.ListTransactions)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/wallet/transactions", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestWalletHandler_ListTransactions_Pagination(t *testing.T) {
	h := NewWalletHandler(makeWalletService(), nil, nil, nil)
	r := testRouter()
	r.GET("/api/v1/wallet/transactions", withAccountID(1), h.ListTransactions)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/wallet/transactions?page=2&page_size=5", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── Redeem ────────────────────────────────────────────────────────────────

func TestWalletHandler_Redeem_InvalidCode(t *testing.T) {
	h := NewWalletHandler(makeWalletService(), nil, nil, nil)
	r := testRouter()
	r.POST("/api/v1/wallet/redeem", withAccountID(1), h.Redeem)

	body, _ := json.Marshal(map[string]string{"code": "NONEXISTENT"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/wallet/redeem", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	errorBody, _ := resp["error"].(map[string]any)
	if errorBody["code"] != "invalid_code" {
		t.Errorf("error code = %v, want invalid_code", errorBody["code"])
	}
}

func TestWalletHandler_Redeem_MissingCode(t *testing.T) {
	h := NewWalletHandler(makeWalletService(), nil, nil, nil)
	r := testRouter()
	r.POST("/api/v1/wallet/redeem", withAccountID(1), h.Redeem)

	body, _ := json.Marshal(map[string]string{})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/wallet/redeem", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (missing required field)", w.Code)
	}
}

func TestWalletHandler_Redeem_NoAuth(t *testing.T) {
	h := NewWalletHandler(makeWalletService(), nil, nil, nil)
	r := testRouter()
	r.POST("/api/v1/wallet/redeem", h.Redeem) // no auth

	body, _ := json.Marshal(map[string]string{"code": "TEST"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/wallet/redeem", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

// ── TopupInfo ─────────────────────────────────────────────────────────────

func TestWalletHandler_TopupInfo_NoProviders(t *testing.T) {
	h := NewWalletHandler(makeWalletService(), nil, nil, nil)
	r := testRouter()
	r.GET("/api/v1/wallet/topup/info", h.TopupInfo)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/wallet/topup/info", nil)
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

// ── CreateTopup ───────────────────────────────────────────────────────────

func TestWalletHandler_CreateTopup_NoAuth(t *testing.T) {
	h := NewWalletHandler(makeWalletService(), nil, nil, nil)
	r := testRouter()
	r.POST("/api/v1/wallet/topup", h.CreateTopup)

	body, _ := json.Marshal(map[string]any{"amount_cny": 10.0, "payment_method": "stripe"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/wallet/topup", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestWalletHandler_CreateTopup_MissingFields(t *testing.T) {
	h := NewWalletHandler(makeWalletService(), nil, nil, nil)
	r := testRouter()
	r.POST("/api/v1/wallet/topup", withAccountID(1), h.CreateTopup)

	body, _ := json.Marshal(map[string]any{})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/wallet/topup", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestWalletHandler_CreateTopup_AmountAboveMax(t *testing.T) {
	h := NewWalletHandler(makeWalletService(), nil, nil, nil)
	r := testRouter()
	r.POST("/api/v1/wallet/topup", withAccountID(1), h.CreateTopup)

	body, _ := json.Marshal(map[string]any{"amount_cny": 200000.0, "payment_method": "stripe"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/wallet/topup", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (exceeds max)", w.Code)
	}
}

func TestWalletHandler_CreateTopup_UnsupportedMethod(t *testing.T) {
	h := NewWalletHandler(makeWalletService(), nil, nil, nil)
	r := testRouter()
	r.POST("/api/v1/wallet/topup", withAccountID(1), h.CreateTopup)

	body, _ := json.Marshal(map[string]any{"amount_cny": 50.0, "payment_method": "bitcoin"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/wallet/topup", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (unsupported method)", w.Code)
	}
}

// ── ListOrders ────────────────────────────────────────────────────────────

func TestWalletHandler_ListOrders_Success(t *testing.T) {
	h := NewWalletHandler(makeWalletService(), nil, nil, nil)
	r := testRouter()
	r.GET("/api/v1/wallet/orders", withAccountID(1), h.ListOrders)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/wallet/orders", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestWalletHandler_ListOrders_NoAuth(t *testing.T) {
	h := NewWalletHandler(makeWalletService(), nil, nil, nil)
	r := testRouter()
	r.GET("/api/v1/wallet/orders", h.ListOrders)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/wallet/orders", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

// ── GetOrder ──────────────────────────────────────────────────────────────

func TestWalletHandler_GetOrder_NotFound_Extra(t *testing.T) {
	h := NewWalletHandler(makeWalletService(), nil, nil, nil)
	r := testRouter()
	r.GET("/api/v1/wallet/orders/:order_no", withAccountID(1), h.GetOrder)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/wallet/orders/NONEXISTENT", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// ── AdminAdjustWallet ─────────────────────────────────────────────────────

func TestWalletHandler_AdminAdjust_Credit(t *testing.T) {
	h := NewWalletHandler(makeWalletService(), nil, nil, nil)
	r := testRouter()
	r.POST("/admin/v1/accounts/:id/wallet/adjust", h.AdminAdjustWallet)

	body, _ := json.Marshal(map[string]any{"amount": 50.0, "description": "test credit"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/v1/accounts/1/wallet/adjust", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestWalletHandler_AdminAdjust_InvalidID(t *testing.T) {
	h := NewWalletHandler(makeWalletService(), nil, nil, nil)
	r := testRouter()
	r.POST("/admin/v1/accounts/:id/wallet/adjust", h.AdminAdjustWallet)

	body, _ := json.Marshal(map[string]any{"amount": 10.0, "description": "test"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/v1/accounts/abc/wallet/adjust", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestWalletHandler_AdminAdjust_MissingDescription(t *testing.T) {
	h := NewWalletHandler(makeWalletService(), nil, nil, nil)
	r := testRouter()
	r.POST("/admin/v1/accounts/:id/wallet/adjust", h.AdminAdjustWallet)

	body, _ := json.Marshal(map[string]any{"amount": 10.0})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/v1/accounts/1/wallet/adjust", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (missing description)", w.Code)
	}
}

func TestWalletHandler_AdminAdjust_DebitInsufficientBalance(t *testing.T) {
	h := NewWalletHandler(makeWalletService(), nil, nil, nil)
	r := testRouter()
	r.POST("/admin/v1/accounts/:id/wallet/adjust", h.AdminAdjustWallet)

	// Account has 0 balance, trying to debit.
	body, _ := json.Marshal(map[string]any{"amount": -100.0, "description": "test debit"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/v1/accounts/1/wallet/adjust", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusPaymentRequired {
		t.Errorf("status = %d, want 402 (insufficient balance)", w.Code)
	}
}

func TestWalletHandler_AdminAdjust_ZeroAmount(t *testing.T) {
	h := NewWalletHandler(makeWalletService(), nil, nil, nil)
	r := testRouter()
	r.POST("/admin/v1/accounts/:id/wallet/adjust", h.AdminAdjustWallet)

	// Zero amount: no credit or debit, just return current wallet state.
	body, _ := json.Marshal(map[string]any{"amount": 0.0, "description": "noop"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/v1/accounts/1/wallet/adjust", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	// amount=0 fails binding validation (required, non-zero)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (zero amount)", w.Code)
	}
}
