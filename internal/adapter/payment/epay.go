package payment

import (
	"context"
	"fmt"
	"net/url"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// EpayProvider implements Provider for 易支付 (Alipay/WeChat via epay gateway).
type EpayProvider struct {
	client    *epay.Client
	notifyURL string
}

// NewEpayProvider creates a new EpayProvider from config values.
// Returns nil if partner ID or key is empty (feature disabled).
func NewEpayProvider(partnerID, key, gatewayURL, notifyURL string) (*EpayProvider, error) {
	if partnerID == "" || key == "" || gatewayURL == "" {
		return nil, nil // disabled
	}
	client, err := epay.NewClient(&epay.Config{
		PartnerID: partnerID,
		Key:       key,
	}, gatewayURL)
	if err != nil {
		return nil, fmt.Errorf("epay new client: %w", err)
	}
	return &EpayProvider{client: client, notifyURL: notifyURL}, nil
}

// Name returns the provider identifier.
func (p *EpayProvider) Name() string { return "epay_alipay" }

// CreateCheckout builds the payment redirect URL for the given order.
// payType can be overridden per-order via PaymentMethod field ("epay_alipay" → "alipay", "epay_wxpay" → "wxpay").
func (p *EpayProvider) CreateCheckout(ctx context.Context, o *entity.PaymentOrder, returnURL string) (payURL, externalID string, err error) {
	payType := "alipay"
	if o.PaymentMethod == "epay_wxpay" {
		payType = "wxpay"
	}

	notifyU, err := url.Parse(p.notifyURL)
	if err != nil {
		return "", "", fmt.Errorf("parse notify url: %w", err)
	}
	returnU, err := url.Parse(returnURL)
	if err != nil {
		return "", "", fmt.Errorf("parse return url: %w", err)
	}

	baseURL, params, err := p.client.Purchase(&epay.PurchaseArgs{
		Type:           payType,
		ServiceTradeNo: o.OrderNo,
		Name:           fmt.Sprintf("Lurus 充值 %.2f CNY", o.AmountCNY),
		Money:          fmt.Sprintf("%.2f", o.AmountCNY),
		Device:         epay.PC,
		NotifyUrl:      notifyU,
		ReturnUrl:      returnU,
	})
	if err != nil {
		return "", "", fmt.Errorf("epay purchase: %w", err)
	}

	// Build GET redirect URL: baseURL + "?" + params as query string
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", "", fmt.Errorf("parse epay base url: %w", err)
	}
	q := u.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()

	return u.String(), o.OrderNo, nil
}

// VerifyCallback validates an epay GET callback and returns the order number.
func (p *EpayProvider) VerifyCallback(params url.Values) (orderNo string, ok bool) {
	// Convert url.Values ([]string values) to map[string]string (first value)
	flat := make(map[string]string, len(params))
	for k, vs := range params {
		if len(vs) > 0 {
			flat[k] = vs[0]
		}
	}
	res, err := p.client.Verify(flat)
	if err != nil || !res.VerifyStatus {
		return "", false
	}
	if res.TradeStatus != epay.StatusTradeSuccess {
		return "", false
	}
	return res.ServiceTradeNo, true
}
