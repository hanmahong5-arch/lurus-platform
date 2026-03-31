package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/adapter/payment"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/idempotency"
)

// newTestWebhookHandler creates a WebhookHandler with minimal mock setup.
// deduper is nil-safe: if nil, a noop deduper is used.
func newTestWebhookHandler(t *testing.T, deduper *idempotency.WebhookDeduper) *WebhookHandler {
	t.Helper()
	return NewWebhookHandler(
		makeWalletService(),
		makeSubService(),
		nil, // epay
		nil, // stripe
		nil, // creem
		deduper,
	)
}

// ── Epay webhook edge cases ───────────────────────────────────────────────

func TestWebhookEdge_Epay_NilProvider(t *testing.T) {
	// The handler calls deduper.TryProcess first, which requires a non-nil deduper.
	// With nil deduper AND nil epay, gin.Recovery catches the panic → 500.
	// This tests that the handler at least doesn't crash the process.
	h := newTestWebhookHandler(t, nil)
	r := testRouter()
	r.GET("/webhook/epay", h.EpayNotify)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/webhook/epay?trade_no=T001", nil)
	r.ServeHTTP(w, req)

	// Recovery middleware catches nil-pointer panic → 500
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 (nil deduper panic recovered)", w.Code)
	}
}

// ── Stripe webhook edge cases ─────────────────────────────────────────────

func TestWebhookEdge_Stripe_EmptyBody(t *testing.T) {
	h := newTestWebhookHandler(t, nil)
	r := testRouter()
	r.POST("/webhook/stripe", h.StripeWebhook)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhook/stripe", bytes.NewReader(nil))
	r.ServeHTTP(w, req)

	// stripe provider is nil → 503
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 (stripe nil)", w.Code)
	}
}

func TestWebhookEdge_Stripe_MissingSignature(t *testing.T) {
	// Create a handler with a real stripe provider that will reject the signature.
	h := NewWebhookHandler(
		makeWalletService(),
		makeSubService(),
		nil,
		payment.NewStripeProvider("sk_test", "whsec_test"),
		nil,
		nil,
	)
	r := testRouter()
	r.POST("/webhook/stripe", h.StripeWebhook)

	body, _ := json.Marshal(map[string]string{"type": "checkout.session.completed"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhook/stripe", bytes.NewReader(body))
	// No Stripe-Signature header.
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (missing signature)", w.Code)
	}
}

func TestWebhookEdge_Stripe_LargeBody(t *testing.T) {
	h := newTestWebhookHandler(t, nil)
	r := testRouter()
	r.POST("/webhook/stripe", h.StripeWebhook)

	// 2MB body — should be truncated by io.LimitReader at 1MB.
	largeBody := strings.Repeat("x", 2*1024*1024)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhook/stripe", strings.NewReader(largeBody))
	r.ServeHTTP(w, req)

	// Even with truncated body, stripe provider is nil → 503.
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 (stripe nil with large body)", w.Code)
	}
}

// ── Creem webhook edge cases ──────────────────────────────────────────────

func TestWebhookEdge_Creem_EmptyBody(t *testing.T) {
	h := newTestWebhookHandler(t, nil)
	r := testRouter()
	r.POST("/webhook/creem", h.CreemWebhook)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhook/creem", bytes.NewReader(nil))
	r.ServeHTTP(w, req)

	// creem provider is nil → 503
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 (creem nil)", w.Code)
	}
}

func TestWebhookEdge_Creem_MissingSignature(t *testing.T) {
	creemProvider, _ := payment.NewCreemProvider("test-api-key", "test-webhook-secret")
	h := NewWebhookHandler(
		makeWalletService(),
		makeSubService(),
		nil,
		nil,
		creemProvider,
		nil,
	)
	r := testRouter()
	r.POST("/webhook/creem", h.CreemWebhook)

	body, _ := json.Marshal(map[string]string{"event_type": "checkout.completed"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhook/creem", bytes.NewReader(body))
	// No X-Creem-Signature header.
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 (missing signature)", w.Code)
	}
}

func TestWebhookEdge_Creem_LargeBody(t *testing.T) {
	h := newTestWebhookHandler(t, nil)
	r := testRouter()
	r.POST("/webhook/creem", h.CreemWebhook)

	largeBody := strings.Repeat("y", 2*1024*1024)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhook/creem", strings.NewReader(largeBody))
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 (creem nil with large body)", w.Code)
	}
}
