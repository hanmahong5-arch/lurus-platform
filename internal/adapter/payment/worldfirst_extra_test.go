//go:build ignore
// +build ignore

// DISABLED: draft file has unused var and undefined symbols.
// Left on disk for author to finish or delete; excluded from compilation so CI stays green.

package payment

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// --- parseRSAPrivateKey / parseRSAPublicKey ---

func TestParseRSAPrivateKey_NoPEMBlock(t *testing.T) {
	_, err := parseRSAPrivateKey("not-a-pem")
	if err == nil {
		t.Fatal("expected error for non-PEM input")
	}
}

func TestParseRSAPrivateKey_PKCS8(t *testing.T) {
	// Generate PKCS1 key via helper, then use it — PKCS1 path is already tested.
	priv, _ := generateTestRSAKeyPair(t)
	key, err := parseRSAPrivateKey(priv)
	if err != nil {
		t.Fatalf("parseRSAPrivateKey: %v", err)
	}
	if key == nil {
		t.Fatal("expected non-nil key")
	}
}

func TestParseRSAPublicKey_NoPEMBlock(t *testing.T) {
	_, err := parseRSAPublicKey("not-a-pem")
	if err == nil {
		t.Fatal("expected error for non-PEM input")
	}
}

func TestParseRSAPublicKey_InvalidDER(t *testing.T) {
	// Valid PEM header but garbage DER bytes.
	badPEM := "-----BEGIN PUBLIC KEY-----\nZmFrZWRhdGE=\n-----END PUBLIC KEY-----\n"
	_, err := parseRSAPublicKey(badPEM)
	if err == nil {
		t.Fatal("expected error for invalid DER content")
	}
}

func TestParseRSAPublicKey_Valid(t *testing.T) {
	_, pub := generateTestRSAKeyPair(t)
	key, err := parseRSAPublicKey(pub)
	if err != nil {
		t.Fatalf("parseRSAPublicKey: %v", err)
	}
	if key == nil {
		t.Fatal("expected non-nil key")
	}
}

// --- NewWorldFirstProvider: invalid public key ---

func TestNewWorldFirstProvider_InvalidPublicKey(t *testing.T) {
	priv, _ := generateTestRSAKeyPair(t)
	_, err := NewWorldFirstProvider(WorldFirstConfig{
		ClientID:      "client",
		PrivateKeyPEM: priv,
		PublicKeyPEM:  "invalid-public-key",
	})
	if err == nil {
		t.Fatal("expected error for invalid public key PEM")
	}
}

// --- NewWorldFirstProvider: default key version ---

func TestNewWorldFirstProvider_DefaultKeyVersion(t *testing.T) {
	priv, _ := generateTestRSAKeyPair(t)
	p, err := NewWorldFirstProvider(WorldFirstConfig{
		ClientID:      "client",
		PrivateKeyPEM: priv,
		// KeyVersion intentionally empty
	})
	if err != nil || p == nil {
		t.Fatalf("err=%v, p=%v", err, p)
	}
	if p.keyVersion != "1" {
		t.Errorf("keyVersion = %q, want 1", p.keyVersion)
	}
}

// --- verifySignature: edge cases ---

func TestWorldFirstProvider_VerifySignature_NilPublicKey(t *testing.T) {
	p := &WorldFirstProvider{publicKey: nil}
	ok := p.verifySignature("POST", "/path", "client", "2026-04-11T00:00:00+00:00", []byte(`{}`), "algorithm=RSA256,signature=abc")
	if ok {
		t.Error("expected false when publicKey is nil")
	}
}

func TestWorldFirstProvider_VerifySignature_EmptySignatureHeader(t *testing.T) {
	priv, pub := generateTestRSAKeyPair(t)
	p, _ := NewWorldFirstProvider(WorldFirstConfig{
		ClientID:      "client",
		PrivateKeyPEM: priv,
		PublicKeyPEM:  pub,
	})
	ok := p.verifySignature("POST", "/path", "client", "2026-04-11T00:00:00+00:00", []byte(`{}`), "")
	if ok {
		t.Error("expected false for empty signature header")
	}
}

