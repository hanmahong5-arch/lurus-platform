// Package idempotency provides Redis-backed webhook deduplication.
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

// WebhookDeduper deduplicates webhook events using Redis SET NX.
type WebhookDeduper struct {
	rdb *redis.Client
	ttl time.Duration
}

// New creates a WebhookDeduper. If rdb is nil all calls return nil (no dedup).
func New(rdb *redis.Client, ttl time.Duration) *WebhookDeduper {
	if ttl <= 0 {
		ttl = DefaultWebhookTTL
	}
	return &WebhookDeduper{rdb: rdb, ttl: ttl}
}

// TryProcess attempts to mark eventID as processed.
// Returns nil on first call for this ID, ErrAlreadyProcessed on subsequent calls.
// On Redis failure, logs a warning and returns nil (fail-open: process the event).
func (d *WebhookDeduper) TryProcess(ctx context.Context, eventID string) error {
	if d.rdb == nil || eventID == "" {
		return nil
	}
	key := fmt.Sprintf("%s%s", redisKeyPrefix, eventID)

	// SET NX EX — only set if key does not exist.
	set, err := d.rdb.SetNX(ctx, key, "1", d.ttl).Result()
	if err != nil {
		slog.Warn("idempotency: redis error, processing event anyway", "event_id", eventID, "err", err)
		return nil
	}
	if !set {
		return ErrAlreadyProcessed
	}
	return nil
}
