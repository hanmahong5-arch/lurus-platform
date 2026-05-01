package module

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// fast retry policy for tests — zero backoff so failure tests don't sleep.
func fastRetry() RetryPolicy {
	return RetryPolicy{
		MaxAttempts:    3,
		InitialBackoff: 1 * time.Millisecond,
		MaxBackoff:     1 * time.Millisecond,
		JitterFraction: 0,
	}
}

func TestRegistry_FireAccountCreated(t *testing.T) {
	r := NewRegistry().WithRetryPolicy(fastRetry())
	var called int
	r.OnAccountCreated("test-a", func(ctx context.Context, a *entity.Account) error {
		called++
		return nil
	})
	r.OnAccountCreated("test-b", func(ctx context.Context, a *entity.Account) error {
		called++
		return nil
	})
	r.FireAccountCreated(context.Background(), &entity.Account{ID: 1, DisplayName: "test"})
	if called != 2 {
		t.Errorf("expected 2 hooks called, got %d", called)
	}
}

func TestRegistry_FireAccountCreated_ErrorDoesNotBlock(t *testing.T) {
	r := NewRegistry().WithRetryPolicy(fastRetry())
	var secondCalled bool
	r.OnAccountCreated("failing", func(ctx context.Context, a *entity.Account) error {
		return errors.New("hook failed")
	})
	r.OnAccountCreated("ok", func(ctx context.Context, a *entity.Account) error {
		secondCalled = true
		return nil
	})
	r.FireAccountCreated(context.Background(), &entity.Account{ID: 1})
	if !secondCalled {
		t.Error("second hook should still be called despite first hook failure")
	}
}

func TestRegistry_FireAccountDeleted(t *testing.T) {
	r := NewRegistry().WithRetryPolicy(fastRetry())
	var called bool
	r.OnAccountDeleted("test", func(ctx context.Context, a *entity.Account) error {
		called = true
		return nil
	})
	r.FireAccountDeleted(context.Background(), &entity.Account{ID: 1})
	if !called {
		t.Error("expected hook to be called")
	}
}

