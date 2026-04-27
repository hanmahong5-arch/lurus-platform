package payment

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// --- HasProvider ---

func TestRegistry_HasProvider_Registered(t *testing.T) {
	r := NewRegistry()
	r.Register("mypay", &stubProvider{name: "mypay"})
	if !r.HasProvider("mypay") {
		t.Error("HasProvider(mypay) = false, want true")
	}
}

func TestRegistry_HasProvider_NotRegistered(t *testing.T) {
	r := NewRegistry()
	if r.HasProvider("ghost") {
		t.Error("HasProvider(ghost) = true, want false")
	}
}

func TestRegistry_HasProvider_AfterRegister(t *testing.T) {
	r := NewRegistry()
	if r.HasProvider("newpay") {
		t.Error("HasProvider before registration should be false")
	}
	r.Register("newpay", &stubProvider{name: "newpay"})
	if !r.HasProvider("newpay") {
		t.Error("HasProvider after registration should be true")
	}
}

// --- QueryOrder ---

// stubQuerier is a Provider that also implements OrderQuerier for testing.
type stubQuerier struct {
	stubProvider
	result *OrderQueryResult
	err    error
}

func (s *stubQuerier) QueryOrder(_ context.Context, _ string) (*OrderQueryResult, error) {
	return s.result, s.err
}

func TestRegistry_QueryOrder_ProviderNotFound(t *testing.T) {
	r := NewRegistry()
	result, err := r.QueryOrder(context.Background(), "nonexistent", "LO-001")
	if err != nil {
		t.Fatalf("expected nil error for unknown provider, got: %v", err)
	}
	if result != nil {
		t.Error("expected nil result for unknown provider")
	}
}

func TestRegistry_QueryOrder_ProviderDoesNotImplementQuerier(t *testing.T) {
	r := NewRegistry()
	// stubProvider does not implement OrderQuerier.
	r.Register("nq", &stubProvider{name: "nq"})

	result, err := r.QueryOrder(context.Background(), "nq", "LO-001")
	if err != nil {
		t.Fatalf("expected nil error for non-querier provider, got: %v", err)
	}
	if result != nil {
		t.Error("expected nil result for non-querier provider")
	}
}

func TestRegistry_QueryOrder_ProviderReturnsSuccess(t *testing.T) {
	r := NewRegistry()
	q := &stubQuerier{
		stubProvider: stubProvider{name: "querier"},
		result:       &OrderQueryResult{Paid: true, Amount: 100.0},
	}
	r.Register("querier", q)

	result, err := r.QueryOrder(context.Background(), "querier", "LO-001")
	if err != nil {
		t.Fatalf("QueryOrder: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !result.Paid {
		t.Error("expected Paid=true")
	}
	if result.Amount != 100.0 {
		t.Errorf("Amount = %v, want 100.0", result.Amount)
	}
}

func TestRegistry_QueryOrder_ProviderReturnsError(t *testing.T) {
	r := NewRegistry()
	q := &stubQuerier{
		stubProvider: stubProvider{name: "querier-err"},
		err:          errors.New("api timeout"),
	}
	r.Register("querier-err", q)

	_, err := r.QueryOrder(context.Background(), "querier-err", "LO-002")
	if err == nil {
		t.Fatal("expected error from querier")
	}
	if !errors.Is(err, q.err) {
		t.Errorf("expected wrapped api timeout error, got: %v", err)
	}
}

func TestRegistry_QueryOrder_NotPaid(t *testing.T) {
	r := NewRegistry()
	q := &stubQuerier{
		stubProvider: stubProvider{name: "qp"},
		result:       &OrderQueryResult{Paid: false, Amount: 0},
	}
	r.Register("qp", q)

	result, err := r.QueryOrder(context.Background(), "qp", "LO-PENDING")
	if err != nil {
		t.Fatalf("QueryOrder: %v", err)
	}
	if result.Paid {
		t.Error("expected Paid=false")
	}
}

// --- Error types ---

func TestProviderNotAvailableError_Message(t *testing.T) {
	e := &ProviderNotAvailableError{Method: "alipay_qr"}
	if e.Error() != "Payment provider not available: alipay_qr" {
		t.Errorf("Error() = %q", e.Error())
	}
}

func TestProviderCircuitOpenError_Message(t *testing.T) {
	e := &ProviderCircuitOpenError{Provider: "stripe"}
	if e.Error() != "Payment provider temporarily unavailable (circuit open): stripe" {
		t.Errorf("Error() = %q", e.Error())
	}
}

// --- Concurrent access ---

func TestRegistry_Concurrent_Checkout(t *testing.T) {
	r := NewRegistry()
	p := &stubProvider{name: "concurrent"}
	r.Register("concurrent", p, MethodInfo{
		ID:       "concurrent_pay",
		Name:     "Concurrent",
		Provider: "concurrent",
		Type:     "redirect",
	})

	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			order := &entity.PaymentOrder{
				OrderNo:       "ORD-CONC",
				PaymentMethod: "concurrent_pay",
			}
			_, _, err := r.Checkout(context.Background(), order, "https://return.url")
			if err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent checkout error: %v", err)
	}
}

