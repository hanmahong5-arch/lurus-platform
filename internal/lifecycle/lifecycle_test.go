package lifecycle

import (
	"context"
	"testing"
	"time"
)

// nopTask is a Task that returns immediately.
type nopTask struct{ name string }

func (t *nopTask) Run(_ context.Context) error { return nil }
func (t *nopTask) Name() string                { return t.name }

// slowTask runs until the context is cancelled.
type slowTask struct{ name string }

func (t *slowTask) Run(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}
func (t *slowTask) Name() string { return t.name }

func TestManager_Empty_ShutdownImmediate(t *testing.T) {
	m := NewManager()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.Start(ctx)
	if err := m.Shutdown(time.Second); err != nil {
		t.Errorf("Shutdown() = %v, want nil", err)
	}
}

func TestManager_NopTasks_ShutdownCompletesQuickly(t *testing.T) {
	m := NewManager()
	m.Register(&nopTask{"a"})
	m.Register(&nopTask{"b"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.Start(ctx)
	if err := m.Shutdown(2 * time.Second); err != nil {
		t.Errorf("Shutdown() = %v, want nil", err)
	}
}

func TestManager_Shutdown_Timeout(t *testing.T) {
	m := NewManager()
	m.Register(&slowTask{"slow"})
	ctx, cancel := context.WithCancel(context.Background())
	m.Start(ctx)
	// Timeout fires before we cancel the context.
	err := m.Shutdown(10 * time.Millisecond)
	cancel()
	if err != context.DeadlineExceeded {
		t.Errorf("Shutdown() = %v, want DeadlineExceeded", err)
	}
}

func TestManager_Shutdown_AfterContextCancel(t *testing.T) {
	m := NewManager()
	m.Register(&slowTask{"waiter"})
	ctx, cancel := context.WithCancel(context.Background())
	m.Start(ctx)
	// Cancel context so slowTask exits, then Shutdown should succeed.
	cancel()
	if err := m.Shutdown(2 * time.Second); err != nil {
		t.Errorf("Shutdown() after cancel = %v, want nil", err)
	}
}

func TestManager_Register_MultipleAndStart(t *testing.T) {
	m := NewManager()
	for i := 0; i < 5; i++ {
		m.Register(&nopTask{})
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.Start(ctx)
	if err := m.Shutdown(2 * time.Second); err != nil {
		t.Errorf("Shutdown() = %v, want nil", err)
	}
}