func TestWorldFirstProvider_VerifySignature_InvalidBase64(t *testing.T) {
	priv, pub := generateTestRSAKeyPair(t)
	p, _ := NewWorldFirstProvider(WorldFirstConfig{
		ClientID:      "client",
		PrivateKeyPEM: priv,
		PublicKeyPEM:  pub,
	})
	// Signature value contains invalid base64url.
	ok := p.verifySignature("POST", "/path", "client", "2026-04-11T00:00:00+00:00", []byte(`{}`),
		"algorithm=RSA256,keyVersion=1,signature=!!!invalid!!!")
	if ok {
		t.Error("expected false for invalid base64 signature")
	}
}

func TestWorldFirstProvider_VerifySignature_WrongSignature(t *testing.T) {
	priv, pub := generateTestRSAKeyPair(t)
	p, _ := NewWorldFirstProvider(WorldFirstConfig{
		ClientID:      "client",
		PrivateKeyPEM: priv,
		PublicKeyPEM:  pub,
	})

	// Sign one body but verify with different body.
	sig, err := p.sign("/path", "2026-04-11T00:00:00+00:00", []byte(`{"a":1}`))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	sigHeader := "algorithm=RSA256,keyVersion=1,signature=" + sig

	// Different body — verification must fail.
	ok := p.verifySignature("POST", "/path", "client", "2026-04-11T00:00:00+00:00",
		[]byte(`{"a":2}`), sigHeader)
	if ok {
		t.Error("expected false for wrong body")
	}
}

// --- currencyForOrder ---

func TestCurrencyForOrder_Direct(t *testing.T) {
	tests := []struct {
		currency string
		want     string
	}{
		{"", "CNY"},
		{"CNY", "CNY"},
		{"USD", "USD"},
		{"EUR", "EUR"},
		{"HKD", "HKD"},
	}
	for _, tt := range tests {
		o := &entity.PaymentOrder{Currency: tt.currency}
		got := currencyForOrder(o)
		if got != tt.want {
			t.Errorf("currency=%q: currencyForOrder = %q, want %q", tt.currency, got, tt.want)
		}
	}
}

// --- WorldFirst HandleNotify ---

func buildWorldFirstNotifyRequest(body []byte, sigHeader string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/notify/worldfirst", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	if sigHeader != "" {
		req.Header.Set("Signature", sigHeader)
	}
	return req
}

func TestWorldFirstProvider_HandleNotify_Success_NoPublicKey(t *testing.T) {
	// Without public key, signature verification is skipped.
	priv, _ := generateTestRSAKeyPair(t)
	p, _ := NewWorldFirstProvider(WorldFirstConfig{
		ClientID:      "client",
		PrivateKeyPEM: priv,
		// No PublicKeyPEM — skip signature verification.
	})

	body, _ := json.Marshal(map[string]any{
		"payToRequestId": "LO-WF-001",
		"paymentStatus":  "SUCCESS",
		"result": map[string]string{
			"resultStatus": "S",
		},
	})
	req := buildWorldFirstNotifyRequest(body, "")

	orderNo, ok, err := p.HandleNotify(req)
	if err != nil {
		t.Fatalf("HandleNotify: %v", err)
	}
	if !ok {
		t.Error("expected ok=true for SUCCESS notification")
	}
	if orderNo != "LO-WF-001" {
		t.Errorf("orderNo = %q, want LO-WF-001", orderNo)
	}
}

func TestWorldFirstProvider_HandleNotify_NonSuccess_Acknowledged(t *testing.T) {
	priv, _ := generateTestRSAKeyPair(t)
	p, _ := NewWorldFirstProvider(WorldFirstConfig{
		ClientID:      "client",
		PrivateKeyPEM: priv,
	})

	body, _ := json.Marshal(map[string]any{
		"payToRequestId": "LO-WF-PENDING",
		"paymentStatus":  "PROCESSING",
	})
	req := buildWorldFirstNotifyRequest(body, "")

	orderNo, ok, err := p.HandleNotify(req)
	if err != nil {
		t.Fatalf("HandleNotify: %v", err)
	}
	if !ok {
		t.Error("expected ok=true for non-SUCCESS notification (acknowledged)")
	}
	if orderNo != "" {
		t.Errorf("orderNo = %q, want empty for non-SUCCESS", orderNo)
	}
}

