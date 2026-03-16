package payment

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

func creemSign(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

// --- Constructor ---

func TestNewCreemProvider_Disabled(t *testing.T) {
	p, err := NewCreemProvider("", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p != nil {
		t.Error("expected nil provider when API key empty")
	}
}

func TestNewCreemProvider_MissingWebhookSecret(t *testing.T) {
	_, err := NewCreemProvider("sk_test", "")
	if err == nil {
		t.Fatal("expected error when webhook secret missing")
	}
}

func TestNewCreemProvider_Valid(t *testing.T) {
	p, err := NewCreemProvider("sk_test", "whsec_test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
}

// --- Name ---

func TestCreemProvider_Name(t *testing.T) {
	p, _ := NewCreemProvider("sk_test", "whsec_test")
	if p.Name() != "creem" {
		t.Errorf("Name() = %q, want creem", p.Name())
	}
}

// --- VerifyWebhook ---

func TestCreemProvider_VerifyWebhook_ValidPaymentSuccess(t *testing.T) {
	secret := "test-webhook-secret-123"
	p, _ := NewCreemProvider("sk_test", secret)

	payload, _ := json.Marshal(map[string]string{
		"event_type": "payment.success",
		"order_no":   "LO-20260227-001",
	})
	sig := creemSign(payload, secret)

	orderNo, ok := p.VerifyWebhook(payload, sig)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if orderNo != "LO-20260227-001" {
		t.Errorf("orderNo = %q, want LO-20260227-001", orderNo)
	}
}

func TestCreemProvider_VerifyWebhook_RequestIDFallback(t *testing.T) {
	secret := "test-secret"
	p, _ := NewCreemProvider("sk_test", secret)

	payload, _ := json.Marshal(map[string]string{
		"event_type": "payment.success",
		"request_id": "REQ-001",
	})
	sig := creemSign(payload, secret)

	orderNo, ok := p.VerifyWebhook(payload, sig)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if orderNo != "REQ-001" {
		t.Errorf("orderNo = %q, want REQ-001", orderNo)
	}
}

func TestCreemProvider_VerifyWebhook_IrrelevantEvent(t *testing.T) {
	secret := "test-secret"
	p, _ := NewCreemProvider("sk_test", secret)

	payload, _ := json.Marshal(map[string]string{
		"event_type": "subscription.created",
		"order_no":   "LO-001",
	})
	sig := creemSign(payload, secret)

	orderNo, ok := p.VerifyWebhook(payload, sig)
	if !ok {
		t.Fatal("expected ok=true for valid but irrelevant event")
	}
	if orderNo != "" {
		t.Errorf("orderNo = %q, want empty for irrelevant event", orderNo)
	}
}

func TestCreemProvider_VerifyWebhook_InvalidSignature(t *testing.T) {
	p, _ := NewCreemProvider("sk_test", "real-secret")

	payload, _ := json.Marshal(map[string]string{
		"event_type": "payment.success",
		"order_no":   "LO-001",
	})
	wrongSig := creemSign(payload, "wrong-secret")

	_, ok := p.VerifyWebhook(payload, wrongSig)
	if ok {
		t.Error("expected ok=false for invalid signature")
	}
}

func TestCreemProvider_VerifyWebhook_EmptySecretGuard(t *testing.T) {
	// Bypass constructor validation to test defence-in-depth guard
	p := &CreemProvider{apiKey: "sk_test", webhookSecret: ""}

	payload := []byte(`{"event_type":"payment.success","order_no":"LO-001"}`)
	sig := creemSign(payload, "")

	_, ok := p.VerifyWebhook(payload, sig)
	if ok {
		t.Error("expected ok=false when webhook secret is empty (fail-closed)")
	}
}

func TestCreemProvider_VerifyWebhook_EmptyOrderNoAndRequestID(t *testing.T) {
	secret := "test-secret"
	p, _ := NewCreemProvider("sk_test", secret)

	payload, _ := json.Marshal(map[string]string{
		"event_type": "payment.success",
	})
	sig := creemSign(payload, secret)

	_, ok := p.VerifyWebhook(payload, sig)
	if ok {
		t.Error("expected ok=false when both order_no and request_id are empty")
	}
}

func TestCreemProvider_VerifyWebhook_MalformedJSON(t *testing.T) {
	secret := "test-secret"
	p, _ := NewCreemProvider("sk_test", secret)

	payload := []byte(`{invalid json}`)
	sig := creemSign(payload, secret)

	_, ok := p.VerifyWebhook(payload, sig)
	if ok {
		t.Error("expected ok=false for malformed JSON")
	}
}

// --- CreateCheckout ---

func TestCreemProvider_CreateCheckout_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer sk_test" {
			t.Errorf("auth = %q, want Bearer sk_test", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content-type = %q, want application/json", r.Header.Get("Content-Type"))
		}

		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["request_id"] != "LO-001" {
			t.Errorf("request_id = %v, want LO-001", body["request_id"])
		}
		if body["currency"] != "CNY" {
			t.Errorf("currency = %v, want CNY", body["currency"])
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"id":           "creem_checkout_123",
			"checkout_url": "https://checkout.creem.io/pay/123",
		})
	}))
	defer srv.Close()

	p := &CreemProvider{
		apiKey:        "sk_test",
		webhookSecret: "whsec_test",
		httpClient:    srv.Client(),
	}
	// Override API base by injecting test server URL into the request path.
	// We need to use the test server URL, so we construct the provider manually
	// and make the request URL point to our test server.
	// However, CreateCheckout uses the constant creemAPIBase. To test this properly,
	// we'll create a provider with the httpClient from the test server and
	// verify the mock server response handling.

	// Since CreateCheckout hardcodes creemAPIBase, we test the full flow using
	// a custom approach: override httpClient transport to redirect all requests.
	p.httpClient = &http.Client{
		Transport: &redirectTransport{target: srv.URL + "/checkouts"},
	}

	order := &entity.PaymentOrder{
		OrderNo:   "LO-001",
		AmountCNY: 100.0,
		AccountID: 1,
	}

	payURL, externalID, err := p.CreateCheckout(context.Background(), order, "https://lurus.cn/return")
	if err != nil {
		t.Fatalf("CreateCheckout: %v", err)
	}
	if payURL != "https://checkout.creem.io/pay/123" {
		t.Errorf("payURL = %q", payURL)
	}
	if externalID != "creem_checkout_123" {
		t.Errorf("externalID = %q", externalID)
	}
}

