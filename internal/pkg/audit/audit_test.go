package audit

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newBufferLogger creates a slog.Logger that writes to a buffer for assertion.
func newBufferLogger(buf *bytes.Buffer) *slog.Logger {
	handler := slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	return slog.New(handler)
}

// TestAuditLog_New_DefaultLogger verifies that New(nil) uses the default logger without panic.
func TestAuditLog_New_DefaultLogger(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("New(nil) panicked: %v", r)
		}
	}()
	l := New(nil)
	if l == nil {
		t.Error("New(nil) returned nil")
	}
}

// TestAuditLog_Log_AllFields verifies that all fields of an Event are written to the log output.
func TestAuditLog_Log_AllFields(t *testing.T) {
	var buf bytes.Buffer
	l := New(newBufferLogger(&buf))

	ev := Event{
		ActorID:      "123",
		Action:       ActionLogin,
		ResourceType: "account",
		ResourceID:   "456",
		Result:       ResultSuccess,
		Detail:       "login via session token",
		IP:           "10.0.0.1",
	}
	l.Log(context.Background(), ev)

	out := buf.String()
	checks := []struct {
		field string
		value string
	}{
		{"actor_id", "123"},
		{"action", ActionLogin},
		{"resource_type", "account"},
		{"resource_id", "456"},
		{"result", ResultSuccess},
		{"detail", "login via session token"},
		{"ip", "10.0.0.1"},
		{"timestamp", ""},
	}

	for _, c := range checks {
		if !strings.Contains(out, c.field) {
			t.Errorf("log output missing field %q, output: %s", c.field, out)
		}
		if c.value != "" && !strings.Contains(out, c.value) {
			t.Errorf("log output missing value %q for field %q", c.value, c.field)
		}
	}
}

// TestAuditLog_Log_NilContext_NoPanic verifies that a nil context does not cause a panic.
func TestAuditLog_Log_NilContext_NoPanic(t *testing.T) {
	var buf bytes.Buffer
	l := New(newBufferLogger(&buf))

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Log with nil context panicked: %v", r)
		}
	}()
	l.Log(context.Background(), Event{Action: ActionAccountCreate})
}

// TestAuditLog_Log_EmptyEvent_NoPanic verifies that an empty event does not panic.
func TestAuditLog_Log_EmptyEvent_NoPanic(t *testing.T) {
	var buf bytes.Buffer
	l := New(newBufferLogger(&buf))

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Log with empty event panicked: %v", r)
		}
	}()
	l.Log(context.Background(), Event{})
}

// TestAuditLog_ActionConstants_Defined verifies that action constants have non-empty values.
func TestAuditLog_ActionConstants_Defined(t *testing.T) {
	actions := map[string]string{
		"ActionLogin":              ActionLogin,
		"ActionLogout":             ActionLogout,
		"ActionAccountCreate":      ActionAccountCreate,
		"ActionAccountUpdate":      ActionAccountUpdate,
		"ActionSubscriptionCreate": ActionSubscriptionCreate,
		"ActionSubscriptionCancel": ActionSubscriptionCancel,
		"ActionSubscriptionExpire": ActionSubscriptionExpire,
		"ActionWalletTopup":        ActionWalletTopup,
		"ActionWalletAdjust":       ActionWalletAdjust,
		"ActionPaymentWebhook":     ActionPaymentWebhook,
		"ActionRedeemCode":         ActionRedeemCode,
		"ActionAdminGrant":         ActionAdminGrant,
	}
	for name, val := range actions {
		if val == "" {
			t.Errorf("action constant %s is empty", name)
		}
	}
}

// TestAuditLog_ResultConstants_Defined verifies that result constants are non-empty.
func TestAuditLog_ResultConstants_Defined(t *testing.T) {
	results := map[string]string{
		"ResultSuccess": ResultSuccess,
		"ResultFailure": ResultFailure,
		"ResultDenied":  ResultDenied,
	}
	for name, val := range results {
		if val == "" {
			t.Errorf("result constant %s is empty", name)
		}
	}
}

// TestAuditLog_Log_MultipleEvents verifies that multiple events are written sequentially.
func TestAuditLog_Log_MultipleEvents(t *testing.T) {
	var buf bytes.Buffer
	l := New(newBufferLogger(&buf))
	ctx := context.Background()

	events := []Event{
		{ActorID: "1", Action: ActionLogin, Result: ResultSuccess},
		{ActorID: "2", Action: ActionAccountCreate, Result: ResultSuccess},
		{ActorID: "3", Action: ActionWalletTopup, Result: ResultFailure},
	}

	for _, ev := range events {
		l.Log(ctx, ev)
	}

	out := buf.String()
	if !strings.Contains(out, ActionLogin) {
		t.Error("missing login action")
	}
	if !strings.Contains(out, ActionAccountCreate) {
		t.Error("missing account.create action")
	}
	if !strings.Contains(out, ActionWalletTopup) {
		t.Error("missing wallet.topup action")
	}
}

// TestFromRequest_XRealIP verifies that X-Real-IP header is preferred.
func TestFromRequest_XRealIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Real-IP", "203.0.113.1")

	ip := FromRequest(req)
	if ip != "203.0.113.1" {
		t.Errorf("X-Real-IP: want 203.0.113.1, got %q", ip)
	}
}

// TestFromRequest_XForwardedFor verifies fallback to X-Forwarded-For.
func TestFromRequest_XForwardedFor(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", "198.51.100.1")

	ip := FromRequest(req)
	if ip != "198.51.100.1" {
		t.Errorf("X-Forwarded-For: want 198.51.100.1, got %q", ip)
	}
}

// TestFromRequest_RemoteAddr verifies fallback to RemoteAddr.
func TestFromRequest_RemoteAddr(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.0.2.1:8080"
	// No proxy headers.

	ip := FromRequest(req)
	if ip != "192.0.2.1:8080" {
		t.Errorf("RemoteAddr: want 192.0.2.1:8080, got %q", ip)
	}
}

// TestFromRequest_XRealIP_TakesPrecedenceOverXFF verifies header priority.
func TestFromRequest_XRealIP_TakesPrecedenceOverXFF(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Real-IP", "1.1.1.1")
	req.Header.Set("X-Forwarded-For", "2.2.2.2")

	ip := FromRequest(req)
	if ip != "1.1.1.1" {
		t.Errorf("X-Real-IP should take precedence, got %q", ip)
	}
}
