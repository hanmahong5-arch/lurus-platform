package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// errInvoiceStore returns errors from list methods to exercise error paths.
type errInvoiceStore struct {
	mockInvoiceStore
}

func (s *errInvoiceStore) ListByAccount(_ context.Context, _ int64, _, _ int) ([]entity.Invoice, int64, error) {
	return nil, 0, fmt.Errorf("db unavailable")
}

func (s *errInvoiceStore) AdminList(_ context.Context, _ int64, _, _ int) ([]entity.Invoice, int64, error) {
	return nil, 0, fmt.Errorf("db unavailable")
}

func makeInvoiceServiceWithPaidOrder(accountID int64, orderNo string) *app.InvoiceService {
	ws := newMockWalletStore()
	ws.orders[orderNo] = &entity.PaymentOrder{
		OrderNo:       orderNo,
		AccountID:     accountID,
		Status:        entity.OrderStatusPaid,
		AmountCNY:     99.0,
		ProductID:     "lurus_api",
		PaymentMethod: "wallet",
	}
	return app.NewInvoiceService(&mockInvoiceStore{}, ws)
}

func TestInvoiceHandler_GenerateInvoice_Success(t *testing.T) {
	h := NewInvoiceHandler(makeInvoiceServiceWithPaidOrder(1, "ORD-001"))
	r := testRouter()
	r.POST("/api/v1/invoices", withAccountID(1), h.GenerateInvoice)

	body, _ := json.Marshal(map[string]string{"order_no": "ORD-001"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/invoices", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["invoice_no"] == nil {
		t.Error("response missing invoice_no")
	}
}

func TestInvoiceHandler_GenerateInvoice_OrderNotFound(t *testing.T) {
	h := NewInvoiceHandler(makeInvoiceService())
	r := testRouter()
	r.POST("/api/v1/invoices", withAccountID(1), h.GenerateInvoice)

	body, _ := json.Marshal(map[string]string{"order_no": "ORD-NONEXISTENT"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/invoices", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestInvoiceHandler_AdminList_InvalidAccountID(t *testing.T) {
	h := NewInvoiceHandler(makeInvoiceService())
	r := testRouter()
	r.GET("/admin/v1/invoices", h.AdminList)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/v1/invoices?account_id=abc", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestInvoiceHandler_GenerateInvoice_MissingOrderNo(t *testing.T) {
	h := NewInvoiceHandler(makeInvoiceService())
	r := testRouter()
	r.POST("/api/v1/invoices", withAccountID(1), h.GenerateInvoice)

	body, _ := json.Marshal(map[string]string{})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/invoices", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestInvoiceHandler_ListInvoices(t *testing.T) {
	h := NewInvoiceHandler(makeInvoiceService())
	r := testRouter()
	r.GET("/api/v1/invoices", withAccountID(1), h.ListInvoices)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/invoices", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestInvoiceHandler_GetInvoice_NotFound(t *testing.T) {
	h := NewInvoiceHandler(makeInvoiceService())
	r := testRouter()
	r.GET("/api/v1/invoices/:invoice_no", withAccountID(1), h.GetInvoice)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/invoices/INV-NONE", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestInvoiceHandler_AdminList(t *testing.T) {
	h := NewInvoiceHandler(makeInvoiceService())
	r := testRouter()
	r.GET("/admin/v1/invoices", h.AdminList)

	tests := []struct {
		name  string
		query string
	}{
		{"no_filter", ""},
		{"with_account_filter", "?account_id=1"},
		{"pagination", "?page=2&page_size=5"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/admin/v1/invoices"+tt.query, nil)
			r.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
			}
		})
	}
}

func TestInvoiceHandler_ListInvoices_Error(t *testing.T) {
	svc := app.NewInvoiceService(&errInvoiceStore{}, newMockWalletStore())
	h := NewInvoiceHandler(svc)
	r := testRouter()
	r.GET("/api/v1/invoices", withAccountID(1), h.ListInvoices)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/invoices", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500; body: %s", w.Code, w.Body.String())
	}
}

func TestInvoiceHandler_AdminList_InternalError(t *testing.T) {
	svc := app.NewInvoiceService(&errInvoiceStore{}, newMockWalletStore())
	h := NewInvoiceHandler(svc)
	r := testRouter()
	r.GET("/admin/v1/invoices", h.AdminList)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/v1/invoices", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500; body: %s", w.Code, w.Body.String())
	}
}
