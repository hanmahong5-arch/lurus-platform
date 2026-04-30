package newapi

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/metrics"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/tracing"
)

// Resilience layer for the NewAPI HTTP client (P1-1 + P1-2 + P1-7 in
// docs/平台硬化清单.md). All three concerns share one wrapper so a NewAPI
// call gets retry, circuit-breaker, and tracing in a single composable
// pass — `Client.do()` calls this and stays unchanged below the line.
//
// Why bundle the three:
//
//   - Retry without breaker = thundering herd against a dying NewAPI
//   - Breaker without retry = single transient blip ruins a user's request
//   - Adding tracing in a third place = three coordinated logs vs one
//
// "完全适配 NewAPI" (per ADR) means **NewAPI itself is untouched**. All
// resilience logic lives here, in the platform's HTTP client.

// ── Configuration ────────────────────────────────────────────────────────

// Retry policy. Tuned for NewAPI's typical p99 (<200ms) — three tries
// total with exponential backoff + jitter takes worst-case ~1.4 s, well
// inside the gateway's outer timeout. Aggressively retrying further would
// just stack pressure on a dying upstream.
const (
	defaultMaxAttempts   = 3
	defaultRetryBaseWait = 100 * time.Millisecond
	defaultRetryMaxWait  = 1 * time.Second
	defaultRetryJitter   = 0.3 // ±30% wall-time jitter on each backoff
)

// Breaker policy. The Closed→Open trip threshold is conservative: it
// takes 5 consecutive failures (i.e. retries already exhausted on each)
// before we stop calling NewAPI entirely. Open→HalfOpen window is short
// (15 s) so a recovered NewAPI is back online quickly; HalfOpen lets
// one probe through and Closes again on success.
const (
	defaultBreakerTrip       = 5
	defaultBreakerOpenWindow = 15 * time.Second
)

// breakerState is a tiny three-state machine. Strings are stable for
// metrics labels so don't rename them post-deploy.
type breakerState string

const (
	stateClosed   breakerState = "closed"
	stateOpen     breakerState = "open"
	stateHalfOpen breakerState = "half_open"
)

// ErrCircuitOpen is returned to the caller when the breaker is Open
// and the NewAPI call short-circuits without hitting the network. A
// distinct error type lets caller-level logic (e.g. /readyz soft probe,
// account_provisioned hook) classify "we tried but the upstream is
// known-down" separately from a fresh transport error.
var ErrCircuitOpen = errors.New("newapi: circuit breaker open (upstream is known-down)")

// resilience holds breaker state for a single Client instance. Created
// lazily so tests that don't need the wrapper stay cheap.
type resilience struct {
	mu               sync.Mutex
	state            breakerState
	consecutiveFails int
	openedAt         time.Time
	halfOpenInFlight bool

	// Tunables, exposed for tests. Production values come from the
	// constants above.
	maxAttempts   int
	baseWait      time.Duration
	maxWait       time.Duration
	jitter        float64
	tripThreshold int
	openWindow    time.Duration
}

func newResilience() *resilience {
	return &resilience{
		state:         stateClosed,
		maxAttempts:   defaultMaxAttempts,
		baseWait:      defaultRetryBaseWait,
		maxWait:       defaultRetryMaxWait,
		jitter:        defaultRetryJitter,
		tripThreshold: defaultBreakerTrip,
		openWindow:    defaultBreakerOpenWindow,
	}
}

// retriableError classifies whether a failure deserves another attempt.
// 4xx (caller bug) is never retried. 5xx + network errors + timeouts are.
//
// statusErr from do() carries an HTTP code; we test that explicitly. Any
// other error (DNS, conn reset, ctx deadline) is treated as transient.
//
// Caller-cancelled ctx (context.Canceled) is NEVER retried — that's the
// caller telling us to stop, not a transport hiccup.
func retriableError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	var se *statusErr
	if errors.As(err, &se) {
		// 408 Request Timeout, 429 Too Many Requests, 5xx — retriable.
		switch {
		case se.code == 408, se.code == 429:
			return true
		case se.code >= 500 && se.code <= 599:
			return true
		default:
			return false
		}
	}
	// All other errors (network, ctx.DeadlineExceeded, json) → transient.
	return true
}

// ── Breaker state transitions ─────────────────────────────────────────────

// shouldShortCircuit returns true when the breaker is Open and the
// open-window hasn't elapsed; i.e. the call should fail fast. When the
// window has elapsed, transitions Open→HalfOpen (allowing exactly one
// probe through) and returns false.
func (r *resilience) shouldShortCircuit() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	switch r.state {
	case stateClosed:
		return false
	case stateOpen:
		if time.Since(r.openedAt) >= r.openWindow {
			// Window elapsed — let exactly one probe through.
			r.state = stateHalfOpen
			r.halfOpenInFlight = true
			metrics.RecordNewAPISyncOp("breaker_half_open", "transition")
			return false
		}
		return true
	case stateHalfOpen:
		// Another caller's probe is in flight. Fail fast so we don't
		// pile pressure on the still-unverified upstream.
		if r.halfOpenInFlight {
			return true
		}
		// Probe slot free (rare race — previous probe finished but we
		// haven't transitioned yet). Take it.
		r.halfOpenInFlight = true
		return false
	}
	return false
}

