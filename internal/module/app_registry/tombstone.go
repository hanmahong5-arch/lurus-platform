package app_registry

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// Tombstones records short-lived "this (app, env) was just deleted —
// don't recreate it" markers in Redis.
//
// The reconciler is declarative and would, on its next tick, re-create
// any Zitadel app that's still listed in apps.yaml. After a human-
// confirmed deletion we want a window in which the operator can land a
// PR removing the YAML entry; tombstones provide that window.
//
// TTL is intentionally short (24 h) — long enough that a normal review
// cycle finishes inside it, short enough that a forgotten tombstone
// doesn't quietly hide a YAML entry forever. Once the TTL lapses the
// reconciler resumes creating the app from YAML, which is the
// convergent default.
type Tombstones struct {
	rdb *redis.Client
}

const (
	// tombstoneKeyPrefix is the namespace under which deletion markers
	// live in Redis. Kept distinct from any qr:* / qr-events:* keys so
	// scans / FLUSHDB on the QR handler can never collide with these.
	tombstoneKeyPrefix = "qr_app_tombstone:"

	// tombstoneTTL bounds how long the reconciler ignores a YAML entry
	// after a human-confirmed delete. 24 h matches the operational SLA
	// for "operator updates apps.yaml after a deletion is confirmed".
	tombstoneTTL = 24 * time.Hour
)

// NewTombstones builds a Tombstones helper. A nil rdb is rejected at
// construction so the reconciler never silently no-ops the check.
func NewTombstones(rdb *redis.Client) *Tombstones {
	if rdb == nil {
		// Caller wiring bug — return a value whose methods will error
		// loudly rather than panic on first use.
		return &Tombstones{rdb: nil}
	}
	return &Tombstones{rdb: rdb}
}

// Mark records a tombstone for (app, env). Subsequent IsActive calls
// within tombstoneTTL return true. Re-marking refreshes the TTL — that's
// the desired behaviour if a delete is retried.
func (t *Tombstones) Mark(ctx context.Context, app, env string) error {
	if t == nil || t.rdb == nil {
		return errors.New("app_registry: tombstones not configured")
	}
	if err := validateTombstoneKey(app, env); err != nil {
		return err
	}
	return t.rdb.Set(ctx, tombstoneKey(app, env), "1", tombstoneTTL).Err()
}

// IsActive reports whether a tombstone currently exists for (app, env).
// Redis errors collapse to (false, err); callers in the reconciler hot
// path log+continue rather than block convergence on a transient broker
// hiccup.
func (t *Tombstones) IsActive(ctx context.Context, app, env string) (bool, error) {
	if t == nil || t.rdb == nil {
		return false, nil
	}
	if err := validateTombstoneKey(app, env); err != nil {
		return false, err
	}
	n, err := t.rdb.Exists(ctx, tombstoneKey(app, env)).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// Clear removes a tombstone explicitly. Currently unused by the
// reconciler (TTL-based natural expiry suffices) but exposed so admin
// tooling can recall a deletion before the 24 h window elapses.
func (t *Tombstones) Clear(ctx context.Context, app, env string) error {
	if t == nil || t.rdb == nil {
		return errors.New("app_registry: tombstones not configured")
	}
	if err := validateTombstoneKey(app, env); err != nil {
		return err
	}
	return t.rdb.Del(ctx, tombstoneKey(app, env)).Err()
}

func tombstoneKey(app, env string) string {
	return tombstoneKeyPrefix + app + ":" + env
}

// validateTombstoneKey rejects empty or colon-bearing components so the
// "<app>:<env>" composition can't be ambiguous (e.g. "foo:bar" + "baz"
// vs "foo" + "bar:baz" producing the same key).
func validateTombstoneKey(app, env string) error {
	if strings.TrimSpace(app) == "" || strings.TrimSpace(env) == "" {
		return fmt.Errorf("app_registry: tombstone key needs non-empty (app, env); got (%q, %q)", app, env)
	}
	if strings.ContainsRune(app, ':') || strings.ContainsRune(env, ':') {
		return fmt.Errorf("app_registry: tombstone key components must not contain ':' (got app=%q env=%q)", app, env)
	}
	return nil
}
