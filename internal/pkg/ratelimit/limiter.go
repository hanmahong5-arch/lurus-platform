// Package ratelimit provides Redis-backed sliding-window rate limiting middleware.
package ratelimit

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

const (
	// contextKeyAccountID must match auth.ContextKeyAccountID.
	contextKeyAccountID = "account_id"
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
	rdb *redis.Client
	cfg Config
}

// New creates a Limiter. If rdb is nil, all middleware calls are no-ops (fail-open).
func New(rdb *redis.Client, cfg Config) *Limiter {
	return &Limiter{rdb: rdb, cfg: cfg}
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
		if !l.allow(c.Request.Context(), key, l.cfg.IPLimit, l.cfg.Window) {
			retryAfter := int(l.cfg.Window.Seconds())
			c.Header("Retry-After", strconv.Itoa(retryAfter))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error":       "rate limit exceeded",
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
		if !l.allow(c.Request.Context(), key, l.cfg.UserLimit, l.cfg.Window) {
			retryAfter := int(l.cfg.Window.Seconds())
			c.Header("Retry-After", strconv.Itoa(retryAfter))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error":       "rate limit exceeded",
				"retry_after": retryAfter,
			})
			return
		}
		c.Next()
	}
}

// allow implements a Redis sliding-window counter.
// Returns true if the request is within the limit; false if it exceeds it.
// On Redis failure the function fails-open (returns true) and logs a warning.
func (l *Limiter) allow(ctx context.Context, key string, limit int, window time.Duration) bool {
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
		slog.Warn("ratelimit: redis error, failing open", "key", key, "err", err)
		return true // fail-open: do not block requests if Redis is unavailable
	}

	count := countCmd.Val()
	return count < int64(limit)
}
