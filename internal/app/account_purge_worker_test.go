package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/event"
)

// fakePurgeStore is an in-memory AccountPurgeStore used by every
// table-driven case. The Claim primitive simulates the real Postgres
// UPDATE...RETURNING + SKIP LOCKED contract: a single Claim call sees
// every eligible row at most once, regardless of how many goroutines
// race against the same store. This is what makes the multi-replica
// safety tests honest — a buggy claim would observe doubles here too.
type fakePurgeStore struct {
	mu sync.Mutex
	// rows is keyed by request id. Each row's status / completedAt
	// fields are mutated in-place to keep the lifecycle observable
	// from tests.
	rows []*entity.AccountDeleteRequest
	// claimErr / completeErr / expireErr trigger the failure paths.
	claimErr    error
	completeErr error
	expireErr   error
	// claimCalls counts every ClaimExpiredPending call so the disabled-
	// flag test can assert no scan happened.
	claimCalls int
}

func newFakePurgeStore() *fakePurgeStore {
	return &fakePurgeStore{}
}

func (f *fakePurgeStore) seed(row *entity.AccountDeleteRequest) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *row
	if cp.Status == "" {
		cp.Status = entity.AccountDeleteRequestStatusPending
	}
	f.rows = append(f.rows, &cp)
}

func (f *fakePurgeStore) ClaimExpiredPending(_ context.Context, limit int) ([]*entity.AccountDeleteRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.claimCalls++
	if f.claimErr != nil {
		return nil, f.claimErr
	}
	now := time.Now()
	var claimed []*entity.AccountDeleteRequest
	for _, r := range f.rows {
		if len(claimed) >= limit {
			break
		}
		if r.Status != entity.AccountDeleteRequestStatusPending {
			continue
		}
		if r.CoolingOffUntil.After(now) {
			continue
		}
		// Atomic flip: mirrors the real UPDATE ... SET status='processing'
		// behaviour so a second concurrent Claim cannot see the same
		// row again.
		r.Status = entity.AccountDeleteRequestStatusProcessing
		cp := *r
		claimed = append(claimed, &cp)
	}
	return claimed, nil
}

func (f *fakePurgeStore) MarkCompleted(_ context.Context, id int64, completedAt time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.completeErr != nil {
		return f.completeErr
	}
	for _, r := range f.rows {
		if r.ID == id && r.Status == entity.AccountDeleteRequestStatusProcessing {
			r.Status = entity.AccountDeleteRequestStatusCompleted
			r.CompletedAt = &completedAt
			return nil
		}
	}
	return nil
}

func (f *fakePurgeStore) MarkExpired(_ context.Context, id int64, completedAt time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.expireErr != nil {
		return f.expireErr
	}
	for _, r := range f.rows {
		if r.ID == id && r.Status == entity.AccountDeleteRequestStatusProcessing {
			r.Status = entity.AccountDeleteRequestStatusExpired
			r.CompletedAt = &completedAt
			return nil
		}
	}
	return nil
}

// statusOf is a tiny test helper.
func (f *fakePurgeStore) statusOf(id int64) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows {
		if r.ID == id {
			return r.Status
		}
	}
	return ""
}

// fakePurgeCascade records every PurgeAccount call. Optional cascadeErr
// is returned for whichever account_id matches failOn; everything else
// succeeds.
type fakePurgeCascade struct {
	mu         sync.Mutex
	calls      []int64
	failOn     int64
	cascadeErr error
}

func (f *fakePurgeCascade) PurgeAccount(_ context.Context, accountID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, accountID)
	if f.failOn == accountID {
		return f.cascadeErr
	}
	return nil
}

func (f *fakePurgeCascade) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// runOneTick spins up the worker, lets it run a single tick, and
// returns. The interval is set tighter than the test deadline so the
// initial tick fires without a wall-clock wait, then ctx.Cancel
// terminates the loop before the second tick.
func runOneTick(t *testing.T, w *AccountPurgeWorker) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = w.Run(ctx)
		close(done)
	}()
	// Worker calls tick() synchronously before the first ticker fire,
	// so a brief sleep is enough for the in-memory cascade to land.
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not exit after ctx cancel")
	}
}

