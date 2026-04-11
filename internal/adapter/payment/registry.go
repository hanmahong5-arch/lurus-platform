// Package payment — Registry is the centralized store for all payment providers.
// It replaces per-handler provider fields and duplicate resolveCheckout logic.
// Each provider is wrapped with a circuit breaker that trips after consecutive
// failures, preventing cascading hangs when a provider is down.
package payment

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	gobreaker "github.com/sony/gobreaker/v2"

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

// ProviderCircuitOpenError is returned when the provider's circuit breaker is open.
type ProviderCircuitOpenError struct {
	Provider string
}

func (e *ProviderCircuitOpenError) Error() string {
	return "Payment provider temporarily unavailable (circuit open): " + e.Provider
}

// ProviderStatus describes the current state of a provider's circuit breaker.
type ProviderStatus struct {
	Name   string `json:"name"`
	State  string `json:"state"`  // "closed", "half-open", "open"
	Counts struct {
		Requests             uint32 `json:"requests"`
		TotalSuccesses       uint32 `json:"total_successes"`
		TotalFailures        uint32 `json:"total_failures"`
		ConsecutiveSuccesses uint32 `json:"consecutive_successes"`
		ConsecutiveFailures  uint32 `json:"consecutive_failures"`
	} `json:"counts"`
}

// Circuit breaker defaults.
const (
	cbMaxRequests = 1               // half-open: allow 1 probe request
	cbInterval    = 0               // don't reset counts on interval
	cbTimeout     = 30 * time.Second // time before retrying after open
	cbFailures    = 5               // consecutive failures to trip
)

// Prometheus metrics for circuit breaker state changes.
var (
	cbStateChanges = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "lurus",
			Subsystem: "payment",
			Name:      "circuit_breaker_state_changes_total",
			Help:      "Number of circuit breaker state transitions by provider and new state.",
		},
		[]string{"provider", "state"},
	)
	cbCheckoutTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "lurus",
			Subsystem: "payment",
			Name:      "checkout_total",
			Help:      "Total checkout attempts by provider and result.",
		},
		[]string{"provider", "result"},
	)
)

// providerEntry holds a provider and its circuit breaker.
type providerEntry struct {
	provider Provider
	breaker  *gobreaker.CircuitBreaker[checkoutResult]
}

type checkoutResult struct {
	payURL     string
	externalID string
}

// FallbackRule defines a fallback provider to try when the primary is circuit-open.
type FallbackRule struct {
	Provider       string // registry name of fallback provider
	MethodOverride string // payment method to set on the order for the fallback (e.g., "epay_alipay")
}

// Registry stores all payment providers and routes checkout by payment method.
type Registry struct {
	entries   map[string]*providerEntry  // name → entry (provider + breaker)
	routing   map[string]string          // method_id → provider name
	fallbacks map[string]FallbackRule    // method_id → fallback rule
	methods   []MethodInfo               // ordered list of exposed methods
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		entries:   make(map[string]*providerEntry),
		routing:   make(map[string]string),
		fallbacks: make(map[string]FallbackRule),
	}
}

// Register adds a provider under the given name (for webhook lookups).
// Optional methods are added to the checkout routing table and the exposed method list.
// Safe to call multiple times for the same name (appends methods, reuses existing breaker).
func (r *Registry) Register(name string, p Provider, methods ...MethodInfo) {
	if _, exists := r.entries[name]; !exists {
		r.entries[name] = &providerEntry{
			provider: p,
			breaker:  newBreaker(name),
		}
	} else {
		r.entries[name].provider = p
	}
	for _, m := range methods {
		r.routing[m.ID] = name
		r.methods = append(r.methods, m)
	}
}

// SetFallback configures a fallback provider for a payment method.
// When the primary provider's circuit breaker is open, the fallback is tried.
// methodOverride replaces order.PaymentMethod for the fallback call (e.g., "epay_alipay").
func (r *Registry) SetFallback(methodID string, fallbackProvider, methodOverride string) {
	r.fallbacks[methodID] = FallbackRule{Provider: fallbackProvider, MethodOverride: methodOverride}
}

// Checkout routes the order to the correct payment provider and calls CreateCheckout
// through the provider's circuit breaker. On circuit-open, tries the fallback if configured.
func (r *Registry) Checkout(ctx context.Context, order *entity.PaymentOrder, returnURL string) (payURL, externalID string, err error) {
	providerName, ok := r.routing[order.PaymentMethod]
	if !ok {
		return "", "", &ProviderNotAvailableError{Method: order.PaymentMethod}
	}

	url, extID, err := r.checkoutVia(ctx, providerName, order, returnURL)
	if err != nil {
		// On circuit open, try fallback if available.
		var ce *ProviderCircuitOpenError
		if errors.As(err, &ce) {
			if fb, hasFB := r.fallbacks[order.PaymentMethod]; hasFB {
				slog.Warn("payment: primary circuit open, trying fallback",
					"method", order.PaymentMethod,
					"primary", providerName,
					"fallback", fb.Provider)
				cbCheckoutTotal.WithLabelValues(providerName, "fallback").Inc()
				return r.checkoutViaFallback(ctx, fb, order, returnURL)
			}
		}
		return "", "", err
	}
	return url, extID, nil
}

