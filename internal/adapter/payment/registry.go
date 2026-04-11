// Package payment — Registry is the centralized store for all payment providers.
// It replaces per-handler provider fields and duplicate resolveCheckout logic.
package payment

import (
	"context"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// MethodInfo describes a payment method exposed by a provider.
type MethodInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Type     string `json:"type"` // "redirect" or "qr"
}

// ProviderNotAvailableError is returned when a payment method has no registered provider.
type ProviderNotAvailableError struct {
	Method string
}

func (e *ProviderNotAvailableError) Error() string {
	return "Payment provider not available: " + e.Method
}

// Registry stores all payment providers and routes checkout by payment method.
type Registry struct {
	providers map[string]Provider   // name → Provider (for webhook handler lookups)
	routing   map[string]Provider   // method_id → Provider (for checkout routing)
	methods   []MethodInfo          // ordered list of exposed methods
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]Provider),
		routing:   make(map[string]Provider),
	}
}

// Register adds a provider under the given name (for webhook lookups).
// Optional methods are added to the checkout routing table and the exposed method list.
// Safe to call multiple times for the same name (appends methods, overwrites provider ref).
func (r *Registry) Register(name string, p Provider, methods ...MethodInfo) {
	r.providers[name] = p
	for _, m := range methods {
		r.routing[m.ID] = p
		r.methods = append(r.methods, m)
	}
}

// Checkout routes the order to the correct payment provider and calls CreateCheckout.
// Returns ProviderNotAvailableError if no provider is registered for the order's PaymentMethod.
func (r *Registry) Checkout(ctx context.Context, order *entity.PaymentOrder, returnURL string) (payURL, externalID string, err error) {
	p, ok := r.routing[order.PaymentMethod]
	if !ok {
		return "", "", &ProviderNotAvailableError{Method: order.PaymentMethod}
	}
	return p.CreateCheckout(ctx, order, returnURL)
}

// ListMethods returns all registered payment methods in registration order.
func (r *Registry) ListMethods() []MethodInfo {
	return r.methods
}

// HasMethod returns whether a method is registered for checkout.
func (r *Registry) HasMethod(methodID string) bool {
	_, ok := r.routing[methodID]
	return ok
}

// Get returns a provider by registration name (for webhook handlers to type-assert).
func (r *Registry) Get(name string) (Provider, bool) {
	p, ok := r.providers[name]
	return p, ok
}

// HasProvider returns whether a provider with the given name is registered.
func (r *Registry) HasProvider(name string) bool {
	_, ok := r.providers[name]
	return ok
}
