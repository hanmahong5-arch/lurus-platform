package payment

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
	"time"
)

// generateStripeSig creates a valid Stripe webhook signature header value.
// Format: t=<unix>,v1=<hmac_sha256_hex>
// Signed payload: "<unix>.<body>"
func generateStripeSig(payload []byte, secret string, ts int64) string {
	signed := fmt.Sprintf("%d", ts) + "." + string(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signed))
	sig := hex.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("t=%d,v1=%s", ts, sig)
}

// stripeEvent builds a realistic Stripe event JSON payload.
// api_version must match stripe-go v76's expected version (2023-10-16).
func stripeEvent(eventType, clientRefID string) []byte {
	obj := `{"id":"cs_test","object":"checkout.session","payment_status":"paid","status":"complete"}`
	if clientRefID != "" {
		obj = fmt.Sprintf(`{"id":"cs_test","object":"checkout.session","client_reference_id":"%s","payment_status":"paid","status":"complete"}`, clientRefID)
	}
	return []byte(fmt.Sprintf(
		`{"id":"evt_test","object":"event","api_version":"2023-10-16","created":%d,"data":{"object":%s},"livemode":false,"pending_webhooks":1,"request":{"id":"req_test"},"type":"%s"}`,
		time.Now().Unix(), obj, eventType,
	))
}

// --- Constructor ---

func TestNewStripeProvider_Disabled(t *testing.T) {
	p := NewStripeProvider("", "")
	if p != nil {
		t.Error("expected nil provider when secret key empty")
	}
}

func TestNewStripeProvider_Valid(t *testing.T) {
	p := NewStripeProvider("sk_test_123", "whsec_test_123")
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
}

// --- Name ---

func TestStripeProvider_Name(t *testing.T) {
	p := NewStripeProvider("sk_test", "whsec_test")
	if p.Name() != "stripe" {
		t.Errorf("Name() = %q, want stripe", p.Name())
	}
}

// --- VerifyWebhook ---

func TestStripeProvider_VerifyWebhook_EmptySecret(t *testing.T) {
	p := &StripeProvider{secretKey: "sk_test", webhookSecret: ""}

	_, _, ok := p.VerifyWebhook([]byte(`{}`), "t=1,v1=abc")
	if ok {
		t.Error("expected ok=false when webhook secret is empty")
	}
}

func TestStripeProvider_VerifyWebhook_InvalidSignature(t *testing.T) {
	p := NewStripeProvider("sk_test", "whsec_valid")

	payload := stripeEvent("checkout.session.completed", "LO-001")
	wrongSig := generateStripeSig(payload, "whsec_wrong", time.Now().Unix())

	_, _, ok := p.VerifyWebhook(payload, wrongSig)
	if ok {
		t.Error("expected ok=false for invalid signature")
	}
}

func TestStripeProvider_VerifyWebhook_ValidCheckoutCompleted(t *testing.T) {
	secret := "whsec_test_secret"
	p := NewStripeProvider("sk_test", secret)

	payload := stripeEvent("checkout.session.completed", "LO-20260227-001")
	ts := time.Now().Unix()
	sig := generateStripeSig(payload, secret, ts)

	orderNo, _, ok := p.VerifyWebhook(payload, sig)
	if !ok {
		t.Fatal("expected ok=true for valid checkout.session.completed")
	}
	if orderNo != "LO-20260227-001" {
		t.Errorf("orderNo = %q, want LO-20260227-001", orderNo)
	}
}

func TestStripeProvider_VerifyWebhook_IrrelevantEvent(t *testing.T) {
	secret := "whsec_test_secret"
	p := NewStripeProvider("sk_test", secret)

	payload := stripeEvent("payment_intent.created", "")
	ts := time.Now().Unix()
	sig := generateStripeSig(payload, secret, ts)

	orderNo, _, ok := p.VerifyWebhook(payload, sig)
	if !ok {
		t.Fatal("expected ok=true for valid but irrelevant event")
	}
	if orderNo != "" {
		t.Errorf("orderNo = %q, want empty for irrelevant event", orderNo)
	}
}

func TestStripeProvider_VerifyWebhook_MissingClientReferenceID(t *testing.T) {
	secret := "whsec_test_secret"
	p := NewStripeProvider("sk_test", secret)

	payload := stripeEvent("checkout.session.completed", "")
	ts := time.Now().Unix()
	sig := generateStripeSig(payload, secret, ts)

	orderNo, _, ok := p.VerifyWebhook(payload, sig)
	if ok && orderNo != "" {
		t.Errorf("expected empty orderNo when client_reference_id missing, got %q", orderNo)
	}
}

func TestStripeProvider_VerifyWebhook_StaleTimestamp(t *testing.T) {
	secret := "whsec_test_secret"
	p := NewStripeProvider("sk_test", secret)

	payload := stripeEvent("checkout.session.completed", "LO-001")
	staleTS := time.Now().Add(-10 * time.Minute).Unix()
	sig := generateStripeSig(payload, secret, staleTS)

	_, _, ok := p.VerifyWebhook(payload, sig)
	if ok {
		t.Error("expected ok=false for stale timestamp (>5min)")
	}
}
