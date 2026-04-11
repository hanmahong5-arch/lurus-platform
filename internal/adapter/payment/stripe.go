package payment

import (
	"context"
	"fmt"

	stripe "github.com/stripe/stripe-go/v76"
	"github.com/stripe/stripe-go/v76/checkout/session"
	"github.com/stripe/stripe-go/v76/webhook"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// StripeProvider implements Provider for Stripe Checkout.
type StripeProvider struct {
	secretKey     string
	webhookSecret string
	usdRate       float64 // CNY → USD conversion rate (configured via STRIPE_USD_RATE)
}

// NewStripeProvider creates a StripeProvider.
// Returns nil if secret key is empty (feature disabled).
// usdRate is the CNY→USD rate used to convert order amounts to USD cents.
// Pass cfg.StripeUSDRate — it defaults to 7.1 when STRIPE_USD_RATE is unset.
func NewStripeProvider(secretKey, webhookSecret string, usdRate float64) *StripeProvider {
	if secretKey == "" {
		return nil
	}
	if usdRate <= 0 {
		usdRate = 7.1 // defensive fallback — should never be needed if config.Load() is used
	}
	p := &StripeProvider{secretKey: secretKey, webhookSecret: webhookSecret, usdRate: usdRate}
	// Set the API key once at construction time to avoid data races
	// when multiple goroutines call CreateCheckout concurrently.
	stripe.Key = secretKey
	return p
}

// Name returns the provider identifier.
func (p *StripeProvider) Name() string { return "stripe" }

// CreateCheckout creates a Stripe Checkout Session and returns the hosted URL.
func (p *StripeProvider) CreateCheckout(ctx context.Context, o *entity.PaymentOrder, returnURL string) (payURL, externalID string, err error) {
	amountUSD := int64(o.AmountCNY / p.usdRate * 100) // convert to USD cents using configured rate
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

// QueryOrder checks the payment status of a Stripe Checkout Session.
// orderNo is the platform order number; Stripe uses ClientReferenceID to store it.
// This method iterates recent sessions to find the match. For efficiency, prefer
// storing the Stripe session ID in PaymentOrder.ExternalID and calling QueryByExternalID.
func (p *StripeProvider) QueryOrder(ctx context.Context, orderNo string) (*OrderQueryResult, error) {
	stripe.Key = p.secretKey
	// If we have the session ID, retrieve directly. Otherwise we'd need to list sessions
	// which is expensive. Return an unsupported error to signal that ExternalID-based
	// lookup should be used instead.
	return nil, fmt.Errorf("stripe QueryOrder by orderNo not supported; use ExternalID (session ID)")
}

// QueryByExternalID checks a Stripe Checkout Session by its session ID.
func (p *StripeProvider) QueryByExternalID(ctx context.Context, sessionID string) (*OrderQueryResult, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("stripe: empty session ID")
	}
	stripe.Key = p.secretKey
	s, err := session.Get(sessionID, nil)
	if err != nil {
		return nil, fmt.Errorf("stripe get session: %w", err)
	}
	paid := s.PaymentStatus == stripe.CheckoutSessionPaymentStatusPaid
	var amount float64
	if paid {
		amount = float64(s.AmountTotal) / 100.0 // cents → dollars
	}
	return &OrderQueryResult{Paid: paid, Amount: amount}, nil
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