// ── happy path: one row claimed, cascade succeeds, status=completed ─

func TestAccountPurgeWorker_HappyPath_RowCompleted(t *testing.T) {
	store := newFakePurgeStore()
	cascade := &fakePurgeCascade{}
	store.seed(&entity.AccountDeleteRequest{
		ID: 1, AccountID: 100,
		CoolingOffUntil: time.Now().Add(-time.Hour),
	})
	w := NewAccountPurgeWorker(store, cascade, AccountPurgeWorkerConfig{
		Interval: time.Hour, Batch: 20, Enabled: true,
	})
	runOneTick(t, w)

	if got := cascade.callCount(); got != 1 {
		t.Fatalf("cascade calls = %d; want 1", got)
	}
	if got := store.statusOf(1); got != entity.AccountDeleteRequestStatusCompleted {
		t.Fatalf("status of row 1 = %q; want %q",
			got, entity.AccountDeleteRequestStatusCompleted)
	}
}

// ── no eligible rows: idle scan, cascade never invoked ──────────────

func TestAccountPurgeWorker_NoEligibleRows_IdleScan(t *testing.T) {
	store := newFakePurgeStore()
	cascade := &fakePurgeCascade{}
	// Cooling-off in the future — should not be claimed yet.
	store.seed(&entity.AccountDeleteRequest{
		ID: 2, AccountID: 200,
		CoolingOffUntil: time.Now().Add(time.Hour),
	})
	w := NewAccountPurgeWorker(store, cascade, AccountPurgeWorkerConfig{
		Interval: time.Hour, Batch: 20, Enabled: true,
	})
	runOneTick(t, w)

	if got := cascade.callCount(); got != 0 {
		t.Fatalf("cascade calls = %d; want 0", got)
	}
	if got := store.statusOf(2); got != entity.AccountDeleteRequestStatusPending {
		t.Fatalf("status = %q; want %q (must remain pending)",
			got, entity.AccountDeleteRequestStatusPending)
	}
}

// ── cascade failure: status=expired, no infinite retry ───────────────

func TestAccountPurgeWorker_CascadeFailure_RowExpired(t *testing.T) {
	store := newFakePurgeStore()
	cascade := &fakePurgeCascade{
		failOn:     300,
		cascadeErr: errors.New("simulated zitadel outage"),
	}
	store.seed(&entity.AccountDeleteRequest{
		ID: 3, AccountID: 300,
		CoolingOffUntil: time.Now().Add(-time.Hour),
	})
	w := NewAccountPurgeWorker(store, cascade, AccountPurgeWorkerConfig{
		Interval: time.Hour, Batch: 20, Enabled: true,
	})
	runOneTick(t, w)

	if got := cascade.callCount(); got != 1 {
		t.Fatalf("cascade calls = %d; want exactly 1 (no retry on failure)", got)
	}
	if got := store.statusOf(3); got != entity.AccountDeleteRequestStatusExpired {
		t.Fatalf("status = %q; want %q",
			got, entity.AccountDeleteRequestStatusExpired)
	}
}

// ── concurrent claim: second worker gets 0 rows from claim UPDATE ────

