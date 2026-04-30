package idempotency

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func setupMiniredis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return mr, rdb
}

func TestWebhookDeduper_TryProcess_FirstCall(t *testing.T) {
	_, rdb := setupMiniredis(t)
	d := New(rdb, DefaultWebhookTTL)

	err := d.TryProcess(context.Background(), "evt-001")
	if err != nil {
		t.Fatalf("first TryProcess: %v", err)
	}
}

func TestWebhookDeduper_TryProcess_DuplicateCall(t *testing.T) {
	_, rdb := setupMiniredis(t)
	d := New(rdb, DefaultWebhookTTL)
	ctx := context.Background()

	d.TryProcess(ctx, "evt-002")

	err := d.TryProcess(ctx, "evt-002")
	if err != ErrAlreadyProcessed {
		t.Fatalf("second TryProcess = %v, want ErrAlreadyProcessed", err)
	}
}

func TestWebhookDeduper_TryProcess_EmptyEventID(t *testing.T) {
	_, rdb := setupMiniredis(t)
	d := New(rdb, DefaultWebhookTTL)

	// Empty event ID must be rejected to prevent deduplication bypass.
	err := d.TryProcess(context.Background(), "")
	if err != ErrEmptyEventID {
		t.Fatalf("empty event ID: want ErrEmptyEventID, got %v", err)
	}
}

func TestWebhookDeduper_TryProcess_NilRedis(t *testing.T) {
	d := New(nil, DefaultWebhookTTL)

	// nil Redis means no deduplication — always returns nil.
	err := d.TryProcess(context.Background(), "evt-003")
	if err != nil {
		t.Fatalf("nil Redis: %v", err)
	}
	err = d.TryProcess(context.Background(), "evt-003")
	if err != nil {
		t.Fatalf("nil Redis second call: %v", err)
	}
}

func TestWebhookDeduper_TryProcess_DifferentEventIDs(t *testing.T) {
	_, rdb := setupMiniredis(t)
	d := New(rdb, DefaultWebhookTTL)
	ctx := context.Background()

	// Different event IDs should each succeed.
	for _, id := range []string{"a", "b", "c"} {
		if err := d.TryProcess(ctx, id); err != nil {
			t.Fatalf("TryProcess(%q): %v", id, err)
		}
	}

	// Each should be seen on second call.
	for _, id := range []string{"a", "b", "c"} {
		if err := d.TryProcess(ctx, id); err != ErrAlreadyProcessed {
			t.Fatalf("duplicate TryProcess(%q) = %v, want ErrAlreadyProcessed", id, err)
		}
	}
}

func TestWebhookDeduper_TryProcess_TTLExpiry(t *testing.T) {
	mr, rdb := setupMiniredis(t)
	d := New(rdb, 2*time.Second)
	ctx := context.Background()

	d.TryProcess(ctx, "evt-ttl")

	// Duplicate before TTL.
	if err := d.TryProcess(ctx, "evt-ttl"); err != ErrAlreadyProcessed {
		t.Fatalf("before TTL: %v", err)
	}

	// Fast-forward miniredis past TTL.
	mr.FastForward(3 * time.Second)

	// After TTL, event can be processed again.
	if err := d.TryProcess(ctx, "evt-ttl"); err != nil {
		t.Fatalf("after TTL: %v", err)
	}
}

func TestWebhookDeduper_TryProcess_RedisFailure(t *testing.T) {
	mr, rdb := setupMiniredis(t)
	d := New(rdb, DefaultWebhookTTL)

	// Close miniredis to simulate failure.
	mr.Close()

	// Fail-open: should return nil (process the event).
	err := d.TryProcess(context.Background(), "evt-fail")
	if err != nil {
		t.Fatalf("Redis failure should fail-open: %v", err)
	}
}

func TestNew_DefaultTTL(t *testing.T) {
	_, rdb := setupMiniredis(t)

	// Zero TTL should use default.
	d := New(rdb, 0)
	if d.ttl != DefaultWebhookTTL {
		t.Errorf("ttl = %v, want %v", d.ttl, DefaultWebhookTTL)
	}

	// Negative TTL should use default.
	d = New(rdb, -1*time.Second)
	if d.ttl != DefaultWebhookTTL {
		t.Errorf("ttl = %v, want %v", d.ttl, DefaultWebhookTTL)
	}
}

