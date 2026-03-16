package payment

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

const creemAPIBase = "https://api.creem.io/v1"

// CreemProvider implements Provider for Creem.
type CreemProvider struct {
	apiKey        string
	webhookSecret string
	httpClient    *http.Client
}

// NewCreemProvider creates a CreemProvider.
// Returns nil if API key is empty (feature disabled).
// Returns an error if API key is set but webhook secret is missing — a missing
// secret would cause VerifyWebhook to accept any unsigned payload (fail-open),
// which is equivalent to disabling payment authentication entirely.
func NewCreemProvider(apiKey, webhookSecret string) (*CreemProvider, error) {
	if apiKey == "" {
		return nil, nil
	}
	if webhookSecret == "" {
		return nil, fmt.Errorf("CREEM_WEBHOOK_SECRET is required when CREEM_API_KEY is set")
	}
	return &CreemProvider{
		apiKey:        apiKey,
		webhookSecret: webhookSecret,
		httpClient:    &http.Client{Timeout: 15 * time.Second},
	}, nil
}

// Name returns the provider identifier.
func (p *CreemProvider) Name() string { return "creem" }

// CreateCheckout calls the Creem API to create a checkout session.
func (p *CreemProvider) CreateCheckout(ctx context.Context, o *entity.PaymentOrder, returnURL string) (payURL, externalID string, err error) {
	reqBody, err := json.Marshal(map[string]any{
		"request_id": o.OrderNo,
		"amount":     int64(o.AmountCNY * 100), // store in fen (smallest unit)
		"currency":   "CNY",
		"return_url": returnURL + "?order_no=" + o.OrderNo + "&status=success",
	})
	if err != nil {
		return "", "", fmt.Errorf("marshal creem request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, creemAPIBase+"/checkouts", bytes.NewReader(reqBody))
	if err != nil {
		return "", "", fmt.Errorf("create creem request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("creem http: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return "", "", fmt.Errorf("read creem response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return "", "", fmt.Errorf("creem api error %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		ID         string `json:"id"`
		CheckoutURL string `json:"checkout_url"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", "", fmt.Errorf("decode creem response: %w", err)
	}
	if result.CheckoutURL == "" {
		return "", "", fmt.Errorf("creem returned empty checkout_url")
	}
	return result.CheckoutURL, result.ID, nil
}

// VerifyWebhook validates a Creem webhook via HMAC-SHA256 and extracts the order number.
// sig is the raw value of the X-Creem-Signature header.
// Fails closed: returns (_, false) if the secret is empty or signature mismatches.
func (p *CreemProvider) VerifyWebhook(payload []byte, sig string) (orderNo string, ok bool) {
	// Fail-closed: an empty secret rejects all requests rather than skipping
	// verification. NewCreemProvider already prevents construction with an empty
	// secret, but this guard provides defence-in-depth.
	if p.webhookSecret == "" {
		return "", false
	}
	mac := hmac.New(sha256.New, []byte(p.webhookSecret))
	mac.Write(payload)
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(sig)) {
		return "", false
	}

	var event struct {
		EventType string `json:"event_type"`
		OrderNo   string `json:"order_no"`
		RequestID string `json:"request_id"` // alternative field name used by some versions
	}
	if err := json.Unmarshal(payload, &event); err != nil {
		return "", false
	}
	if event.EventType != "payment.success" {
		return "", true // valid but irrelevant event
	}
	no := event.OrderNo
	if no == "" {
		no = event.RequestID
	}
	if no == "" {
		return "", false
	}
	return no, true
}