func TestAccountPurgeWorker_ConcurrentClaim_OnceEachRow(t *testing.T) {
	// Two workers share a store and cascade. Both run a tick. The fake
	// store's Claim guarantees the same atomic semantics as the real
	// UPDATE...RETURNING SKIP LOCKED — a row is at most claimed once.
	// The test asserts the cascade ran exactly once for each row, even
	// with overlapping ticks.
	store := newFakePurgeStore()
	cascade := &fakePurgeCascade{}
	for i := int64(1); i <= 5; i++ {
		store.seed(&entity.AccountDeleteRequest{
			ID: i, AccountID: 1000 + i,
			CoolingOffUntil: time.Now().Add(-time.Hour),
		})
	}
	w1 := NewAccountPurgeWorker(store, cascade, AccountPurgeWorkerConfig{
		Interval: time.Hour, Batch: 20, Enabled: true,
	})
	w2 := NewAccountPurgeWorker(store, cascade, AccountPurgeWorkerConfig{
		Interval: time.Hour, Batch: 20, Enabled: true,
	})
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _ = w1.Run(ctx) }()
	go func() { defer wg.Done(); _ = w2.Run(ctx) }()
	time.Sleep(100 * time.Millisecond)
	cancel()
	wg.Wait()

	if got := cascade.callCount(); got != 5 {
		t.Fatalf("cascade calls = %d; want 5 (one per seeded row, no doubles)", got)
	}
	for i := int64(1); i <= 5; i++ {
		if got := store.statusOf(i); got != entity.AccountDeleteRequestStatusCompleted {
			t.Errorf("row %d status = %q; want %q", i, got,
				entity.AccountDeleteRequestStatusCompleted)
		}
	}
}

// ── disabled flag: no scan, no cascade ──────────────────────────────

func TestAccountPurgeWorker_Disabled_NoScan(t *testing.T) {
	store := newFakePurgeStore()
	cascade := &fakePurgeCascade{}
	store.seed(&entity.AccountDeleteRequest{
		ID: 4, AccountID: 400,
		CoolingOffUntil: time.Now().Add(-time.Hour),
	})
	w := NewAccountPurgeWorker(store, cascade, AccountPurgeWorkerConfig{
		Interval: time.Millisecond, Batch: 20, Enabled: false,
	})
	// When disabled, Run returns immediately. No tick wait needed.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := w.Run(ctx); err != nil {
		t.Fatalf("disabled Run returned error: %v", err)
	}
	if store.claimCalls != 0 {
		t.Errorf("claim called %d times; want 0", store.claimCalls)
	}
	if got := cascade.callCount(); got != 0 {
		t.Errorf("cascade calls = %d; want 0", got)
	}
	if got := store.statusOf(4); got != entity.AccountDeleteRequestStatusPending {
		t.Errorf("status = %q; want %q (untouched)",
			got, entity.AccountDeleteRequestStatusPending)
	}
}

// ── claim error: logged but loop survives ──────────────────────────

func TestAccountPurgeWorker_ClaimError_LoopSurvives(t *testing.T) {
	store := newFakePurgeStore()
	store.claimErr = errors.New("simulated db outage")
	cascade := &fakePurgeCascade{}
	w := NewAccountPurgeWorker(store, cascade, AccountPurgeWorkerConfig{
		Interval: time.Hour, Batch: 20, Enabled: true,
	})
	runOneTick(t, w)
	// No assertion on cascade — the point is "loop didn't panic".
	if got := cascade.callCount(); got != 0 {
		t.Fatalf("cascade calls = %d; want 0 on claim error", got)
	}
}

// ── interval gating via Name + defaults ────────────────────────────

func TestAccountPurgeWorker_Defaults_FillFromZero(t *testing.T) {
	store := newFakePurgeStore()
	cascade := &fakePurgeCascade{}
	w := NewAccountPurgeWorker(store, cascade, AccountPurgeWorkerConfig{
		// All zero — should adopt package defaults.
	})
	if w.interval != DefaultAccountPurgeInterval {
		t.Errorf("interval = %v; want default %v", w.interval, DefaultAccountPurgeInterval)
	}
	if w.batch != DefaultAccountPurgeBatch {
		t.Errorf("batch = %d; want default %d", w.batch, DefaultAccountPurgeBatch)
	}
	if w.Name() != "account_purge_worker" {
		t.Errorf("Name() = %q; want %q", w.Name(), "account_purge_worker")
	}
}

// ── PIPL §47 emit: capturePublisher records every Publish call ──────

type capturePublisher struct {
	mu     sync.Mutex
	events []*event.IdentityEvent
}

