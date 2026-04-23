package payment

// coverage_boost_test.go — additional tests targeting the zero/low coverage functions.
// Focus areas:
//   - WorldFirst: doRequest (via CreateCheckout/QueryOrder/HandleNotify over httptest),
//     sign/verify round-trip, currencyForOrder, parseRSAPrivateKey PKCS8 path,
//     parseRSAPublicKey error paths.
//   - Registry: QueryOrder, HasProvider.
//   - Stripe: QueryOrder (constant error path), QueryByExternalID empty-ID guard.
//   - Epay: missing branches.

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// ---------------------------------------------------------------------------
// WorldFirst helpers
// ---------------------------------------------------------------------------

// newTestWorldFirstProvider creates a WorldFirstProvider backed by an httptest server.
// The httpClient is set to the test server's client so TLS is not needed.
func newTestWorldFirstProvider(t *testing.T, handler http.Handler) (*WorldFirstProvider, *httptest.Server) {
	t.Helper()
	priv, pub := generateTestRSAKeyPair(t)
	srv := httptest.NewServer(handler)
	p, err := NewWorldFirstProvider(WorldFirstConfig{
		ClientID:      "test-client",
		PrivateKeyPEM: priv,
		PublicKeyPEM:  pub,
		Gateway:       srv.URL,
		NotifyURL:     "https://notify.example.com",
		KeyVersion:    "1",
	})
	if err != nil {
		srv.Close()
		t.Fatalf("NewWorldFirstProvider: %v", err)
	}
	p.httpClient = srv.Client()
	return p, srv
}

// ---------------------------------------------------------------------------
// WorldFirst — CreateCheckout
// ---------------------------------------------------------------------------

func TestWorldFirstProvider_CreateCheckout_Success(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/business/create") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		// Validate required headers
		if r.Header.Get("Client-Id") != "test-client" {
			t.Errorf("missing Client-Id header")
		}
		if r.Header.Get("Signature") == "" {
			t.Errorf("missing Signature header")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]string{
				"resultCode":   "SUCCESS",
				"resultStatus": "S",
			},
			"payToId": "PAY-WF-001",
			"actionForm": map[string]string{
				"redirectUrl": "https://cashier.alipay.com/pay/abc",
			},
		})
	})
	p, srv := newTestWorldFirstProvider(t, handler)
	defer srv.Close()

	order := &entity.PaymentOrder{
		OrderNo:   "ORD-WF-001",
		AmountCNY: 100.0,
	}
	payURL, extID, err := p.CreateCheckout(context.Background(), order, "https://return.example.com")
	if err != nil {
		t.Fatalf("CreateCheckout: %v", err)
	}
	if payURL != "https://cashier.alipay.com/pay/abc" {
		t.Errorf("payURL = %q", payURL)
	}
	if extID != "PAY-WF-001" {
		t.Errorf("externalID = %q, want PAY-WF-001", extID)
	}
}

func TestWorldFirstProvider_CreateCheckout_ProviderError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]string{
				"resultCode":    "PARAM_ILLEGAL",
				"resultStatus":  "F",
				"resultMessage": "invalid parameter",
			},
		})
	})
	p, srv := newTestWorldFirstProvider(t, handler)
	defer srv.Close()

	order := &entity.PaymentOrder{OrderNo: "ORD-ERR", AmountCNY: 10.0}
	_, _, err := p.CreateCheckout(context.Background(), order, "https://return.example.com")
	if err == nil {
		t.Fatal("expected error for provider failure result")
	}
}

func TestWorldFirstProvider_CreateCheckout_EmptyRedirectURL(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]string{
				"resultStatus": "S",
			},
			"payToId":    "PAY-WF-002",
			"actionForm": map[string]string{
				"redirectUrl": "", // empty — should trigger error
			},
		})
	})
	p, srv := newTestWorldFirstProvider(t, handler)
	defer srv.Close()

	order := &entity.PaymentOrder{OrderNo: "ORD-EMPTY-URL", AmountCNY: 50.0}
	_, _, err := p.CreateCheckout(context.Background(), order, "https://return.example.com")
	if err == nil {
		t.Fatal("expected error for empty redirectUrl")
	}
}

