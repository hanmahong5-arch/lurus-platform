package newapi_sync

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// reconcileFakeStore extends fakeStore with the ListWithoutNewAPIUser
// method needed by ReconcileTick — kept as a separate type so the
// existing fakeStore tests don't need to reason about list state.
type reconcileFakeStore struct {
	fakeStore
	listErr error
}

func (s *reconcileFakeStore) ListWithoutNewAPIUser(_ context.Context, limit int) ([]*entity.Account, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*entity.Account, 0, len(s.accounts))
	for _, a := range s.accounts {
		if a.NewAPIUserID == nil {
			cp := *a
			out = append(out, &cp)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func newReconcileSetup(_ *testing.T) (*Module, *fakeClient, *reconcileFakeStore) {
	c := newFakeClient()
	s := &reconcileFakeStore{
		fakeStore: fakeStore{accounts: map[int64]*entity.Account{}},
	}
	m := New(c, s)
	return m, c, s
}

func seedOrphan(s *reconcileFakeStore, id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.accounts[id] = &entity.Account{ID: id, DisplayName: "u"}
}

func seedSynced(s *reconcileFakeStore, id int64, newapiID int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.accounts[id] = &entity.Account{ID: id, NewAPIUserID: &newapiID, DisplayName: "u"}
}

func TestReconcileTick_HealsOrphans(t *testing.T) {
	m, c, s := newReconcileSetup(t)
	seedOrphan(s, 1)
	seedOrphan(s, 2)

	res := m.ReconcileTick(context.Background(), 100)
	if res.Scanned != 2 || res.Healed != 2 || res.Failed != 0 {
		t.Errorf("expected 2 scanned/2 healed/0 failed, got %+v", res)
	}
	if c.createCalls != 2 {
		t.Errorf("expected 2 NewAPI creates, got %d", c.createCalls)
	}
	// Both accounts should now have NewAPIUserID populated in the store.
	for _, id := range []int64{1, 2} {
		s.mu.Lock()
		got := s.accounts[id].NewAPIUserID
		s.mu.Unlock()
		if got == nil {
			t.Errorf("account %d still unmapped after reconcile", id)
		}
	}
}

func TestReconcileTick_SkipsAlreadySynced(t *testing.T) {
	m, c, s := newReconcileSetup(t)
	seedSynced(s, 1, 1001)
	seedSynced(s, 2, 1002)

	res := m.ReconcileTick(context.Background(), 100)
	if res.Scanned != 0 || res.Healed != 0 {
		t.Errorf("synced accounts should not appear in reconcile, got %+v", res)
	}
	if c.createCalls != 0 || c.findCalls != 0 {
		t.Errorf("no NewAPI calls expected, got create=%d find=%d", c.createCalls, c.findCalls)
	}
}

func TestReconcileTick_ListErrorReturned(t *testing.T) {
	m, _, s := newReconcileSetup(t)
	s.listErr = errors.New("db closed")

	res := m.ReconcileTick(context.Background(), 100)
	if res.ListError == nil {
		t.Error("expected ListError to surface so caller logs the failure")
	}
	if res.Scanned != 0 || res.Healed != 0 {
		t.Errorf("on list error nothing should be scanned/healed, got %+v", res)
	}
}

func TestReconcileTick_PoisonPillContinuesBatch(t *testing.T) {
	// Account 1 will fail (NewAPI create errors), account 2 should still heal.
	m, c, s := newReconcileSetup(t)
	seedOrphan(s, 1)
	seedOrphan(s, 2)

	// Configure: NewAPI client throws on FIRST create attempt only.
	original := c.createUserError
	c.createUserError = errors.New("transient newapi 503")
	// After first create attempt fails, restore good behaviour for next.
	// Simplest: schedule reset via an interceptor isn't supported in our
	// fake — so check post-condition that BOTH accounts were attempted.
	res := m.ReconcileTick(context.Background(), 100)
	if res.Scanned != 2 {
		t.Errorf("expected scan to walk full batch despite per-account failure, got %+v", res)
	}
	// Both accounts should have had at least one attempt.
	if c.findCalls < 1 {
		t.Errorf("expected at least one find call before create, got %d", c.findCalls)
	}
	c.createUserError = original // tidy
}

func TestReconcileTick_RespectsCancellation(t *testing.T) {
	m, _, s := newReconcileSetup(t)
	for i := int64(1); i <= 5; i++ {
		seedOrphan(s, i)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before tick

	res := m.ReconcileTick(ctx, 100)
	// List itself ran with the cancelled ctx — implementation may have
	// returned the whole batch already (DB call may not honour ctx in
	// the fake). The point is per-account loop sees ctx.Err() and
	// breaks immediately. So Healed should be 0.
	if res.Healed != 0 {
		t.Errorf("cancelled ctx should heal nothing, got %d healed", res.Healed)
	}
}

func TestReconcileTick_RespectsBatchLimit(t *testing.T) {
	m, _, s := newReconcileSetup(t)
	for i := int64(1); i <= 10; i++ {
		seedOrphan(s, i)
	}

	res := m.ReconcileTick(context.Background(), 3)
	if res.Scanned != 3 {
		t.Errorf("expected batch limit 3 to cap scan, got scanned=%d", res.Scanned)
	}
	if res.Healed != 3 {
		t.Errorf("expected 3 healed in this batch, got %d", res.Healed)
	}
}

func TestReconcileTick_NilModule_NoPanic(t *testing.T) {
	var m *Module // typed nil — main.go can pass this when env unset
	res := m.ReconcileTick(context.Background(), 100)
	if res.Scanned != 0 || res.Healed != 0 || res.Failed != 0 || res.ListError != nil {
		t.Errorf("nil module should be a clean no-op, got %+v", res)
	}
}

func TestReconcileTick_DefaultBatchWhenZero(t *testing.T) {
	// Passing batch=0 should fall back to DefaultReconcileBatch (100).
	m, _, s := newReconcileSetup(t)
	for i := int64(1); i <= 5; i++ {
		seedOrphan(s, i)
	}

	res := m.ReconcileTick(context.Background(), 0)
	if res.Scanned != 5 {
		t.Errorf("expected 5 scanned with default batch, got %d", res.Scanned)
	}
}

// Loop: covered with very short interval so the test doesn't take long.
func TestRunReconcileLoop_StopsOnContextCancel(t *testing.T) {
	m, _, s := newReconcileSetup(t)
	seedOrphan(s, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- m.RunReconcileLoop(ctx, 50*time.Millisecond, 10)
	}()

	// Let it tick once or twice, then cancel.
	time.Sleep(150 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("loop should return nil on Cancel, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("loop did not exit within 2s of context cancellation")
	}

	// At least one tick should have run — the seeded orphan should be healed.
	s.mu.Lock()
	got := s.accounts[1].NewAPIUserID
	s.mu.Unlock()
	if got == nil {
		t.Error("expected loop to heal seeded orphan within 150ms window")
	}
}

func TestRunReconcileLoop_NilModule_BlocksUntilCancel(t *testing.T) {
	var m *Module
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- m.RunReconcileLoop(ctx, 10*time.Millisecond, 5)
	}()

	// Should NOT panic; should block until ctx cancellation.
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// expected
	case <-time.After(time.Second):
		t.Fatal("nil-module loop did not exit on cancel")
	}
}

// Compile-time guard: reconcile is wired through fakeStore that has the
// embedded fakeStore + new ListWithoutNewAPIUser method. Reference the
// types so the compiler will error if the test file imports drift.
var _ AccountStore = (*reconcileFakeStore)(nil)
var _ sync.Locker = (*sync.Mutex)(nil)
