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

func TestRedisFailure_FailOpen(t *testing.T) {
	mr, lim := setupLimiter(t, 1, 1)
	router := gin.New()
	router.Use(lim.PerIP())
	router.GET("/test", func(c *gin.Context) { c.String(200, "ok") })

	// Close Redis.
	mr.Close()

	// Should fail-open and allow the request.
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	router.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("Redis failure status = %d, want 200 (fail-open)", w.Code)
	}
}
