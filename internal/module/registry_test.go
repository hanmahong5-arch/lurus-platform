package module

import (
	"context"
	"errors"
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

func TestRegistry_FireAccountCreated(t *testing.T) {
	r := NewRegistry()
	var called int
	r.OnAccountCreated(func(ctx context.Context, a *entity.Account) error {
		called++
		return nil
	})
	r.OnAccountCreated(func(ctx context.Context, a *entity.Account) error {
		called++
		return nil
	})
	r.FireAccountCreated(context.Background(), &entity.Account{ID: 1, DisplayName: "test"})
	if called != 2 {
		t.Errorf("expected 2 hooks called, got %d", called)
	}
}

func TestRegistry_FireAccountCreated_ErrorDoesNotBlock(t *testing.T) {
	r := NewRegistry()
	var secondCalled bool
	r.OnAccountCreated(func(ctx context.Context, a *entity.Account) error {
		return errors.New("hook failed")
	})
	r.OnAccountCreated(func(ctx context.Context, a *entity.Account) error {
		secondCalled = true
		return nil
	})
	r.FireAccountCreated(context.Background(), &entity.Account{ID: 1})
	if !secondCalled {
		t.Error("second hook should still be called despite first hook failure")
	}
}

func TestRegistry_FireAccountDeleted(t *testing.T) {
	r := NewRegistry()
	var called bool
	r.OnAccountDeleted(func(ctx context.Context, a *entity.Account) error {
		called = true
		return nil
	})
	r.FireAccountDeleted(context.Background(), &entity.Account{ID: 1})
	if !called {
		t.Error("expected hook to be called")
	}
}

func TestRegistry_FirePlanChanged(t *testing.T) {
	r := NewRegistry()
	var receivedPlanCode string
	r.OnPlanChanged(func(ctx context.Context, a *entity.Account, p *entity.ProductPlan) error {
		receivedPlanCode = p.Code
		return nil
	})
	r.FirePlanChanged(context.Background(), &entity.Account{ID: 1}, &entity.ProductPlan{Code: "pro"})
	if receivedPlanCode != "pro" {
		t.Errorf("expected plan code 'pro', got '%s'", receivedPlanCode)
	}
}

func TestRegistry_NoHooks(t *testing.T) {
	r := NewRegistry()
	// Should not panic with no hooks registered.
	r.FireAccountCreated(context.Background(), &entity.Account{ID: 1})
	r.FireAccountDeleted(context.Background(), &entity.Account{ID: 1})
	r.FirePlanChanged(context.Background(), &entity.Account{ID: 1}, &entity.ProductPlan{Code: "free"})
}

func TestRegistry_HookCount(t *testing.T) {
	r := NewRegistry()
	if r.HookCount() != 0 {
		t.Errorf("expected 0, got %d", r.HookCount())
	}
	r.OnAccountCreated(func(ctx context.Context, a *entity.Account) error { return nil })
	r.OnAccountDeleted(func(ctx context.Context, a *entity.Account) error { return nil })
	r.OnPlanChanged(func(ctx context.Context, a *entity.Account, p *entity.ProductPlan) error { return nil })
	if r.HookCount() != 3 {
		t.Errorf("expected 3, got %d", r.HookCount())
	}
}