func TestNew_CustomTTL(t *testing.T) {
	_, rdb := setupMiniredis(t)
	d := New(rdb, 5*time.Minute)
	if d.ttl != 5*time.Minute {
		t.Errorf("ttl = %v, want 5m", d.ttl)
	}
}

func TestWebhookDeduper_RedisKeyFormat(t *testing.T) {
	mr, rdb := setupMiniredis(t)
	d := New(rdb, DefaultWebhookTTL)

	d.TryProcess(context.Background(), "test-key-format")

	// Verify the key exists in Redis with the expected prefix.
	keys := mr.Keys()
	found := false
	for _, k := range keys {
		if k == "webhook:seen:test-key-format" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected key 'webhook:seen:test-key-format' in Redis, got keys: %v", keys)
	}
}

// ── Fail-closed mode (money/billing pipelines) ─────────────────────────────

func TestWebhookDeduper_FailClosed_RedisDownReturnsErrRedisUnavailable(t *testing.T) {
	mr, rdb := setupMiniredis(t)
	d := New(rdb, DefaultWebhookTTL).WithFailClosed()

	// Sanity check: works while Redis is up.
	if err := d.TryProcess(context.Background(), "evt-fc-ok"); err != nil {
		t.Fatalf("first call should succeed: %v", err)
	}

	// Kill Redis: fail-closed mode must surface ErrRedisUnavailable so
	// the caller NAKs the message rather than silently double-processing.
	mr.Close()
	err := d.TryProcess(context.Background(), "evt-fc-down")
	if err != ErrRedisUnavailable {
		t.Errorf("fail-closed + Redis down: got %v, want ErrRedisUnavailable", err)
	}
}

func TestWebhookDeduper_FailClosed_NoChangeOnHappyPath(t *testing.T) {
	_, rdb := setupMiniredis(t)
	d := New(rdb, DefaultWebhookTTL).WithFailClosed()
	ctx := context.Background()

	if err := d.TryProcess(ctx, "evt-fc-1"); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if err := d.TryProcess(ctx, "evt-fc-1"); err != ErrAlreadyProcessed {
		t.Errorf("duplicate in fail-closed: got %v, want ErrAlreadyProcessed", err)
	}
}

func TestWebhookDeduper_FailClosed_NilRedisStillNoOps(t *testing.T) {
	// Even fail-closed must let traffic through when Redis is not wired
	// at all (operator hasn't configured it yet) — this is a
	// configuration state, not a runtime failure.
	d := New(nil, DefaultWebhookTTL).WithFailClosed()
	if err := d.TryProcess(context.Background(), "evt-nil"); err != nil {
		t.Errorf("nil Redis + fail-closed: %v, want nil", err)
	}
}

func TestWebhookDeduper_WithKeyPrefix_NamespacesKeys(t *testing.T) {
	mr, rdb := setupMiniredis(t)
	d := New(rdb, DefaultWebhookTTL).WithKeyPrefix("newapi:topup:seen:")

	if err := d.TryProcess(context.Background(), "evt-ns"); err != nil {
		t.Fatalf("TryProcess: %v", err)
	}

	// Verify custom prefix is honoured (so two pipelines don't collide).
	if !mr.Exists("newapi:topup:seen:evt-ns") {
		t.Errorf("expected custom-prefix key, got: %v", mr.Keys())
	}
	// Default prefix must NOT have been written.
	if mr.Exists("webhook:seen:evt-ns") {
		t.Errorf("default prefix unexpectedly populated: %v", mr.Keys())
	}
}

func TestWebhookDeduper_FailOpenPolicyUnchanged(t *testing.T) {
	// Regression guard: existing webhook callers must continue seeing
	// the fail-open behaviour (don't break callers who chose the default).
	mr, rdb := setupMiniredis(t)
	d := New(rdb, DefaultWebhookTTL) // no WithFailClosed

	mr.Close()
	if err := d.TryProcess(context.Background(), "evt-fo"); err != nil {
		t.Errorf("fail-open + Redis down: got %v, want nil (fail-open)", err)
	}
}
