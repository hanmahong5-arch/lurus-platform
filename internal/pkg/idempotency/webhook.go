// Package idempotency provides Redis-backed event deduplication.
//
// Two failure-mode policies are exposed:
//
//   - **fail-open** (default — webhook semantics)
//     Redis unreachable → return nil and let the caller process the event.
//     Trade-off: protects availability of low-stakes webhooks (e.g. ping
//     receivers) at the cost of potential duplicate processing during
//     Redis outage. Acceptable when the downstream operation is itself
//     idempotent or where double-execution is harmless.
//
//   - **fail-closed** (opt-in via WithFailClosed — money / billing semantics)
//     Redis unreachable → return ErrRedisUnavailable so the caller NAKs
//     the message and JetStream retries with backoff. Trade-off: pauses
//     processing during Redis outage to guarantee no duplicate. The right
//     default whenever the downstream operation moves money or otherwise
//     causes irreversible state change.
//
// The choice is per-deduper, not per-call, so a misuse can't be silently
// downgraded by a buggy caller. Money pipelines build a fail-closed
// instance once at boot; webhook handlers keep the fail-open default.
package idempotency

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// DefaultWebhookTTL is how long processed event IDs are retained.
	DefaultWebhookTTL = 24 * time.Hour

	redisKeyPrefix = "webhook:seen:"
)

// ErrAlreadyProcessed is returned when an event has already been handled.
var ErrAlreadyProcessed = errors.New("idempotency: event already processed")

// ErrEmptyEventID is returned when an empty event ID is passed to TryProcess.
var ErrEmptyEventID = errors.New("idempotency: empty event ID")

// ErrRedisUnavailable is returned only when the deduper was constructed
// fail-closed and Redis returned an error. Callers should NAK the
// underlying message so it gets retried after Redis recovers — never
// process the event without a successful dedup check on this path.
var ErrRedisUnavailable = errors.New("idempotency: redis unavailable (fail-closed)")

// WebhookDeduper deduplicates events using Redis SET NX.
type WebhookDeduper struct {
	rdb        *redis.Client
	ttl        time.Duration
	keyPrefix  string
	failClosed bool
}

// New creates a WebhookDeduper. If rdb is nil all calls return nil (no dedup).
func New(rdb *redis.Client, ttl time.Duration) *WebhookDeduper {
	if ttl <= 0 {
		ttl = DefaultWebhookTTL
	}
	return &WebhookDeduper{rdb: rdb, ttl: ttl, keyPrefix: redisKeyPrefix}
}

// WithFailClosed flips the Redis-error policy to "fail closed" — Redis
// unavailable returns ErrRedisUnavailable rather than silently allowing
// the event through. Use this for money / billing pipelines where double
// processing is unacceptable. Chainable.
func (d *WebhookDeduper) WithFailClosed() *WebhookDeduper {
	d.failClosed = true
	return d
}

// WithKeyPrefix overrides the Redis key prefix. Use this to namespace
// different event streams so a single Redis instance can dedupe webhooks
// AND topup events without collision (e.g. "newapi:topup:seen:").
// Chainable.
func (d *WebhookDeduper) WithKeyPrefix(prefix string) *WebhookDeduper {
	if prefix != "" {
		d.keyPrefix = prefix
	}
	return d
}

// TryProcess attempts to mark eventID as processed.
//
// Return values:
//   - nil                    → first time we've seen this ID; caller proceeds
//   - ErrAlreadyProcessed    → seen before; caller skips, ACKs the message
//   - ErrEmptyEventID        → caller passed "" (programming error / bad
//                               envelope); caller decides what to do, but
//                               never silently dedupes a missing ID
//   - ErrRedisUnavailable    → fail-closed mode + Redis error; caller MUST
//                               NAK the message (retry once Redis recovers)
//
// Fail-open mode swallows Redis errors and returns nil. Use only when
// duplicate processing is harmless.
func (d *WebhookDeduper) TryProcess(ctx context.Context, eventID string) error {
	if eventID == "" {
		return ErrEmptyEventID
	}
	if d.rdb == nil {
		// No Redis configured at all — degrade gracefully even in
		// fail-closed mode (operator hasn't wired Redis yet, no money is
		// flowing). The startup log + readiness probe surface this.
		return nil
	}
	key := fmt.Sprintf("%s%s", d.keyPrefix, eventID)

	// SET NX EX — only set if key does not exist.
	set, err := d.rdb.SetNX(ctx, key, "1", d.ttl).Result()
	if err != nil {
		if d.failClosed {
			slog.Error("idempotency: redis error in fail-closed mode → caller MUST NAK",
				"event_id", eventID, "err", err)
			return ErrRedisUnavailable
		}
		slog.Warn("idempotency: redis error, processing event anyway (fail-open)",
			"event_id", eventID, "err", err)
		return nil
	}
	if !set {
		return ErrAlreadyProcessed
	}
	return nil
}
