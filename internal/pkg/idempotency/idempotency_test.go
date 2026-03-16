package idempotency

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// newTestDeduper creates a WebhookDeduper backed by miniredis.
func newTestDeduper(t *testing.T, ttl time.Duration) (*WebhookDeduper, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	t.Cleanup(mr.Close)

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	return New(rdb, ttl), mr
}

// TestIdempotency_FirstRequest_Allow verifies that the first request for an event ID is allowed.
func TestIdempotency_FirstRequest_Allow(t *testing.T) {
	d, _ := newTestDeduper(t, time.Hour)

	err := d.TryProcess(context.Background(), "evt-001")
	if err != nil {
		t.Errorf("first request should return nil, got %v", err)
	}
}

// TestIdempotency_DuplicateRequest_Block verifies that a duplicate event ID is rejected.
func TestIdempotency_DuplicateRequest_Block(t *testing.T) {
	d, _ := newTestDeduper(t, time.Hour)
	ctx := context.Background()

	_ = d.TryProcess(ctx, "evt-002") // first call
	err := d.TryProcess(ctx, "evt-002") // duplicate
	if err != ErrAlreadyProcessed {
		t.Errorf("duplicate: want ErrAlreadyProcessed, got %v", err)
	}
}

// TestIdempotency_Expiry_AllowAfterTTL verifies that after TTL expires, the same event can be processed.
func TestIdempotency_Expiry_AllowAfterTTL(t *testing.T) {
	ttl := 100 * time.Millisecond
	d, mr := newTestDeduper(t, ttl)
	ctx := context.Background()

	_ = d.TryProcess(ctx, "evt-003")

	// Duplicate before expiry.
	if err := d.TryProcess(ctx, "evt-003"); err != ErrAlreadyProcessed {
		t.Fatal("should be blocked before TTL")
	}

	// Advance time past TTL.
	mr.FastForward(ttl + 10*time.Millisecond)

	// After TTL, should be allowed again.
	if err := d.TryProcess(ctx, "evt-003"); err != nil {
		t.Errorf("after TTL expiry: want nil, got %v", err)
	}
}

// TestIdempotency_ConcurrentSameKey verifies that only one concurrent caller succeeds.
func TestIdempotency_ConcurrentSameKey(t *testing.T) {
	d, _ := newTestDeduper(t, time.Hour)
	ctx := context.Background()

	const goroutines = 20
	var wg sync.WaitGroup
	var mu sync.Mutex
	successes := 0
	duplicates := 0

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			err := d.TryProcess(ctx, "evt-concurrent")
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				successes++
			} else if err == ErrAlreadyProcessed {
				duplicates++
			}
		}()
	}
	wg.Wait()

	// Exactly one goroutine should succeed.
	if successes != 1 {
		t.Errorf("exactly 1 goroutine should succeed, got %d", successes)
	}
	if duplicates != goroutines-1 {
		t.Errorf("exactly %d goroutines should be blocked, got %d", goroutines-1, duplicates)
	}
}

// TestIdempotency_EmptyEventID_NoDedup verifies that empty event IDs are not deduplicated
// (pass-through for missing event IDs).
func TestIdempotency_EmptyEventID_NoDedup(t *testing.T) {
	d, _ := newTestDeduper(t, time.Hour)
	ctx := context.Background()

	// Multiple calls with empty event ID should all succeed (no dedup on empty).
	for i := 0; i < 3; i++ {
		if err := d.TryProcess(ctx, ""); err != nil {
			t.Errorf("empty event ID call %d: want nil, got %v", i+1, err)
		}
	}
}

// TestIdempotency_NilRedis_FailOpen verifies that a nil Redis client always allows processing.
func TestIdempotency_NilRedis_FailOpen(t *testing.T) {
	d := New(nil, time.Hour)

	for i := 0; i < 5; i++ {
		if err := d.TryProcess(context.Background(), "evt-nil"); err != nil {
			t.Errorf("nil redis: call %d should return nil (fail-open), got %v", i+1, err)
		}
	}
}

// TestIdempotency_DefaultTTL verifies that zero TTL uses the default.
func TestIdempotency_DefaultTTL(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	d := New(rdb, 0) // zero TTL should use DefaultWebhookTTL
	if d.ttl != DefaultWebhookTTL {
		t.Errorf("zero TTL: want DefaultWebhookTTL (%v), got %v", DefaultWebhookTTL, d.ttl)
	}
}

// TestIdempotency_DifferentEventIDs_Independent verifies that different event IDs are independent.
func TestIdempotency_DifferentEventIDs_Independent(t *testing.T) {
	d, _ := newTestDeduper(t, time.Hour)
	ctx := context.Background()

	_ = d.TryProcess(ctx, "evtA")
	_ = d.TryProcess(ctx, "evtA") // evtA is duplicate

	// evtB is a different event, should be allowed.
	if err := d.TryProcess(ctx, "evtB"); err != nil {
		t.Errorf("different event ID should be allowed, got %v", err)
	}
}
