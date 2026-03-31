package module

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

type notifCapture struct {
	mu       sync.Mutex
	requests []notifyRequest
	authHdr  string
	contentType string
}

func newNotifServer(t *testing.T, statusCode int, capture *notifCapture) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if capture != nil {
			capture.mu.Lock()
			capture.authHdr = r.Header.Get("Authorization")
			capture.contentType = r.Header.Get("Content-Type")
			var req notifyRequest
			json.NewDecoder(r.Body).Decode(&req)
			capture.requests = append(capture.requests, req)
			capture.mu.Unlock()
		}
		w.WriteHeader(statusCode)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func testNotifModule(t *testing.T, srv *httptest.Server) *NotificationModule {
	t.Helper()
	return NewNotificationModule(NotificationConfig{
		Enabled:    true,
		ServiceURL: srv.URL,
		APIKey:     "test-api-key",
	})
}

// ── OnAccountCreated ──────────────────────────────────────────────────────

func TestNotificationModule_OnAccountCreated_Success(t *testing.T) {
	capture := &notifCapture{}
	srv := newNotifServer(t, http.StatusOK, capture)
	m := testNotifModule(t, srv)

	err := m.OnAccountCreated(context.Background(), &entity.Account{
		ID: 1, DisplayName: "Alice", LurusID: "LU0000001", Email: "alice@test.com",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(capture.requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(capture.requests))
	}
	req := capture.requests[0]
	if req.EventType != "identity.account.created" {
		t.Errorf("event_type = %s, want identity.account.created", req.EventType)
	}
	if req.AccountID != 1 {
		t.Errorf("account_id = %d, want 1", req.AccountID)
	}
	if len(req.Channels) != 2 {
		t.Errorf("channels count = %d, want 2 (in_app + email)", len(req.Channels))
	}
	if req.EmailAddr != "alice@test.com" {
		t.Errorf("email_addr = %s, want alice@test.com", req.EmailAddr)
	}
}

func TestNotificationModule_OnAccountCreated_ServiceDown(t *testing.T) {
	srv := newNotifServer(t, http.StatusInternalServerError, nil)
	m := testNotifModule(t, srv)

	err := m.OnAccountCreated(context.Background(), &entity.Account{ID: 2, DisplayName: "Bob"})
	if err == nil {
		t.Fatal("expected error for service down, got nil")
	}
}

func TestNotificationModule_OnAccountCreated_EventIDFormat(t *testing.T) {
	capture := &notifCapture{}
	srv := newNotifServer(t, http.StatusOK, capture)
	m := testNotifModule(t, srv)

	_ = m.OnAccountCreated(context.Background(), &entity.Account{ID: 42, DisplayName: "Eve"})

	if len(capture.requests) != 1 {
		t.Fatal("expected 1 request")
	}
	if capture.requests[0].EventID != "welcome_42" {
		t.Errorf("event_id = %s, want welcome_42", capture.requests[0].EventID)
	}
}

// ── OnCheckin ─────────────────────────────────────────────────────────────

func TestNotificationModule_OnCheckin_Regular(t *testing.T) {
	capture := &notifCapture{}
	srv := newNotifServer(t, http.StatusOK, capture)
	m := testNotifModule(t, srv)

	err := m.OnCheckin(context.Background(), 1, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Regular check-in: only 1 notification (no milestone).
	if len(capture.requests) != 1 {
		t.Fatalf("expected 1 request for non-milestone, got %d", len(capture.requests))
	}
	if capture.requests[0].EventType != "identity.checkin.success" {
		t.Errorf("event_type = %s, want identity.checkin.success", capture.requests[0].EventType)
	}
}

func TestNotificationModule_OnCheckin_Milestone7(t *testing.T) {
	capture := &notifCapture{}
	srv := newNotifServer(t, http.StatusOK, capture)
	m := testNotifModule(t, srv)

	err := m.OnCheckin(context.Background(), 1, 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Milestone 7: regular + milestone = 2 notifications.
	if len(capture.requests) != 2 {
		t.Fatalf("expected 2 requests for 7-day milestone, got %d", len(capture.requests))
	}
	if capture.requests[1].EventType != "identity.checkin.milestone" {
		t.Errorf("second event_type = %s, want identity.checkin.milestone", capture.requests[1].EventType)
	}
}

func TestNotificationModule_OnCheckin_Milestone30(t *testing.T) {
	capture := &notifCapture{}
	srv := newNotifServer(t, http.StatusOK, capture)
	m := testNotifModule(t, srv)

	err := m.OnCheckin(context.Background(), 1, 30)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(capture.requests) != 2 {
		t.Fatalf("expected 2 requests for 30-day milestone, got %d", len(capture.requests))
	}
}

func TestNotificationModule_OnCheckin_Milestone100(t *testing.T) {
	capture := &notifCapture{}
	srv := newNotifServer(t, http.StatusOK, capture)
	m := testNotifModule(t, srv)

	err := m.OnCheckin(context.Background(), 1, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(capture.requests) != 2 {
		t.Fatalf("expected 2 requests for 100-day milestone, got %d", len(capture.requests))
	}
}

func TestNotificationModule_OnCheckin_NonMilestone(t *testing.T) {
	capture := &notifCapture{}
	srv := newNotifServer(t, http.StatusOK, capture)
	m := testNotifModule(t, srv)

	_ = m.OnCheckin(context.Background(), 1, 15)
	if len(capture.requests) != 1 {
		t.Errorf("expected 1 request for non-milestone streak 15, got %d", len(capture.requests))
	}
}

func TestNotificationModule_OnCheckin_ServiceError(t *testing.T) {
	srv := newNotifServer(t, http.StatusInternalServerError, nil)
	m := testNotifModule(t, srv)

	err := m.OnCheckin(context.Background(), 1, 5)
	if err == nil {
		t.Fatal("expected error for service failure, got nil")
	}
}

// ── OnReferralSignup ──────────────────────────────────────────────────────

func TestNotificationModule_OnReferralSignup_Success(t *testing.T) {
	capture := &notifCapture{}
	srv := newNotifServer(t, http.StatusOK, capture)
	m := testNotifModule(t, srv)

	err := m.OnReferralSignup(context.Background(), 10, "NewUser")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(capture.requests) != 1 {
		t.Fatal("expected 1 request")
	}
	req := capture.requests[0]
	if req.EventType != "identity.referral.signup" {
		t.Errorf("event_type = %s, want identity.referral.signup", req.EventType)
	}
	if req.Vars["referred_name"] != "NewUser" {
		t.Errorf("referred_name = %s, want NewUser", req.Vars["referred_name"])
	}
}

func TestNotificationModule_OnReferralSignup_ServiceError(t *testing.T) {
	srv := newNotifServer(t, http.StatusServiceUnavailable, nil)
	m := testNotifModule(t, srv)

	err := m.OnReferralSignup(context.Background(), 10, "User")
	if err == nil {
		t.Fatal("expected error for service failure, got nil")
	}
}

// ── Register ──────────────────────────────────────────────────────────────

func TestNotificationModule_Register(t *testing.T) {
	srv := newNotifServer(t, http.StatusOK, nil)
	m := testNotifModule(t, srv)
	r := NewRegistry()

	m.Register(r)

	// Notification module registers 3 hooks: account_created, checkin, referral_signup.
	if r.HookCount() != 3 {
		t.Errorf("HookCount = %d, want 3", r.HookCount())
	}
}

// ── Auth & Content-Type ───────────────────────────────────────────────────

func TestNotificationModule_AuthHeader(t *testing.T) {
	capture := &notifCapture{}
	srv := newNotifServer(t, http.StatusOK, capture)
	m := testNotifModule(t, srv)

	_ = m.OnAccountCreated(context.Background(), &entity.Account{ID: 1, DisplayName: "A"})

	if capture.authHdr != "Bearer test-api-key" {
		t.Errorf("auth header = %s, want Bearer test-api-key", capture.authHdr)
	}
	if capture.contentType != "application/json" {
		t.Errorf("content-type = %s, want application/json", capture.contentType)
	}
}
