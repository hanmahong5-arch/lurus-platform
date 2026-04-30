// Package readiness implements the /readyz probe contract used by
// Kubernetes and other orchestrators to decide when a pod is ready to
// receive traffic. Liveness (/health) only answers "is the process
// alive"; readiness additionally asserts that every critical dependency
// (Redis, Postgres, NATS) is reachable right now.
//
// Design choices:
//
//   - Checkers are an explicit, composable interface rather than a grab
//     bag of boolean fields, so new dependencies can be added without
//     touching this package.
//   - Each Check is wrapped in its own 2 s timeout. A slow dependency is
//     indistinguishable from a down dependency for readiness purposes,
//     and we must never let a wedged probe hold up the rollout.
//   - Failures return 503 (not 500) so that K8s and most load balancers
//     treat the pod as NotReady and pull it out of the Service endpoints
//     automatically. 200 means all checkers passed.
//   - Response shape is stable JSON: { "ready": bool, "failures"?: [...] }
//     so alerting rules can grep on keys without string parsing.
package readiness

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	natsgo "github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
)

// defaultCheckTimeout bounds each individual Check call. The readiness
// probe itself should answer quickly; a slow dependency is treated the
// same as a failing one.
const defaultCheckTimeout = 2 * time.Second

// Checker is the contract for a single readiness probe.
//
// Name returns a stable, lowercase identifier used in the failure
// payload (e.g. "redis", "postgres", "nats"). Check returns a non-nil
// error when the dependency is unreachable or misbehaving. Implementations
// must honour ctx cancellation.
type Checker interface {
	Name() string
	Check(ctx context.Context) error
}

// Set is a collection of Checkers evaluated together on every probe.
// The zero value is a valid empty set that always reports ready.
//
// Two classes of checker:
//
//   - **Critical** (default): a failure flips Ready→false and the probe
//     returns 503; K8s pulls the pod from Service endpoints. Use for
//     Redis / Postgres / NATS — anything where serving traffic without
//     it is worse than serving none.
//
//   - **Soft** (added via WithSoftChecker): a failure surfaces in the
//     `degraded` field of the response body but **does not** flip Ready.
//     The probe still returns 200. Use for OPTIONAL deps (e.g. NewAPI
//     for newapi_sync) where the platform serves correctly without
//     them but operators want a cheap signal that the integration is
//     unhealthy. Avoids the trap of "pulling a healthy pod out of LB
//     because a non-critical sidecar service is flaky".
//
// Both classes share the same Checker interface and timeout machinery —
// the distinction is purely about how failures translate to HTTP status.
type Set struct {
	checkers     []Checker
	softCheckers []Checker
	// timeout bounds each individual check. Zero = defaultCheckTimeout.
	timeout time.Duration
}

// NewSet constructs a readiness Set from zero or more **critical** Checkers.
// Nil entries are silently dropped so callers can pass conditionally-wired
// dependencies (e.g. NATS when optional).
//
// To add soft (non-failing) checkers, chain WithSoftChecker after construction.
func NewSet(cs ...Checker) *Set {
	set := &Set{timeout: defaultCheckTimeout}
	for _, c := range cs {
		if c == nil {
			continue
		}
		set.checkers = append(set.checkers, c)
	}
	return set
}

// WithSoftChecker appends a soft checker. Its failure shows up in the
// `degraded` JSON field but the overall probe still returns 200 OK. nil
// is silently ignored (mirrors NewSet). Chainable.
func (s *Set) WithSoftChecker(c Checker) *Set {
	if s == nil || c == nil {
		return s
	}
	s.softCheckers = append(s.softCheckers, c)
	return s
}

// failure is the per-checker entry surfaced in the JSON response.
type failure struct {
	Name  string `json:"name"`
	Error string `json:"err"`
}

// response is the full readiness payload. Failures and Degraded are
// emitted only when non-empty — alerting tools that hard-code field
// presence as "dep is unhealthy" need a stable shape.
type response struct {
	Ready    bool      `json:"ready"`
	Failures []failure `json:"failures,omitempty"`
	Degraded []failure `json:"degraded,omitempty"`
}

// Evaluate runs every CRITICAL checker sequentially and returns the
// collected failures. Soft checkers are NOT evaluated here — use
// EvaluateAll if both classes are needed. Returned slice is nil (not
// empty) when all checkers passed, which mirrors the JSON omitempty
// behaviour. Sequential execution is intentional: the probe runs rarely,
// timeouts are short, and a goroutine fan-out would obscure per-dep
// error correlation in logs.
func (s *Set) Evaluate(ctx context.Context) []failure {
	return s.runCheckers(ctx, s.checkers)
}

