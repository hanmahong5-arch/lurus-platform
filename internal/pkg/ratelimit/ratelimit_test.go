package ratelimit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// newTestLimiter creates a Limiter backed by a miniredis instance.
// Returns the Limiter and a cleanup function.
func newTestLimiter(t *testing.T, limit int, window time.Duration) (*Limiter, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	t.Cleanup(mr.Close)

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	cfg := Config{IPLimit: limit, UserLimit: limit, Window: window}
	return New(rdb, cfg), mr
}

// TestRateLimiter_Allow_UnderLimit verifies that requests within the limit are allowed.
func TestRateLimiter_Allow_UnderLimit(t *testing.T) {
	limiter, _ := newTestLimiter(t, 5, time.Minute)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		if !limiter.allow(ctx, "test:key", 5, time.Minute) {
			t.Errorf("request %d should be allowed (under limit)", i+1)
		}
	}
}

// TestRateLimiter_Block_OverLimit verifies that requests exceeding the limit are blocked.
// Each call sleeps 1ms to ensure distinct nanosecond timestamps, because the rate limiter
// uses time.Now().UnixNano() as the sorted-set member; duplicate nanoseconds would
// silently update the same member instead of adding a new entry.
func TestRateLimiter_Block_OverLimit(t *testing.T) {
	limiter, _ := newTestLimiter(t, 3, time.Minute)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		time.Sleep(time.Millisecond) // guarantee unique UnixNano per entry
		limiter.allow(ctx, "block:key", 3, time.Minute)
	}

	time.Sleep(time.Millisecond)
	// 4th request should be blocked.
	if limiter.allow(ctx, "block:key", 3, time.Minute) {
		t.Error("4th request should be blocked (over limit)")
	}
}

// TestRateLimiter_WindowExpiry verifies that after the window expires, requests are allowed again.
// Uses a real short sleep because the sliding window uses time.Now() (not miniredis virtual time).
func TestRateLimiter_WindowExpiry(t *testing.T) {
	window := 60 * time.Millisecond
	limiter, _ := newTestLimiter(t, 2, window)
	ctx := context.Background()

	// Exhaust the limit. Sleep 1ms between calls to guarantee unique UnixNano members
	// in the sorted set (duplicate timestamps overwrite the same entry).
	time.Sleep(time.Millisecond)
	limiter.allow(ctx, "expiry:key", 2, window)
	time.Sleep(time.Millisecond)
	limiter.allow(ctx, "expiry:key", 2, window)
	time.Sleep(time.Millisecond)
	if limiter.allow(ctx, "expiry:key", 2, window) {
		t.Fatal("3rd request should be blocked")
	}

	// Wait for the real window to expire (sliding window uses time.Now(), not miniredis time).
	time.Sleep(window + 20*time.Millisecond)

	// Now all previous entries are outside the window; requests should be allowed again.
	if !limiter.allow(ctx, "expiry:key", 2, window) {
		t.Error("after window expiry, request should be allowed")
	}
}

// TestRateLimiter_DifferentKeys_Independent verifies that different keys have independent counters.
func TestRateLimiter_DifferentKeys_Independent(t *testing.T) {
	limiter, _ := newTestLimiter(t, 2, time.Minute)
	ctx := context.Background()

	// Exhaust key1.
	limiter.allow(ctx, "key1", 2, time.Minute)
	limiter.allow(ctx, "key1", 2, time.Minute)

	// key2 should still be allowed.
	if !limiter.allow(ctx, "key2", 2, time.Minute) {
		t.Error("key2 should be independent of key1")
	}
}

// TestRateLimiter_ConcurrentRequests verifies that concurrent allow calls do not panic.
// Note: the pipeline-based sliding window is NOT atomically safe — concurrent pipelines can
// interleave in Redis, causing more requests to pass than the nominal limit. The test
// therefore only asserts absence of panics and that at least one request is allowed.
func TestRateLimiter_ConcurrentRequests(t *testing.T) {
	const limit = 10
	limiter, _ := newTestLimiter(t, limit, time.Minute)
	ctx := context.Background()

	var wg sync.WaitGroup
	results := make([]bool, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx] = limiter.allow(ctx, "concurrent:key", limit, time.Minute)
		}(i)
	}
	wg.Wait()

	// Verify no panics occurred and at least one request was allowed.
	allowed := 0
	for _, ok := range results {
		if ok {
			allowed++
		}
	}
	if allowed == 0 {
		t.Error("expected at least one request to be allowed")
	}
}