func TestCreemProvider_CreateCheckout_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"invalid amount"}`)
	}))
	defer srv.Close()

	p := &CreemProvider{
		apiKey:        "sk_test",
		webhookSecret: "whsec_test",
		httpClient: &http.Client{
			Transport: &redirectTransport{target: srv.URL + "/checkouts"},
		},
	}

	_, _, err := p.CreateCheckout(context.Background(), &entity.PaymentOrder{
		OrderNo:   "LO-ERR",
		AmountCNY: -1,
		AccountID: 1,
	}, "https://lurus.cn/return")
	if err == nil {
		t.Fatal("expected error for API 400 response")
	}
}

func TestCreemProvider_CreateCheckout_EmptyCheckoutURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"id":           "creem_123",
			"checkout_url": "",
		})
	}))
	defer srv.Close()

	p := &CreemProvider{
		apiKey:        "sk_test",
		webhookSecret: "whsec_test",
		httpClient: &http.Client{
			Transport: &redirectTransport{target: srv.URL + "/checkouts"},
		},
	}

	_, _, err := p.CreateCheckout(context.Background(), &entity.PaymentOrder{
		OrderNo:   "LO-EMPTY",
		AmountCNY: 50,
		AccountID: 1,
	}, "https://lurus.cn/return")
	if err == nil {
		t.Fatal("expected error for empty checkout_url")
	}
}

// redirectTransport redirects all HTTP requests to a target URL (for httptest).
type redirectTransport struct {
	target string
}

func (t *redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	newReq := req.Clone(req.Context())
	u, _ := http.NewRequest(req.Method, t.target, req.Body)
	newReq.URL = u.URL
	newReq.Host = u.URL.Host
	return http.DefaultTransport.RoundTrip(newReq)
}


// failTransport always returns an error, simulating total network failure.
type failTransport struct{}

func (ft *failTransport) RoundTrip(_ *http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("connection refused")
}

// TestCreemProvider_CreateCheckout_HTTP_Error verifies error returned on network failure.
func TestCreemProvider_CreateCheckout_HTTP_Error(t *testing.T) {
	p := &CreemProvider{
		apiKey:        "sk_test",
		webhookSecret: "whsec_test",
		httpClient:    &http.Client{Transport: &failTransport{}},
	}

	order := &entity.PaymentOrder{OrderNo: "LO-NET-ERR", AmountCNY: 10.0, AccountID: 1}
	_, _, err := p.CreateCheckout(context.Background(), order, "https://return.example.com")
	if err == nil {
		t.Error("expected error when HTTP transport fails")
	}
}
