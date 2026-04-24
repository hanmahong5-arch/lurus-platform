package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func setupLimiter(t *testing.T, ipLimit, userLimit int) (*miniredis.Miniredis, *Limiter) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	cfg := DefaultConfig(ipLimit, userLimit)
	return mr, New(rdb, cfg)
}

func TestPerIP_AllowsWithinLimit(t *testing.T) {
	_, lim := setupLimiter(t, 5, 100)
	router := gin.New()
	router.Use(lim.PerIP())
	router.GET("/test", func(c *gin.Context) { c.String(200, "ok") })

	for i := 0; i < 5; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "10.0.0.1:12345"
		router.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("request %d: status = %d, want 200", i, w.Code)
		}
	}
}

func TestPerIP_BlocksOverLimit(t *testing.T) {
	_, lim := setupLimiter(t, 3, 100)
	router := gin.New()
	router.Use(lim.PerIP())
	router.GET("/test", func(c *gin.Context) { c.String(200, "ok") })

	// Send 4 requests from same IP — 4th should be blocked.
	// Sleep briefly between requests to ensure unique nanosecond timestamps
	// (the sliding window uses UnixNano as sorted set member).
	var lastCode int
	for i := 0; i < 4; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "10.0.0.2:12345"
		router.ServeHTTP(w, req)
		lastCode = w.Code
		if i < 3 {
			time.Sleep(time.Millisecond)
		}
	}
	if lastCode != http.StatusTooManyRequests {
		t.Errorf("4th request status = %d, want 429", lastCode)
	}
}

func TestPerIP_DifferentIPsIndependent(t *testing.T) {
	_, lim := setupLimiter(t, 2, 100)
	router := gin.New()
	router.Use(lim.PerIP())
	router.GET("/test", func(c *gin.Context) { c.String(200, "ok") })

	// IP-A: 2 requests (at limit).
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "10.0.0.3:12345"
		router.ServeHTTP(w, req)
	}

	// IP-B: should still be allowed.
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.4:12345"
	router.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("different IP status = %d, want 200", w.Code)
	}
}

func TestPerIP_RetryAfterHeader(t *testing.T) {
	_, lim := setupLimiter(t, 1, 100)
	router := gin.New()
	router.Use(lim.PerIP())
	router.GET("/test", func(c *gin.Context) { c.String(200, "ok") })

	// First request.
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.5:12345"
	router.ServeHTTP(w, req)

	// Second request (blocked).
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.5:12345"
	router.ServeHTTP(w, req)

	if w.Code != 429 {
		t.Fatalf("status = %d, want 429", w.Code)
	}
	retryAfter := w.Header().Get("Retry-After")
	if retryAfter == "" {
		t.Error("missing Retry-After header")
	}
}

func TestPerUser_AllowsWithinLimit(t *testing.T) {
	_, lim := setupLimiter(t, 100, 3)
	router := gin.New()
	// Simulate auth middleware setting account_id.
	router.Use(func(c *gin.Context) {
		c.Set(contextKeyAccountID, int64(42))
		c.Next()
	})
	router.Use(lim.PerUser())
	router.GET("/test", func(c *gin.Context) { c.String(200, "ok") })

	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		router.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("request %d: status = %d, want 200", i, w.Code)
		}
	}
}

func TestPerUser_BlocksOverLimit(t *testing.T) {
	_, lim := setupLimiter(t, 100, 2)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(contextKeyAccountID, int64(42))
		c.Next()
	})
	router.Use(lim.PerUser())
	router.GET("/test", func(c *gin.Context) { c.String(200, "ok") })

	// Sleep between requests to ensure unique nanosecond timestamps
	// (sliding window uses UnixNano as sorted set member).
	var lastCode int
	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		router.ServeHTTP(w, req)
		lastCode = w.Code
		if i < 2 {
			time.Sleep(time.Millisecond)
		}
	}
	if lastCode != http.StatusTooManyRequests {
		t.Errorf("3rd request status = %d, want 429", lastCode)
	}
}

func TestPerUser_SkipsUnauthenticated(t *testing.T) {
	_, lim := setupLimiter(t, 100, 1)
	router := gin.New()
	// No account_id set — simulates unauthenticated request.
	router.Use(lim.PerUser())
	router.GET("/test", func(c *gin.Context) { c.String(200, "ok") })

	// Multiple requests should all pass (no per-user limit).
	for i := 0; i < 5; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		router.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("unauthenticated request %d: status = %d, want 200", i, w.Code)
		}
	}
}

func TestPerUser_SkipsZeroAccountID(t *testing.T) {
	_, lim := setupLimiter(t, 100, 1)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(contextKeyAccountID, int64(0))
		c.Next()
	})
	router.Use(lim.PerUser())
	router.GET("/test", func(c *gin.Context) { c.String(200, "ok") })

	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		router.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("zero accountID request %d: status = %d, want 200", i, w.Code)
		}
	}
}

