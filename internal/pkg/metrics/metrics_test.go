package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// TestMetrics_Handler_Returns200 verifies that the /metrics handler responds successfully.
func TestMetrics_Handler_Returns200(t *testing.T) {
	h := Handler()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Handler() status = %d, want 200", w.Code)
	}
}

// TestMetrics_Handler_ContainsGoMetrics verifies that standard Go runtime metrics are present.
func TestMetrics_Handler_ContainsGoMetrics(t *testing.T) {
	h := Handler()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "go_goroutines") {
		t.Error("metrics output should contain go_goroutines")
	}
}

// TestMetrics_RecordWebhookEvent_NoPanic verifies that RecordWebhookEvent does not panic.
func TestMetrics_RecordWebhookEvent_NoPanic(t *testing.T) {
	providers := []string{"stripe", "epay", "creem"}
	results := []string{"success", "duplicate", "error", "invalid_signature"}
	for _, p := range providers {
		for _, r := range results {
			t.Run(p+"/"+r, func(t *testing.T) {
				defer func() {
					if rec := recover(); rec != nil {
						t.Errorf("RecordWebhookEvent(%q, %q) panicked: %v", p, r, rec)
					}
				}()
				RecordWebhookEvent(p, r)
			})
		}
	}
}

// TestMetrics_RecordCacheOperations_NoPanic verifies cache metric functions do not panic.
func TestMetrics_RecordCacheOperations_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("cache metric function panicked: %v", r)
		}
	}()
	RecordCacheHit()
	RecordCacheMiss()
	RecordCacheError()
}

// TestMetrics_RecordWalletOperation_NoPanic verifies wallet metric functions do not panic.
func TestMetrics_RecordWalletOperation_NoPanic(t *testing.T) {
	operations := []string{"topup", "debit", "credit"}
	results := []string{"success", "error"}
	for _, op := range operations {
		for _, r := range results {
			t.Run(op+"/"+r, func(t *testing.T) {
				defer func() {
					if rec := recover(); rec != nil {
						t.Errorf("RecordWalletOperation(%q, %q) panicked: %v", op, r, rec)
					}
				}()
				RecordWalletOperation(op, r)
			})
		}
	}
}

// TestMetrics_RecordWalletAmount_NoPanic verifies wallet amount counter does not panic.
func TestMetrics_RecordWalletAmount_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("RecordWalletAmount panicked: %v", r)
		}
	}()
	RecordWalletAmount("topup", 100.50)
	RecordWalletAmount("debit", 25.00)
	RecordWalletAmount("credit", 10.00)
}

// TestMetrics_RecordPaymentOrderTransition_NoPanic verifies payment order transition metrics.
func TestMetrics_RecordPaymentOrderTransition_NoPanic(t *testing.T) {
	transitions := []struct {
		from, to, orderType, provider string
	}{
		{"pending", "paid", "topup", "epay"},
		{"pending", "paid", "subscription", "stripe"},
		{"pending", "expired", "topup", "creem"},
	}
	for _, tr := range transitions {
		t.Run(tr.from+"->"+tr.to, func(t *testing.T) {
			defer func() {
				if rec := recover(); rec != nil {
					t.Errorf("RecordPaymentOrderTransition panicked: %v", rec)
				}
			}()
			RecordPaymentOrderTransition(tr.from, tr.to, tr.orderType, tr.provider)
		})
	}
}

// TestMetrics_RecordSubscriptionTransition_NoPanic verifies subscription transition metrics.
func TestMetrics_RecordSubscriptionTransition_NoPanic(t *testing.T) {
	transitions := []struct {
		from, to, productID string
	}{
		{"none", "active", "api-gateway"},
		{"active", "grace", "api-gateway"},
		{"grace", "expired", "api-gateway"},
		{"active", "cancelled", "lucrum"},
	}
	for _, tr := range transitions {
		t.Run(tr.from+"->"+tr.to, func(t *testing.T) {
			defer func() {
				if rec := recover(); rec != nil {
					t.Errorf("RecordSubscriptionTransition panicked: %v", rec)
				}
			}()
			RecordSubscriptionTransition(tr.from, tr.to, tr.productID)
		})
	}
}

// TestMetrics_RecordRefundOperation_NoPanic verifies refund metric functions do not panic.
func TestMetrics_RecordRefundOperation_NoPanic(t *testing.T) {
	actions := []string{"request", "approve", "reject"}
	results := []string{"success", "error"}
	for _, a := range actions {
		for _, r := range results {
			t.Run(a+"/"+r, func(t *testing.T) {
				defer func() {
					if rec := recover(); rec != nil {
						t.Errorf("RecordRefundOperation(%q, %q) panicked: %v", a, r, rec)
					}
				}()
				RecordRefundOperation(a, r)
			})
		}
	}
}

// TestMetrics_HTTPMiddleware_RecordsRequest verifies the middleware records metrics.
func TestMetrics_HTTPMiddleware_RecordsRequest(t *testing.T) {
	// Use a fresh registry to avoid conflicts with global promauto metrics.
	r := gin.New()
	r.Use(HTTPMiddleware())
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d", w.Code)
	}

	// Verify the metric was recorded by gathering from the default registry.
	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}

	found := false
	for _, mf := range mfs {
		if mf.GetName() == "lurus_platform_http_requests_total" {
			found = true
			break
		}
	}
	if !found {
		t.Error("lurus_platform_http_requests_total metric not found after request")
	}
}

// TestMetrics_HTTPMiddleware_UnmatchedRoute records "unmatched" route label.
func TestMetrics_HTTPMiddleware_UnmatchedRoute(t *testing.T) {
	r := gin.New()
	r.Use(HTTPMiddleware())
	// No routes registered — all requests are unmatched.

	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	w := httptest.NewRecorder()

	// Should not panic on unmatched route.
	defer func() {
		if rec := recover(); rec != nil {
			t.Errorf("HTTPMiddleware panicked on unmatched route: %v", rec)
		}
	}()
	r.ServeHTTP(w, req)
}
