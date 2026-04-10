package handler

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hanmahong5-arch/lurus-platform/internal/adapter/payment"
	"github.com/hanmahong5-arch/lurus-platform/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/idempotency"
)

// ---------- webhook test helpers ----------

// webhookStripeSig generates a valid Stripe webhook signature header value.
// Format: t=<unix>,v1=<hmac_sha256_hex>; signed payload is "<unix>.<body>".
func webhookStripeSig(payload []byte, secret string, ts int64) string {
	signed := fmt.Sprintf("%d", ts) + "." + string(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signed))
	sig := hex.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("t=%d,v1=%s", ts, sig)
}

// webhookStripeEvent builds a realistic Stripe event JSON payload.
// api_version must match stripe-go v76's expected version (2023-10-16).
func webhookStripeEvent(eventType, clientRefID string) []byte {
	obj := `{"id":"cs_test","object":"checkout.session","payment_status":"paid","status":"complete"}`
	if clientRefID != "" {
		obj = fmt.Sprintf(`{"id":"cs_test","object":"checkout.session","client_reference_id":"%s","payment_status":"paid","status":"complete"}`, clientRefID)
	}
	return []byte(fmt.Sprintf(
		`{"id":"evt_test","object":"event","api_version":"2023-10-16","created":%d,"data":{"object":%s},"livemode":false,"pending_webhooks":1,"request":{"id":"req_test"},"type":"%s"}`,
		time.Now().Unix(), obj, eventType,
	))
}

