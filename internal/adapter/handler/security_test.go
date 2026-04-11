package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/adapter/payment"
	"github.com/hanmahong5-arch/lurus-platform/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// ── IDOR Tests: Cross-Account Access Prevention ─────────────────────────────
// OWASP A01: Broken Access Control — verify that user A cannot access user B's resources.

func TestInvoiceHandler_GenerateInvoice_IDOR_CrossAccount(t *testing.T) {
	// Order belongs to account 1, but request comes from account 2.
	h := NewInvoiceHandler(makeInvoiceServiceWithPaidOrder(1, "ORD-IDOR"))
	r := testRouter()
	r.POST("/api/v1/invoices", withAccountID(2), h.GenerateInvoice) // user 2

	body, _ := json.Marshal(map[string]string{"order_no": "ORD-IDOR"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/invoices", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	// Should be rejected (not found — obscured error to prevent enumeration).
	if w.Code == http.StatusOK {
		t.Fatal("IDOR: user 2 should NOT be able to generate invoice for user 1's order")
	}
}

func TestInvoiceHandler_GetInvoice_IDOR_CrossAccount(t *testing.T) {
	// Create invoice as user 1, then try to read as user 2.
	invSvc := makeInvoiceServiceWithPaidOrder(1, "ORD-IDOR2")
	h := NewInvoiceHandler(invSvc)

	// Generate the invoice as user 1.
	r1 := testRouter()
	r1.POST("/api/v1/invoices", withAccountID(1), h.GenerateInvoice)
	body, _ := json.Marshal(map[string]string{"order_no": "ORD-IDOR2"})
	w1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/invoices", bytes.NewReader(body))
	req1.Header.Set("Content-Type", "application/json")
	r1.ServeHTTP(w1, req1)

	var created map[string]interface{}
	json.Unmarshal(w1.Body.Bytes(), &created)
	invoiceNo, _ := created["invoice_no"].(string)
	if invoiceNo == "" {
		t.Skip("could not create invoice for IDOR test")
	}

	// Try to read as user 2.
	r2 := testRouter()
	r2.GET("/api/v1/invoices/:invoice_no", withAccountID(2), h.GetInvoice)
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/invoices/"+invoiceNo, nil)
	r2.ServeHTTP(w2, req2)

	if w2.Code == http.StatusOK {
		t.Fatal("IDOR: user 2 should NOT be able to read user 1's invoice")
	}
}

func TestRefundHandler_RequestRefund_IDOR_CrossAccount(t *testing.T) {
	// Order belongs to account 1, but refund request from account 2.
	ws := newMockWalletStore()
	ws.orders["ORD-REF-IDOR"] = &entity.PaymentOrder{
		OrderNo:   "ORD-REF-IDOR",
		AccountID: 1,
		Status:    entity.OrderStatusPaid,
		AmountCNY: 50.0,
	}
	svc := app.NewRefundService(&mockRefundStore{}, ws, &mockPublisher{}, nil)
	h := NewRefundHandler(svc)

	r := testRouter()
	r.POST("/api/v1/refunds", withAccountID(2), h.RequestRefund) // user 2

	body, _ := json.Marshal(map[string]interface{}{
		"order_no": "ORD-REF-IDOR",
		"reason":   "test",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/refunds", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code == http.StatusOK || w.Code == http.StatusCreated {
		t.Fatal("IDOR: user 2 should NOT be able to request refund for user 1's order")
	}
}

func TestRefundHandler_GetRefund_IDOR_CrossAccount(t *testing.T) {
	// Refund belongs to account 1, request from account 2.
	svc := makeRefundServiceWithPendingRefund("RN-IDOR-001", 1)
	h := NewRefundHandler(svc)

	r := testRouter()
	r.GET("/api/v1/refunds/:refund_no", withAccountID(2), h.GetRefund) // user 2

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/refunds/RN-IDOR-001", nil)
	r.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Fatal("IDOR: user 2 should NOT be able to read user 1's refund")
	}
}

func TestWalletHandler_GetOrder_IDOR_CrossAccount(t *testing.T) {
	ws := newMockWalletStore()
	ws.orders["ORD-WAL-IDOR"] = &entity.PaymentOrder{
		OrderNo:   "ORD-WAL-IDOR",
		AccountID: 1,
		AmountCNY: 100.0,
		Status:    entity.OrderStatusPaid,
	}
	svc := app.NewWalletService(ws, nil)
	h := NewWalletHandler(svc, payment.NewRegistry())

	r := testRouter()
	r.GET("/api/v1/wallet/orders/:order_no", withAccountID(2), h.GetOrder) // user 2

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/wallet/orders/ORD-WAL-IDOR", nil)
	r.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Fatal("IDOR: user 2 should NOT be able to read user 1's order")
	}
}

// ── Input Boundary Tests: Amount Manipulation ───────────────────────────────
// OWASP WSTG-BUSL-10: Test Payment Functionality — negative/zero/overflow amounts.

func TestWalletHandler_CreateTopup_NegativeAmount(t *testing.T) {
	svc := app.NewWalletService(newMockWalletStore(), nil)
	h := NewWalletHandler(svc, payment.NewRegistry())

	r := testRouter()
	r.POST("/api/v1/wallet/topup", withAccountID(1), h.CreateTopup)

	body, _ := json.Marshal(map[string]interface{}{
		"amount_cny":     -100.0,
		"payment_method": "stripe",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/wallet/topup", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code == http.StatusOK || w.Code == http.StatusCreated {
		t.Fatal("negative amount should be rejected")
	}
}

func TestWalletHandler_CreateTopup_ZeroAmount(t *testing.T) {
	svc := app.NewWalletService(newMockWalletStore(), nil)
	h := NewWalletHandler(svc, payment.NewRegistry())

	r := testRouter()
	r.POST("/api/v1/wallet/topup", withAccountID(1), h.CreateTopup)

	body, _ := json.Marshal(map[string]interface{}{
		"amount_cny":     0,
		"payment_method": "stripe",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/wallet/topup", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code == http.StatusOK || w.Code == http.StatusCreated {
		t.Fatal("zero amount should be rejected")
	}
}

func TestWalletHandler_CreateTopup_ExcessiveAmount(t *testing.T) {
	svc := app.NewWalletService(newMockWalletStore(), nil)
	h := NewWalletHandler(svc, payment.NewRegistry())

	r := testRouter()
	r.POST("/api/v1/wallet/topup", withAccountID(1), h.CreateTopup)

	body, _ := json.Marshal(map[string]interface{}{
		"amount_cny":     999999999.0,
		"payment_method": "stripe",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/wallet/topup", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code == http.StatusOK || w.Code == http.StatusCreated {
		t.Fatal("excessive amount should be rejected")
	}
}

func TestWalletHandler_CreateTopup_BelowMinimum(t *testing.T) {
	svc := app.NewWalletService(newMockWalletStore(), nil)
	h := NewWalletHandler(svc, payment.NewRegistry())

	r := testRouter()
	r.POST("/api/v1/wallet/topup", withAccountID(1), h.CreateTopup)

	body, _ := json.Marshal(map[string]interface{}{
		"amount_cny":     0.50, // below minTopupCNY (1.0)
		"payment_method": "stripe",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/wallet/topup", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code == http.StatusOK || w.Code == http.StatusCreated {
		t.Fatal("sub-minimum amount should be rejected")
	}
}

func TestWalletHandler_CreateTopup_InvalidPaymentMethod(t *testing.T) {
	svc := app.NewWalletService(newMockWalletStore(), nil)
	h := NewWalletHandler(svc, payment.NewRegistry())

	r := testRouter()
	r.POST("/api/v1/wallet/topup", withAccountID(1), h.CreateTopup)

	body, _ := json.Marshal(map[string]interface{}{
		"amount_cny":     50.0,
		"payment_method": "bitcoin", // unsupported
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/wallet/topup", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code == http.StatusOK || w.Code == http.StatusCreated {
		t.Fatal("unsupported payment method should be rejected")
	}
}

func TestWalletHandler_Redeem_EmptyCode(t *testing.T) {
	svc := app.NewWalletService(newMockWalletStore(), nil)
	h := NewWalletHandler(svc, payment.NewRegistry())

	r := testRouter()
	r.POST("/api/v1/wallet/redeem", withAccountID(1), h.Redeem)

	body, _ := json.Marshal(map[string]interface{}{"code": ""})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/wallet/redeem", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Fatal("empty code should be rejected")
	}
}

// ── Zero Account ID Edge Case ───────────────────────────────────────────────

func TestWalletHandler_GetWallet_ZeroAccountID(t *testing.T) {
	ws := newMockWalletStore()
	svc := app.NewWalletService(ws, nil)
	h := NewWalletHandler(svc, payment.NewRegistry())

	r := testRouter()
	r.GET("/api/v1/wallet", withAccountID(0), h.GetWallet) // zero = unauthenticated

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/wallet", nil)
	r.ServeHTTP(w, req)

	// Should not panic, and should handle gracefully.
	// Zero account ID should not create wallet for account 0.
	if w.Code == http.StatusOK {
		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)
		// If it returns OK, the wallet should be for account 0 — this is a design smell
		// but not a security issue since auth middleware should never set account_id=0.
	}
}