// EvaluateAll runs both critical and soft checkers, returning each set
// independently. Used by HTTPHandler so the response can express the
// "ready but degraded" state distinctly from "fully ready".
func (s *Set) EvaluateAll(ctx context.Context) (critical, soft []failure) {
	return s.runCheckers(ctx, s.checkers), s.runCheckers(ctx, s.softCheckers)
}

func (s *Set) runCheckers(ctx context.Context, list []Checker) []failure {
	if len(list) == 0 {
		return nil
	}
	timeout := s.timeout
	if timeout <= 0 {
		timeout = defaultCheckTimeout
	}
	var failures []failure
	for _, c := range list {
		cctx, cancel := context.WithTimeout(ctx, timeout)
		err := c.Check(cctx)
		cancel()
		if err != nil {
			failures = append(failures, failure{Name: c.Name(), Error: err.Error()})
		}
	}
	return failures
}

// HTTPHandler returns a Gin handler that evaluates the set and writes
// the canonical readiness response:
//
//	all critical pass + soft pass     → 200 {"ready": true}
//	all critical pass + soft fail(s)  → 200 {"ready": true, "degraded": [...]}
//	any critical fail                  → 503 {"ready": false, "failures": [...]}
//
// Note `ready` stays true when only soft checks fail — the HTTP status
// is 200 in that case. K8s won't pull the pod, but operators see the
// degraded list at /readyz and via the underlying metrics.
//
// The response body is hand-written via json.Marshal (not c.JSON) so
// the Content-Type and shape are identical on both paths — some alerting
// tools treat a missing field as "test skipped" rather than "healthy".
func (s *Set) HTTPHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		critFails, softFails := s.EvaluateAll(c.Request.Context())
		resp := response{
			Ready:    len(critFails) == 0,
			Failures: critFails,
			Degraded: softFails,
		}
		status := http.StatusOK
		if !resp.Ready {
			status = http.StatusServiceUnavailable
		}
		body, _ := json.Marshal(resp)
		c.Data(status, "application/json; charset=utf-8", body)
	}
}

// ── Prebuilt checkers ──────────────────────────────────────────────────────

// redisChecker pings a Redis client.
type redisChecker struct{ rdb *redis.Client }

// RedisChecker returns a Checker that verifies the given Redis client
// responds to PING within the probe timeout. Returns nil when rdb is
// nil so callers don't have to special-case optional deployments.
func RedisChecker(rdb *redis.Client) Checker {
	if rdb == nil {
		return nil
	}
	return &redisChecker{rdb: rdb}
}

func (c *redisChecker) Name() string { return "redis" }

func (c *redisChecker) Check(ctx context.Context) error {
	return c.rdb.Ping(ctx).Err()
}

// postgresChecker pings a *sql.DB.
type postgresChecker struct{ db *sql.DB }

// PostgresChecker returns a Checker that verifies the given *sql.DB
// responds to PingContext. Pass nil to disable — useful when the
// database is optional for a given deployment profile.
func PostgresChecker(db *sql.DB) Checker {
	if db == nil {
		return nil
	}
	return &postgresChecker{db: db}
}

func (c *postgresChecker) Name() string { return "postgres" }

func (c *postgresChecker) Check(ctx context.Context) error {
	return c.db.PingContext(ctx)
}

// natsChecker snapshots the connection state of a *nats.Conn. Unlike
// Redis/Postgres there is no cheap round-trip on nats.Conn that takes a
// context, so we read the cached IsConnected flag. nats.go itself
// maintains this with its own reconnect goroutine.
type natsChecker struct{ nc *natsgo.Conn }

// NATSChecker returns a Checker that asserts the NATS client is
// currently connected. Passing nil yields a nil Checker so probes
// remain green in deployments without NATS.
func NATSChecker(nc *natsgo.Conn) Checker {
	if nc == nil {
		return nil
	}
	return &natsChecker{nc: nc}
}

func (c *natsChecker) Name() string { return "nats" }

// errNATSDisconnected is returned when IsConnected reports false. A
// sentinel error keeps the response body deterministic for test and
// alert matching.
type natsError string

func (e natsError) Error() string { return string(e) }

const errNATSDisconnected natsError = "nats connection is not currently connected"

func (c *natsChecker) Check(_ context.Context) error {
	if c.nc.IsConnected() {
		return nil
	}
	return errNATSDisconnected
}