func TestRegistry_FirePlanChanged(t *testing.T) {
	r := NewRegistry().WithRetryPolicy(fastRetry())
	var receivedPlanCode string
	r.OnPlanChanged("test", func(ctx context.Context, a *entity.Account, p *entity.ProductPlan) error {
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
	r.OnAccountCreated("a", func(ctx context.Context, a *entity.Account) error { return nil })
	r.OnAccountDeleted("b", func(ctx context.Context, a *entity.Account) error { return nil })
	r.OnPlanChanged("c", func(ctx context.Context, a *entity.Account, p *entity.ProductPlan) error { return nil })
	if r.HookCount() != 3 {
		t.Errorf("expected 3, got %d", r.HookCount())
	}
}

// ── FireCheckin ───────────────────────────────────────────────────────────

func TestRegistry_FireCheckin(t *testing.T) {
	r := NewRegistry().WithRetryPolicy(fastRetry())
	var gotAccountID int64
	var gotStreak int
	r.OnCheckin("test", func(ctx context.Context, accountID int64, streak int) error {
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
	r := NewRegistry().WithRetryPolicy(fastRetry())
	var secondCalled bool
	r.OnCheckin("failing", func(ctx context.Context, accountID int64, streak int) error {
		return errors.New("hook failed")
	})
	r.OnCheckin("ok", func(ctx context.Context, accountID int64, streak int) error {
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
	r := NewRegistry().WithRetryPolicy(fastRetry())
	var gotReferrerID int64
	var gotReferredName string
	r.OnReferralSignup("test", func(ctx context.Context, referrerAccountID int64, referredName string) error {
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
	r := NewRegistry().WithRetryPolicy(fastRetry())
	var secondCalled bool
	r.OnReferralSignup("failing", func(ctx context.Context, referrerAccountID int64, referredName string) error {
		return errors.New("hook failed")
	})
	r.OnReferralSignup("ok", func(ctx context.Context, referrerAccountID int64, referredName string) error {
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
	r := NewRegistry().WithRetryPolicy(fastRetry())
	var secondCalled bool
	r.OnAccountDeleted("failing", func(ctx context.Context, a *entity.Account) error {
		return errors.New("hook failed")
	})
	r.OnAccountDeleted("ok", func(ctx context.Context, a *entity.Account) error {
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
	r := NewRegistry().WithRetryPolicy(fastRetry())
	var secondCalled bool
	r.OnPlanChanged("failing", func(ctx context.Context, a *entity.Account, p *entity.ProductPlan) error {
		return errors.New("hook failed")
	})
	r.OnPlanChanged("ok", func(ctx context.Context, a *entity.Account, p *entity.ProductPlan) error {
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
	r.OnAccountCreated("a", func(ctx context.Context, a *entity.Account) error { return nil })
	r.OnAccountDeleted("b", func(ctx context.Context, a *entity.Account) error { return nil })
	r.OnPlanChanged("c", func(ctx context.Context, a *entity.Account, p *entity.ProductPlan) error { return nil })
	r.OnCheckin("d", func(ctx context.Context, accountID int64, streak int) error { return nil })
	r.OnReferralSignup("e", func(ctx context.Context, referrerAccountID int64, referredName string) error { return nil })
	if r.HookCount() != 5 {
		t.Errorf("HookCount = %d, want 5 (all hook types)", r.HookCount())
	}
}

// ── Empty hook-name guard ────────────────────────────────────────────────

func TestRegistry_OnAccountCreated_EmptyNamePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on empty hook name")
		}
	}()
	r := NewRegistry()
	r.OnAccountCreated("", func(ctx context.Context, a *entity.Account) error { return nil })
}

// ── Retry / DLQ behaviour ────────────────────────────────────────────────

// memDLQ is a thread-safe in-memory DeadLetterStore for tests.
type memDLQ struct {
	mu      sync.Mutex
	rows    []*entity.HookFailure
	saveErr error
	nextID  int64
}

func (m *memDLQ) Save(_ context.Context, f *entity.HookFailure) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.saveErr != nil {
		return m.saveErr
	}
	m.nextID++
	f.ID = m.nextID
	m.rows = append(m.rows, f)
	return nil
}

func (m *memDLQ) List(_ context.Context, pendingOnly bool, limit, offset int) ([]entity.HookFailure, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]entity.HookFailure, 0, len(m.rows))
	for _, f := range m.rows {
		if pendingOnly && f.ReplayedAt != nil {
			continue
		}
		out = append(out, *f)
	}
	return out, int64(len(out)), nil
}

func (m *memDLQ) GetByID(_ context.Context, id int64) (*entity.HookFailure, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, f := range m.rows {
		if f.ID == id {
			cp := *f
			return &cp, nil
		}
	}
	return nil, nil
}

func (m *memDLQ) MarkReplayed(_ context.Context, id int64, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, f := range m.rows {
		if f.ID == id {
			t := at
			f.ReplayedAt = &t
			return nil
		}
	}
	return errors.New("not found")
}

// fakeMetrics counts outcome events.
type fakeMetrics struct {
	mu       sync.Mutex
	outcomes []string
}

func (f *fakeMetrics) RecordHookOutcome(event, hook, result string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.outcomes = append(f.outcomes, event+"|"+hook+"|"+result)
}
func (f *fakeMetrics) SetDLQDepth(int64) {}

func TestRegistry_RetryThenSucceed_NoDLQ(t *testing.T) {
	r := NewRegistry().WithRetryPolicy(fastRetry())
	dlq := &memDLQ{}
	mx := &fakeMetrics{}
	r.WithDLQ(dlq).WithMetrics(mx)

	var calls int
	r.OnAccountCreated("flaky", func(ctx context.Context, a *entity.Account) error {
		calls++
		if calls < 2 {
			return errors.New("transient")
		}
		return nil
	})

	r.FireAccountCreated(context.Background(), &entity.Account{ID: 7})
	if calls != 2 {
		t.Errorf("expected 2 attempts, got %d", calls)
	}
	if len(dlq.rows) != 0 {
		t.Errorf("expected no DLQ row on retry-success, got %d", len(dlq.rows))
	}
	if got := mx.outcomes; len(got) != 1 || got[0] != "account_created|flaky|retry_succeeded" {
		t.Errorf("unexpected metrics: %v", got)
	}
}

func TestRegistry_AllAttemptsFail_LandsInDLQ(t *testing.T) {
	r := NewRegistry().WithRetryPolicy(fastRetry())
	dlq := &memDLQ{}
	mx := &fakeMetrics{}
	r.WithDLQ(dlq).WithMetrics(mx)

	var calls int
	r.OnAccountCreated("permanent", func(ctx context.Context, a *entity.Account) error {
		calls++
		return errors.New("nope")
	})

	r.FireAccountCreated(context.Background(), &entity.Account{ID: 99})
	if calls != 3 {
		t.Errorf("expected 3 attempts, got %d", calls)
	}
	if len(dlq.rows) != 1 {
		t.Fatalf("expected 1 DLQ row, got %d", len(dlq.rows))
	}
	row := dlq.rows[0]
	if row.Event != "account_created" || row.HookName != "permanent" {
		t.Errorf("unexpected DLQ row metadata: event=%s hook=%s", row.Event, row.HookName)
	}
	if row.AccountID == nil || *row.AccountID != 99 {
		t.Errorf("DLQ row account_id = %v, want 99", row.AccountID)
	}
	if row.Attempts != 3 {
		t.Errorf("DLQ row attempts = %d, want 3", row.Attempts)
	}
	if row.Error != "nope" {
		t.Errorf("DLQ row error = %q, want nope", row.Error)
	}
	// Payload should be valid JSON containing account_id.
	var payload map[string]any
	if err := json.Unmarshal(row.Payload, &payload); err != nil {
		t.Errorf("DLQ payload not valid JSON: %v", err)
	}
}

func TestRegistry_Replay_FetchesFreshAccount(t *testing.T) {
	r := NewRegistry().WithRetryPolicy(fastRetry())
	dlq := &memDLQ{}
	r.WithDLQ(dlq)

	// Account fetcher returns "fresh" name even though the DLQ row stored
	// the stale snapshot.
	r.WithAccountFetcher(func(ctx context.Context, id int64) (*entity.Account, error) {
		return &entity.Account{ID: id, DisplayName: "fresh"}, nil
	})

	var seenName string
	r.OnAccountCreated("hook-x", func(ctx context.Context, a *entity.Account) error {
		seenName = a.DisplayName
		return nil
	})

	row := &entity.HookFailure{
		Event:     "account_created",
		HookName:  "hook-x",
		AccountID: ptrInt64(7),
		Payload:   json.RawMessage(`{"display_name":"stale"}`),
	}
	if err := r.Replay(context.Background(), row); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if seenName != "fresh" {
		t.Errorf("hook saw display_name=%q, want fresh", seenName)
	}
}

func TestRegistry_Replay_AccountGone_ReturnsSentinel(t *testing.T) {
	r := NewRegistry().WithRetryPolicy(fastRetry())
	r.WithAccountFetcher(func(ctx context.Context, id int64) (*entity.Account, error) {
		return nil, nil
	})
	r.OnAccountCreated("hook-x", func(ctx context.Context, a *entity.Account) error {
		t.Fatal("hook should not be invoked when account is gone")
		return nil
	})
	row := &entity.HookFailure{
		Event:     "account_created",
		HookName:  "hook-x",
		AccountID: ptrInt64(404),
	}
	err := r.Replay(context.Background(), row)
	if !errors.Is(err, ErrAccountAlreadyGone) {
		t.Errorf("expected ErrAccountAlreadyGone, got %v", err)
	}
}

func TestRegistry_Replay_HookNotRegistered(t *testing.T) {
	r := NewRegistry().WithRetryPolicy(fastRetry())
	r.WithAccountFetcher(func(ctx context.Context, id int64) (*entity.Account, error) {
		return &entity.Account{ID: id}, nil
	})
	row := &entity.HookFailure{
		Event:     "account_created",
		HookName:  "phantom",
		AccountID: ptrInt64(1),
	}
	err := r.Replay(context.Background(), row)
	if err == nil {
		t.Error("expected error on unregistered hook")
	}
}

func ptrInt64(v int64) *int64 { return &v }
