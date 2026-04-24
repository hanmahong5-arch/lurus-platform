package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/event"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/retry"
)

// flakyPublisher fails the first `failures` attempts then succeeds.
type flakyPublisher struct {
	failures int32
	calls    atomic.Int32
}

func (f *flakyPublisher) Publish(_ context.Context, _ *event.IdentityEvent) error {
	n := f.calls.Add(1)
	if int32(n) <= f.failures {
		return errors.New("broker offline")
	}
	return nil
}

// alwaysFailPublisher fails every call.
type alwaysFailPublisher struct {
	calls atomic.Int32
	err   error
}

func (a *alwaysFailPublisher) Publish(_ context.Context, _ *event.IdentityEvent) error {
	a.calls.Add(1)
	return a.err
}

// newTestRedis returns an miniredis-backed client tied to t.
func newTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })
	return rdb
}

func mkEvent(t *testing.T) *event.IdentityEvent {
	t.Helper()
	ev, err := event.NewEvent(event.SubjectOrgMemberJoined, 42, "", "", event.OrgMemberJoinedPayload{
		OrgID:          7,
		AccountID:      42,
		Role:           "member",
		JoinedAt:       time.Now().UTC().Format(time.RFC3339),
		ConfirmedViaQR: true,
	})
	if err != nil {
		t.Fatalf("NewEvent: %v", err)
	}
	return ev
}

// fastRetry keeps unit tests snappy by collapsing the default backoff.
func fastRetry() *retry.Config {
	return &retry.Config{
		MaxAttempts:  3,
		InitialDelay: time.Millisecond,
		MaxDelay:     2 * time.Millisecond,
		Jitter:       0,
	}
}

// ── Happy path ────────────────────────────────────────────────────────────

func TestDLQPublisher_PublishSuccessOnFirstAttempt(t *testing.T) {
	up := &flakyPublisher{failures: 0}
	rdb := newTestRedis(t)
	p := New(up, rdb, Config{RetryConfig: fastRetry()})

	if err := p.Publish(context.Background(), mkEvent(t)); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if up.calls.Load() != 1 {
		t.Errorf("upstream calls = %d, want 1", up.calls.Load())
	}
	if n, _ := rdb.LLen(context.Background(), DefaultDLQKey).Result(); n != 0 {
		t.Errorf("DLQ should be empty, got %d entries", n)
	}
}

func TestDLQPublisher_PublishSuccessAfterRetry(t *testing.T) {
	up := &flakyPublisher{failures: 2} // attempt 1+2 fail, 3 succeeds
	rdb := newTestRedis(t)
	p := New(up, rdb, Config{MaxRetries: 3, RetryConfig: fastRetry()})

	if err := p.Publish(context.Background(), mkEvent(t)); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if up.calls.Load() != 3 {
		t.Errorf("upstream calls = %d, want 3", up.calls.Load())
	}
	if n, _ := rdb.LLen(context.Background(), DefaultDLQKey).Result(); n != 0 {
		t.Errorf("DLQ should be empty after retry-success, got %d", n)
	}
}

// ── DLQ fallback ──────────────────────────────────────────────────────────

func TestDLQPublisher_AllRetriesFailFallsBackToDLQ(t *testing.T) {
	up := &alwaysFailPublisher{err: errors.New("nats: connection closed")}
	rdb := newTestRedis(t)
	p := New(up, rdb, Config{MaxRetries: 3, RetryConfig: fastRetry()})
	ev := mkEvent(t)

	// Publish returns nil because the DLQ captured the event durably.
	if err := p.Publish(context.Background(), ev); err != nil {
		t.Fatalf("Publish should succeed (DLQ captured), got %v", err)
	}

	if up.calls.Load() != 3 {
		t.Errorf("upstream calls = %d, want 3", up.calls.Load())
	}

	// Event should be in the DLQ list.
	ctx := context.Background()
	n, err := rdb.LLen(ctx, DefaultDLQKey).Result()
	if err != nil || n != 1 {
		t.Fatalf("DLQ LLen = %d (err=%v), want 1", n, err)
	}
	raw, err := rdb.LRange(ctx, DefaultDLQKey, 0, -1).Result()
	if err != nil {
		t.Fatalf("LRange: %v", err)
	}
	var got event.IdentityEvent
	if err := json.Unmarshal([]byte(raw[0]), &got); err != nil {
		t.Fatalf("DLQ payload not valid JSON: %v", err)
	}
	if got.EventID != ev.EventID {
		t.Errorf("DLQ event_id = %q, want %q", got.EventID, ev.EventID)
	}
	if got.EventType != ev.EventType {
		t.Errorf("DLQ event_type = %q, want %q", got.EventType, ev.EventType)
	}
}

