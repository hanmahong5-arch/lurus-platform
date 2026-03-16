// Package payment defines the common interface for all payment providers.
package payment

import (
	"context"
	"net/url"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// Provider is the minimal interface every payment backend must implement.
type Provider interface {
	// Name returns the canonical provider identifier (e.g. "epay_alipay", "stripe").
	Name() string

	// CreateCheckout creates a hosted payment page and returns:
	//   payURL    – URL to redirect / QR-encode for the user
	//   externalID – provider-side transaction reference (stored for reconciliation)
	CreateCheckout(ctx context.Context, o *entity.PaymentOrder, returnURL string) (payURL, externalID string, err error)
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
