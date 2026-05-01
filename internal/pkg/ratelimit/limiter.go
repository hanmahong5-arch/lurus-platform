// Package ratelimit provides Redis-backed sliding-window rate limiting middleware.
package ratelimit

import (
	"container/list"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"golang.org/x/time/rate"

	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/metrics"
)

const (
	// contextKeyAccountID must match auth.ContextKeyAccountID.
	contextKeyAccountID = "account_id"

	// fallbackQuotaMultiplier is the factor applied to the configured
	// Redis-path quota when serving traffic from the in-process fallback.
	// We intentionally run looser than Redis so a healthy-but-slow Redis
	// that blips briefly does not cause a user-visible burst of 429s; the
	// goal of the fallback is to preserve *basic* protection (no DoS
	// amplification via Redis outage) rather than pixel-perfect parity.
	fallbackQuotaMultiplier = 2

	// fallbackMaxEntries caps memory usage of the local bucket store.
	// With ~80 bytes/entry this is ~800 KB worst case — negligible next
	// to the rest of the process, and large enough to cover real
	// production per-IP cardinality for the short windows during which
	// Redis is unavailable. LRU eviction kicks in past this bound.
	fallbackMaxEntries = 10000
)

// Config holds rate limit thresholds.
type Config struct {
	// IPLimit is the maximum number of requests per IP per Window.
	IPLimit int
	// UserLimit is the maximum number of requests per authenticated user per Window.
	UserLimit int
	// Window is the sliding window duration.
	Window time.Duration
}

// DefaultConfig returns sensible production defaults.
func DefaultConfig(ipPerMinute, userPerMinute int) Config {
	return Config{
		IPLimit:   ipPerMinute,
		UserLimit: userPerMinute,
		Window:    time.Minute,
	}
}

// Limiter wraps the Redis client and config, exposing Gin middlewares.
type Limiter struct {
	rdb      *redis.Client
	cfg      Config
	fallback *localFallback
}

// New creates a Limiter. If rdb is nil, all middleware calls are no-ops (fail-open).
// The local fallback is always initialized: when Redis is non-nil but transiently
// unreachable, the limiter routes checks through the fallback instead of
// fail-opening, preventing Redis outages from becoming DoS amplifiers.
func New(rdb *redis.Client, cfg Config) *Limiter {
	return &Limiter{
		rdb:      rdb,
		cfg:      cfg,
		fallback: newLocalFallback(fallbackMaxEntries),
	}
}

// PerIP returns a middleware that limits requests by client IP.
func (l *Limiter) PerIP() gin.HandlerFunc {
	return func(c *gin.Context) {
		if l.rdb == nil {
			c.Next()
			return
		}
		ip := c.ClientIP()
		key := fmt.Sprintf("rl:ip:%s", ip)
		if !l.allow(c.Request.Context(), key, "ip", ip, l.cfg.IPLimit, l.cfg.Window) {
			retryAfter := int(l.cfg.Window.Seconds())
			c.Header("Retry-After", strconv.Itoa(retryAfter))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error":       "rate_limited",
				"message":     "Rate limit exceeded; retry later",
				"retry_after": retryAfter,
			})
			return
		}
		c.Next()
	}
}

// PerUser returns a middleware that limits requests by authenticated user ID.
// Must be applied after the JWT auth middleware.
func (l *Limiter) PerUser() gin.HandlerFunc {
	return func(c *gin.Context) {
		if l.rdb == nil {
			c.Next()
			return
		}
		v, exists := c.Get(contextKeyAccountID)
		if !exists {
			// Not authenticated — skip per-user limiting (per-IP still applies).
			c.Next()
			return
		}
		accountID, _ := v.(int64)
		if accountID == 0 {
			c.Next()
			return
		}
		key := fmt.Sprintf("rl:user:%d", accountID)
		bucketID := strconv.FormatInt(accountID, 10)
		if !l.allow(c.Request.Context(), key, "user", bucketID, l.cfg.UserLimit, l.cfg.Window) {
			retryAfter := int(l.cfg.Window.Seconds())
			c.Header("Retry-After", strconv.Itoa(retryAfter))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error":       "rate_limited",
				"message":     "Rate limit exceeded; retry later",
				"retry_after": retryAfter,
			})
			return
		}
		c.Next()
	}
}