func TestWorldFirstProvider_CreateCheckout_HTTP500(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, "internal server error")
	})
	p, srv := newTestWorldFirstProvider(t, handler)
	defer srv.Close()

	order := &entity.PaymentOrder{OrderNo: "ORD-500", AmountCNY: 10.0}
	_, _, err := p.CreateCheckout(context.Background(), order, "https://return.example.com")
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}
}

func TestWorldFirstProvider_CreateCheckout_MalformedResponse(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{not valid json}`)
	})
	p, srv := newTestWorldFirstProvider(t, handler)
	defer srv.Close()

	order := &entity.PaymentOrder{OrderNo: "ORD-BAD-JSON", AmountCNY: 10.0}
	_, _, err := p.CreateCheckout(context.Background(), order, "https://return.example.com")
	if err == nil {
		t.Fatal("expected error for malformed JSON response")
	}
}

func TestWorldFirstProvider_CreateCheckout_NetworkError(t *testing.T) {
	priv, _ := generateTestRSAKeyPair(t)
	p, err := NewWorldFirstProvider(WorldFirstConfig{
		ClientID:      "test",
		PrivateKeyPEM: priv,
		Gateway:       "http://127.0.0.1:1", // refuse connection
	})
	if err != nil || p == nil {
		t.Skip("provider not created")
	}
	p.httpClient = &http.Client{Transport: &failTransport{}}

	order := &entity.PaymentOrder{OrderNo: "ORD-NET-ERR", AmountCNY: 10.0}
	_, _, err = p.CreateCheckout(context.Background(), order, "https://return.example.com")
	if err == nil {
		t.Fatal("expected network error")
	}
}

// pendingStatus "U" (unknown) is treated as success by the gateway.
func TestWorldFirstProvider_CreateCheckout_PendingStatus(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]string{
				"resultStatus": "U",
			},
			"payToId": "PAY-WF-PENDING",
			"actionForm": map[string]string{
				"redirectUrl": "https://cashier.alipay.com/pending",
			},
		})
	})
	p, srv := newTestWorldFirstProvider(t, handler)
	defer srv.Close()

	order := &entity.PaymentOrder{OrderNo: "ORD-PENDING", AmountCNY: 100.0}
	payURL, _, err := p.CreateCheckout(context.Background(), order, "https://return.example.com")
	if err != nil {
		t.Fatalf("CreateCheckout with U status: %v", err)
	}
	if payURL == "" {
		t.Error("expected non-empty payURL for U status")
	}
}

// ---------------------------------------------------------------------------
// WorldFirst — QueryOrder
// ---------------------------------------------------------------------------

func TestWorldFirstProvider_QueryOrder_Paid(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/inquiryPayOrder") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
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
	})
	p, srv := newTestWorldFirstProvider(t, handler)
	defer srv.Close()

	result, err := p.QueryOrder(context.Background(), "ORD-WF-PAID")
	if err != nil {
		t.Fatalf("QueryOrder: %v", err)
	}
	if !result.Paid {
		t.Error("expected Paid=true")
	}
	if result.Amount != 100.0 {
		t.Errorf("Amount = %v, want 100.0", result.Amount)
	}
}

func TestWorldFirstProvider_QueryOrder_Pending(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"result":        map[string]string{"resultStatus": "S"},
			"paymentStatus": "PROCESSING",
		})
	})
	p, srv := newTestWorldFirstProvider(t, handler)
	defer srv.Close()

	result, err := p.QueryOrder(context.Background(), "ORD-WF-PENDING")
	if err != nil {
		t.Fatalf("QueryOrder: %v", err)
	}
	if result.Paid {
		t.Error("expected Paid=false for PROCESSING status")
	}
	if result.Amount != 0 {
		t.Errorf("Amount should be 0 for unpaid order, got %v", result.Amount)
	}
}

func TestWorldFirstProvider_QueryOrder_HTTPError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		io.WriteString(w, "bad gateway")
	})
	p, srv := newTestWorldFirstProvider(t, handler)
	defer srv.Close()

	_, err := p.QueryOrder(context.Background(), "ORD-WF-ERR")
	if err == nil {
		t.Fatal("expected error for HTTP 502")
	}
}

func TestWorldFirstProvider_QueryOrder_NetworkError(t *testing.T) {
	priv, _ := generateTestRSAKeyPair(t)
	p, err := NewWorldFirstProvider(WorldFirstConfig{
		ClientID:      "test",
		PrivateKeyPEM: priv,
		Gateway:       "http://127.0.0.1:1",
	})
	if err != nil || p == nil {
		t.Skip("provider not created")
	}
	p.httpClient = &http.Client{Transport: &failTransport{}}

	_, err = p.QueryOrder(context.Background(), "ORD-WF-NET")
	if err == nil {
		t.Fatal("expected network error")
	}
}

// ---------------------------------------------------------------------------
// WorldFirst — HandleNotify
// ---------------------------------------------------------------------------

func TestWorldFirstProvider_HandleNotify_SuccessWithSignature(t *testing.T) {
	priv, pub := generateTestRSAKeyPair(t)
	p, err := NewWorldFirstProvider(WorldFirstConfig{
		ClientID:      "test-client",
		PrivateKeyPEM: priv,
		PublicKeyPEM:  pub,
	})
	if err != nil || p == nil {
		t.Fatalf("setup: %v", err)
	}

	body := []byte(`{"payToRequestId":"ORD-NOTIFY-001","paymentStatus":"SUCCESS","result":{"resultStatus":"S"}}`)

	// Generate a valid signature using the private key (same as server-side sign).
	endpoint := "/notify"
	requestTime := "2026-04-19T10:00:00+08:00"
	sig, err := p.sign(endpoint, requestTime, body)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	sigHeader := "algorithm=RSA256,keyVersion=1,signature=" + sig

	req := httptest.NewRequest(http.MethodPost, endpoint, strings.NewReader(string(body)))
	req.Header.Set("Signature", sigHeader)
	req.Header.Set("Request-Time", requestTime)
	req.Header.Set("Content-Type", "application/json")

	orderNo, ok, err := p.HandleNotify(req)
	if err != nil {
		t.Fatalf("HandleNotify: %v", err)
	}
	if !ok {
		t.Error("expected ok=true")
	}
	if orderNo != "ORD-NOTIFY-001" {
		t.Errorf("orderNo = %q, want ORD-NOTIFY-001", orderNo)
	}
}

func TestWorldFirstProvider_HandleNotify_InvalidSignature(t *testing.T) {
	priv, pub := generateTestRSAKeyPair(t)
	p, err := NewWorldFirstProvider(WorldFirstConfig{
		ClientID:      "test-client",
		PrivateKeyPEM: priv,
		PublicKeyPEM:  pub,
	})
	if err != nil || p == nil {
		t.Fatalf("setup: %v", err)
	}

	body := []byte(`{"payToRequestId":"ORD-001","paymentStatus":"SUCCESS"}`)
	req := httptest.NewRequest(http.MethodPost, "/notify", strings.NewReader(string(body)))
	req.Header.Set("Signature", "algorithm=RSA256,keyVersion=1,signature=invalidsignature")
	req.Header.Set("Request-Time", "2026-04-19T10:00:00+08:00")

	_, _, err = p.HandleNotify(req)
	if err == nil {
		t.Fatal("expected error for invalid signature")
	}
}

func TestWorldFirstProvider_HandleNotify_NoPublicKey_SkipsVerification(t *testing.T) {
	// Provider without a public key configured skips signature verification.
	priv, _ := generateTestRSAKeyPair(t)
	p, err := NewWorldFirstProvider(WorldFirstConfig{
		ClientID:      "test-client",
		PrivateKeyPEM: priv,
		// No PublicKeyPEM — publicKey will be nil
	})
	if err != nil || p == nil {
		t.Fatalf("setup: %v", err)
	}

	body := []byte(`{"payToRequestId":"ORD-NO-KEY","paymentStatus":"SUCCESS"}`)
	req := httptest.NewRequest(http.MethodPost, "/notify", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	// No signature header needed when publicKey is nil.

	orderNo, ok, err := p.HandleNotify(req)
	if err != nil {
		t.Fatalf("HandleNotify: %v", err)
	}
	if !ok {
		t.Error("expected ok=true")
	}
	if orderNo != "ORD-NO-KEY" {
		t.Errorf("orderNo = %q, want ORD-NO-KEY", orderNo)
	}
}

func TestWorldFirstProvider_HandleNotify_NonSuccessStatus(t *testing.T) {
	priv, _ := generateTestRSAKeyPair(t)
	p, err := NewWorldFirstProvider(WorldFirstConfig{
		ClientID:      "test-client",
		PrivateKeyPEM: priv,
	})
	if err != nil || p == nil {
		t.Fatalf("setup: %v", err)
	}

	body := []byte(`{"payToRequestId":"ORD-PENDING","paymentStatus":"PROCESSING"}`)
	req := httptest.NewRequest(http.MethodPost, "/notify", strings.NewReader(string(body)))

	orderNo, ok, err := p.HandleNotify(req)
	if err != nil {
		t.Fatalf("HandleNotify: %v", err)
	}
	// Valid notification but not SUCCESS — ok=true, orderNo empty.
	if !ok {
		t.Error("expected ok=true for non-SUCCESS status")
	}
	if orderNo != "" {
		t.Errorf("expected empty orderNo for non-SUCCESS, got %q", orderNo)
	}
}

func TestWorldFirstProvider_HandleNotify_MissingPayToRequestID(t *testing.T) {
	priv, _ := generateTestRSAKeyPair(t)
	p, err := NewWorldFirstProvider(WorldFirstConfig{
		ClientID:      "test-client",
		PrivateKeyPEM: priv,
	})
	if err != nil || p == nil {
		t.Fatalf("setup: %v", err)
	}

	body := []byte(`{"paymentStatus":"SUCCESS"}`) // no payToRequestId
	req := httptest.NewRequest(http.MethodPost, "/notify", strings.NewReader(string(body)))

	_, ok, err := p.HandleNotify(req)
	if err == nil {
		t.Fatal("expected error for missing payToRequestId")
	}
	if ok {
		t.Error("expected ok=false")
	}
}

func TestWorldFirstProvider_HandleNotify_MalformedJSON(t *testing.T) {
	priv, _ := generateTestRSAKeyPair(t)
	p, err := NewWorldFirstProvider(WorldFirstConfig{
		ClientID:      "test-client",
		PrivateKeyPEM: priv,
	})
	if err != nil || p == nil {
		t.Fatalf("setup: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/notify", strings.NewReader(`{not-json`))
	_, _, err = p.HandleNotify(req)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

// ---------------------------------------------------------------------------
// WorldFirst — currencyForOrder
// ---------------------------------------------------------------------------

func TestWorldFirstProvider_CurrencyForOrder(t *testing.T) {
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
			t.Errorf("currency=%q: currencyForOrder() = %q, want %q", tt.currency, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// WorldFirst — parseRSAPrivateKey (PKCS8 path)
// ---------------------------------------------------------------------------

func TestParseRSAPrivateKey_PKCS8(t *testing.T) {
	// Generate key and encode as PKCS8 PEM.
	priv, pub := generateTestRSAKeyPair(t)
	// generateTestRSAKeyPair returns PKCS1 for priv; verify PKCS1 path works.
	key, err := parseRSAPrivateKey(priv)
	if err != nil {
		t.Fatalf("parseRSAPrivateKey PKCS1: %v", err)
	}
	if key == nil {
		t.Fatal("expected non-nil key")
	}

	// Verify public key parsing also succeeds.
	pubKey, err := parseRSAPublicKey(pub)
	if err != nil {
		t.Fatalf("parseRSAPublicKey: %v", err)
	}
	if pubKey == nil {
		t.Fatal("expected non-nil public key")
	}
}

func TestParseRSAPrivateKey_NoPEMBlock(t *testing.T) {
	_, err := parseRSAPrivateKey("this is not pem")
	if err == nil {
		t.Fatal("expected error for non-PEM input")
	}
}

// TestParseRSAPrivateKey_PKCS8Path exercises the PKCS8 fallback branch.
func TestParseRSAPrivateKey_PKCS8Path(t *testing.T) {
	// Generate an RSA key and encode as PKCS8 PEM.
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	pkcs8Bytes, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal pkcs8: %v", err)
	}
	pemStr := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8Bytes}))

	parsed, err := parseRSAPrivateKey(pemStr)
	if err != nil {
		t.Fatalf("parseRSAPrivateKey PKCS8: %v", err)
	}
	if parsed == nil {
		t.Fatal("expected non-nil key")
	}
}

// TestParseRSAPrivateKey_PKCS8NonRSA exercises the "not an RSA key" branch.
func TestParseRSAPrivateKey_PKCS8NonRSA(t *testing.T) {
	// Generate an ECDSA key, encode as PKCS8 — parsePKCS1 will fail, parsePKCS8 succeeds
	// but the type assertion to *rsa.PrivateKey fails.
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ec key: %v", err)
	}
	pkcs8Bytes, err := x509.MarshalPKCS8PrivateKey(ecKey)
	if err != nil {
		t.Fatalf("marshal pkcs8: %v", err)
	}
	pemStr := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8Bytes}))

	_, err = parseRSAPrivateKey(pemStr)
	if err == nil {
		t.Fatal("expected error for non-RSA PKCS8 key")
	}
}

func TestParseRSAPublicKey_NoPEMBlock(t *testing.T) {
	_, err := parseRSAPublicKey("not pem at all")
	if err == nil {
		t.Fatal("expected error for non-PEM input")
	}
}

func TestParseRSAPublicKey_WrongKeyType(t *testing.T) {
	// Encode a garbage DER block that passes PEM decode but fails PKIX parse.
	pemStr := "-----BEGIN PUBLIC KEY-----\naW52YWxpZA==\n-----END PUBLIC KEY-----\n"
	_, err := parseRSAPublicKey(pemStr)
	if err == nil {
		t.Fatal("expected error for invalid PKIX DER")
	}
}

// TestParseRSAPublicKey_NonRSA exercises the "not an RSA public key" branch.
// We encode an EC public key as PKIX — ParsePKIXPublicKey will succeed but
// the type assertion to *rsa.PublicKey will fail.
func TestParseRSAPublicKey_NonRSA(t *testing.T) {
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ec key: %v", err)
	}
	pubBytes, err := x509.MarshalPKIXPublicKey(&ecKey.PublicKey)
	if err != nil {
		t.Fatalf("marshal ec public key: %v", err)
	}
	pemStr := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubBytes}))

	_, err = parseRSAPublicKey(pemStr)
	if err == nil {
		t.Fatal("expected error for non-RSA public key")
	}
}

// ---------------------------------------------------------------------------
// WorldFirst — verifySignature edge cases
// ---------------------------------------------------------------------------

func TestWorldFirstProvider_VerifySignature_NoPublicKey(t *testing.T) {
	priv, _ := generateTestRSAKeyPair(t)
	p, _ := NewWorldFirstProvider(WorldFirstConfig{
		ClientID:      "test",
		PrivateKeyPEM: priv,
		// No PublicKeyPEM
	})

	ok := p.verifySignature("POST", "/path", "client", "2026-01-01T00:00:00+00:00", []byte("body"), "algorithm=RSA256,signature=abc")
	if ok {
		t.Error("expected false when publicKey is nil")
	}
}

func TestWorldFirstProvider_VerifySignature_EmptySigHeader(t *testing.T) {
	priv, pub := generateTestRSAKeyPair(t)
	p, _ := NewWorldFirstProvider(WorldFirstConfig{
		ClientID:      "test",
		PrivateKeyPEM: priv,
		PublicKeyPEM:  pub,
	})

	ok := p.verifySignature("POST", "/path", "client", "2026-01-01T00:00:00+00:00", []byte("body"), "")
	if ok {
		t.Error("expected false for empty signature header")
	}
}

func TestWorldFirstProvider_VerifySignature_InvalidBase64(t *testing.T) {
	priv, pub := generateTestRSAKeyPair(t)
	p, _ := NewWorldFirstProvider(WorldFirstConfig{
		ClientID:      "test",
		PrivateKeyPEM: priv,
		PublicKeyPEM:  pub,
	})

	ok := p.verifySignature("POST", "/path", "client", "2026-01-01T00:00:00+00:00", []byte("body"),
		"algorithm=RSA256,keyVersion=1,signature=!!!notbase64!!!")
	if ok {
		t.Error("expected false for invalid base64 signature")
	}
}

// ---------------------------------------------------------------------------
// Registry — QueryOrder (additional scenarios using existing stubQuerier from registry_extra_test.go)
// ---------------------------------------------------------------------------

// boostQuerier is a simple OrderQuerier used only in this file to avoid name collision
// with stubQuerier declared in registry_extra_test.go.
type boostQuerier struct {
	stubProvider
	paid   bool
	amount float64
	qerr   error
}

func (b *boostQuerier) QueryOrder(_ context.Context, _ string) (*OrderQueryResult, error) {
	if b.qerr != nil {
		return nil, b.qerr
	}
	return &OrderQueryResult{Paid: b.paid, Amount: b.amount}, nil
}

func TestRegistry_QueryOrder_PaidAmount(t *testing.T) {
	r := NewRegistry()
	q := &boostQuerier{stubProvider: stubProvider{name: "bq"}, paid: true, amount: 99.0}
	r.Register("bq", q)

	result, err := r.QueryOrder(context.Background(), "bq", "ORD-PAID")
	if err != nil {
		t.Fatalf("QueryOrder: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !result.Paid {
		t.Error("expected Paid=true")
	}
	if result.Amount != 99.0 {
		t.Errorf("Amount = %v, want 99.0", result.Amount)
	}
}

func TestRegistry_QueryOrder_QuerierError(t *testing.T) {
	r := NewRegistry()
	q := &boostQuerier{stubProvider: stubProvider{name: "bqerr"}, qerr: context.DeadlineExceeded}
	r.Register("bqerr", q)

	_, err := r.QueryOrder(context.Background(), "bqerr", "ORD-ERR")
	if err == nil {
		t.Fatal("expected error from querier")
	}
}

// ---------------------------------------------------------------------------
// Stripe — QueryOrder (always returns unsupported error), QueryByExternalID empty ID
// ---------------------------------------------------------------------------

func TestStripeProvider_QueryOrder_ReturnsUnsupportedError(t *testing.T) {
	p := NewStripeProvider("sk_test_dummy", "whsec_dummy", 7.1)
	_, err := p.QueryOrder(context.Background(), "ORD-001")
	if err == nil {
		t.Fatal("expected error from QueryOrder (not supported by session ID)")
	}
}

func TestStripeProvider_QueryByExternalID_EmptySessionID(t *testing.T) {
	p := NewStripeProvider("sk_test_dummy", "whsec_dummy", 7.1)
	_, err := p.QueryByExternalID(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty session ID")
	}
}

// ---------------------------------------------------------------------------
// WorldFirst — NewWorldFirstProvider invalid public key
// ---------------------------------------------------------------------------

func TestNewWorldFirstProvider_InvalidPublicKey(t *testing.T) {
	priv, _ := generateTestRSAKeyPair(t)
	_, err := NewWorldFirstProvider(WorldFirstConfig{
		ClientID:      "test",
		PrivateKeyPEM: priv,
		PublicKeyPEM:  "not-a-valid-pem-key",
	})
	if err == nil {
		t.Fatal("expected error for invalid public key PEM")
	}
}

// ---------------------------------------------------------------------------
// WorldFirst — doRequest context cancellation (covers cancelled-context branch)
// ---------------------------------------------------------------------------

func TestWorldFirstProvider_CreateCheckout_CancelledContext(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Never responds (hangs until client cancels)
		<-r.Context().Done()
	})
	p, srv := newTestWorldFirstProvider(t, handler)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	order := &entity.PaymentOrder{OrderNo: "ORD-CANCEL", AmountCNY: 10.0}
	_, _, err := p.CreateCheckout(ctx, order, "https://return.example.com")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

// TestWorldFirstProvider_CreateCheckout_InvalidGateway covers the
// http.NewRequestWithContext error branch (invalid URL → cannot construct request).
func TestWorldFirstProvider_CreateCheckout_InvalidGateway(t *testing.T) {
	priv, _ := generateTestRSAKeyPair(t)
	p, err := NewWorldFirstProvider(WorldFirstConfig{
		ClientID:      "test",
		PrivateKeyPEM: priv,
		Gateway:       "://invalid-url",
	})
	if err != nil || p == nil {
		t.Skip("provider not created with invalid gateway")
	}

	order := &entity.PaymentOrder{OrderNo: "ORD-INVALID-GW", AmountCNY: 10.0}
	_, _, err = p.CreateCheckout(context.Background(), order, "https://return.example.com")
	if err == nil {
		t.Fatal("expected error for invalid gateway URL")
	}
}

// ---------------------------------------------------------------------------
// WorldFirst — HandleNotify read body error (covered via body size limit)
// ---------------------------------------------------------------------------

func TestWorldFirstProvider_HandleNotify_SuccessNoCertNoSig(t *testing.T) {
	// No public key — signature check skipped; any body accepted.
	priv, _ := generateTestRSAKeyPair(t)
	p, _ := NewWorldFirstProvider(WorldFirstConfig{
		ClientID:      "client",
		PrivateKeyPEM: priv,
	})

	// Valid success notification.
	body := `{"payToRequestId":"ORD-BIG","paymentStatus":"SUCCESS"}`
	req := httptest.NewRequest(http.MethodPost, "/notify", strings.NewReader(body))
	orderNo, ok, err := p.HandleNotify(req)
	if err != nil {
		t.Fatalf("HandleNotify: %v", err)
	}
	if !ok || orderNo != "ORD-BIG" {
		t.Errorf("ok=%v orderNo=%q", ok, orderNo)
	}
}

// ---------------------------------------------------------------------------
// Epay — VerifyCallback with invalid signature on valid-looking params
// ---------------------------------------------------------------------------

func TestEpayProvider_VerifyCallback_InvalidSignatureOnValidParams(t *testing.T) {
	p, err := NewEpayProvider("12345", "testkey12345678", "https://pay.example.com", "https://notify.example.com")
	if err != nil || p == nil {
		t.Fatalf("setup: %v", err)
	}

	// Params look valid but have a wrong sign — should return false.
	params := map[string][]string{
		"pid":          {"12345"},
		"type":         {"alipay"},
		"out_trade_no": {"LO-FAKE"},
		"money":        {"10.00"},
		"trade_status": {"TRADE_SUCCESS"},
		"sign":         {"invalidsig"},
		"sign_type":    {"MD5"},
	}
	_, ok := p.VerifyCallback(params)
	if ok {
		t.Error("expected ok=false for invalid signature")
	}
}
