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

// stalwartCapture records requests sent to the mock Stalwart server.
type stalwartCapture struct {
	mu       sync.Mutex
	method   string
	path     string
	body     map[string]any
	requests int
}

func newStalwartServer(t *testing.T, statusCode int, capture *stalwartCapture) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if capture != nil {
			capture.mu.Lock()
			capture.method = r.Method
			capture.path = r.URL.Path
			capture.requests++
			if r.Body != nil {
				var body map[string]any
				json.NewDecoder(r.Body).Decode(&body)
				capture.body = body
			}
			capture.mu.Unlock()
		}
		w.WriteHeader(statusCode)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func testMailModule(t *testing.T, srv *httptest.Server) *MailModule {
	t.Helper()
	return NewMailModule(MailConfig{
		Enabled:          true,
		StalwartAdminURL: srv.URL,
		StalwartUser:     "admin",
		StalwartPassword: "secret",
		DefaultQuotaMB:   1024,
		MailDomain:       "test.lurus.cn",
	})
}

// ── OnAccountCreated ──────────────────────────────────────────────────────

func TestMailModule_OnAccountCreated_Success(t *testing.T) {
	capture := &stalwartCapture{}
	srv := newStalwartServer(t, http.StatusOK, capture)
	m := testMailModule(t, srv)

	err := m.OnAccountCreated(context.Background(), &entity.Account{
		ID: 1, Username: "alice", Email: "alice@external.com",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capture.method != http.MethodPost {
		t.Errorf("method = %s, want POST", capture.method)
	}
	if capture.path != "/api/account" {
		t.Errorf("path = %s, want /api/account", capture.path)
	}
	// Verify emails array contains both addresses.
	emails, _ := capture.body["emails"].([]any)
	if len(emails) != 2 {
		t.Errorf("emails count = %d, want 2", len(emails))
	}
}

func TestMailModule_OnAccountCreated_WithAlias(t *testing.T) {
	capture := &stalwartCapture{}
	srv := newStalwartServer(t, http.StatusOK, capture)
	m := testMailModule(t, srv)

	// When personal email equals lurus email, only one email in array.
	err := m.OnAccountCreated(context.Background(), &entity.Account{
		ID: 2, Username: "bob", Email: "bob@test.lurus.cn",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	emails, _ := capture.body["emails"].([]any)
	if len(emails) != 1 {
		t.Errorf("emails count = %d, want 1 (same address deduped)", len(emails))
	}
}

func TestMailModule_OnAccountCreated_SameEmail(t *testing.T) {
	capture := &stalwartCapture{}
	srv := newStalwartServer(t, http.StatusOK, capture)
	m := testMailModule(t, srv)

	err := m.OnAccountCreated(context.Background(), &entity.Account{
		ID: 3, Username: "carol", Email: "",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	emails, _ := capture.body["emails"].([]any)
	if len(emails) != 1 {
		t.Errorf("emails count = %d, want 1 (no personal email)", len(emails))
	}
}

func TestMailModule_OnAccountCreated_EmptyUsername(t *testing.T) {
	capture := &stalwartCapture{}
	srv := newStalwartServer(t, http.StatusOK, capture)
	m := testMailModule(t, srv)

	err := m.OnAccountCreated(context.Background(), &entity.Account{ID: 4, Username: ""})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capture.requests != 0 {
		t.Error("should not call Stalwart for empty username")
	}
}

func TestMailModule_OnAccountCreated_PhoneUsername(t *testing.T) {
	capture := &stalwartCapture{}
	srv := newStalwartServer(t, http.StatusOK, capture)
	m := testMailModule(t, srv)

	err := m.OnAccountCreated(context.Background(), &entity.Account{ID: 5, Username: "13800138000"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capture.requests != 0 {
		t.Error("should not call Stalwart for phone-number username")
	}
}

func TestMailModule_OnAccountCreated_Stalwart500(t *testing.T) {
	srv := newStalwartServer(t, http.StatusInternalServerError, nil)
	m := testMailModule(t, srv)

	err := m.OnAccountCreated(context.Background(), &entity.Account{ID: 6, Username: "dave"})
	if err == nil {
		t.Fatal("expected error for Stalwart 500, got nil")
	}
}

func TestMailModule_OnAccountCreated_StalwartTimeout(t *testing.T) {
	// Closed server simulates timeout/connection refused.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	m := NewMailModule(MailConfig{
		Enabled:          true,
		StalwartAdminURL: srv.URL,
		StalwartUser:     "admin",
		StalwartPassword: "secret",
		MailDomain:       "test.lurus.cn",
	})

	err := m.OnAccountCreated(context.Background(), &entity.Account{ID: 7, Username: "eve"})
	if err == nil {
		t.Fatal("expected error for connection refused, got nil")
	}
}

// ── OnAccountDeleted ──────────────────────────────────────────────────────

func TestMailModule_OnAccountDeleted_Success(t *testing.T) {
	capture := &stalwartCapture{}
	srv := newStalwartServer(t, http.StatusOK, capture)
	m := testMailModule(t, srv)

	err := m.OnAccountDeleted(context.Background(), &entity.Account{ID: 8, Username: "frank"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capture.method != http.MethodDelete {
		t.Errorf("method = %s, want DELETE", capture.method)
	}
	if capture.path != "/api/account/frank@test.lurus.cn" {
		t.Errorf("path = %s, want /api/account/frank@test.lurus.cn", capture.path)
	}
}

func TestMailModule_OnAccountDeleted_EmptyUsername(t *testing.T) {
	capture := &stalwartCapture{}
	srv := newStalwartServer(t, http.StatusOK, capture)
	m := testMailModule(t, srv)

	err := m.OnAccountDeleted(context.Background(), &entity.Account{ID: 9, Username: ""})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capture.requests != 0 {
		t.Error("should not call Stalwart for empty username")
	}
}

func TestMailModule_OnAccountDeleted_404(t *testing.T) {
	srv := newStalwartServer(t, http.StatusNotFound, nil)
	m := testMailModule(t, srv)

	err := m.OnAccountDeleted(context.Background(), &entity.Account{ID: 10, Username: "ghost"})
	if err == nil {
		t.Fatal("expected error for Stalwart 404, got nil")
	}
}

// ── OnPlanChanged ─────────────────────────────────────────────────────────

func TestMailModule_OnPlanChanged_WithQuota(t *testing.T) {
	capture := &stalwartCapture{}
	srv := newStalwartServer(t, http.StatusOK, capture)
	m := testMailModule(t, srv)

	features, _ := json.Marshal(map[string]any{"mail_quota_mb": 2048.0})
	err := m.OnPlanChanged(context.Background(),
		&entity.Account{ID: 11, Username: "grace"},
		&entity.ProductPlan{Features: features},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capture.method != http.MethodPatch {
		t.Errorf("method = %s, want PATCH", capture.method)
	}
	quota, _ := capture.body["quota"].(float64)
	expectedQuota := float64(2048 * 1024 * 1024)
	if quota != expectedQuota {
		t.Errorf("quota = %f, want %f", quota, expectedQuota)
	}
}

func TestMailModule_OnPlanChanged_NoQuota(t *testing.T) {
	capture := &stalwartCapture{}
	srv := newStalwartServer(t, http.StatusOK, capture)
	m := testMailModule(t, srv)

	err := m.OnPlanChanged(context.Background(),
		&entity.Account{ID: 12, Username: "henry"},
		&entity.ProductPlan{},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Default quota (1024 MB) should be used.
	quota, _ := capture.body["quota"].(float64)
	expectedQuota := float64(1024 * 1024 * 1024)
	if quota != expectedQuota {
		t.Errorf("quota = %f, want %f (default)", quota, expectedQuota)
	}
}

func TestMailModule_OnPlanChanged_MalformedJSON(t *testing.T) {
	capture := &stalwartCapture{}
	srv := newStalwartServer(t, http.StatusOK, capture)
	m := testMailModule(t, srv)

	err := m.OnPlanChanged(context.Background(),
		&entity.Account{ID: 13, Username: "ivy"},
		&entity.ProductPlan{Features: []byte(`{invalid`)},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Malformed JSON falls back to default quota.
	quota, _ := capture.body["quota"].(float64)
	expectedQuota := float64(1024 * 1024 * 1024)
	if quota != expectedQuota {
		t.Errorf("quota = %f, want %f (default fallback)", quota, expectedQuota)
	}
}

func TestMailModule_OnPlanChanged_ZeroQuota(t *testing.T) {
	capture := &stalwartCapture{}
	srv := newStalwartServer(t, http.StatusOK, capture)
	m := testMailModule(t, srv)

	features, _ := json.Marshal(map[string]any{"mail_quota_mb": 0.0})
	err := m.OnPlanChanged(context.Background(),
		&entity.Account{ID: 14, Username: "jack"},
		&entity.ProductPlan{Features: features},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Zero quota falls back to default.
	quota, _ := capture.body["quota"].(float64)
	expectedQuota := float64(1024 * 1024 * 1024)
	if quota != expectedQuota {
		t.Errorf("quota = %f, want %f (default for zero)", quota, expectedQuota)
	}
}

func TestMailModule_OnPlanChanged_EmptyUsername(t *testing.T) {
	capture := &stalwartCapture{}
	srv := newStalwartServer(t, http.StatusOK, capture)
	m := testMailModule(t, srv)

	err := m.OnPlanChanged(context.Background(),
		&entity.Account{ID: 15, Username: ""},
		&entity.ProductPlan{},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capture.requests != 0 {
		t.Error("should not call Stalwart for empty username")
	}
}

// ── Register ──────────────────────────────────────────────────────────────

func TestMailModule_Register_HookCount(t *testing.T) {
	srv := newStalwartServer(t, http.StatusOK, nil)
	m := testMailModule(t, srv)
	r := NewRegistry()

	m.Register(r)

	// Mail module registers 3 hooks: account_created, account_deleted, plan_changed.
	if r.HookCount() != 3 {
		t.Errorf("HookCount = %d, want 3", r.HookCount())
	}
}

func TestMailModule_Register_CustomDomain(t *testing.T) {
	srv := newStalwartServer(t, http.StatusOK, nil)
	m := NewMailModule(MailConfig{
		Enabled:          true,
		StalwartAdminURL: srv.URL,
		MailDomain:       "custom.example.com",
	})

	email := m.lurusEmail(&entity.Account{Username: "test"})
	if email != "test@custom.example.com" {
		t.Errorf("email = %s, want test@custom.example.com", email)
	}
}

func TestMailModule_DefaultValues(t *testing.T) {
	srv := newStalwartServer(t, http.StatusOK, nil)
	m := NewMailModule(MailConfig{
		Enabled:          true,
		StalwartAdminURL: srv.URL,
		// DefaultQuotaMB and MailDomain left empty.
	})

	email := m.lurusEmail(&entity.Account{Username: "default"})
	if email != "default@lurus.cn" {
		t.Errorf("email = %s, want default@lurus.cn", email)
	}
}