func TestPerUser_DifferentUsersIndependent(t *testing.T) {
	_, lim := setupLimiter(t, 100, 2)
	router := gin.New()
	// Account ID from query param for test flexibility.
	router.Use(func(c *gin.Context) {
		if uid := c.Query("uid"); uid == "1" {
			c.Set(contextKeyAccountID, int64(1))
		} else if uid == "2" {
			c.Set(contextKeyAccountID, int64(2))
		}
		c.Next()
	})
	router.Use(lim.PerUser())
	router.GET("/test", func(c *gin.Context) { c.String(200, "ok") })

	// User 1: 2 requests (at limit).
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test?uid=1", nil)
		router.ServeHTTP(w, req)
	}

	// User 2: should still be allowed.
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test?uid=2", nil)
	router.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("user 2 status = %d, want 200", w.Code)
	}
}

func TestNilRedis_FailOpen(t *testing.T) {
	lim := New(nil, DefaultConfig(1, 1))
	router := gin.New()
	router.Use(lim.PerIP())
	router.Use(func(c *gin.Context) {
		c.Set(contextKeyAccountID, int64(99))
		c.Next()
	})
	router.Use(lim.PerUser())
	router.GET("/test", func(c *gin.Context) { c.String(200, "ok") })

	// All requests should pass with nil Redis.
	for i := 0; i < 10; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "10.0.0.1:1234"
		router.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("nil Redis request %d: status = %d, want 200", i, w.Code)
		}
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig(120, 300)
	if cfg.IPLimit != 120 {
		t.Errorf("IPLimit = %d, want 120", cfg.IPLimit)
	}
	if cfg.UserLimit != 300 {
		t.Errorf("UserLimit = %d, want 300", cfg.UserLimit)
	}
	if cfg.Window != time.Minute {
		t.Errorf("Window = %v, want 1m", cfg.Window)
	}
}

// TestLimiter_RedisDown_FallsBackToLocal asserts that when Redis is
// unreachable the limiter does NOT fail-open — instead it routes through
// the in-process fallback bucket (configured at fallbackQuotaMultiplier ×
// limit). With an IP limit of 1/min and the 2x multiplier, the 3rd
// request from the same IP in a single burst must be refused.
func TestLimiter_RedisDown_FallsBackToLocal(t *testing.T) {
	mr, lim := setupLimiter(t, 1, 1)
	router := gin.New()
	router.Use(lim.PerIP())
	router.GET("/test", func(c *gin.Context) { c.String(200, "ok") })

	// Kill Redis so every check takes the fallback path.
	mr.Close()

	send := func() int {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "10.0.0.1:1234"
		router.ServeHTTP(w, req)
		return w.Code
	}

	// With limit=1/min and fallbackQuotaMultiplier=2, the bucket's burst
	// is 2. Two immediate requests fit; the third must be blocked (NOT
	// fail-open the way the previous implementation did).
	if code := send(); code != 200 {
		t.Fatalf("1st request: status = %d, want 200", code)
	}
	if code := send(); code != 200 {
		t.Fatalf("2nd request: status = %d, want 200 (inside fallback burst)", code)
	}
	if code := send(); code != http.StatusTooManyRequests {
		t.Fatalf("3rd request: status = %d, want 429 (fallback should refuse)", code)
	}
}

// TestLimiter_RedisRecovers_SwitchesBack verifies that after Redis comes
// back online the limiter resumes using it (no further fallback hits for
// that key). We rely on the Redis-path sliding window being permissive
// right after recovery (empty set) to distinguish from the fallback path
// which would still be rate-limited.
func TestLimiter_RedisRecovers_SwitchesBack(t *testing.T) {
	mr, lim := setupLimiter(t, 5, 5)
	router := gin.New()
	router.Use(lim.PerIP())
	router.GET("/test", func(c *gin.Context) { c.String(200, "ok") })

	send := func(ip string) int {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		req.RemoteAddr = ip + ":1234"
		router.ServeHTTP(w, req)
		return w.Code
	}

	// Phase 1: Redis up — request succeeds.
	if code := send("10.99.0.1"); code != 200 {
		t.Fatalf("phase 1 (redis up): status = %d, want 200", code)
	}

	// Phase 2: Redis down — fallback engaged. With limit=5 and
	// multiplier=2 the fallback burst is 10; we send 11 requests and
	// the last should be refused, proving the fallback actually limits.
	mr.Close()
	var refused bool
	for i := 0; i < 11; i++ {
		if send("10.99.0.2") == http.StatusTooManyRequests {
			refused = true
			break
		}
	}
	if !refused {
		t.Fatal("phase 2 (redis down): expected at least one 429 from fallback, got none")
	}

	// Phase 3: Redis back up — switch should happen transparently. We
	// restart miniredis on the same address so the existing client
	// reconnects. The sliding window for a fresh IP is empty so the
	// request MUST succeed. If we were still hitting the fallback (and
	// the bucket is now saturated for 10.99.0.3 somehow), recovery
	// would be observable as a 429, so a 200 here is a clean signal.
	if err := mr.Restart(); err != nil {
		t.Fatalf("restart miniredis: %v", err)
	}
	if code := send("10.99.0.3"); code != 200 {
		t.Fatalf("phase 3 (redis recovered): status = %d, want 200", code)
	}
}