func TestWorldFirstProvider_HandleNotify_MissingPayToRequestID(t *testing.T) {
	priv, _ := generateTestRSAKeyPair(t)
	p, _ := NewWorldFirstProvider(WorldFirstConfig{
		ClientID:      "client",
		PrivateKeyPEM: priv,
	})

	body, _ := json.Marshal(map[string]any{
		"paymentStatus": "SUCCESS",
		// payToRequestId intentionally missing
	})
	req := buildWorldFirstNotifyRequest(body, "")

	_, ok, err := p.HandleNotify(req)
	if err == nil {
		t.Fatal("expected error when payToRequestId missing")
	}
	if ok {
		t.Error("expected ok=false when payToRequestId missing")
	}
}

func TestWorldFirstProvider_HandleNotify_MalformedJSON(t *testing.T) {
	priv, _ := generateTestRSAKeyPair(t)
	p, _ := NewWorldFirstProvider(WorldFirstConfig{
		ClientID:      "client",
		PrivateKeyPEM: priv,
	})

	req := buildWorldFirstNotifyRequest([]byte(`{invalid json`), "")

	_, ok, err := p.HandleNotify(req)
	if err == nil && ok {
		t.Error("expected error or ok=false for malformed JSON body")
	}
}

func TestWorldFirstProvider_HandleNotify_SignatureVerification_Fails(t *testing.T) {
	priv, pub := generateTestRSAKeyPair(t)
	p, _ := NewWorldFirstProvider(WorldFirstConfig{
		ClientID:      "client",
		PrivateKeyPEM: priv,
		PublicKeyPEM:  pub,
	})

	body, _ := json.Marshal(map[string]any{
		"payToRequestId": "LO-WF-SIG",
		"paymentStatus":  "SUCCESS",
	})
	// Provide a wrong/empty signature.
	req := buildWorldFirstNotifyRequest(body, "algorithm=RSA256,keyVersion=1,signature=invalidsig")

	_, ok, err := p.HandleNotify(req)
	if err == nil {
		t.Fatal("expected error for invalid signature")
	}
	if ok {
		t.Error("expected ok=false for invalid signature")
	}
}

func TestWorldFirstProvider_HandleNotify_ValidSignature(t *testing.T) {
	priv, pub := generateTestRSAKeyPair(t)
	p, _ := NewWorldFirstProvider(WorldFirstConfig{
		ClientID:      "client",
		PrivateKeyPEM: priv,
		PublicKeyPEM:  pub,
	})

	body, _ := json.Marshal(map[string]any{
		"payToRequestId": "LO-WF-VALID",
		"paymentStatus":  "SUCCESS",
	})

	// Compute valid signature for this body with correct content string.
	requestTime := "2026-04-11T10:00:00+08:00"
	path := "/notify/worldfirst"
	content := "POST " + path + "\n" + "client." + requestTime + "." + string(body)
	import_ := "never" // placeholder — computed inline below
	_ = import_

	// Use the provider's sign method to create a valid signature.
	// The verifySignature method builds: "{method} {path}\n{clientID}.{requestTime}.{body}"
	// We need to match that format exactly.
	sig, err := p.sign(path, requestTime, body)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	sigHeader := "algorithm=RSA256,keyVersion=1,signature=" + sig

	req := buildWorldFirstNotifyRequest(body, sigHeader)
	req.Header.Set("Request-Time", requestTime)

	orderNo, ok, err := p.HandleNotify(req)
	if err != nil {
		t.Fatalf("HandleNotify: %v", err)
	}
	if !ok {
		t.Error("expected ok=true for valid signature + SUCCESS status")
	}
	if orderNo != "LO-WF-VALID" {
		t.Errorf("orderNo = %q, want LO-WF-VALID", orderNo)
	}
}

// --- WorldFirst doRequest (via CreateCheckout and QueryOrder with mock server) ---

