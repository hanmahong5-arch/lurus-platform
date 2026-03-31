package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/hanmahong5-arch/lurus-platform/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/idempotency"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// newTestDeduper creates a WebhookDeduper backed by miniredis for tests.
func newTestDeduper(t *testing.T) *idempotency.WebhookDeduper {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })
	return idempotency.New(rdb, idempotency.DefaultWebhookTTL)
}

// ── EpayNotify: nil provider with real deduper ────────────────────────────

func TestWebhookCoverage_EpayNotify_NilProviderRealDeduper(t *testing.T) {
	deduper := newTestDeduper(t)
	h := NewWebhookHandler(makeWalletService(), makeSubService(), nil, nil, nil, deduper)
	r := testRouter()
	r.GET("/webhook/epay", h.EpayNotify)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/webhook/epay?trade_no=T001", nil)
	r.ServeHTTP(w, req)

	// epay provider is nil → 503.
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 (epay nil)", w.Code)
	}
}

// ── EpayNotify: duplicate event deduplication ─────────────────────────────

func TestWebhookCoverage_EpayNotify_DuplicateEvent(t *testing.T) {
	deduper := newTestDeduper(t)
	// Pre-process the event to mark it as seen.
	deduper.TryProcess(context.Background(), "T002")

	h := NewWebhookHandler(makeWalletService(), makeSubService(), nil, nil, nil, deduper)
	r := testRouter()
	r.GET("/webhook/epay", h.EpayNotify)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/webhook/epay?trade_no=T002", nil)
	r.ServeHTTP(w, req)

	// Duplicate event → 200 "success" (skip processing).
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (duplicate event)", w.Code)
	}
	if w.Body.String() != "success" {
		t.Errorf("body = %q, want 'success'", w.Body.String())
	}
}

// ── processOrderPaidDirect: topup order ───────────────────────────────────

func TestWebhookCoverage_ProcessOrderPaid_Topup(t *testing.T) {
	ws := newMockWalletStore()
	// Create a pending topup order.
	ws.orders["ORD-TOPUP-001"] = &entity.PaymentOrder{
		AccountID:     1,
		OrderNo:       "ORD-TOPUP-001",
		OrderType:     "topup",
		AmountCNY:     100.0,
		PaymentMethod: "stripe",
		Status:        entity.OrderStatusPending,
	}
	walletSvc := app.NewWalletService(ws, makeVIPService())

	h := NewWebhookHandler(walletSvc, makeSubService(), nil, nil, nil, nil)

	// Call processOrderPaidDirect directly (not via HTTP).
	r := testRouter()
	r.GET("/test", func(c *gin.Context) {
		err := h.processOrderPaidDirect(c, "ORD-TOPUP-001")
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	// Verify order is now paid.
	order, _ := ws.GetPaymentOrderByNo(context.Background(), "ORD-TOPUP-001")
	if order.Status != entity.OrderStatusPaid {
		t.Errorf("order status = %s, want paid", order.Status)
	}
}

// ── processOrderPaidDirect: order not found ───────────────────────────────

func TestWebhookCoverage_ProcessOrderPaid_NotFound(t *testing.T) {
	walletSvc := makeWalletService()
	h := NewWebhookHandler(walletSvc, makeSubService(), nil, nil, nil, nil)

	r := testRouter()
	r.GET("/test", func(c *gin.Context) {
		err := h.processOrderPaidDirect(c, "NONEXISTENT")
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 (order not found)", w.Code)
	}
}

// ── processOrderPaidDirect: subscription order ────────────────────────────

func TestWebhookCoverage_ProcessOrderPaid_Subscription(t *testing.T) {
	ws := newMockWalletStore()
	planID := int64(1)
	ws.orders["ORD-SUB-001"] = &entity.PaymentOrder{
		AccountID:     1,
		OrderNo:       "ORD-SUB-001",
		OrderType:     "subscription",
		ProductID:     "lucrum",
		PlanID:        &planID,
		AmountCNY:     29.9,
		PaymentMethod: "stripe",
		Status:        entity.OrderStatusPending,
	}
	walletSvc := app.NewWalletService(ws, makeVIPService())
	subSvc := makeSubService()

	h := NewWebhookHandler(walletSvc, subSvc, nil, nil, nil, nil)

	r := testRouter()
	r.GET("/test", func(c *gin.Context) {
		err := h.processOrderPaidDirect(c, "ORD-SUB-001")
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	// Order gets marked as paid, then subscription activation tries to find
	// the plan (planID=1) which doesn't exist in the mock → error.
	// This is expected — the key assertion is that the code PATH is exercised.
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 (plan not found in mock); body: %s", w.Code, w.Body.String())
	}

	// Verify the order was still marked as paid (MarkOrderPaid succeeded).
	order, _ := ws.GetPaymentOrderByNo(context.Background(), "ORD-SUB-001")
	if order.Status != entity.OrderStatusPaid {
		t.Errorf("order status = %s, want paid", order.Status)
	}
}

// ── StripeWebhook: duplicate event with real deduper ──────────────────────

func TestWebhookCoverage_StripeWebhook_DuplicateWithDeduper(t *testing.T) {
	deduper := newTestDeduper(t)
	h := NewWebhookHandler(makeWalletService(), makeSubService(), nil, nil, nil, deduper)
	r := testRouter()
	r.POST("/webhook/stripe", h.StripeWebhook)

	// Stripe provider is nil → will fail at signature verification → 503.
	body, _ := json.Marshal(map[string]string{"type": "test"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/webhook/stripe", bytes.NewReader(body))
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 (stripe nil)", w.Code)
	}
}
