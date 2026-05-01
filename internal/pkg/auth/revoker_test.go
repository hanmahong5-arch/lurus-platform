package auth

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func setupRevokerRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return mr, rdb
}

func TestSessionRevoker_RevokeThenIsRevoked(t *testing.T) {
	_, rdb := setupRevokerRedis(t)
	r := NewSessionRevoker(rdb)
	ctx := context.Background()

	if r.IsRevoked(ctx, "tok-1") {
		t.Fatal("fresh token should not be revoked")
	}
	if err := r.Revoke(ctx, "tok-1", time.Hour); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if !r.IsRevoked(ctx, "tok-1") {
		t.Fatal("after Revoke the token should be revoked")
	}
	// Different token must remain unaffected.
	if r.IsRevoked(ctx, "tok-2") {
		t.Fatal("unrelated token should not be revoked")
	}
}

func TestSessionRevoker_TTLExpiresEntry(t *testing.T) {
	mr, rdb := setupRevokerRedis(t)
	r := NewSessionRevoker(rdb)
	ctx := context.Background()

	if err := r.Revoke(ctx, "tok-x", 5*time.Minute); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if !r.IsRevoked(ctx, "tok-x") {
		t.Fatal("expected revoked before TTL elapses")
	}
	mr.FastForward(6 * time.Minute)
	if r.IsRevoked(ctx, "tok-x") {
		t.Fatal("expected entry to expire after TTL")
	}
}

func TestSessionRevoker_NonPositiveTTLNoOp(t *testing.T) {
	_, rdb := setupRevokerRedis(t)
	r := NewSessionRevoker(rdb)
	ctx := context.Background()

	// A token already past its natural expiry has nothing to revoke
	// against — Revoke must be a clean no-op so callers don't race
	// to compute "should I bother".
	if err := r.Revoke(ctx, "tok-late", 0); err != nil {
		t.Fatalf("ttl=0: %v", err)
	}
	if err := r.Revoke(ctx, "tok-late", -time.Second); err != nil {
		t.Fatalf("ttl<0: %v", err)
	}
	if r.IsRevoked(ctx, "tok-late") {
		t.Fatal("non-positive TTL should not write a revoke entry")
	}
}

func TestSessionRevoker_NilSafe(t *testing.T) {
	ctx := context.Background()
	// Nil receiver — simulates "feature unwired".
	var nilR *SessionRevoker
	if err := nilR.Revoke(ctx, "x", time.Hour); err != nil {
		t.Fatalf("nil receiver Revoke should be no-op, got %v", err)
	}
	if nilR.IsRevoked(ctx, "x") {
		t.Fatal("nil receiver IsRevoked must return false")
	}
	// Non-nil receiver, nil rdb — same behaviour. Lets callers wire
	// NewSessionRevoker(nil) in dev/test without branching.
	r := NewSessionRevoker(nil)
	if err := r.Revoke(ctx, "x", time.Hour); err != nil {
		t.Fatalf("nil rdb Revoke: %v", err)
	}
	if r.IsRevoked(ctx, "x") {
		t.Fatal("nil rdb IsRevoked must return false")
	}
}

func TestSessionRevoker_RedisError_FailOpen(t *testing.T) {
	mr, rdb := setupRevokerRedis(t)
	r := NewSessionRevoker(rdb)
	ctx := context.Background()

	if err := r.Revoke(ctx, "tok-down", time.Hour); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	// Kill Redis. Next IsRevoked must return false (fail-open) rather
	// than block / fail-closed which would lock everyone out during an
	// outage. The Revoke side will surface the error to its caller —
	// fail-open only applies to the read path.
	mr.Close()
	if r.IsRevoked(ctx, "tok-down") {
		t.Fatal("redis-down IsRevoked should fail-open and return false")
	}
}

func TestSessionRevoker_RevokeKey_StableAndIsolated(t *testing.T) {
	// The key prefix is contract for ops scripts (e.g. "wipe all revoke
	// entries"). Lock it down.
	got := revokeKey("any-token")
	if !startsWith(got, "auth:revoked:") {
		t.Fatalf("revokeKey prefix changed: %q — would silently break ops scripts", got)
	}
	// Different inputs → different keys.
	if revokeKey("a") == revokeKey("b") {
		t.Fatal("hash collision on tiny inputs — sanity")
	}
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
