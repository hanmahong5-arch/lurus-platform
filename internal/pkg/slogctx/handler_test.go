package slogctx

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestWithAccountID(t *testing.T) {
	ctx := WithAccountID(context.Background(), 42)
	v := ctx.Value(keyAccountID)
	if v == nil {
		t.Fatal("expected account_id in context")
	}
	if v.(int64) != 42 {
		t.Errorf("account_id = %v, want 42", v)
	}
}

func TestWithRequestID(t *testing.T) {
	ctx := WithRequestID(context.Background(), "req-abc")
	v := ctx.Value(keyRequestID)
	if v == nil {
		t.Fatal("expected request_id in context")
	}
	if v.(string) != "req-abc" {
		t.Errorf("request_id = %v, want 'req-abc'", v)
	}
}

func TestHandler_InjectsAccountAndRequestID(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	h := New(inner)
	logger := slog.New(h)

	ctx := context.Background()
	ctx = WithAccountID(ctx, 99)
	ctx = WithRequestID(ctx, "req-xyz")

	logger.InfoContext(ctx, "test message")

	var entry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("unmarshal log: %v (raw: %s)", err, buf.String())
	}

	if entry["account_id"] != "99" {
		t.Errorf("account_id = %v, want '99'", entry["account_id"])
	}
	if entry["request_id"] != "req-xyz" {
		t.Errorf("request_id = %v, want 'req-xyz'", entry["request_id"])
	}
}

func TestHandler_NoContextValues(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	h := New(inner)
	logger := slog.New(h)

	logger.InfoContext(context.Background(), "test message")

	var entry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Should not have account_id or request_id.
	if _, ok := entry["account_id"]; ok {
		t.Error("unexpected account_id in log without context value")
	}
	if _, ok := entry["request_id"]; ok {
		t.Error("unexpected request_id in log without context value")
	}
}

func TestHandler_Enabled(t *testing.T) {
	inner := slog.NewJSONHandler(nil, &slog.HandlerOptions{Level: slog.LevelWarn})
	h := New(inner)

	if h.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("expected Info to be disabled when level is Warn")
	}
	if !h.Enabled(context.Background(), slog.LevelWarn) {
		t.Error("expected Warn to be enabled")
	}
}

func TestHandler_WithAttrs(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	h := New(inner)

	h2 := h.WithAttrs([]slog.Attr{slog.String("service", "platform")})
	logger := slog.New(h2)
	logger.InfoContext(context.Background(), "test")

	if !strings.Contains(buf.String(), `"service":"platform"`) {
		t.Errorf("log = %s, missing service attr", buf.String())
	}
}

func TestHandler_WithGroup(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	h := New(inner)

	h2 := h.WithGroup("grp")
	logger := slog.New(h2)
	logger.InfoContext(context.Background(), "test", "key", "val")

	if !strings.Contains(buf.String(), `"grp"`) {
		t.Errorf("log = %s, missing group", buf.String())
	}
}

// ── Middleware ───────────────────────────────────────────────────────────────

func init() {
	gin.SetMode(gin.TestMode)
}

func TestMiddleware_GeneratesRequestID(t *testing.T) {
	router := gin.New()
	router.Use(Middleware())
	router.GET("/test", func(c *gin.Context) {
		rid := c.Writer.Header().Get("X-Request-ID")
		c.String(200, rid)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	rid := w.Body.String()
	if rid == "" {
		t.Error("expected non-empty X-Request-ID")
	}
	// UUID format: 8-4-4-4-12.
	if len(rid) != 36 {
		t.Errorf("request_id = %q, expected UUID format", rid)
	}
}

func TestMiddleware_PreservesExistingRequestID(t *testing.T) {
	router := gin.New()
	router.Use(Middleware())
	router.GET("/test", func(c *gin.Context) {
		rid := c.Writer.Header().Get("X-Request-ID")
		c.String(200, rid)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Request-ID", "custom-request-id")
	router.ServeHTTP(w, req)

	if w.Body.String() != "custom-request-id" {
		t.Errorf("request_id = %q, want 'custom-request-id'", w.Body.String())
	}
}
