package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// ── mockPurgeStore ───────────────────────────────────────────────────────────

// mockPurgeStore is an in-memory accountPurgeStore. Mirrors the
// production behaviour close enough for AccountService unit tests:
//   - BeginPurge fails ErrPurgeInFlight when an existing row for the
//     same account is still in 'purging' status.
//   - MarkCompleted / MarkFailed perform terminal status flips.
type mockPurgeStore struct {
	mu      sync.Mutex
	rows    map[int64]*entity.AccountPurge
	nextID  int64
	beginCB func(*entity.AccountPurge) error // optional override for failure-path tests
}

func newMockPurgeStore() *mockPurgeStore {
	return &mockPurgeStore{
		rows:   make(map[int64]*entity.AccountPurge),
		nextID: 1,
	}
}

func (m *mockPurgeStore) BeginPurge(_ context.Context, p *entity.AccountPurge) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.beginCB != nil {
		return m.beginCB(p)
	}
	for _, existing := range m.rows {
		if existing.AccountID == p.AccountID && existing.Status == entity.AccountPurgeStatusInflight {
			return ErrPurgeInFlight
		}
	}
	p.ID = m.nextID
	m.nextID++
	if p.Status == "" {
		p.Status = entity.AccountPurgeStatusInflight
	}
	p.StartedAt = time.Now().UTC()
	cp := *p
	m.rows[cp.ID] = &cp
	return nil
}

func (m *mockPurgeStore) MarkCompleted(_ context.Context, purgeID, approvedBy int64, completedAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.rows[purgeID]
	if !ok {
		return errors.New("not found")
	}
	row.Status = entity.AccountPurgeStatusCompleted
	row.ApprovedBy = &approvedBy
	row.CompletedAt = &completedAt
	return nil
}

func (m *mockPurgeStore) MarkFailed(_ context.Context, purgeID int64, errMsg string, completedAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.rows[purgeID]
	if !ok {
		return errors.New("not found")
	}
	row.Status = entity.AccountPurgeStatusFailed
	row.Error = errMsg
	row.CompletedAt = &completedAt
	return nil
}

func (m *mockPurgeStore) get(id int64) *entity.AccountPurge {
	m.mu.Lock()
	defer m.mu.Unlock()
	if row, ok := m.rows[id]; ok {
		cp := *row
		return &cp
	}
	return nil
}

// ── helpers ─────────────────────────────────────────────────────────────────

func newAccountSvcWithPurge(t *testing.T) (*AccountService, *mockAccountStore, *mockPurgeStore) {
	t.Helper()
	accountStore := newMockAccountStore()
	purgeStore := newMockPurgeStore()
	svc := NewAccountService(accountStore, newMockWalletStore(), newMockVIPStore(nil)).
		WithPurgeStore(purgeStore)
	return svc, accountStore, purgeStore
}

func seedActiveAccount(t *testing.T, svc *AccountService) *entity.Account {
	t.Helper()
	a, err := svc.UpsertByZitadelSub(context.Background(), "sub-purge-"+t.Name(), "user-"+t.Name()+"@e.com", "User", "")
	if err != nil {
		t.Fatalf("seed account: %v", err)
	}
	return a
}

// ── BeginPurge ──────────────────────────────────────────────────────────────

func TestAccountService_BeginPurge_Happy(t *testing.T) {
	svc, _, purges := newAccountSvcWithPurge(t)
	a := seedActiveAccount(t, svc)

	purgeID, err := svc.BeginPurge(context.Background(), PurgeBeginRequest{
		AccountID: a.ID, InitiatedBy: 1,
	})
	if err != nil {
		t.Fatalf("BeginPurge: %v", err)
	}
	if purgeID == 0 {
		t.Fatal("purgeID should be non-zero")
	}
	row := purges.get(purgeID)
	if row == nil {
		t.Fatal("audit row missing")
	}
	if row.Status != entity.AccountPurgeStatusInflight {
		t.Errorf("status = %q, want purging", row.Status)
	}
}

func TestAccountService_BeginPurge_AlreadyDeleted_ReturnsSentinel(t *testing.T) {
	svc, store, _ := newAccountSvcWithPurge(t)
	a := seedActiveAccount(t, svc)
	a.Status = entity.AccountStatusDeleted
	if err := store.Update(context.Background(), a); err != nil {
		t.Fatalf("seed deleted: %v", err)
	}

	purgeID, err := svc.BeginPurge(context.Background(), PurgeBeginRequest{
		AccountID: a.ID, InitiatedBy: 1,
	})
	if !errors.Is(err, ErrAccountAlreadyPurged) {
		t.Fatalf("err = %v; want ErrAccountAlreadyPurged", err)
	}
	if purgeID != 0 {
		t.Errorf("purgeID = %d; want 0 on idempotent path", purgeID)
	}
}

func TestAccountService_BeginPurge_InFlight_Returns409(t *testing.T) {
	svc, _, _ := newAccountSvcWithPurge(t)
	a := seedActiveAccount(t, svc)
	ctx := context.Background()

	// First mint — succeeds.
	if _, err := svc.BeginPurge(ctx, PurgeBeginRequest{AccountID: a.ID, InitiatedBy: 1}); err != nil {
		t.Fatalf("first BeginPurge: %v", err)
	}

	// Second mint while first is in-flight — must reject.
	_, err := svc.BeginPurge(ctx, PurgeBeginRequest{AccountID: a.ID, InitiatedBy: 2})
	if !errors.Is(err, ErrPurgeInFlight) {
		t.Fatalf("err = %v; want ErrPurgeInFlight", err)
	}
}