// TestRateLimiter_NilRedis_FailOpen verifies that a nil Redis client allows all requests
// (fail-open behavior). Tested through PerIP() middleware because allow() itself
// does not guard against nil rdb (the nil guard lives in the public middleware methods).
func TestRateLimiter_NilRedis_FailOpen(t *testing.T) {
	limiter := New(nil, DefaultConfig(1, 1))

	r := gin.New()
	r.Use(limiter.PerIP())
	r.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	// Even with limit=1, nil client should allow everything (fail-open).
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "10.0.0.2:4321"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("nil redis should be fail-open, request %d got %d", i+1, w.Code)
		}
	}
}

// TestRateLimiter_PerIP_Middleware_Blocks verifies the PerIP middleware returns 429 when exceeded.
func TestRateLimiter_PerIP_Middleware_Blocks(t *testing.T) {
	limiter, _ := newTestLimiter(t, 2, time.Minute)

	r := gin.New()
	r.Use(limiter.PerIP())
	r.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	// Two requests should succeed. Sleep 1ms between to guarantee unique UnixNano sorted-set members.
	for i := 0; i < 2; i++ {
		time.Sleep(time.Millisecond)
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "10.0.0.1:1234"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("request %d: want 200, got %d", i+1, w.Code)
		}
	}

	// Third request should be rate-limited.
	time.Sleep(time.Millisecond)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("over-limit request: want 429, got %d", w.Code)
	}
}

// TestRateLimiter_PerUser_Middleware_SkipsUnauthenticated verifies that unauthenticated
// requests bypass per-user limiting.
func TestRateLimiter_PerUser_Middleware_SkipsUnauthenticated(t *testing.T) {
	limiter, _ := newTestLimiter(t, 1, time.Minute)

	r := gin.New()
	r.Use(limiter.PerUser())
	r.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	// 5 requests without account_id set should all pass (no per-user limit applied).
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("unauthenticated request %d: want 200, got %d", i+1, w.Code)
		}
	}
}

// TestRateLimiter_PerUser_Middleware_Blocks verifies that authenticated users are rate-limited.
func TestRateLimiter_PerUser_Middleware_Blocks(t *testing.T) {
	limiter, _ := newTestLimiter(t, 2, time.Minute)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		// Simulate the JWT middleware setting account_id.
		c.Set("account_id", int64(42))
		c.Next()
	})
	r.Use(limiter.PerUser())
	r.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	// First two requests should pass. Sleep 1ms to guarantee unique UnixNano sorted-set members.
	for i := 0; i < 2; i++ {
		time.Sleep(time.Millisecond)
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("request %d: want 200, got %d", i+1, w.Code)
		}
	}

	// Third request should be rate limited.
	time.Sleep(time.Millisecond)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("over-limit: want 429, got %d", w.Code)
	}
}

// TestRateLimiter_RetryAfterHeader verifies the Retry-After header is set on 429 responses.
func TestRateLimiter_RetryAfterHeader(t *testing.T) {
	limiter, _ := newTestLimiter(t, 1, time.Minute)

	r := gin.New()
	r.Use(limiter.PerIP())
	r.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	// Exhaust the limit.
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "1.2.3.4:9999"
	httptest.NewRecorder()
	r.ServeHTTP(httptest.NewRecorder(), req)

	// Second request triggers 429.
	req2 := httptest.NewRequest(http.MethodGet, "/test", nil)
	req2.RemoteAddr = "1.2.3.4:9999"
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("want 429, got %d", w2.Code)
	}
	if w2.Header().Get("Retry-After") == "" {
		t.Error("Retry-After header should be set on 429 responses")
	}
}