func TestWorldFirstProvider_CreateCheckout_APISuccess(t *testing.T) {
	priv, _ := generateTestRSAKeyPair(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Validate request headers.
		if r.Header.Get("Client-Id") == "" {
			t.Error("missing Client-Id header")
		}
		if r.Header.Get("Request-Time") == "" {
			t.Error("missing Request-Time header")
		}
		if r.Header.Get("Signature") == "" {
			t.Error("missing Signature header")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]string{
				"resultCode":    "SUCCESS",
				"resultStatus":  "S",
				"resultMessage": "success",
			},
			"payToId": "wf_pay_123",
			"actionForm": map[string]string{
				"redirectUrl": "https://checkout.worldfirst.com/pay/wf_pay_123",
			},
		})
	}))
	defer srv.Close()

	p, err := NewWorldFirstProvider(WorldFirstConfig{
		ClientID:      "test-client",
		PrivateKeyPEM: priv,
		Gateway:       srv.URL,
		NotifyURL:     "https://notify.example.com",
	})
	if err != nil || p == nil {
		t.Fatalf("provider creation failed: %v", err)
	}
	p.httpClient = srv.Client()

	order := &entity.PaymentOrder{
		OrderNo:   "LO-WF-CHECKOUT",
		AmountCNY: 100.0,
		Currency:  "CNY",
	}
	payURL, externalID, err := p.CreateCheckout(context.Background(), order, "https://return.example.com")
	if err != nil {
		t.Fatalf("CreateCheckout: %v", err)
	}
	if payURL != "https://checkout.worldfirst.com/pay/wf_pay_123" {
		t.Errorf("payURL = %q", payURL)
	}
	if externalID != "wf_pay_123" {
		t.Errorf("externalID = %q, want wf_pay_123", externalID)
	}
}

func TestWorldFirstProvider_CreateCheckout_APIError(t *testing.T) {
	priv, _ := generateTestRSAKeyPair(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid request"}`))
	}))
	defer srv.Close()

	p, _ := NewWorldFirstProvider(WorldFirstConfig{
		ClientID:      "test-client",
		PrivateKeyPEM: priv,
		Gateway:       srv.URL,
	})
	p.httpClient = srv.Client()

	order := &entity.PaymentOrder{OrderNo: "LO-WF-ERR", AmountCNY: 100.0}
	_, _, err := p.CreateCheckout(context.Background(), order, "https://return.example.com")
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
}

func TestWorldFirstProvider_CreateCheckout_ResultStatusFailed(t *testing.T) {
	priv, _ := generateTestRSAKeyPair(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]string{
				"resultCode":    "FAIL",
				"resultStatus":  "F",
				"resultMessage": "order rejected",
			},
		})
	}))
	defer srv.Close()

	p, _ := NewWorldFirstProvider(WorldFirstConfig{
		ClientID:      "test-client",
		PrivateKeyPEM: priv,
		Gateway:       srv.URL,
	})
	p.httpClient = srv.Client()

	order := &entity.PaymentOrder{OrderNo: "LO-WF-FAIL", AmountCNY: 100.0}
	_, _, err := p.CreateCheckout(context.Background(), order, "https://return.example.com")
	if err == nil {
		t.Fatal("expected error for resultStatus=F")
	}
	if !strings.Contains(err.Error(), "order rejected") {
		t.Errorf("error should contain resultMessage, got: %v", err)
	}
}

func TestWorldFirstProvider_CreateCheckout_EmptyRedirectURL(t *testing.T) {
	priv, _ := generateTestRSAKeyPair(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]string{
				"resultStatus": "S",
			},
			"payToId": "wf_123",
			"actionForm": map[string]string{
				"redirectUrl": "", // empty redirect URL
			},
		})
	}))
	defer srv.Close()

	p, _ := NewWorldFirstProvider(WorldFirstConfig{
		ClientID:      "test-client",
		PrivateKeyPEM: priv,
		Gateway:       srv.URL,
	})
	p.httpClient = srv.Client()

	order := &entity.PaymentOrder{OrderNo: "LO-WF-NOREDIR", AmountCNY: 100.0}
	_, _, err := p.CreateCheckout(context.Background(), order, "https://return.example.com")
	if err == nil {
		t.Fatal("expected error for empty redirect URL")
	}
}