func TestDLQPublisher_DLQKeyCustom(t *testing.T) {
	up := &alwaysFailPublisher{err: errors.New("broker down")}
	rdb := newTestRedis(t)
	p := New(up, rdb, Config{
		MaxRetries:  2,
		DLQKey:      "custom:dlq",
		RetryConfig: fastRetry(),
	})

	if err := p.Publish(context.Background(), mkEvent(t)); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	n, _ := rdb.LLen(context.Background(), "custom:dlq").Result()
	if n != 1 {
		t.Errorf("custom DLQ LLen = %d, want 1", n)
	}
	// Default key should be untouched.
	n2, _ := rdb.LLen(context.Background(), DefaultDLQKey).Result()
	if n2 != 0 {
		t.Errorf("default DLQ should be empty, got %d", n2)
	}
}

func TestDLQPublisher_DLQTTLSet(t *testing.T) {
	up := &alwaysFailPublisher{err: errors.New("broker down")}
	rdb := newTestRedis(t)
	p := New(up, rdb, Config{
		MaxRetries:  2,
		DLQTTL:      10 * time.Minute,
		RetryConfig: fastRetry(),
	})

	if err := p.Publish(context.Background(), mkEvent(t)); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	// TTL should be > 0 and <= configured.
	ttl, err := rdb.TTL(context.Background(), DefaultDLQKey).Result()
	if err != nil {
		t.Fatalf("TTL: %v", err)
	}
	if ttl <= 0 || ttl > 10*time.Minute {
		t.Errorf("DLQ TTL = %v, want (0, 10m]", ttl)
	}
}

// ── Degenerate inputs ─────────────────────────────────────────────────────

func TestDLQPublisher_NilEvent(t *testing.T) {
	p := New(&alwaysFailPublisher{err: errors.New("x")}, newTestRedis(t), Config{})
	if err := p.Publish(context.Background(), nil); err == nil {
		t.Error("expected error on nil event, got nil")
	}
}

func TestDLQPublisher_NilUpstream_GoesDirectToDLQ(t *testing.T) {
	rdb := newTestRedis(t)
	p := New(nil, rdb, Config{MaxRetries: 1, RetryConfig: fastRetry()})

	if err := p.Publish(context.Background(), mkEvent(t)); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if n, _ := rdb.LLen(context.Background(), DefaultDLQKey).Result(); n != 1 {
		t.Errorf("DLQ LLen = %d, want 1", n)
	}
}

func TestDLQPublisher_NilRedis_ReturnsErrorOnFailure(t *testing.T) {
	up := &alwaysFailPublisher{err: errors.New("broker down")}
	p := New(up, nil, Config{MaxRetries: 2, RetryConfig: fastRetry()})

	err := p.Publish(context.Background(), mkEvent(t))
	if err == nil {
		t.Error("expected error when DLQ is disabled and upstream failed")
	}
}

// DefaultConfig_ShapesSane verifies package defaults are plausible.
func TestNew_DefaultConfigShapesSane(t *testing.T) {
	p := New(&flakyPublisher{}, newTestRedis(t), Config{})
	if p.cfg.MaxRetries != DefaultMaxRetries {
		t.Errorf("MaxRetries = %d, want %d", p.cfg.MaxRetries, DefaultMaxRetries)
	}
	if p.cfg.DLQKey != DefaultDLQKey {
		t.Errorf("DLQKey = %q, want %q", p.cfg.DLQKey, DefaultDLQKey)
	}
	if p.cfg.DLQTTL != DefaultDLQTTL {
		t.Errorf("DLQTTL = %v, want %v", p.cfg.DLQTTL, DefaultDLQTTL)
	}
}

// ── Context propagation ───────────────────────────────────────────────────

// TestDLQPublisher_CancelledCtx_StillPushesDLQ verifies that even when the
// caller's context is cancelled (e.g. the HTTP request ended), the DLQ push
// still succeeds because we detach the context.
func TestDLQPublisher_CancelledCtx_StillPushesDLQ(t *testing.T) {
	up := &alwaysFailPublisher{err: errors.New("broker down")}
	rdb := newTestRedis(t)
	p := New(up, rdb, Config{MaxRetries: 1, RetryConfig: fastRetry()})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	// Publish should still log + land event in DLQ. With MaxAttempts=1 the
	// retry loop runs once, returns the error, then we detach and push.
	_ = p.Publish(ctx, mkEvent(t))

	if n, _ := rdb.LLen(context.Background(), DefaultDLQKey).Result(); n != 1 {
		t.Errorf("DLQ LLen = %d, want 1 (detached ctx should still push)", n)
	}
}