// checkoutVia executes a checkout through a specific provider's circuit breaker.
func (r *Registry) checkoutVia(ctx context.Context, providerName string, order *entity.PaymentOrder, returnURL string) (string, string, error) {
	entry, ok := r.entries[providerName]
	if !ok {
		return "", "", &ProviderNotAvailableError{Method: order.PaymentMethod}
	}

	result, err := entry.breaker.Execute(func() (checkoutResult, error) {
		url, extID, callErr := entry.provider.CreateCheckout(ctx, order, returnURL)
		if callErr != nil {
			return checkoutResult{}, callErr
		}
		return checkoutResult{payURL: url, externalID: extID}, nil
	})
	if err != nil {
		if errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests) {
			cbCheckoutTotal.WithLabelValues(providerName, "circuit_open").Inc()
			return "", "", &ProviderCircuitOpenError{Provider: providerName}
		}
		cbCheckoutTotal.WithLabelValues(providerName, "error").Inc()
		return "", "", err
	}
	cbCheckoutTotal.WithLabelValues(providerName, "success").Inc()
	return result.payURL, result.externalID, nil
}

// checkoutViaFallback calls the fallback provider with an overridden payment method.
func (r *Registry) checkoutViaFallback(ctx context.Context, fb FallbackRule, order *entity.PaymentOrder, returnURL string) (string, string, error) {
	// Temporarily override PaymentMethod for the fallback provider's dispatch logic.
	original := order.PaymentMethod
	order.PaymentMethod = fb.MethodOverride
	url, extID, err := r.checkoutVia(ctx, fb.Provider, order, returnURL)
	order.PaymentMethod = original // restore so the caller sees the original method
	return url, extID, err
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
	entry, ok := r.entries[name]
	if !ok {
		return nil, false
	}
	return entry.provider, true
}

// HasProvider returns whether a provider with the given name is registered.
func (r *Registry) HasProvider(name string) bool {
	_, ok := r.entries[name]
	return ok
}

// QueryOrder queries a provider for the status of an order.
// Returns (nil, nil) if the provider doesn't implement OrderQuerier.
func (r *Registry) QueryOrder(ctx context.Context, providerName, orderNo string) (*OrderQueryResult, error) {
	entry, ok := r.entries[providerName]
	if !ok {
		return nil, nil
	}
	querier, ok := entry.provider.(OrderQuerier)
	if !ok {
		return nil, nil // provider doesn't support order queries
	}
	return querier.QueryOrder(ctx, orderNo)
}

// ProviderStatuses returns the circuit breaker state of all registered providers.
func (r *Registry) ProviderStatuses() []ProviderStatus {
	statuses := make([]ProviderStatus, 0, len(r.entries))
	for name, entry := range r.entries {
		counts := entry.breaker.Counts()
		ps := ProviderStatus{
			Name:  name,
			State: entry.breaker.State().String(),
		}
		ps.Counts.Requests = counts.Requests
		ps.Counts.TotalSuccesses = counts.TotalSuccesses
		ps.Counts.TotalFailures = counts.TotalFailures
		ps.Counts.ConsecutiveSuccesses = counts.ConsecutiveSuccesses
		ps.Counts.ConsecutiveFailures = counts.ConsecutiveFailures
		statuses = append(statuses, ps)
	}
	return statuses
}

// newBreaker creates a circuit breaker with standard settings for a payment provider.
func newBreaker(name string) *gobreaker.CircuitBreaker[checkoutResult] {
	return gobreaker.NewCircuitBreaker[checkoutResult](gobreaker.Settings{
		Name:        fmt.Sprintf("payment:%s", name),
		MaxRequests: cbMaxRequests,
		Interval:    cbInterval,
		Timeout:     cbTimeout,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= cbFailures
		},
		IsSuccessful: func(err error) bool {
			// Context cancellations are the caller's fault, not the provider's.
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return true
			}
			return err == nil
		},
		OnStateChange: func(name string, from, to gobreaker.State) {
			slog.Warn("payment circuit breaker state change",
				"breaker", name, "from", from.String(), "to", to.String())
			cbStateChanges.WithLabelValues(name, to.String()).Inc()
		},
	})
}
