package newapi

import (
	"context"
	"sync"
	"time"
)

// ReadinessChecker is a readiness.Checker (declared via duck typing —
// no readiness import needed here) that pings NewAPI with a TTL'd cache,
// so /readyz can run frequently without hammering NewAPI.
//
// Wire it as a SOFT checker (readiness.Set.WithSoftChecker) — NewAPI is
// an OPTIONAL dependency for the platform: account creation and topup
// sync hooks degrade gracefully if NewAPI is down (their failures are
// logged + counted in metrics, not propagated to users). Failing /readyz
// would pull a healthy platform pod out of the LB, which is a worse
// outcome than serving correctly without the integration.
//
// Cache semantics:
//
//   - On miss / expired entry: probe runs synchronously, result memoised
//     for `ttl` (default 30 s). Failure is also cached — back-to-back
//     /readyz polls won't flood NewAPI during an outage.
//   - Cache update + read are mutex-protected; the probe call itself
//     happens with the mutex released so multiple concurrent /readyz
//     don't queue behind a single slow probe (worst case: brief
//     thundering herd of probes immediately after expiry; acceptable
//     given /readyz cadence and 3 s timeout).
//
// Closed lifecycle (Stop / shutdown) isn't needed — the checker holds
// no goroutines or external resources beyond the underlying *Client.
type ReadinessChecker struct {
	client *Client
	ttl    time.Duration

	mu       sync.Mutex
	cachedAt time.Time
	cachedOK bool
	cachedEr error
}

// DefaultReadinessTTL is the cache window between live NewAPI pings.
// 30 s balances probe responsiveness (operator sees a NewAPI outage
// reflected within half a minute) against load on NewAPI (k8s probes
// every 10 s would otherwise generate 8000+/day per pod).
const DefaultReadinessTTL = 30 * time.Second

// NewReadinessChecker constructs the checker. Returns nil when client is
// nil — readiness.Set drops nil checkers, so callers can pass the
// (potentially nil) module reference without an extra branch.
func NewReadinessChecker(client *Client) *ReadinessChecker {
	if client == nil {
		return nil
	}
	return &ReadinessChecker{client: client, ttl: DefaultReadinessTTL}
}

// WithTTL overrides the cache window. Mostly useful for tests; production
// should stick to the default unless there's a specific reason.
func (r *ReadinessChecker) WithTTL(ttl time.Duration) *ReadinessChecker {
	if r != nil && ttl > 0 {
		r.ttl = ttl
	}
	return r
}

// Name implements readiness.Checker.
func (r *ReadinessChecker) Name() string { return "newapi" }

// Check implements readiness.Checker. Returns nil on cache hit if last
// probe succeeded; runs a live probe on miss / expiry.
func (r *ReadinessChecker) Check(ctx context.Context) error {
	if r == nil {
		return nil
	}

	r.mu.Lock()
	if !r.cachedAt.IsZero() && time.Since(r.cachedAt) < r.ttl {
		// Cache hit. Return whatever the last probe said.
		ok, cachedErr := r.cachedOK, r.cachedEr
		r.mu.Unlock()
		if ok {
			return nil
		}
		return cachedErr
	}
	r.mu.Unlock()

	// Cache miss / expired — run a live probe with the mutex released so
	// concurrent callers don't queue. Idempotent in the sense that
	// multiple concurrent probes converge on the same cache entry; a
	// brief thundering herd is acceptable given /readyz cadence.
	probeErr := r.client.Ping(ctx)

	r.mu.Lock()
	r.cachedAt = time.Now()
	r.cachedOK = probeErr == nil
	r.cachedEr = probeErr
	r.mu.Unlock()

	return probeErr
}
