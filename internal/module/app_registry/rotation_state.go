package app_registry

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// rotationLastKeyPrefix namespaces the per-(app,env) "last rotated"
// timestamp keys in Redis. Keeping a single shared prefix lets ops scan
// the full set with `redis-cli SCAN MATCH qr_app_rotation_last:*`.
const rotationLastKeyPrefix = "qr_app_rotation_last:"

// RotationState tracks when each (app, env) tuple last had its OIDC
// client_secret rotated. The state lives in Redis so a pod restart does
// not reset the rotation clock — a fresh pod inheriting an empty cache
// would otherwise trigger an immediate rotation on every deploy.
//
// Values are stored as Unix-seconds strings (no JSON wrapper) for cheap
// inspection from redis-cli. The keys never expire: the rotation cadence
// is the source of truth for "should we rotate now", not Redis TTL.
type RotationState struct {
	rdb *redis.Client
}

// NewRotationState wires the helper. rdb may be nil — every method then
// returns a typed error rather than panicking, so callers running
// without Redis (e.g. unit tests) can still construct a Reconciler.
func NewRotationState(rdb *redis.Client) *RotationState {
	return &RotationState{rdb: rdb}
}

// errNoRedis is returned by RotationState methods when no client was
// wired. Callers treat it as "rotation tracking disabled" — the
// reconciler skips auto-rotation, but explicit /admin rotate calls
// still go through (they don't need the timestamp to act).
var errNoRedis = errors.New("app_registry: rotation state has no redis client")

// GetLastRotated returns the moment the given (app, env) was last
// rotated. A zero time.Time means "no record" — caller should treat
// that as eligible-for-rotation.
func (r *RotationState) GetLastRotated(ctx context.Context, app, env string) (time.Time, error) {
	if r == nil || r.rdb == nil {
		return time.Time{}, errNoRedis
	}
	key := rotationKey(app, env)
	raw, err := r.rdb.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("app_registry: get rotation timestamp %q: %w", key, err)
	}
	secs, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		// A stray value (probably from a legacy format) should be
		// treated as "unknown" rather than crashing the reconciler.
		return time.Time{}, fmt.Errorf("app_registry: parse rotation timestamp %q=%q: %w", key, raw, err)
	}
	return time.Unix(secs, 0).UTC(), nil
}

// MarkRotated writes the current time as the last-rotated marker for
// (app, env). No TTL — the value is overwritten on next rotation and is
// the policy clock, not a cache.
func (r *RotationState) MarkRotated(ctx context.Context, app, env string) error {
	if r == nil || r.rdb == nil {
		return errNoRedis
	}
	now := time.Now().UTC().Unix()
	key := rotationKey(app, env)
	if err := r.rdb.Set(ctx, key, strconv.FormatInt(now, 10), 0).Err(); err != nil {
		return fmt.Errorf("app_registry: set rotation timestamp %q: %w", key, err)
	}
	return nil
}

// rotationKey is the canonical Redis key for a (app, env) pair. Centralised
// so admin tooling and tests stay in lock-step with the writer.
func rotationKey(app, env string) string {
	return rotationLastKeyPrefix + app + ":" + env
}