func TestRegistry_Concurrent_ListMethods(t *testing.T) {
	r := NewRegistry()
	for i := 0; i < 5; i++ {
		name := "p" + string(rune('a'+i))
		r.Register(name, &stubProvider{name: name},
			MethodInfo{ID: name + "_pay", Name: name, Provider: name, Type: "redirect"},
		)
	}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			methods := r.ListMethods()
			if len(methods) != 5 {
				t.Errorf("concurrent ListMethods: got %d methods, want 5", len(methods))
			}
		}()
	}
	wg.Wait()
}

// --- Re-register provider: replaces provider but keeps circuit breaker ---

func TestRegistry_ReRegister_ReplacesProvider(t *testing.T) {
	r := NewRegistry()
	p1 := &stubProvider{name: "replaceable", fail: true} // always fails
	r.Register("replaceable", p1, MethodInfo{ID: "rep_pay", Name: "Rep", Provider: "replaceable", Type: "redirect"})

	// Replace with a working provider.
	p2 := &stubProvider{name: "replaceable", fail: false}
	r.Register("replaceable", p2) // no new methods, just replaces

	// Checkout should now succeed.
	order := &entity.PaymentOrder{OrderNo: "ORD-REP", PaymentMethod: "rep_pay"}
	url, _, err := r.Checkout(context.Background(), order, "")
	if err != nil {
		t.Fatalf("expected success after re-register: %v", err)
	}
	if url == "" {
		t.Error("expected non-empty URL")
	}
}

// --- Multiple method IDs for one provider ---

func TestRegistry_MultipleMethodsForOneProvider(t *testing.T) {
	r := NewRegistry()
	p := &stubProvider{name: "multi"}
	r.Register("multi", p,
		MethodInfo{ID: "multi_a", Name: "A", Provider: "multi", Type: "redirect"},
		MethodInfo{ID: "multi_b", Name: "B", Provider: "multi", Type: "qr"},
		MethodInfo{ID: "multi_c", Name: "C", Provider: "multi", Type: "redirect"},
	)

	for _, methodID := range []string{"multi_a", "multi_b", "multi_c"} {
		order := &entity.PaymentOrder{OrderNo: "ORD-" + methodID, PaymentMethod: methodID}
		_, _, err := r.Checkout(context.Background(), order, "")
		if err != nil {
			t.Errorf("Checkout(%s): %v", methodID, err)
		}
	}
}

// --- SetFallback does not affect non-circuit-open errors ---

func TestRegistry_SetFallback_FallbackProviderMissing(t *testing.T) {
	r := NewRegistry()
	primary := &stubProvider{name: "primary", fail: true}
	r.Register("primary", primary, MethodInfo{ID: "pay_a", Name: "A", Provider: "primary", Type: "redirect"})
	// SetFallback to a provider that is NOT registered.
	r.SetFallback("pay_a", "ghost_provider", "ghost_pay")

	order := &entity.PaymentOrder{OrderNo: "ORD-GHOST-FB", PaymentMethod: "pay_a"}

	// Trip the primary circuit.
	for i := 0; i < int(cbFailures); i++ {
		_, _, _ = r.Checkout(context.Background(), order, "")
	}

	// On circuit open, fallback tries "ghost_provider" which doesn't exist.
	// checkoutVia returns ProviderNotAvailableError for the fallback.
	_, _, err := r.Checkout(context.Background(), order, "")
	if err == nil {
		t.Fatal("expected error when fallback provider not registered")
	}
}

// --- ProviderStatuses with multiple providers ---

func TestRegistry_ProviderStatuses_AllClosed(t *testing.T) {
	r := NewRegistry()
	for _, name := range []string{"p1", "p2", "p3"} {
		r.Register(name, &stubProvider{name: name})
	}
	statuses := r.ProviderStatuses()
	if len(statuses) != 3 {
		t.Fatalf("len(statuses) = %d, want 3", len(statuses))
	}
	for _, s := range statuses {
		if s.State != "closed" {
			t.Errorf("provider %s state = %q, want closed", s.Name, s.State)
		}
		if s.Counts.TotalFailures != 0 {
			t.Errorf("provider %s has unexpected failures", s.Name)
		}
	}
}

func TestRegistry_ProviderStatuses_EmptyRegistry(t *testing.T) {
	r := NewRegistry()
	statuses := r.ProviderStatuses()
	if len(statuses) != 0 {
		t.Errorf("empty registry should have 0 statuses, got %d", len(statuses))
	}
}
