package app_registry_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/hanmahong5-arch/lurus-platform/internal/module/app_registry"
)

// newTestRedis spins up an in-process miniredis and returns a wired
// go-redis client; cleanup is registered via t.Cleanup so individual
// tests stay terse.
func newTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb
}

// TestRotationState_Roundtrip verifies that a Mark + Get returns a
// timestamp within a couple of seconds of "now", which is all the
// reconciler ever cares about.
func TestRotationState_Roundtrip(t *testing.T) {
	rdb := newTestRedis(t)
	rs := app_registry.NewRotationState(rdb)
	ctx := context.Background()

	if err := rs.MarkRotated(ctx, "admin", "prod"); err != nil {
		t.Fatalf("MarkRotated: %v", err)
	}
	got, err := rs.GetLastRotated(ctx, "admin", "prod")
	if err != nil {
		t.Fatalf("GetLastRotated: %v", err)
	}
	if delta := time.Since(got); delta < 0 || delta > 5*time.Second {
		t.Errorf("returned ts is %v ago — expected ~now", delta)
	}
}

// TestRotationState_Missing verifies that an unknown (app, env) returns
// the zero time + nil error, since the reconciler treats that as
// "eligible for rotation / bootstrap".
func TestRotationState_Missing(t *testing.T) {
	rdb := newTestRedis(t)
	rs := app_registry.NewRotationState(rdb)
	got, err := rs.GetLastRotated(context.Background(), "no", "such")
	if err != nil {
		t.Fatalf("GetLastRotated: %v", err)
	}
	if !got.IsZero() {
		t.Errorf("expected zero time for missing key, got %v", got)
	}
}

// TestRotationState_NilRedisErrs makes sure we degrade gracefully when
// no redis client is wired — neither a panic nor a silent success.
func TestRotationState_NilRedisErrs(t *testing.T) {
	rs := app_registry.NewRotationState(nil)
	ctx := context.Background()
	if _, err := rs.GetLastRotated(ctx, "a", "b"); err == nil {
		t.Error("expected error for nil redis on Get")
	}
	if err := rs.MarkRotated(ctx, "a", "b"); err == nil {
		t.Error("expected error for nil redis on Mark")
	}
}

// TestRotationState_PerEnvIsolation guards against accidental key
// collisions: rotating (admin, prod) must not move the clock for
// (admin, stage).
func TestRotationState_PerEnvIsolation(t *testing.T) {
	rdb := newTestRedis(t)
	rs := app_registry.NewRotationState(rdb)
	ctx := context.Background()

	if err := rs.MarkRotated(ctx, "admin", "prod"); err != nil {
		t.Fatalf("Mark prod: %v", err)
	}
	stage, err := rs.GetLastRotated(ctx, "admin", "stage")
	if err != nil {
		t.Fatalf("Get stage: %v", err)
	}
	if !stage.IsZero() {
		t.Errorf("stage should still be zero, got %v", stage)
	}
}
