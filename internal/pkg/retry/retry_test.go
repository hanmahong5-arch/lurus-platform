package retry

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

var errTransient = errors.New("transient failure")

func TestDo_SuccessOnFirstAttempt(t *testing.T) {
	var calls atomic.Int32
	err := Do(context.Background(), Options{
		Config: DefaultConfig(),
	}, func(_ context.Context) error {
		calls.Add(1)
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls.Load() != 1 {
		t.Errorf("calls = %d, want 1", calls.Load())
	}
}

func TestDo_SuccessOnSecondAttempt(t *testing.T) {
	var calls atomic.Int32
	err := Do(context.Background(), Options{
		Config: Config{MaxAttempts: 3, InitialDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond},
	}, func(_ context.Context) error {
		n := calls.Add(1)
		if n < 2 {
			return errTransient
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls.Load() != 2 {
		t.Errorf("calls = %d, want 2", calls.Load())
	}
}

func TestDo_ExhaustsRetries(t *testing.T) {
	var calls atomic.Int32
	err := Do(context.Background(), Options{
		Config: Config{MaxAttempts: 3, InitialDelay: time.Millisecond, MaxDelay: 5 * time.Millisecond},
	}, func(_ context.Context) error {
		calls.Add(1)
		return errTransient
	})
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if calls.Load() != 3 {
		t.Errorf("calls = %d, want 3", calls.Load())
	}
	if !errors.Is(err, errTransient) {
		t.Errorf("error chain should contain original error, got: %v", err)
	}
}

func TestDo_ShouldRetryFalseStopsEarly(t *testing.T) {
	var calls atomic.Int32
	permanent := errors.New("permanent error")

	err := Do(context.Background(), Options{
		Config: Config{MaxAttempts: 5, InitialDelay: time.Millisecond, MaxDelay: 5 * time.Millisecond},
		ShouldRetry: func(err error, _ int) bool {
			return !errors.Is(err, permanent) // don't retry permanent errors
		},
	}, func(_ context.Context) error {
		calls.Add(1)
		return permanent
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls.Load() != 1 {
		t.Errorf("calls = %d, want 1 (should stop immediately for permanent error)", calls.Load())
	}
}

func TestDo_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var calls atomic.Int32

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := Do(ctx, Options{
		Config: Config{MaxAttempts: 10, InitialDelay: 100 * time.Millisecond, MaxDelay: 1 * time.Second},
	}, func(_ context.Context) error {
		calls.Add(1)
		return errTransient
	})
	if err == nil {
		t.Fatal("expected error from context cancellation")
	}
	// Should have been cancelled before exhausting all 10 attempts.
	if calls.Load() >= 10 {
		t.Errorf("calls = %d, should have been cancelled before 10", calls.Load())
	}
}

func TestDo_OnRetryCallback(t *testing.T) {
	var retryCount atomic.Int32

	Do(context.Background(), Options{
		Config: Config{MaxAttempts: 3, InitialDelay: time.Millisecond, MaxDelay: 5 * time.Millisecond},
		OnRetry: func(attempt, maxAttempts int, delay time.Duration, err error) {
			retryCount.Add(1)
		},
	}, func(_ context.Context) error {
		return errTransient
	})

	if retryCount.Load() != 2 { // 3 attempts = 2 retries
		t.Errorf("retryCount = %d, want 2", retryCount.Load())
	}
}

func TestDo_Jitter(t *testing.T) {
	// Run multiple times and verify delays vary (jitter adds randomness).
	delays := make([]time.Duration, 0, 5)

	for i := 0; i < 5; i++ {
		start := time.Now()
		var attempt int
		Do(context.Background(), Options{
			Config: Config{MaxAttempts: 2, InitialDelay: 10 * time.Millisecond, MaxDelay: 100 * time.Millisecond, Jitter: 0.5},
			OnRetry: func(a, _ int, d time.Duration, _ error) {
				attempt = a
				_ = attempt
			},
		}, func(_ context.Context) error {
			return errTransient
		})
		elapsed := time.Since(start)
		delays = append(delays, elapsed)
	}

	// With 50% jitter, delays should vary. Check that not all are identical.
	allSame := true
	for i := 1; i < len(delays); i++ {
		diff := delays[i] - delays[0]
		if diff < 0 {
			diff = -diff
		}
		if diff > time.Millisecond {
			allSame = false
			break
		}
	}
	if allSame {
		t.Error("with 50% jitter, delays should vary between runs")
	}
}

func TestSimple_Success(t *testing.T) {
	err := Simple(context.Background(), 3, func(_ context.Context) error {
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSimple_Failure(t *testing.T) {
	err := Simple(context.Background(), 2, func(_ context.Context) error {
		return errTransient
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDo_MaxDelayClamp(t *testing.T) {
	var maxSeen time.Duration
	Do(context.Background(), Options{
		Config: Config{
			MaxAttempts:  5,
			InitialDelay: 100 * time.Millisecond,
			MaxDelay:     200 * time.Millisecond, // clamp at 200ms
			Jitter:       0,
		},
		OnRetry: func(_ int, _ int, delay time.Duration, _ error) {
			if delay > maxSeen {
				maxSeen = delay
			}
		},
	}, func(_ context.Context) error {
		return errTransient
	})

	// Without clamp: 100, 200, 400, 800ms. With clamp: 100, 200, 200, 200ms.
	if maxSeen > 250*time.Millisecond { // some tolerance for jitter=0
		t.Errorf("maxSeen = %v, should be clamped to ~200ms", maxSeen)
	}
}
