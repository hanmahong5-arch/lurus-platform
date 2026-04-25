package app_registry_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/hanmahong5-arch/lurus-platform/internal/module/app_registry"
)

func newTombstones(t *testing.T) (*app_registry.Tombstones, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return app_registry.NewTombstones(rdb), mr
}

func TestTombstones_MarkAndIsActive(t *testing.T) {
	tombs, _ := newTombstones(t)
	ctx := context.Background()

	active, err := tombs.IsActive(ctx, "tally", "stage")
	if err != nil {
		t.Fatalf("IsActive (empty): %v", err)
	}
	if active {
		t.Error("IsActive should return false before Mark")
	}

	if err := tombs.Mark(ctx, "tally", "stage"); err != nil {
		t.Fatalf("Mark: %v", err)
	}

	active, err = tombs.IsActive(ctx, "tally", "stage")
	if err != nil {
		t.Fatalf("IsActive (post-mark): %v", err)
	}
	if !active {
		t.Error("IsActive should return true after Mark")
	}

	// Different (app, env) must not collide.
	active, _ = tombs.IsActive(ctx, "tally", "prod")
	if active {
		t.Error("Mark(tally, stage) must not affect (tally, prod)")
	}
	active, _ = tombs.IsActive(ctx, "admin", "stage")
	if active {
		t.Error("Mark(tally, stage) must not affect (admin, stage)")
	}
}

func TestTombstones_TTLExpiry(t *testing.T) {
	tombs, mr := newTombstones(t)
	ctx := context.Background()

	if err := tombs.Mark(ctx, "tally", "stage"); err != nil {
		t.Fatalf("Mark: %v", err)
	}
	mr.FastForward(25 * time.Hour) // > 24h TTL

	active, err := tombs.IsActive(ctx, "tally", "stage")
	if err != nil {
		t.Fatalf("IsActive: %v", err)
	}
	if active {
		t.Error("Tombstone must expire after TTL")
	}
}

func TestTombstones_Clear(t *testing.T) {
	tombs, _ := newTombstones(t)
	ctx := context.Background()

	_ = tombs.Mark(ctx, "tally", "stage")
	if err := tombs.Clear(ctx, "tally", "stage"); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	active, _ := tombs.IsActive(ctx, "tally", "stage")
	if active {
		t.Error("Clear must remove the tombstone")
	}
}

func TestTombstones_Mark_RejectsBadKeys(t *testing.T) {
	tombs, _ := newTombstones(t)
	ctx := context.Background()
	cases := []struct {
		name, app, env string
	}{
		{"empty app", "", "stage"},
		{"empty env", "tally", ""},
		{"whitespace app", "  ", "stage"},
		{"colon in app", "ta:lly", "stage"},
		{"colon in env", "tally", "sta:ge"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tombs.Mark(ctx, tc.app, tc.env); err == nil {
				t.Error("Mark must reject malformed key")
			}
		})
	}
}

func TestTombstones_NilSafe(t *testing.T) {
	// A nil-redis Tombstones is allowed (recovery from misconfigured
	// wiring) — IsActive must return false without panicking; Mark/
	// Clear return errors so callers notice.
	tombs := app_registry.NewTombstones(nil)
	ctx := context.Background()

	active, err := tombs.IsActive(ctx, "tally", "stage")
	if err != nil {
		t.Errorf("IsActive (nil rdb) err = %v; want nil", err)
	}
	if active {
		t.Error("IsActive on nil tombstones must return false")
	}
	if err := tombs.Mark(ctx, "tally", "stage"); err == nil {
		t.Error("Mark on nil tombstones should error")
	}
}
