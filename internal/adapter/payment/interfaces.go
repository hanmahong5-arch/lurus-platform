// Package payment defines the common interface for all payment providers.
package payment

import (
	"context"
	"net/http"
	"net/url"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// TradeType specifies the payment scenario (QR code, H5 mobile, etc.).
type TradeType string

const (
	TradeTypePC     TradeType = "pc"     // Desktop web redirect (Alipay page pay)
	TradeTypeWAP    TradeType = "wap"    // Mobile browser redirect (Alipay WAP / WeChat H5)
	TradeTypeNative TradeType = "native" // QR code (Alipay precreate / WeChat Native)
	TradeTypeJSAPI  TradeType = "jsapi"  // In-app (WeChat mini-program / official account)
)

// Provider is the minimal interface every payment backend must implement.
type Provider interface {
	// Name returns the canonical provider identifier (e.g. "alipay", "wechat_native", "stripe").
	Name() string

	// CreateCheckout creates a hosted payment page and returns:
	//   payURL    – URL to redirect / QR-encode for the user
	//   externalID – provider-side transaction reference (stored for reconciliation)
	CreateCheckout(ctx context.Context, o *entity.PaymentOrder, returnURL string) (payURL, externalID string, err error)
}

// Refunder is an optional interface for providers that support refunds
// back to the original payment method (not just wallet credit).
type Refunder interface {
	// Refund initiates a refund at the payment provider level.
	// refundNo is the platform refund number, orderNo is the original order,
	// refundAmount/totalAmount are in CNY.
	Refund(ctx context.Context, refundNo, orderNo string, refundAmount, totalAmount float64) error
}

// NotifyHandler is an optional interface for providers that handle async
// notifications via POST (Alipay/WeChat v3 standard).
type NotifyHandler interface {
	// HandleNotify parses and verifies an async payment notification from the provider.
	// Returns the platform order number and whether the notification is valid.
	HandleNotify(req *http.Request) (orderNo string, ok bool, err error)
}

// EpayCallbackVerifier is an optional interface for providers that use
// GET-based async notification (易支付 standard).
type EpayCallbackVerifier interface {
	VerifyCallback(params url.Values) (orderNo string, ok bool)
}

// WebhookVerifier is an optional interface for providers that use
// POST-based webhook with a signature header.
type WebhookVerifier interface {
	VerifyWebhook(payload []byte, sig string) (orderNo string, ok bool)
}
