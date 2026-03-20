package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestMaxBodySize_AllowsSmallBody(t *testing.T) {
	r := testRouter()
	r.Use(MaxBodySize(1024))
	r.POST("/test", func(c *gin.Context) {
		var req map[string]string
		if err := c.ShouldBindJSON(&req); err != nil {
			handleBindError(c, err)
			return
		}
		c.String(200, "ok")
	})

	body := `{"key":"value"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestMaxBodySize_RejectsLargeBody(t *testing.T) {
	r := testRouter()
	r.Use(MaxBodySize(100)) // 100 bytes limit
	r.POST("/test", func(c *gin.Context) {
		var req map[string]string
		if err := c.ShouldBindJSON(&req); err != nil {
			handleBindError(c, err)
			return
		}
		c.String(200, "ok")
	})

	body := strings.Repeat("x", 200) // 200 bytes > 100 limit
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code == 200 {
		t.Error("expected rejection for oversized body")
	}
}

func TestMaxBodySize_NilBody(t *testing.T) {
	r := testRouter()
	r.Use(MaxBodySize(1024))
	r.GET("/test", func(c *gin.Context) {
		c.String(200, "ok")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200 for nil body GET", w.Code)
	}
}

func TestRequestTimeout_NormalRequest(t *testing.T) {
	r := testRouter()
	r.Use(RequestTimeout(2 * time.Second))
	r.GET("/fast", func(c *gin.Context) {
		c.String(200, "fast")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/fast", nil)
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestRequestTimeout_ContextCancelled(t *testing.T) {
	r := testRouter()
	r.Use(RequestTimeout(50 * time.Millisecond))
	r.GET("/slow", func(c *gin.Context) {
		// Simulate a handler that respects context cancellation.
		select {
		case <-time.After(500 * time.Millisecond):
			c.String(200, "too late")
		case <-c.Request.Context().Done():
			c.String(http.StatusGatewayTimeout, "timed out")
			return
		}
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/slow", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusGatewayTimeout {
		t.Errorf("status = %d, want 504", w.Code)
	}
}

func TestRequestTimeout_PropagatesDeadline(t *testing.T) {
	r := testRouter()
	r.Use(RequestTimeout(5 * time.Second))
	r.GET("/check", func(c *gin.Context) {
		deadline, ok := c.Request.Context().Deadline()
		if !ok {
			c.String(500, "no deadline set")
			return
		}
		remaining := time.Until(deadline)
		if remaining < 4*time.Second || remaining > 6*time.Second {
			c.String(500, "unexpected deadline")
			return
		}
		c.String(200, "ok")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/check", nil)
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}
