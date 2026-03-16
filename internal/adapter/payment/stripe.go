package payment

import (
	"context"
	"fmt"

	stripe "github.com/stripe/stripe-go/v76"
	"github.com/stripe/stripe-go/v76/checkout/session"
	"github.com/stripe/stripe-go/v76/webhook"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

const (
	// stripeUSDRate is the approximate CNY → USD conversion rate.
	// In production this should come from a live FX feed; a conservative
	// rate is used here to avoid under-charging.
	stripeUSDRate = 7.1
)

// StripeProvider implements Provider for Stripe Checkout.
type StripeProvider struct {
	secretKey      string
	webhookSecret  string
}

// NewStripeProvider creates a StripeProvider.
// Returns nil if secret key is empty (feature disabled).
func NewStripeProvider(secretKey, webhookSecret string) *StripeProvider {
	if secretKey == "" {
		return nil
	}
	return &StripeProvider{secretKey: secretKey, webhookSecret: webhookSecret}
}

// Name returns the provider identifier.
func (p *StripeProvider) Name() string { return "stripe" }

// CreateCheckout creates a Stripe Checkout Session and returns the hosted URL.
func (p *StripeProvider) CreateCheckout(ctx context.Context, o *entity.PaymentOrder, returnURL string) (payURL, externalID string, err error) {
	stripe.Key = p.secretKey

	amountUSD := int64(o.AmountCNY / stripeUSDRate * 100) // convert to USD cents
	if amountUSD < 50 {                                    // Stripe minimum is $0.50
		amountUSD = 50
	}

	successURL := returnURL + "?order_no=" + o.OrderNo + "&status=success"
	cancelURL := returnURL + "?order_no=" + o.OrderNo + "&status=cancelled"

	params := &stripe.CheckoutSessionParams{
		ClientReferenceID: stripe.String(o.OrderNo),
		PaymentMethodTypes: stripe.StringSlice([]string{"card"}),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
					Currency: stripe.String("usd"),
					ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
						Name: stripe.String(fmt.Sprintf("Lurus 充值 %.2f CNY", o.AmountCNY)),
					},
					UnitAmount: stripe.Int64(amountUSD),
				},
				Quantity: stripe.Int64(1),
			},
		},
		Mode:       stripe.String(string(stripe.CheckoutSessionModePayment)),
		SuccessURL: stripe.String(successURL),
		CancelURL:  stripe.String(cancelURL),
	}

	s, err := session.New(params)
	if err != nil {
		return "", "", fmt.Errorf("stripe checkout session: %w", err)
	}
	return s.URL, s.ID, nil
}

// VerifyWebhook validates the Stripe webhook signature and extracts the order number + event ID.
// eventID is the Stripe event's unique identifier, suitable for deduplication.
func (p *StripeProvider) VerifyWebhook(payload []byte, sig string) (orderNo, eventID string, ok bool) {
	if p.webhookSecret == "" {
		return "", "", false
	}
	event, err := webhook.ConstructEvent(payload, sig, p.webhookSecret)
	if err != nil {
		return "", "", false
	}
	if event.Type != "checkout.session.completed" {
		return "", event.ID, true // valid event but not the one we care about
	}
	// Extract ClientReferenceID from the session object
	s, ok2 := event.Data.Object["client_reference_id"].(string)
	if !ok2 || s == "" {
		return "", event.ID, false
	}
	return s, event.ID, true
}