func TestAccountService_BeginPurge_AccountNotFound_Errors(t *testing.T) {
	svc, _, _ := newAccountSvcWithPurge(t)
	_, err := svc.BeginPurge(context.Background(), PurgeBeginRequest{
		AccountID: 99999, InitiatedBy: 1,
	})
	if err == nil {
		t.Fatal("expected error for missing account")
	}
}

func TestAccountService_BeginPurge_PurgeStoreNotWired_Errors(t *testing.T) {
	// Build service WITHOUT the purge store — Phase 4 wiring is opt-in
	// so callers that don't need the flow shouldn't get a partial setup.
	svc := NewAccountService(newMockAccountStore(), newMockWalletStore(), newMockVIPStore(nil))
	_, err := svc.BeginPurge(context.Background(), PurgeBeginRequest{AccountID: 1, InitiatedBy: 1})
	if err == nil {
		t.Fatal("expected error when purge store not wired")
	}
}

func TestAccountService_BeginPurge_RejectsZeroIDs(t *testing.T) {
	svc, _, _ := newAccountSvcWithPurge(t)
	for name, req := range map[string]PurgeBeginRequest{
		"zero account":   {AccountID: 0, InitiatedBy: 1},
		"zero initiator": {AccountID: 1, InitiatedBy: 0},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := svc.BeginPurge(context.Background(), req); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

// ── FinishPurge ─────────────────────────────────────────────────────────────

func TestAccountService_FinishPurge_Success_FlipsAccountAndAudit(t *testing.T) {
	svc, store, purges := newAccountSvcWithPurge(t)
	a := seedActiveAccount(t, svc)
	purgeID, _ := svc.BeginPurge(context.Background(), PurgeBeginRequest{AccountID: a.ID, InitiatedBy: 1})

	err := svc.FinishPurge(context.Background(), FinishPurgeRequest{
		PurgeID: purgeID, AccountID: a.ID, ApprovedBy: 7, Success: true,
	})
	if err != nil {
		t.Fatalf("FinishPurge: %v", err)
	}

	// Audit row → completed with approver.
	row := purges.get(purgeID)
	if row.Status != entity.AccountPurgeStatusCompleted {
		t.Errorf("audit status = %q; want completed", row.Status)
	}
	if row.ApprovedBy == nil || *row.ApprovedBy != 7 {
		t.Errorf("ApprovedBy = %v; want 7", row.ApprovedBy)
	}
	if row.CompletedAt == nil {
		t.Error("CompletedAt should be set")
	}

	// Account row → status=Deleted.
	got, _ := store.GetByID(context.Background(), a.ID)
	if got.Status != entity.AccountStatusDeleted {
		t.Errorf("account status = %d; want Deleted (3)", got.Status)
	}
}

func TestAccountService_FinishPurge_Failure_KeepsAccountActive(t *testing.T) {
	svc, store, purges := newAccountSvcWithPurge(t)
	a := seedActiveAccount(t, svc)
	purgeID, _ := svc.BeginPurge(context.Background(), PurgeBeginRequest{AccountID: a.ID, InitiatedBy: 1})

	err := svc.FinishPurge(context.Background(), FinishPurgeRequest{
		PurgeID: purgeID, AccountID: a.ID, Success: false, ErrMsg: "wallet zero-out failed",
	})
	if err != nil {
		t.Fatalf("FinishPurge(fail): %v", err)
	}

	row := purges.get(purgeID)
	if row.Status != entity.AccountPurgeStatusFailed {
		t.Errorf("audit status = %q; want failed", row.Status)
	}
	if row.Error != "wallet zero-out failed" {
		t.Errorf("error = %q; want propagated message", row.Error)
	}

	// Account must remain active so admin can retry.
	got, _ := store.GetByID(context.Background(), a.ID)
	if got.Status != entity.AccountStatusActive {
		t.Errorf("account status = %d; want still Active", got.Status)
	}
}

// After a successful purge, a follow-up BeginPurge for the same
// account must hit the AlreadyPurged sentinel — never silently re-run.
func TestAccountService_BeginPurge_AfterCompletion_IsIdempotent(t *testing.T) {
	svc, _, _ := newAccountSvcWithPurge(t)
	a := seedActiveAccount(t, svc)
	ctx := context.Background()

	purgeID, _ := svc.BeginPurge(ctx, PurgeBeginRequest{AccountID: a.ID, InitiatedBy: 1})
	if err := svc.FinishPurge(ctx, FinishPurgeRequest{PurgeID: purgeID, AccountID: a.ID, Success: true}); err != nil {
		t.Fatalf("FinishPurge: %v", err)
	}

	// Re-run BeginPurge — must short-circuit cleanly.
	_, err := svc.BeginPurge(ctx, PurgeBeginRequest{AccountID: a.ID, InitiatedBy: 99})
	if !errors.Is(err, ErrAccountAlreadyPurged) {
		t.Fatalf("err = %v; want ErrAccountAlreadyPurged", err)
	}
}