// recordSuccess closes the breaker (whether we were Open via probe or
// just running normally). Resets failure counter.
func (r *resilience) recordSuccess() {
	r.mu.Lock()
	defer r.mu.Unlock()
	prev := r.state
	r.state = stateClosed
	r.consecutiveFails = 0
	r.halfOpenInFlight = false
	if prev != stateClosed {
		metrics.RecordNewAPISyncOp("breaker_closed", "transition")
	}
}

// recordFailure increments the failure counter and trips the breaker
// when the threshold is hit. In HalfOpen state, a single failure
// reverts to Open (the probe failed → upstream still bad).
func (r *resilience) recordFailure() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.halfOpenInFlight = false

	if r.state == stateHalfOpen {
		// Probe failed — back to Open.
		r.state = stateOpen
		r.openedAt = time.Now()
		metrics.RecordNewAPISyncOp("breaker_open", "transition")
		return
	}

	r.consecutiveFails++
	if r.state == stateClosed && r.consecutiveFails >= r.tripThreshold {
		r.state = stateOpen
		r.openedAt = time.Now()
		metrics.RecordNewAPISyncOp("breaker_open", "transition")
	}
}

// ── Public wrapper ────────────────────────────────────────────────────────

// resilientCall wraps `op` with retry, circuit-breaking, and tracing.
// Used by Client.do() and Client.Ping() so every outbound HTTP call has
// the same resilience profile.
//
// `name` is the span name + log key (e.g. "POST /api/user/").
//
// Returns ErrCircuitOpen immediately when the breaker is open, without
// running `op`.
func (r *resilience) call(ctx context.Context, name string, op func(ctx context.Context) error) error {
	if r == nil {
		// No resilience wired (test path) — execute as-is.
		return op(ctx)
	}
	if r.shouldShortCircuit() {
		metrics.RecordNewAPISyncOp("breaker_short_circuit", "blocked")
		return ErrCircuitOpen
	}

	tracer := tracing.Tracer("lurus-platform")
	ctx, span := tracer.Start(ctx, "newapi."+name)
	defer span.End()

	var lastErr error
	for attempt := 1; attempt <= r.maxAttempts; attempt++ {
		span.SetAttributes(attribute.Int("newapi.attempt", attempt))
		err := op(ctx)
		if err == nil {
			r.recordSuccess()
			span.SetStatus(codes.Ok, "")
			if attempt > 1 {
				span.SetAttributes(attribute.Bool("newapi.recovered_after_retry", true))
				metrics.RecordNewAPISyncOp("retry_recovered", "success")
			}
			return nil
		}
		lastErr = err

		if !retriableError(err) || attempt == r.maxAttempts {
			r.recordFailure()
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			span.SetAttributes(attribute.Int("newapi.attempts_used", attempt))
			if attempt == r.maxAttempts && retriableError(err) {
				metrics.RecordNewAPISyncOp("retry_exhausted", "error")
			}
			return err
		}

		// Retriable + budget remaining → backoff + retry.
		metrics.RecordNewAPISyncOp("retry_attempt", "transient")
		wait := r.backoffDuration(attempt)
		select {
		case <-ctx.Done():
			r.recordFailure()
			span.RecordError(ctx.Err())
			span.SetStatus(codes.Error, "context cancelled during backoff")
			return fmt.Errorf("newapi %s: %w (after %d attempts)", name, ctx.Err(), attempt)
		case <-time.After(wait):
			// retry
		}
	}
	// Defensive — loop should always return inside.
	return lastErr
}

// backoffDuration computes the wait for retry attempt N (1-indexed,
// where 1 = "after first failure, before second attempt"). Exponential
// growth with multiplicative jitter; capped at maxWait.
func (r *resilience) backoffDuration(attempt int) time.Duration {
	// attempt=1 → 0 backoff would mean "retry immediately"; we want
	// at least baseWait between attempts even on the first retry.
	exp := 1
	for i := 1; i < attempt; i++ {
		exp *= 2
	}
	wait := time.Duration(exp) * r.baseWait
	if wait > r.maxWait {
		wait = r.maxWait
	}
	// Jitter ±r.jitter. Math/rand is fine — we don't need crypto-strength
	// for backoff timing.
	if r.jitter > 0 {
		// jitter range [-r.jitter, +r.jitter]
		factor := 1.0 + (rand.Float64()*2-1)*r.jitter
		wait = time.Duration(float64(wait) * factor)
		if wait < 0 {
			wait = 0
		}
	}
	return wait
}
