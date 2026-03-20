package app

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// mockReconciliationWalletStore is a minimal walletStore that tracks
// ExpireStalePendingOrders and ExpireStalePreAuths calls.
type mockReconciliationWalletStore struct {
	mockWalletStore
	expireOrdersCalled   atomic.Int32
	expirePreAuthCalled  atomic.Int32
	expireOrdersReturn   int64
	expirePreAuthReturn  int64
	expireOrdersErr      error
	expirePreAuthErr     error
}

func (m *mockReconciliationWalletStore) ExpireStalePendingOrders(_ context.Context, _ time.Duration) (int64, error) {
	m.expireOrdersCalled.Add(1)
	return m.expireOrdersReturn, m.expireOrdersErr
}

func (m *mockReconciliationWalletStore) ExpireStalePreAuths(_ context.Context) (int64, error) {
	m.expirePreAuthCalled.Add(1)
	return m.expirePreAuthReturn, m.expirePreAuthErr
}

func TestReconciliationWorker_TickCallsBothMethods(t *testing.T) {
	store := &mockReconciliationWalletStore{
		mockWalletStore:     *newMockWalletStore(),
		expireOrdersReturn:  2,
		expirePreAuthReturn: 1,
	}
	ws := NewWalletService(store, nil)
	w := NewReconciliationWorker(ws)

	ctx := context.Background()
	w.tick(ctx)

	if got := store.expireOrdersCalled.Load(); got != 1 {
		t.Errorf("ExpireStalePendingOrders called %d times, want 1", got)
	}
	if got := store.expirePreAuthCalled.Load(); got != 1 {
		t.Errorf("ExpireStalePreAuths called %d times, want 1", got)
	}
}

func TestReconciliationWorker_TickContinuesOnOrderError(t *testing.T) {
	store := &mockReconciliationWalletStore{
		mockWalletStore: *newMockWalletStore(),
		expireOrdersErr: context.DeadlineExceeded,
	}
	ws := NewWalletService(store, nil)
	w := NewReconciliationWorker(ws)

	ctx := context.Background()
	w.tick(ctx)

	// Even though orders failed, pre-auths should still be called.
	if got := store.expirePreAuthCalled.Load(); got != 1 {
		t.Errorf("ExpireStalePreAuths should still be called after order error, got %d", got)
	}
}

func TestReconciliationWorker_TickContinuesOnPreAuthError(t *testing.T) {
	store := &mockReconciliationWalletStore{
		mockWalletStore:  *newMockWalletStore(),
		expirePreAuthErr: context.DeadlineExceeded,
	}
	ws := NewWalletService(store, nil)
	w := NewReconciliationWorker(ws)

	ctx := context.Background()
	w.tick(ctx)

	// Both should be called regardless.
	if got := store.expireOrdersCalled.Load(); got != 1 {
		t.Errorf("ExpireStalePendingOrders called %d times, want 1", got)
	}
	if got := store.expirePreAuthCalled.Load(); got != 1 {
		t.Errorf("ExpireStalePreAuths called %d times, want 1", got)
	}
}

func TestReconciliationWorker_StartRunsImmediatelyAndStopsOnCancel(t *testing.T) {
	store := &mockReconciliationWalletStore{
		mockWalletStore: *newMockWalletStore(),
	}
	ws := NewWalletService(store, nil)
	w := NewReconciliationWorker(ws)
	w.interval = 50 * time.Millisecond // fast ticks for test

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		w.Start(ctx)
		close(done)
	}()

	// Wait enough for at least the immediate tick.
	time.Sleep(30 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Worker stopped correctly.
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not stop after context cancellation")
	}

	if got := store.expireOrdersCalled.Load(); got < 1 {
		t.Errorf("ExpireStalePendingOrders called %d times, want >= 1 (immediate tick)", got)
	}
}

func TestReconciliationWorker_Defaults(t *testing.T) {
	ws := NewWalletService(newMockWalletStore(), nil)
	w := NewReconciliationWorker(ws)

	if w.interval != defaultReconciliationInterval {
		t.Errorf("interval = %v, want %v", w.interval, defaultReconciliationInterval)
	}
	if w.orderMaxAge != defaultOrderMaxAge {
		t.Errorf("orderMaxAge = %v, want %v", w.orderMaxAge, defaultOrderMaxAge)
	}
}