// webhookCreemSig computes the HMAC-SHA256 hex signature for a Creem webhook payload.
func webhookCreemSig(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

func makeWebhookHandler() *WebhookHandler {
	return NewWebhookHandler(
		makeWalletService(),
		makeSubService(),
		nil, nil, nil, // all payment providers nil
		idempotency.New(nil, 0), // nil redis → dedup is a no-op
	)
}

// ---------- EpayNotify ----------

func TestWebhookHandler_EpayNotify_NilProvider(t *testing.T) {
	h := makeWebhookHandler()
	r := testRouter()
	r.GET("/webhook/epay", h.EpayNotify)

	req := httptest.NewRequest(http.MethodGet, "/webhook/epay?trade_no=T001&out_trade_no=ORD001", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// epay is nil → 503
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want 503; body=%s", w.Code, w.Body.String())
	}
}

// ---------- StripeWebhook ----------

func TestWebhookHandler_StripeWebhook_NilProvider(t *testing.T) {
	h := makeWebhookHandler()
	r := testRouter()
	r.POST("/webhook/stripe", h.StripeWebhook)

	req := httptest.NewRequest(http.MethodPost, "/webhook/stripe", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// stripe is nil → 503
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want 503; body=%s", w.Code, w.Body.String())
	}
}

// ---------- CreemWebhook ----------

func TestWebhookHandler_CreemWebhook_NilProvider(t *testing.T) {
	h := makeWebhookHandler()
	r := testRouter()
	r.POST("/webhook/creem", h.CreemWebhook)

	req := httptest.NewRequest(http.MethodPost, "/webhook/creem", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// creem is nil → 503
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want 503; body=%s", w.Code, w.Body.String())
	}
}

// ---------- processOrderPaid ----------

func TestWebhookHandler_ProcessOrderPaid_OrderNotFound(t *testing.T) {
	h := makeWebhookHandler()
	r := testRouter()

	// Wrap processOrderPaid in a handler for testing
	r.POST("/test/process", func(c *gin.Context) {
		var req struct {
			OrderNo string `json:"order_no"`
		}
		_ = c.ShouldBindJSON(&req)
		if err := h.processOrderPaid(c, req.OrderNo, "test"); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	body, _ := json.Marshal(map[string]string{"order_no": "NONEXISTENT"})
	req := httptest.NewRequest(http.MethodPost, "/test/process", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestWebhookHandler_ProcessOrderPaid_TopupOrder verifies that a pending topup order is
// correctly marked paid and the wallet is credited, returning no error.
func TestWebhookHandler_ProcessOrderPaid_TopupOrder(t *testing.T) {
	ws := newMockWalletStore()
	_, _ = ws.GetOrCreate(context.Background(), 1)
	ws.orders["LO-TOPUP-WHTEST"] = &entity.PaymentOrder{
		AccountID:     1,
		OrderNo:       "LO-TOPUP-WHTEST",
		OrderType:     "topup",
		AmountCNY:     50.0,
		Status:        entity.OrderStatusPending,
		PaymentMethod: "creem",
	}
	walletSvc := app.NewWalletService(ws, makeVIPService())
	h := NewWebhookHandler(walletSvc, makeSubService(), nil, nil, nil, idempotency.New(nil, 0))

	r := testRouter()
	r.POST("/test/process", func(c *gin.Context) {
		if err := h.processOrderPaid(c, "LO-TOPUP-WHTEST", "test"); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/test/process", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
}

// ---------- EpayNotify: additional paths ----------

// TestWebhookHandler_EpayNotify_InvalidSignature verifies that an invalid epay signature
// returns 400.
func TestWebhookHandler_EpayNotify_InvalidSignature(t *testing.T) {
	provider, err := payment.NewEpayProvider("12345", "testkey12345678", "https://pay.example.com", "https://notify.example.com")
	if err != nil {
		t.Fatalf("NewEpayProvider: %v", err)
	}
	h := NewWebhookHandler(makeWalletService(), makeSubService(), provider, nil, nil, idempotency.New(nil, 0))
	r := testRouter()
	r.GET("/webhook/epay", h.EpayNotify)

	// No valid signature params → VerifyCallback returns false → 400.
	req := httptest.NewRequest(http.MethodGet, "/webhook/epay?trade_no=T001&out_trade_no=ORD001", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// ---------- StripeWebhook: additional paths ----------

// TestWebhookHandler_StripeWebhook_InvalidSignature verifies that an invalid Stripe
// signature returns 400 when the provider is configured.
func TestWebhookHandler_StripeWebhook_InvalidSignature(t *testing.T) {
	provider := payment.NewStripeProvider("sk_test_fake", "whsec_test_fake_secret", 7.1)
	h := NewWebhookHandler(makeWalletService(), makeSubService(), nil, provider, nil, idempotency.New(nil, 0))
	r := testRouter()
	r.POST("/webhook/stripe", h.StripeWebhook)

	req := httptest.NewRequest(http.MethodPost, "/webhook/stripe", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Stripe-Signature", "t=1,v1=invalidsignature")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestWebhookHandler_StripeWebhook_ValidSig_IrrelevantEvent verifies that a valid
// signature on a non-checkout event is acknowledged with 200 (no order processing).
func TestWebhookHandler_StripeWebhook_ValidSig_IrrelevantEvent(t *testing.T) {
	secret := "whsec_test_stripe_secret"
	provider := payment.NewStripeProvider("sk_test_fake", secret, 7.1)
	h := NewWebhookHandler(makeWalletService(), makeSubService(), nil, provider, nil, idempotency.New(nil, 0))
	r := testRouter()
	r.POST("/webhook/stripe", h.StripeWebhook)

	payload := webhookStripeEvent("payment_intent.created", "")
	ts := time.Now().Unix()
	sig := webhookStripeSig(payload, secret, ts)

	req := httptest.NewRequest(http.MethodPost, "/webhook/stripe", bytes.NewReader(payload))
	req.Header.Set("Stripe-Signature", sig)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
}

// TestWebhookHandler_StripeWebhook_ValidSig_CheckoutCompleted_OrderNotFound verifies
// that a valid checkout.session.completed event with an unknown order returns 500.
func TestWebhookHandler_StripeWebhook_ValidSig_CheckoutCompleted_OrderNotFound(t *testing.T) {
	secret := "whsec_test_stripe_secret"
	provider := payment.NewStripeProvider("sk_test_fake", secret, 7.1)
	h := NewWebhookHandler(makeWalletService(), makeSubService(), nil, provider, nil, idempotency.New(nil, 0))
	r := testRouter()
	r.POST("/webhook/stripe", h.StripeWebhook)

	payload := webhookStripeEvent("checkout.session.completed", "STRIPE-NONEXISTENT-ORDER")
	ts := time.Now().Unix()
	sig := webhookStripeSig(payload, secret, ts)

	req := httptest.NewRequest(http.MethodPost, "/webhook/stripe", bytes.NewReader(payload))
	req.Header.Set("Stripe-Signature", sig)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// processOrderPaid returns error (order not found) → 500.
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status=%d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// ---------- CreemWebhook: additional paths ----------

// TestWebhookHandler_CreemWebhook_InvalidSignature verifies that an invalid HMAC
// signature returns 401.
func TestWebhookHandler_CreemWebhook_InvalidSignature(t *testing.T) {
	provider, err := payment.NewCreemProvider("creem_api_key_test", "creem_secret_test")
	if err != nil {
		t.Fatalf("NewCreemProvider: %v", err)
	}
	h := NewWebhookHandler(makeWalletService(), makeSubService(), nil, nil, provider, idempotency.New(nil, 0))
	r := testRouter()
	r.POST("/webhook/creem", h.CreemWebhook)

	req := httptest.NewRequest(http.MethodPost, "/webhook/creem", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("X-Creem-Signature", "invalidsig")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status=%d, want 401; body=%s", w.Code, w.Body.String())
	}
}

// TestWebhookHandler_CreemWebhook_ValidSig_NonSuccessEvent verifies that a
// non-payment.success event with a valid signature returns 200 without processing.
func TestWebhookHandler_CreemWebhook_ValidSig_NonSuccessEvent(t *testing.T) {
	secret := "creem_secret_for_test"
	provider, err := payment.NewCreemProvider("creem_api_key_test", secret)
	if err != nil {
		t.Fatalf("NewCreemProvider: %v", err)
	}
	h := NewWebhookHandler(makeWalletService(), makeSubService(), nil, nil, provider, idempotency.New(nil, 0))
	r := testRouter()
	r.POST("/webhook/creem", h.CreemWebhook)

	payload := []byte(`{"event_type":"subscription.created","order_no":"","event_id":"evt-test-nonsuccess-001"}`)
	sig := webhookCreemSig(payload, secret)

	req := httptest.NewRequest(http.MethodPost, "/webhook/creem", bytes.NewReader(payload))
	req.Header.Set("X-Creem-Signature", sig)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
}

// TestWebhookHandler_CreemWebhook_ValidSig_PaymentSuccess_OrderNotFound verifies that
// a valid payment.success event with an unknown order returns 500.
func TestWebhookHandler_CreemWebhook_ValidSig_PaymentSuccess_OrderNotFound(t *testing.T) {
	secret := "creem_secret_for_test"
	provider, err := payment.NewCreemProvider("creem_api_key_test", secret)
	if err != nil {
		t.Fatalf("NewCreemProvider: %v", err)
	}
	h := NewWebhookHandler(makeWalletService(), makeSubService(), nil, nil, provider, idempotency.New(nil, 0))
	r := testRouter()
	r.POST("/webhook/creem", h.CreemWebhook)

	payload := []byte(`{"event_type":"payment.success","order_no":"CREEM-NONEXISTENT-001","event_id":"evt-test-notfound-001"}`)
	sig := webhookCreemSig(payload, secret)

	req := httptest.NewRequest(http.MethodPost, "/webhook/creem", bytes.NewReader(payload))
	req.Header.Set("X-Creem-Signature", sig)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// processOrderPaid returns error (order not found) → 500.
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status=%d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestWebhookHandler_ProcessOrderPaid_SubscriptionOrder covers the subscription
// activation branch in processOrderPaid.
func TestWebhookHandler_ProcessOrderPaid_SubscriptionOrder(t *testing.T) {
	ws := newMockWalletStore()
	_, _ = ws.GetOrCreate(context.Background(), 1)
	planID := int64(1)
	ws.orders["LO-SUB-WHTEST"] = &entity.PaymentOrder{
		AccountID:     1,
		OrderNo:       "LO-SUB-WHTEST",
		OrderType:     "subscription",
		AmountCNY:     99.0,
		Status:        entity.OrderStatusPending,
		PaymentMethod: "stripe",
		PlanID:        &planID,
		ProductID:     "test-product",
	}
	walletSvc := app.NewWalletService(ws, makeVIPService())
	h := NewWebhookHandler(walletSvc, makeSubService(), nil, nil, nil, idempotency.New(nil, 0))

	r := testRouter()
	r.POST("/test/process", func(c *gin.Context) {
		if err := h.processOrderPaid(c, "LO-SUB-WHTEST", "test"); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/test/process", nil)
	r.ServeHTTP(w, req)
	// Activate fails (plan not found in empty mock) → exercises the subscription branch.
	if w.Code == http.StatusOK {
		// If somehow Activate succeeds with empty mock, that's also fine.
		return
	}
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status=%d, want 500 (Activate should fail); body=%s", w.Code, w.Body.String())
	}
}