// allow implements a Redis sliding-window counter with an in-process
// fallback when Redis is unreachable.
//
// Decision tree:
//
//   - Redis OK → sliding-window CAS decides. Fallback untouched.
//   - Redis error → delegate to fallback (token bucket at 2x quota).
//     Records ratelimit_fallback_engaged_total{scope}.
//
// scope/bucketID select which per-identity local bucket to use when falling
// back. Returns true iff the request is within the active limit.
func (l *Limiter) allow(ctx context.Context, key, scope, bucketID string, limit int, window time.Duration) bool {
	now := time.Now()
	windowStart := now.Add(-window)

	pipe := l.rdb.Pipeline()
	// Remove entries outside the window.
	pipe.ZRemRangeByScore(ctx, key, "0", strconv.FormatInt(windowStart.UnixNano(), 10))
	// Count remaining entries.
	countCmd := pipe.ZCard(ctx, key)
	// Add current request.
	pipe.ZAdd(ctx, key, redis.Z{Score: float64(now.UnixNano()), Member: now.UnixNano()})
	// Set TTL so the key expires automatically.
	pipe.Expire(ctx, key, window+time.Second)

	if _, err := pipe.Exec(ctx); err != nil {
		// Redis unavailable — defer to in-process bucket rather than
		// fail-open. A transient outage should not let attackers flood
		// the service at unlimited rate; the fallback preserves basic
		// protection at a deliberately looser quota (see multiplier).
		slog.Warn("ratelimit: redis error, using local fallback",
			"key", key, "scope", scope, "err", err)
		metrics.RecordRateLimitFallbackEngaged(scope)
		return l.fallback.allow(scope, bucketID, limit, window)
	}

	count := countCmd.Val()
	return count < int64(limit)
}

// ── Local fallback ──────────────────────────────────────────────────────────

// localFallback is an in-process token-bucket rate limiter used when Redis
// is unreachable. Each (scope, bucketID) pair gets its own rate.Limiter;
// entries are tracked in an LRU so memory is bounded regardless of key
// cardinality (e.g. attackers rotating IPs when Redis is down).
//
// Bucket fill rate is derived from the caller-supplied limit/window at first
// touch; subsequent calls reuse the same bucket. Changing the configured
// quota at runtime without a restart will not resize already-created
// buckets, which is acceptable for a fallback path.
type localFallback struct {
	mu       sync.Mutex
	buckets  map[string]*list.Element // key = scope + "|" + bucketID
	lru      *list.List
	capacity int
}

type fallbackEntry struct {
	key     string
	limiter *rate.Limiter
}

func newLocalFallback(capacity int) *localFallback {
	return &localFallback{
		buckets:  make(map[string]*list.Element, capacity),
		lru:      list.New(),
		capacity: capacity,
	}
}

// allow returns true iff the request for (scope, bucketID) is within the
// loosened fallback quota (fallbackQuotaMultiplier × limit per window).
func (f *localFallback) allow(scope, bucketID string, limit int, window time.Duration) bool {
	if limit <= 0 || window <= 0 {
		// Defensive: a zero limit from bad config would otherwise allow
		// every request. Treat as "block" rather than "open".
		return false
	}
	key := scope + "|" + bucketID
	lim := f.getOrCreate(key, limit, window)
	return lim.Allow()
}

// getOrCreate returns the rate.Limiter for key, creating one if needed and
// evicting the oldest entry when capacity is reached. Holding the mutex
// across Allow() is avoided: we only lock to manage the map/LRU.
func (f *localFallback) getOrCreate(key string, limit int, window time.Duration) *rate.Limiter {
	f.mu.Lock()
	defer f.mu.Unlock()

	if el, ok := f.buckets[key]; ok {
		f.lru.MoveToFront(el)
		return el.Value.(*fallbackEntry).limiter
	}

	// Build a new token bucket. Fill rate and burst are both scaled by
	// fallbackQuotaMultiplier so the effective quota is looser than the
	// Redis path but still finite.
	effective := limit * fallbackQuotaMultiplier
	// Tokens per second = (effective requests per window).
	fillRate := rate.Limit(float64(effective) / window.Seconds())
	burst := effective
	if burst < 1 {
		burst = 1
	}
	lim := rate.NewLimiter(fillRate, burst)
	entry := &fallbackEntry{key: key, limiter: lim}
	el := f.lru.PushFront(entry)
	f.buckets[key] = el

	// LRU eviction: drop oldest until we are within capacity.
	for f.lru.Len() > f.capacity {
		oldest := f.lru.Back()
		if oldest == nil {
			break
		}
		f.lru.Remove(oldest)
		delete(f.buckets, oldest.Value.(*fallbackEntry).key)
	}
	return lim
}
