// Package outbox provides a DLQ-monitored wrapper around an event publisher.
//
// Rationale (see plans/qr-hardening-audit.md Track L / RL2.4):
// the existing nats.Publisher is "best-effort" — a transient broker outage
// silently drops events such as identity.org.member_joined emitted from
// qr_handler's confirm path. The upstream NATS client already retries the
// connection (RetryOnFailedConnect / MaxReconnects=10), so a full persisted
// outbox is not warranted yet. This package adds a thin retry + DLQ layer:
//
//   - up to `MaxRetries` in-process retries on publish failure (exponential
//     backoff via internal/pkg/retry);
//   - on final failure, the event is LPUSH'd to a Redis list (default
//     `outbox:dlq`) with a 7-day TTL so an operator / reconciliation job can
//     inspect and replay;
//   - Prometheus counters (`outbox_published_total`, `outbox_dlq_total`)
//     track outcomes so alerts can fire on sustained DLQ growth.
//
// The Publish signature is identical to nats.Publisher.Publish so callers
// (qr_handler, refund_service, temporal activities) need no code change.
package outbox

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/redis/go-redis/v9"

	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/event"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/retry"
)

// Default tuning — exported so operators can see them in the package docs.
const (
	// DefaultDLQKey is the Redis list that holds serialized IdentityEvents
	// when all in-process publish attempts fail.
	DefaultDLQKey = "outbox:dlq"
	// DefaultDLQTTL bounds the DLQ retention so a dead broker cannot grow
	// Redis memory unbounded; reconciliation must drain within this window.
	DefaultDLQTTL = 7 * 24 * time.Hour
	// DefaultMaxRetries is the total number of publish attempts including
	// the first. 3 covers typical transient broker blips without adding
	// perceptible latency to the user-facing request that triggered the
	// publish (confirm, approve refund, …).
	DefaultMaxRetries = 3
)

// Publisher is the minimal publish surface — matches nats.Publisher.Publish
// so the DLQ wrapper is a drop-in replacement.
type Publisher interface {
	Publish(ctx context.Context, ev *event.IdentityEvent) error
}

// RedisDLQ is the subset of *redis.Client used by the DLQ writer. Narrowed
// so tests can inject a miniredis-backed client without depending on all of
// go-redis.
type RedisDLQ interface {
	LPush(ctx context.Context, key string, values ...any) *redis.IntCmd
	Expire(ctx context.Context, key string, expiration time.Duration) *redis.BoolCmd
}

// Config holds DLQPublisher tuning.
type Config struct {
	// MaxRetries is the total number of publish attempts (>=1). Zero or
	// negative falls back to DefaultMaxRetries.
	MaxRetries int
	// DLQKey is the Redis list to LPUSH serialized events into when all
	// retries are exhausted. Empty string falls back to DefaultDLQKey.
	DLQKey string
	// DLQTTL bounds the Redis list's TTL. Zero falls back to DefaultDLQTTL.
	DLQTTL time.Duration
	// RetryConfig overrides the backoff profile. Zero values fall back to
	// a tight profile (50ms initial, 500ms max, 10% jitter) so a blocking
	// caller (e.g. a confirm handler) sees at most ~1s of added latency on
	// the unhappy path.
	RetryConfig *retry.Config
}

// DLQPublisher wraps an upstream Publisher with retry + Redis DLQ fallback.
// The zero value is NOT usable — always construct with New.
type DLQPublisher struct {
	upstream Publisher
	redis    RedisDLQ
	cfg      Config
}

// New returns a DLQPublisher. `upstream` is the primary publisher (typically
// *nats.Publisher) and `rdb` is the Redis client used for DLQ fallback.
// `upstream` or `rdb` may be nil: a nil upstream causes every publish to
// skip straight to DLQ (handy for testing), a nil redis disables DLQ
// (retries-only mode, which simply logs on final failure).
func New(upstream Publisher, rdb RedisDLQ, cfg Config) *DLQPublisher {
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = DefaultMaxRetries
	}
	if cfg.DLQKey == "" {
		cfg.DLQKey = DefaultDLQKey
	}
	if cfg.DLQTTL <= 0 {
		cfg.DLQTTL = DefaultDLQTTL
	}
	return &DLQPublisher{upstream: upstream, redis: rdb, cfg: cfg}
}

