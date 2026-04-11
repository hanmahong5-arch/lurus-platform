package payment

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// WorldFirstProvider implements Provider, OrderQuerier, and NotifyHandler
// for WorldFirst (万里汇) Cashier Payment API built on Alipay+ infrastructure.
type WorldFirstProvider struct {
	clientID     string
	privateKey   *rsa.PrivateKey
	publicKey    *rsa.PublicKey // WorldFirst's public key for response/webhook verification
	gateway      string        // e.g. "https://open-sea-global.alipay.com"
	notifyURL    string
	keyVersion   string
	httpClient   *http.Client
}

// WorldFirstConfig holds configuration for WorldFirst integration.
type WorldFirstConfig struct {
	ClientID          string // WORLDFIRST_CLIENT_ID
	PrivateKeyPEM     string // WORLDFIRST_PRIVATE_KEY (merchant RSA private key, PEM)
	PublicKeyPEM      string // WORLDFIRST_PUBLIC_KEY (WorldFirst's RSA public key, PEM)
	Gateway           string // WORLDFIRST_GATEWAY (default: https://open-sea-global.alipay.com)
	NotifyURL         string // WORLDFIRST_NOTIFY_URL
	KeyVersion        string // WORLDFIRST_KEY_VERSION (default: "1")
}

// NewWorldFirstProvider creates a WorldFirst provider.
// Returns (nil, nil) if ClientID or PrivateKeyPEM is empty (feature disabled).
func NewWorldFirstProvider(cfg WorldFirstConfig) (*WorldFirstProvider, error) {
	if cfg.ClientID == "" || cfg.PrivateKeyPEM == "" {
		return nil, nil
	}
	if cfg.Gateway == "" {
		cfg.Gateway = "https://open-sea-global.alipay.com"
	}
	if cfg.KeyVersion == "" {
		cfg.KeyVersion = "1"
	}

	privKey, err := parseRSAPrivateKey(cfg.PrivateKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("worldfirst parse private key: %w", err)
	}

	var pubKey *rsa.PublicKey
	if cfg.PublicKeyPEM != "" {
		pubKey, err = parseRSAPublicKey(cfg.PublicKeyPEM)
		if err != nil {
			return nil, fmt.Errorf("worldfirst parse public key: %w", err)
		}
	}

	return &WorldFirstProvider{
		clientID:   cfg.ClientID,
		privateKey: privKey,
		publicKey:  pubKey,
		gateway:    cfg.Gateway,
		notifyURL:  cfg.NotifyURL,
		keyVersion: cfg.KeyVersion,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// Name returns the provider identifier.
func (p *WorldFirstProvider) Name() string { return "worldfirst" }

// CreateCheckout calls createCashierPayment and returns the redirect URL.
func (p *WorldFirstProvider) CreateCheckout(ctx context.Context, o *entity.PaymentOrder, returnURL string) (payURL, externalID string, err error) {
	endpoint := "/amsin/api/v1/business/create"

	body := map[string]any{
		"payToRequestId": o.OrderNo,
		"orderAmount": map[string]string{
			"currency": currencyForOrder(o),
			"value":    fmt.Sprintf("%.0f", o.AmountCNY*100), // smallest unit (cents/fen)
		},
		"orderDescription": fmt.Sprintf("Lurus topup %.2f %s", o.AmountCNY, currencyForOrder(o)),
		"envInfo": map[string]string{
			"terminalType": "WEB",
		},
		"returnUrl": returnURL,
		"notifyUrl": p.notifyURL,
	}

	var resp struct {
		Result struct {
			ResultCode   string `json:"resultCode"`
			ResultStatus string `json:"resultStatus"`
			ResultMessage string `json:"resultMessage"`
		} `json:"result"`
		PayToID    string `json:"payToId"`
		ActionForm struct {
			RedirectURL string `json:"redirectUrl"`
		} `json:"actionForm"`
	}
	if err := p.doRequest(ctx, endpoint, body, &resp); err != nil {
		return "", "", fmt.Errorf("worldfirst create payment: %w", err)
	}
	if resp.Result.ResultStatus != "S" && resp.Result.ResultStatus != "U" {
		return "", "", fmt.Errorf("worldfirst create payment: %s (%s)", resp.Result.ResultMessage, resp.Result.ResultCode)
	}
	if resp.ActionForm.RedirectURL == "" {
		return "", "", fmt.Errorf("worldfirst: empty redirect URL")
	}
	return resp.ActionForm.RedirectURL, resp.PayToID, nil
}

// QueryOrder checks the payment status via inquirePayment.
func (p *WorldFirstProvider) QueryOrder(ctx context.Context, orderNo string) (*OrderQueryResult, error) {
	endpoint := "/amsin/api/v1/business/inquiryPayOrder"

	body := map[string]string{
		"payToRequestId": orderNo,
	}
	var resp struct {
		Result struct {
			ResultStatus string `json:"resultStatus"`
		} `json:"result"`
		PaymentStatus string `json:"paymentStatus"`
		PaymentAmount struct {
			Currency string `json:"currency"`
			Value    string `json:"value"`
		} `json:"paymentAmount"`
	}
	if err := p.doRequest(ctx, endpoint, body, &resp); err != nil {
		return nil, fmt.Errorf("worldfirst query order: %w", err)
	}
	paid := resp.PaymentStatus == "SUCCESS"
	var amount float64
	if paid {
		_, _ = fmt.Sscanf(resp.PaymentAmount.Value, "%f", &amount)
		amount /= 100.0 // smallest unit → standard
	}
	return &OrderQueryResult{Paid: paid, Amount: amount}, nil
}

// HandleNotify verifies and parses a WorldFirst async payment notification.
func (p *WorldFirstProvider) HandleNotify(req *http.Request) (orderNo string, ok bool, err error) {
	bodyBytes, err := io.ReadAll(io.LimitReader(req.Body, 1<<20))
	if err != nil {
		return "", false, fmt.Errorf("worldfirst read notify body: %w", err)
	}

	// Verify signature if public key is configured.
	if p.publicKey != nil {
		sig := req.Header.Get("Signature")
		if !p.verifySignature("POST", req.URL.Path, p.clientID, req.Header.Get("Request-Time"), bodyBytes, sig) {
			return "", false, fmt.Errorf("worldfirst notify: signature verification failed")
		}
	}

	var notify struct {
		PayToRequestID string `json:"payToRequestId"`
		PaymentStatus  string `json:"paymentStatus"`
		Result         struct {
			ResultStatus string `json:"resultStatus"`
		} `json:"result"`
	}
	if err := json.Unmarshal(bodyBytes, &notify); err != nil {
		return "", false, fmt.Errorf("worldfirst notify: unmarshal: %w", err)
	}
	if notify.PaymentStatus != "SUCCESS" {
		return "", true, nil // valid but not a success
	}
	if notify.PayToRequestID == "" {
		return "", false, fmt.Errorf("worldfirst notify: missing payToRequestId")
	}
	return notify.PayToRequestID, true, nil
}

// --- Internal helpers ---

// doRequest signs and sends a POST request to the WorldFirst API.
func (p *WorldFirstProvider) doRequest(ctx context.Context, endpoint string, body any, result any) error {
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	requestTime := time.Now().UTC().Format("2006-01-02T15:04:05-07:00")
	sig, err := p.sign(endpoint, requestTime, bodyBytes)
	if err != nil {
		return fmt.Errorf("sign request: %w", err)
	}

	url := p.gateway + endpoint
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	req.Header.Set("Client-Id", p.clientID)
	req.Header.Set("Request-Time", requestTime)
	req.Header.Set("Signature", fmt.Sprintf("algorithm=RSA256,keyVersion=%s,signature=%s", p.keyVersion, sig))

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("api error %d: %s", resp.StatusCode, string(respBody))
	}

	return json.Unmarshal(respBody, result)
}

// sign generates the RSA256 signature for a WorldFirst API request.
// Content to sign: POST {endpoint}\n{clientId}.{requestTime}.{requestBody}
func (p *WorldFirstProvider) sign(endpoint, requestTime string, body []byte) (string, error) {
	content := fmt.Sprintf("POST %s\n%s.%s.%s", endpoint, p.clientID, requestTime, string(body))
	hash := sha256.Sum256([]byte(content))
	sigBytes, err := rsa.SignPKCS1v15(rand.Reader, p.privateKey, crypto.SHA256, hash[:])
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(sigBytes), nil
}

// verifySignature verifies a WorldFirst response/webhook signature.
func (p *WorldFirstProvider) verifySignature(method, path, clientID, requestTime string, body []byte, sigHeader string) bool {
	if p.publicKey == nil {
		return false
	}
	// Parse signature from header: "algorithm=RSA256,keyVersion=1,signature=<base64url>"
	sigValue := extractSignatureValue(sigHeader)
	if sigValue == "" {
		return false
	}
	sigBytes, err := base64.URLEncoding.DecodeString(sigValue)
	if err != nil {
		return false
	}
	content := fmt.Sprintf("%s %s\n%s.%s.%s", method, path, clientID, requestTime, string(body))
	hash := sha256.Sum256([]byte(content))
	return rsa.VerifyPKCS1v15(p.publicKey, crypto.SHA256, hash[:], sigBytes) == nil
}

// extractSignatureValue parses the signature= field from a WorldFirst Signature header.
func extractSignatureValue(header string) string {
	const prefix = "signature="
	for i := 0; i <= len(header)-len(prefix); i++ {
		if header[i:i+len(prefix)] == prefix {
			return header[i+len(prefix):]
		}
	}
	return ""
}

// currencyForOrder returns the currency for a WorldFirst order.
// Defaults to CNY if no currency is set on the order.
func currencyForOrder(o *entity.PaymentOrder) string {
	if o.Currency != "" && o.Currency != "CNY" {
		return o.Currency
	}
	return "CNY"
}

// parseRSAPrivateKey parses a PEM-encoded RSA private key (PKCS1 or PKCS8).
func parseRSAPrivateKey(pemStr string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, fmt.Errorf("no PEM block found")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("not an RSA key")
	}
	return rsaKey, nil
}

// parseRSAPublicKey parses a PEM-encoded RSA public key (PKIX).
func parseRSAPublicKey(pemStr string) (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, fmt.Errorf("no PEM block found")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse public key: %w", err)
	}
	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("not an RSA public key")
	}
	return rsaPub, nil
}