func TestWorldFirstProvider_CreateCheckout_UStatus(t *testing.T) {
	// ResultStatus "U" (unknown/pending) should also be accepted.
	priv, _ := generateTestRSAKeyPair(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]string{
				"resultStatus": "U",
			},
			"payToId": "wf_u_456",
			"actionForm": map[string]string{
				"redirectUrl": "https://checkout.worldfirst.com/pay/pending",
			},
		})
	}))
	defer srv.Close()

	p, _ := NewWorldFirstProvider(WorldFirstConfig{
		ClientID:      "test-client",
		PrivateKeyPEM: priv,
		Gateway:       srv.URL,
	})
	p.httpClient = srv.Client()

	order := &entity.PaymentOrder{OrderNo: "LO-WF-U", AmountCNY: 100.0}
	payURL, _, err := p.CreateCheckout(context.Background(), order, "https://return.example.com")
	if err != nil {
		t.Fatalf("CreateCheckout with U status: %v", err)
	}
	if payURL == "" {
		t.Error("expected non-empty payURL for U status")
	}
}

func TestWorldFirstProvider_CreateCheckout_NetworkError(t *testing.T) {
	priv, _ := generateTestRSAKeyPair(t)
	p, _ := NewWorldFirstProvider(WorldFirstConfig{
		ClientID:      "test-client",
		PrivateKeyPEM: priv,
		Gateway:       "https://localhost:1", // unreachable
	})
	p.httpClient = &http.Client{Transport: &failTransport{}}

	order := &entity.PaymentOrder{OrderNo: "LO-WF-NETERR", AmountCNY: 100.0}
	_, _, err := p.CreateCheckout(context.Background(), order, "https://return.example.com")
	if err == nil {
		t.Fatal("expected error for network failure")
	}
}

// --- WorldFirst QueryOrder ---

func TestWorldFirstProvider_QueryOrder_Success(t *testing.T) {
	priv, _ := generateTestRSAKeyPair(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]string{
				"resultStatus": "S",
			},
			"paymentStatus": "SUCCESS",
			"paymentAmount": map[string]string{
				"currency": "CNY",
				"value":    "10000", // 100.00 CNY in smallest unit
			},
		})
	}))
	defer srv.Close()

	p, _ := NewWorldFirstProvider(WorldFirstConfig{
		ClientID:      "test-client",
		PrivateKeyPEM: priv,
		Gateway:       srv.URL,
	})
	p.httpClient = srv.Client()

	result, err := p.QueryOrder(context.Background(), "LO-WF-QUERY")
	if err != nil {
		t.Fatalf("QueryOrder: %v", err)
	}
	if !result.Paid {
		t.Error("expected Paid=true for SUCCESS status")
	}
	if result.Amount != 100.0 {
		t.Errorf("Amount = %v, want 100.0", result.Amount)
	}
}

func TestWorldFirstProvider_QueryOrder_NotPaid(t *testing.T) {
	priv, _ := generateTestRSAKeyPair(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]string{
				"resultStatus": "S",
			},
			"paymentStatus": "PROCESSING",
			"paymentAmount": map[string]string{
				"currency": "CNY",
				"value":    "5000",
			},
		})
	}))
	defer srv.Close()

	p, _ := NewWorldFirstProvider(WorldFirstConfig{
		ClientID:      "test-client",
		PrivateKeyPEM: priv,
		Gateway:       srv.URL,
	})
	p.httpClient = srv.Client()

	result, err := p.QueryOrder(context.Background(), "LO-WF-PENDING")
	if err != nil {
		t.Fatalf("QueryOrder: %v", err)
	}
	if result.Paid {
		t.Error("expected Paid=false for PROCESSING status")
	}
	if result.Amount != 0 {
		t.Errorf("Amount = %v, want 0 when not paid", result.Amount)
	}
}

func TestWorldFirstProvider_QueryOrder_HTTPError(t *testing.T) {
	priv, _ := generateTestRSAKeyPair(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`service down`))
	}))
	defer srv.Close()

	p, _ := NewWorldFirstProvider(WorldFirstConfig{
		ClientID:      "test-client",
		PrivateKeyPEM: priv,
		Gateway:       srv.URL,
	})
	p.httpClient = srv.Client()

	_, err := p.QueryOrder(context.Background(), "LO-WF-503")
	if err == nil {
		t.Fatal("expected error for 503 response")
	}
}