func (c *capturePublisher) Publish(_ context.Context, ev *event.IdentityEvent) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, ev)
	return nil
}

func (c *capturePublisher) snapshot() []*event.IdentityEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]*event.IdentityEvent, len(c.events))
	copy(out, c.events)
	return out
}

// errorPublisher always fails — used to prove emit failure does not
// undo the purge bookkeeping.
type errorPublisher struct {
	calls int
	mu    sync.Mutex
	err   error
}

func (e *errorPublisher) Publish(_ context.Context, _ *event.IdentityEvent) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls++
	return e.err
}

func (e *errorPublisher) callCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls
}

// TestPurgeWorker_EmitsAccountDeleted_AfterCascadeSuccess proves the
// cascade-success path also publishes identity.account.deleted with
// the expected envelope. Asserts subject + AccountID round-trip and
// that LurusID is the deterministic GenerateLurusID derivation.
func TestPurgeWorker_EmitsAccountDeleted_AfterCascadeSuccess(t *testing.T) {
	store := newFakePurgeStore()
	cascade := &fakePurgeCascade{}
	pub := &capturePublisher{}
	store.seed(&entity.AccountDeleteRequest{
		ID: 10, AccountID: 4242,
		CoolingOffUntil: time.Now().Add(-time.Hour),
	})
	w := NewAccountPurgeWorker(store, cascade, AccountPurgeWorkerConfig{
		Interval: time.Hour, Batch: 20, Enabled: true,
	}).WithPublisher(pub)

	runOneTick(t, w)

	if got := cascade.callCount(); got != 1 {
		t.Fatalf("cascade calls = %d; want 1", got)
	}
	if got := store.statusOf(10); got != entity.AccountDeleteRequestStatusCompleted {
		t.Fatalf("status = %q; want %q", got, entity.AccountDeleteRequestStatusCompleted)
	}
	got := pub.snapshot()
	if len(got) != 1 {
		t.Fatalf("captured events = %d; want 1", len(got))
	}
	ev := got[0]
	if ev.EventType != event.SubjectAccountDeleted {
		t.Errorf("EventType = %q; want %q", ev.EventType, event.SubjectAccountDeleted)
	}
	if ev.AccountID != 4242 {
		t.Errorf("AccountID = %d; want 4242", ev.AccountID)
	}
	if want := entity.GenerateLurusID(4242); ev.LurusID != want {
		t.Errorf("LurusID = %q; want %q", ev.LurusID, want)
	}
	if len(ev.Payload) == 0 {
		t.Errorf("Payload is empty; expected AccountDeletedPayload JSON")
	}
}

// TestPurgeWorker_PublishFailureDoesNotFailPurge proves the emit is
// best-effort: a publisher that always errors must not leave the row
// stuck in 'processing' nor flip it to 'expired'. The bookkeeping
// already landed before the publish was attempted, and that is the
// load-bearing invariant for PIPL §47 audit semantics.
func TestPurgeWorker_PublishFailureDoesNotFailPurge(t *testing.T) {
	store := newFakePurgeStore()
	cascade := &fakePurgeCascade{}
	pub := &errorPublisher{err: errors.New("nats down")}
	store.seed(&entity.AccountDeleteRequest{
		ID: 11, AccountID: 5555,
		CoolingOffUntil: time.Now().Add(-time.Hour),
	})
	w := NewAccountPurgeWorker(store, cascade, AccountPurgeWorkerConfig{
		Interval: time.Hour, Batch: 20, Enabled: true,
	}).WithPublisher(pub)

	runOneTick(t, w)

	if got := cascade.callCount(); got != 1 {
		t.Fatalf("cascade calls = %d; want 1", got)
	}
	if got := store.statusOf(11); got != entity.AccountDeleteRequestStatusCompleted {
		t.Errorf("status = %q; want %q (publish failure must NOT undo purge)",
			got, entity.AccountDeleteRequestStatusCompleted)
	}
	if got := pub.callCount(); got != 1 {
		t.Errorf("publisher calls = %d; want 1", got)
	}
}