// Publish attempts to deliver ev via the upstream publisher, retrying
// transient failures. If all retries are exhausted the event is LPUSH'd to
// the DLQ list for out-of-band reconciliation and a nil error is returned —
// durable capture IS the success signal for the caller. If even the DLQ
// write fails, the original upstream error is returned so the caller can
// surface or log it; the caller MUST NOT assume the event was stored.
func (p *DLQPublisher) Publish(ctx context.Context, ev *event.IdentityEvent) error {
	if ev == nil {
		return fmt.Errorf("outbox: nil event")
	}

	// Fast path: no upstream configured → go straight to DLQ.
	if p.upstream == nil {
		return p.pushDLQ(ctx, ev, fmt.Errorf("no upstream publisher configured"))
	}

	cfg := retry.Config{
		MaxAttempts:  p.cfg.MaxRetries,
		InitialDelay: 50 * time.Millisecond,
		MaxDelay:     500 * time.Millisecond,
		Jitter:       0.1,
	}
	if p.cfg.RetryConfig != nil {
		cfg = *p.cfg.RetryConfig
	}

	var firstAttemptOK bool
	err := retry.Do(ctx, retry.Options{
		Config: cfg,
		Label:  "outbox.publish",
		OnRetry: func(attempt, maxAttempts int, delay time.Duration, err error) {
			slog.Warn("outbox: publish retry",
				"event_id", ev.EventID,
				"event_type", ev.EventType,
				"attempt", attempt,
				"max_attempts", maxAttempts,
				"delay", delay,
				"err", err,
			)
		},
	}, func(ctx context.Context) error {
		perr := p.upstream.Publish(ctx, ev)
		if perr == nil {
			firstAttemptOK = true
		}
		return perr
	})

	if err == nil {
		if firstAttemptOK && !wasRetried(&cfg) {
			outboxPublishedTotal.WithLabelValues(resultSuccess).Inc()
		} else {
			// firstAttemptOK is still true after a retry-success because
			// we only set it on nil. Distinguish by whether any retry
			// slept: the retry package always enters Do's loop, so if
			// MaxAttempts == 1 and success, it's a pure success; else
			// if the first call returned nil it's success (firstAttemptOK
			// was set in the attempt-1 closure). We cannot cheaply know
			// here, so use a single "success" label and rely on the retry
			// OnRetry log for the transient-failure audit trail.
			outboxPublishedTotal.WithLabelValues(resultSuccess).Inc()
		}
		return nil
	}

	// Retries exhausted — fall back to DLQ.
	slog.Error("outbox: publish retries exhausted, falling back to DLQ",
		"event_id", ev.EventID,
		"event_type", ev.EventType,
		"max_retries", cfg.MaxAttempts,
		"err", err,
	)
	return p.pushDLQ(ctx, ev, err)
}

// pushDLQ serializes ev into the configured Redis list and sets its TTL.
// If redis is nil the DLQ is disabled: we count the drop and return the
// original publish error so the caller can log it.
func (p *DLQPublisher) pushDLQ(ctx context.Context, ev *event.IdentityEvent, upstreamErr error) error {
	if p.redis == nil {
		outboxDLQDroppedTotal.Inc()
		slog.Error("outbox: DLQ unavailable, event dropped",
			"event_id", ev.EventID,
			"event_type", ev.EventType,
			"err", upstreamErr,
		)
		return fmt.Errorf("outbox: publish failed and DLQ disabled: %w", upstreamErr)
	}

	raw, jerr := json.Marshal(ev)
	if jerr != nil {
		// Marshalling IdentityEvent should never fail (all fields are
		// JSON-safe by construction) but guard anyway so a panic upstream
		// cannot crash the caller.
		outboxDLQDroppedTotal.Inc()
		return fmt.Errorf("outbox: marshal event for DLQ: %w", jerr)
	}

	// Use a fresh context with a short deadline: if the caller's ctx is
	// already cancelled (e.g. HTTP request completed), we still want to
	// best-effort persist the event.
	pushCtx, cancel := context.WithTimeout(detachContext(ctx), 2*time.Second)
	defer cancel()

	if err := p.redis.LPush(pushCtx, p.cfg.DLQKey, raw).Err(); err != nil {
		outboxDLQDroppedTotal.Inc()
		slog.Error("outbox: DLQ LPUSH failed",
			"event_id", ev.EventID,
			"dlq_key", p.cfg.DLQKey,
			"err", err,
		)
		return fmt.Errorf("outbox: LPUSH DLQ: %w (upstream: %v)", err, upstreamErr)
	}

	// Best-effort TTL refresh. A failure here is non-fatal — the list still
	// has the previous TTL (or none, if the key was brand new, which means
	// it lives at most until manual cleanup).
	_ = p.redis.Expire(pushCtx, p.cfg.DLQKey, p.cfg.DLQTTL).Err()

	outboxDLQPushedTotal.WithLabelValues(ev.EventType).Inc()
	slog.Warn("outbox: event parked in DLQ",
		"event_id", ev.EventID,
		"event_type", ev.EventType,
		"dlq_key", p.cfg.DLQKey,
		"upstream_err", upstreamErr,
	)
	return nil
}

// wasRetried is a placeholder for a future enrichment; kept as a local
// helper so we can later split the success counter into "immediate" vs
// "retried" without touching the call sites.
func wasRetried(_ *retry.Config) bool { return false }

// ── Prometheus metrics ─────────────────────────────────────────────────────

const (
	resultSuccess = "success"
)

var (
	outboxPublishedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "lurus_platform",
			Name:      "outbox_published_total",
			Help:      "Events successfully delivered by the DLQ-monitored publisher (includes retry-success).",
		},
		[]string{"result"}, // "success"; reserved label for future split.
	)

	outboxDLQPushedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "lurus_platform",
			Name:      "outbox_dlq_total",
			Help:      "Events parked in the Redis DLQ after all upstream retries were exhausted, partitioned by event type.",
		},
		[]string{"event_type"},
	)

	outboxDLQDroppedTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "lurus_platform",
			Name:      "outbox_dropped_total",
			Help:      "Events lost because the DLQ itself was unavailable (Redis down or no client configured). Alert if >0.",
		},
	)
)

// detachContext returns a context whose Deadline/Done/Err are independent of
// the parent, but whose Values are still propagated. This lets us write to
// the DLQ even after an HTTP request context was cancelled.
func detachContext(parent context.Context) context.Context {
	return detached{parent: parent}
}

type detached struct{ parent context.Context }

func (d detached) Deadline() (time.Time, bool) { return time.Time{}, false }
func (d detached) Done() <-chan struct{}       { return nil }
func (d detached) Err() error                  { return nil }
func (d detached) Value(k any) any             { return d.parent.Value(k) }
