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

// ── FireCheckin ───────────────────────────────────────────────────────────

func TestRegistry_FireCheckin(t *testing.T) {
	r := NewRegistry()
	var gotAccountID int64
	var gotStreak int
	r.OnCheckin(func(ctx context.Context, accountID int64, streak int) error {
		gotAccountID = accountID
		gotStreak = streak
		return nil
	})
	r.FireCheckin(context.Background(), 42, 7)
	if gotAccountID != 42 {
		t.Errorf("accountID = %d, want 42", gotAccountID)
	}
	if gotStreak != 7 {
		t.Errorf("streak = %d, want 7", gotStreak)
	}
}

func TestRegistry_FireCheckin_ErrorDoesNotBlock(t *testing.T) {
	r := NewRegistry()
	var secondCalled bool
	r.OnCheckin(func(ctx context.Context, accountID int64, streak int) error {
		return errors.New("hook failed")
	})
	r.OnCheckin(func(ctx context.Context, accountID int64, streak int) error {
		secondCalled = true
		return nil
	})
	r.FireCheckin(context.Background(), 1, 3)
	if !secondCalled {
		t.Error("second checkin hook should still be called despite first failure")
	}
}

func TestRegistry_FireCheckin_NoHooks(t *testing.T) {
	r := NewRegistry()
	// Should not panic with no hooks registered.
	r.FireCheckin(context.Background(), 1, 1)
}

// ── FireReferralSignup ────────────────────────────────────────────────────

func TestRegistry_FireReferralSignup(t *testing.T) {
	r := NewRegistry()
	var gotReferrerID int64
	var gotReferredName string
	r.OnReferralSignup(func(ctx context.Context, referrerAccountID int64, referredName string) error {
		gotReferrerID = referrerAccountID
		gotReferredName = referredName
		return nil
	})
	r.FireReferralSignup(context.Background(), 10, "NewUser")
	if gotReferrerID != 10 {
		t.Errorf("referrerID = %d, want 10", gotReferrerID)
	}
	if gotReferredName != "NewUser" {
		t.Errorf("referredName = %s, want NewUser", gotReferredName)
	}
}

func TestRegistry_FireReferralSignup_ErrorDoesNotBlock(t *testing.T) {
	r := NewRegistry()
	var secondCalled bool
	r.OnReferralSignup(func(ctx context.Context, referrerAccountID int64, referredName string) error {
		return errors.New("hook failed")
	})
	r.OnReferralSignup(func(ctx context.Context, referrerAccountID int64, referredName string) error {
		secondCalled = true
		return nil
	})
	r.FireReferralSignup(context.Background(), 1, "User")
	if !secondCalled {
		t.Error("second referral hook should still be called despite first failure")
	}
}

func TestRegistry_FireReferralSignup_NoHooks(t *testing.T) {
	r := NewRegistry()
	r.FireReferralSignup(context.Background(), 1, "User")
}

// ── FireAccountDeleted error path ─────────────────────────────────────────

func TestRegistry_FireAccountDeleted_ErrorDoesNotBlock(t *testing.T) {
	r := NewRegistry()
	var secondCalled bool
	r.OnAccountDeleted(func(ctx context.Context, a *entity.Account) error {
		return errors.New("hook failed")
	})
	r.OnAccountDeleted(func(ctx context.Context, a *entity.Account) error {
		secondCalled = true
		return nil
	})
	r.FireAccountDeleted(context.Background(), &entity.Account{ID: 1})
	if !secondCalled {
		t.Error("second delete hook should be called despite first failure")
	}
}

// ── FirePlanChanged error path ────────────────────────────────────────────

func TestRegistry_FirePlanChanged_ErrorDoesNotBlock(t *testing.T) {
	r := NewRegistry()
	var secondCalled bool
	r.OnPlanChanged(func(ctx context.Context, a *entity.Account, p *entity.ProductPlan) error {
		return errors.New("hook failed")
	})
	r.OnPlanChanged(func(ctx context.Context, a *entity.Account, p *entity.ProductPlan) error {
		secondCalled = true
		return nil
	})
	r.FirePlanChanged(context.Background(), &entity.Account{ID: 1}, &entity.ProductPlan{Code: "pro"})
	if !secondCalled {
		t.Error("second plan hook should be called despite first failure")
	}
}

// ── HookCount with all hook types ─────────────────────────────────────────

func TestRegistry_HookCount_AllTypes(t *testing.T) {
	r := NewRegistry()
	r.OnAccountCreated(func(ctx context.Context, a *entity.Account) error { return nil })
	r.OnAccountDeleted(func(ctx context.Context, a *entity.Account) error { return nil })
	r.OnPlanChanged(func(ctx context.Context, a *entity.Account, p *entity.ProductPlan) error { return nil })
	r.OnCheckin(func(ctx context.Context, accountID int64, streak int) error { return nil })
	r.OnReferralSignup(func(ctx context.Context, referrerAccountID int64, referredName string) error { return nil })
	if r.HookCount() != 5 {
		t.Errorf("HookCount = %d, want 5 (all hook types)", r.HookCount())
	}
}
