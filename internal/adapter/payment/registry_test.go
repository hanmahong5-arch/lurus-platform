package payment

import (
	"context"
	"errors"
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// stubProvider is a minimal Provider for testing the registry.
type stubProvider struct {
	name    string
	fail    bool   // if true, CreateCheckout returns an error
	failErr error  // custom error to return on failure
}

func (s *stubProvider) Name() string { return s.name }

func (s *stubProvider) CreateCheckout(_ context.Context, o *entity.PaymentOrder, _ string) (string, string, error) {
	if s.fail {
		if s.failErr != nil {
			return "", "", s.failErr
		}
		return "", "", errors.New("provider error")
	}
	return "https://pay.example.com/" + o.OrderNo, "ext-" + o.OrderNo, nil
}

func TestRegistry_Checkout_Success(t *testing.T) {
	r := NewRegistry()
	p := &stubProvider{name: "test"}
	r.Register("test", p, MethodInfo{ID: "test_pay", Name: "Test", Provider: "test", Type: "redirect"})

	order := &entity.PaymentOrder{OrderNo: "ORD-001", PaymentMethod: "test_pay"}
	url, extID, err := r.Checkout(context.Background(), order, "https://return.url")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != "https://pay.example.com/ORD-001" {
		t.Errorf("payURL = %q, want https://pay.example.com/ORD-001", url)
	}
	if extID != "ext-ORD-001" {
		t.Errorf("externalID = %q, want ext-ORD-001", extID)
	}
}

func TestRegistry_Checkout_UnknownMethod(t *testing.T) {
	r := NewRegistry()
	order := &entity.PaymentOrder{OrderNo: "ORD-001", PaymentMethod: "nonexistent"}
	_, _, err := r.Checkout(context.Background(), order, "")
	if err == nil {
		t.Fatal("expected error for unknown method")
	}
	var pe *ProviderNotAvailableError
	if !errors.As(err, &pe) {
		t.Errorf("expected ProviderNotAvailableError, got %T: %v", err, err)
	}
}

func TestRegistry_CircuitBreaker_TripsAfterConsecutiveFailures(t *testing.T) {
	r := NewRegistry()
	p := &stubProvider{name: "flaky", fail: true}
	r.Register("flaky", p, MethodInfo{ID: "flaky_pay", Name: "Flaky", Provider: "flaky", Type: "redirect"})

	order := &entity.PaymentOrder{OrderNo: "ORD-CB", PaymentMethod: "flaky_pay"}

	// First cbFailures (5) calls should return the provider error.
	for i := 0; i < int(cbFailures); i++ {
		_, _, err := r.Checkout(context.Background(), order, "")
		if err == nil {
			t.Fatalf("call %d: expected error", i)
		}
		// Should be the provider's error, not circuit open yet.
		var ce *ProviderCircuitOpenError
		if errors.As(err, &ce) {
			t.Fatalf("call %d: circuit opened too early", i)
		}
	}

	// Next call should get ProviderCircuitOpenError (breaker is open).
	_, _, err := r.Checkout(context.Background(), order, "")
	if err == nil {
		t.Fatal("expected circuit open error")
	}
	var ce *ProviderCircuitOpenError
	if !errors.As(err, &ce) {
		t.Errorf("expected ProviderCircuitOpenError, got %T: %v", err, err)
	}
	if ce.Provider != "flaky" {
		t.Errorf("circuit open provider = %q, want flaky", ce.Provider)
	}
}

func TestRegistry_CircuitBreaker_ContextCancelled_DoesNotCountAsFailure(t *testing.T) {
	r := NewRegistry()
	p := &stubProvider{name: "ctx", fail: true, failErr: context.Canceled}
	r.Register("ctx", p, MethodInfo{ID: "ctx_pay", Name: "Ctx", Provider: "ctx", Type: "redirect"})

	order := &entity.PaymentOrder{OrderNo: "ORD-CTX", PaymentMethod: "ctx_pay"}

	// Send more than cbFailures context.Canceled errors — should NOT trip the breaker.
	for i := 0; i < int(cbFailures)+3; i++ {
		_, _, err := r.Checkout(context.Background(), order, "")
		if err == nil {
			t.Fatalf("call %d: expected error", i)
		}
		// Should never be a circuit open error.
		var ce *ProviderCircuitOpenError
		if errors.As(err, &ce) {
			t.Fatalf("call %d: circuit should not open for context.Canceled", i)
		}
	}
}

func TestRegistry_ListMethods(t *testing.T) {
	r := NewRegistry()
	r.Register("a", &stubProvider{name: "a"},
		MethodInfo{ID: "a1", Name: "A1", Provider: "a", Type: "qr"},
		MethodInfo{ID: "a2", Name: "A2", Provider: "a", Type: "redirect"},
	)
	r.Register("b", &stubProvider{name: "b"},
		MethodInfo{ID: "b1", Name: "B1", Provider: "b", Type: "redirect"},
	)

	methods := r.ListMethods()
	if len(methods) != 3 {
		t.Fatalf("len(methods) = %d, want 3", len(methods))
	}
	ids := make([]string, len(methods))
	for i, m := range methods {
		ids[i] = m.ID
	}
	if ids[0] != "a1" || ids[1] != "a2" || ids[2] != "b1" {
		t.Errorf("method IDs = %v, want [a1, a2, b1]", ids)
	}
}

func TestRegistry_HasMethod(t *testing.T) {
	r := NewRegistry()
	r.Register("x", &stubProvider{name: "x"},
		MethodInfo{ID: "x_pay", Name: "X", Provider: "x", Type: "redirect"})

	if !r.HasMethod("x_pay") {
		t.Error("HasMethod(x_pay) = false, want true")
	}
	if r.HasMethod("nonexistent") {
		t.Error("HasMethod(nonexistent) = true, want false")
	}
}

func TestRegistry_Get(t *testing.T) {
	r := NewRegistry()
	p := &stubProvider{name: "demo"}
	r.Register("demo", p)

	got, ok := r.Get("demo")
	if !ok || got != p {
		t.Error("Get(demo) failed")
	}
	_, ok = r.Get("missing")
	if ok {
		t.Error("Get(missing) should return false")
	}
}

func TestRegistry_ProviderStatuses(t *testing.T) {
	r := NewRegistry()
	r.Register("a", &stubProvider{name: "a"})
	r.Register("b", &stubProvider{name: "b"})

	statuses := r.ProviderStatuses()
	if len(statuses) != 2 {
		t.Fatalf("len(statuses) = %d, want 2", len(statuses))
	}
	for _, s := range statuses {
		if s.State != "closed" {
			t.Errorf("provider %s state = %q, want closed", s.Name, s.State)
		}
	}
}

func TestRegistry_RegisterSameNameTwice_AppendsMethods(t *testing.T) {
	r := NewRegistry()
	p := &stubProvider{name: "epay"}
	r.Register("epay", p, MethodInfo{ID: "epay_alipay", Name: "AliPay", Provider: "epay", Type: "qr"})
	r.Register("epay", p, MethodInfo{ID: "epay_wechat", Name: "WeChat", Provider: "epay", Type: "qr"})

	if len(r.ListMethods()) != 2 {
		t.Errorf("expected 2 methods, got %d", len(r.ListMethods()))
	}
	if !r.HasMethod("epay_alipay") || !r.HasMethod("epay_wechat") {
		t.Error("both epay methods should be registered")
	}
}

func TestRegistry_Fallback_OnCircuitOpen(t *testing.T) {
	r := NewRegistry()
	// Primary: flaky alipay that always fails.
	primary := &stubProvider{name: "alipay", fail: true}
	r.Register("alipay", primary, MethodInfo{ID: "alipay_qr", Name: "Alipay QR", Provider: "alipay", Type: "qr"})
	// Fallback: working epay.
	fallback := &stubProvider{name: "epay"}
	r.Register("epay", fallback, MethodInfo{ID: "epay_alipay", Name: "Epay Alipay", Provider: "epay", Type: "qr"})
	r.SetFallback("alipay_qr", "epay", "epay_alipay")

	order := &entity.PaymentOrder{OrderNo: "ORD-FB", PaymentMethod: "alipay_qr"}

	// Trip the primary's circuit breaker.
	for i := 0; i < int(cbFailures); i++ {
		_, _, _ = r.Checkout(context.Background(), order, "")
	}

	// Next call: primary is circuit-open → should fallback to epay.
	url, _, err := r.Checkout(context.Background(), order, "")
	if err != nil {
		t.Fatalf("expected fallback to succeed, got error: %v", err)
	}
	if url != "https://pay.example.com/ORD-FB" {
		t.Errorf("payURL = %q, want fallback URL", url)
	}
	// PaymentMethod should be restored to original after fallback.
	if order.PaymentMethod != "alipay_qr" {
		t.Errorf("PaymentMethod mutated to %q, want alipay_qr", order.PaymentMethod)
	}
}

func TestRegistry_Fallback_NotTriggered_OnNormalError(t *testing.T) {
	r := NewRegistry()
	// Primary fails but circuit is not yet open.
	primary := &stubProvider{name: "alipay", fail: true}
	r.Register("alipay", primary, MethodInfo{ID: "alipay_qr", Name: "Alipay QR", Provider: "alipay", Type: "qr"})
	fallback := &stubProvider{name: "epay"}
	r.Register("epay", fallback)
	r.SetFallback("alipay_qr", "epay", "epay_alipay")

	order := &entity.PaymentOrder{OrderNo: "ORD-NF", PaymentMethod: "alipay_qr"}
	// Single failure: should return the provider error directly, NOT fall back.
	_, _, err := r.Checkout(context.Background(), order, "")
	if err == nil {
		t.Fatal("expected error on first failure")
	}
	var ce *ProviderCircuitOpenError
	if errors.As(err, &ce) {
		t.Error("should not get ProviderCircuitOpenError on first failure")
	}
}

func TestRegistry_Fallback_NoFallbackConfigured(t *testing.T) {
	r := NewRegistry()
	primary := &stubProvider{name: "stripe", fail: true}
	r.Register("stripe", primary, MethodInfo{ID: "stripe", Name: "Stripe", Provider: "stripe", Type: "redirect"})
	// No fallback set for stripe.

	order := &entity.PaymentOrder{OrderNo: "ORD-NF2", PaymentMethod: "stripe"}
	// Trip circuit.
	for i := 0; i < int(cbFailures); i++ {
		_, _, _ = r.Checkout(context.Background(), order, "")
	}
	// Circuit open, no fallback → should get ProviderCircuitOpenError.
	_, _, err := r.Checkout(context.Background(), order, "")
	var ce *ProviderCircuitOpenError
	if !errors.As(err, &ce) {
		t.Errorf("expected ProviderCircuitOpenError, got %T: %v", err, err)
	}
}
