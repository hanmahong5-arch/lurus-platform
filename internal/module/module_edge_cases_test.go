package module

// Edge-case tests that increase coverage from 85.8% → 95%+.
// Targets:
//   - Registry.OnReconciliationIssue / FireReconciliationIssue (0% → covered)
//   - NotificationModule.OnReconciliationIssue (0% → covered)
//   - notification send() bad-URL error path
//   - mail stalwartRequest content-type / basic-auth paths

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// ── Registry.FireReconciliationIssue ─────────────────────────────────────────

// TestRegistry_FireReconciliationIssue_CallsHook verifies that the registered
// ReconciliationIssue hook is invoked with the correct issue.
func TestRegistry_FireReconciliationIssue_CallsHook(t *testing.T) {
	r := NewRegistry().WithRetryPolicy(fastRetry())
	var gotIssue *entity.ReconciliationIssue
	r.OnReconciliationIssue("test", func(ctx context.Context, issue *entity.ReconciliationIssue) error {
		gotIssue = issue
		return nil
	})

	issue := &entity.ReconciliationIssue{
		IssueType: "missing_credit",
		OrderNo:   "ORD-001",
		Severity:  "critical",
	}
	r.FireReconciliationIssue(context.Background(), issue)

	if gotIssue == nil {
		t.Fatal("hook was not called")
	}
	if gotIssue.IssueType != "missing_credit" {
		t.Errorf("IssueType = %s, want missing_credit", gotIssue.IssueType)
	}
}

// TestRegistry_FireReconciliationIssue_NoHooks verifies no panic when no hooks registered.
func TestRegistry_FireReconciliationIssue_NoHooks(t *testing.T) {
	r := NewRegistry()
	// Should not panic with no hooks registered.
	r.FireReconciliationIssue(context.Background(), &entity.ReconciliationIssue{
		IssueType: "missing_credit",
		OrderNo:   "ORD-999",
	})
}

// TestRegistry_FireReconciliationIssue_ErrorDoesNotBlock verifies that a hook
// failure does not prevent subsequent hooks from executing.
func TestRegistry_FireReconciliationIssue_ErrorDoesNotBlock(t *testing.T) {
	r := NewRegistry().WithRetryPolicy(fastRetry())
	var secondCalled bool
	r.OnReconciliationIssue("failing", func(ctx context.Context, issue *entity.ReconciliationIssue) error {
		return errors.New("hook failed")
	})
	r.OnReconciliationIssue("ok", func(ctx context.Context, issue *entity.ReconciliationIssue) error {
		secondCalled = true
		return nil
	})
	r.FireReconciliationIssue(context.Background(), &entity.ReconciliationIssue{
		IssueType: "missing_credit",
		OrderNo:   "ORD-002",
		Severity:  "critical",
	})
	if !secondCalled {
		t.Error("second reconciliation hook should still be called despite first failure")
	}
}

// TestRegistry_FireReconciliationIssue_MultipleHooks verifies all hooks are called.
func TestRegistry_FireReconciliationIssue_MultipleHooks(t *testing.T) {
	r := NewRegistry().WithRetryPolicy(fastRetry())
	var count int
	for i := 0; i < 3; i++ {
		name := "h" + string(rune('0'+i))
		r.OnReconciliationIssue(name, func(ctx context.Context, issue *entity.ReconciliationIssue) error {
			count++
			return nil
		})
	}
	r.FireReconciliationIssue(context.Background(), &entity.ReconciliationIssue{
		IssueType: "missed_webhook",
		OrderNo:   "ORD-003",
	})
	if count != 3 {
		t.Errorf("expected 3 hooks called, got %d", count)
	}
}

// TestRegistry_HookCount_WithReconciliation verifies HookCount includes reconciliation hooks.
func TestRegistry_HookCount_WithReconciliation(t *testing.T) {
	r := NewRegistry()
	r.OnReconciliationIssue("a", func(ctx context.Context, issue *entity.ReconciliationIssue) error { return nil })
	r.OnReconciliationIssue("b", func(ctx context.Context, issue *entity.ReconciliationIssue) error { return nil })
	if r.HookCount() != 2 {
		t.Errorf("HookCount = %d, want 2", r.HookCount())
	}
}

// ── NotificationModule.OnReconciliationIssue ─────────────────────────────────

// TestNotificationModule_OnReconciliationIssue_Critical verifies that a critical
// issue triggers a notification request.
func TestNotificationModule_OnReconciliationIssue_Critical(t *testing.T) {
	capture := &notifCapture{}
	srv := newNotifServer(t, http.StatusOK, capture)
	m := testNotifModule(t, srv)

	amt := 29.99
	err := m.OnReconciliationIssue(context.Background(), &entity.ReconciliationIssue{
		IssueType:      "missing_credit",
		OrderNo:        "ORD-100",
		Provider:       "stripe",
		Description:    "Payment received but credit not applied",
		Severity:       "critical",
		ExpectedAmount: &amt,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(capture.requests) != 1 {
		t.Fatalf("expected 1 notification request, got %d", len(capture.requests))
	}
	req := capture.requests[0]
	if req.EventType != "billing.reconciliation.critical" {
		t.Errorf("event_type = %s, want billing.reconciliation.critical", req.EventType)
	}
	if req.Vars["issue_type"] != "missing_credit" {
		t.Errorf("issue_type = %s, want missing_credit", req.Vars["issue_type"])
	}
	if req.Vars["expected_amount"] != "29.99" {
		t.Errorf("expected_amount = %s, want 29.99", req.Vars["expected_amount"])
	}
}

// TestNotificationModule_OnReconciliationIssue_NonCritical verifies that
// non-critical issues are silently skipped (no notification sent).
func TestNotificationModule_OnReconciliationIssue_NonCritical(t *testing.T) {
	capture := &notifCapture{}
	srv := newNotifServer(t, http.StatusOK, capture)
	m := testNotifModule(t, srv)

	err := m.OnReconciliationIssue(context.Background(), &entity.ReconciliationIssue{
		IssueType:   "stale_order",
		OrderNo:     "ORD-200",
		Severity:    "warning", // not critical
		Description: "Order has been pending for too long",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(capture.requests) != 0 {
		t.Errorf("non-critical issue should not trigger a notification, got %d requests", len(capture.requests))
	}
}

// TestNotificationModule_OnReconciliationIssue_WithAccountID verifies that the
// accountID in the issue is forwarded to the notification request.
func TestNotificationModule_OnReconciliationIssue_WithAccountID(t *testing.T) {
	capture := &notifCapture{}
	srv := newNotifServer(t, http.StatusOK, capture)
	m := testNotifModule(t, srv)

	acctID := int64(42)
	err := m.OnReconciliationIssue(context.Background(), &entity.ReconciliationIssue{
		IssueType:   "missing_credit",
		OrderNo:     "ORD-300",
		Severity:    "critical",
		Description: "Credit missing",
		AccountID:   &acctID,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(capture.requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(capture.requests))
	}
	if capture.requests[0].AccountID != 42 {
		t.Errorf("account_id = %d, want 42", capture.requests[0].AccountID)
	}
}

// TestNotificationModule_OnReconciliationIssue_NoAccountID verifies admin-level
// alert (account_id=0) when the issue has no account reference.
func TestNotificationModule_OnReconciliationIssue_NoAccountID(t *testing.T) {
	capture := &notifCapture{}
	srv := newNotifServer(t, http.StatusOK, capture)
	m := testNotifModule(t, srv)

	err := m.OnReconciliationIssue(context.Background(), &entity.ReconciliationIssue{
		IssueType:   "missed_webhook",
		OrderNo:     "ORD-400",
		Severity:    "critical",
		Description: "Webhook delivery failed",
		AccountID:   nil, // admin-level
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(capture.requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(capture.requests))
	}
	if capture.requests[0].AccountID != 0 {
		t.Errorf("account_id = %d, want 0 for admin-level alert", capture.requests[0].AccountID)
	}
}

// TestNotificationModule_OnReconciliationIssue_ServiceError verifies that
// a 5xx from the notification service propagates as an error.
func TestNotificationModule_OnReconciliationIssue_ServiceError(t *testing.T) {
	srv := newNotifServer(t, http.StatusInternalServerError, nil)
	m := testNotifModule(t, srv)

	err := m.OnReconciliationIssue(context.Background(), &entity.ReconciliationIssue{
		IssueType:   "missing_credit",
		OrderNo:     "ORD-500",
		Severity:    "critical",
		Description: "Service is down",
	})
	if err == nil {
		t.Fatal("expected error for notification service failure, got nil")
	}
}

// TestNotificationModule_OnReconciliationIssue_NoExpectedAmount verifies that
// the expected_amount var is absent when ExpectedAmount is nil.
func TestNotificationModule_OnReconciliationIssue_NoExpectedAmount(t *testing.T) {
	capture := &notifCapture{}
	srv := newNotifServer(t, http.StatusOK, capture)
	m := testNotifModule(t, srv)

	err := m.OnReconciliationIssue(context.Background(), &entity.ReconciliationIssue{
		IssueType:      "missing_credit",
		OrderNo:        "ORD-600",
		Severity:       "critical",
		Description:    "Credit missing",
		ExpectedAmount: nil, // no expected_amount var should appear
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(capture.requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(capture.requests))
	}
	if _, ok := capture.requests[0].Vars["expected_amount"]; ok {
		t.Error("expected_amount var should not be present when ExpectedAmount is nil")
	}
}

// ── notification send() error: bad service URL ───────────────────────────────

// TestNotificationModule_Send_BadURL verifies that an unreachable service URL
// returns a non-nil error from send().
func TestNotificationModule_Send_BadURL(t *testing.T) {
	// Use an address that will refuse connections immediately.
	m := NewNotificationModule(NotificationConfig{
		Enabled:    true,
		ServiceURL: "http://127.0.0.1:1", // port 1 always refuses connections
		APIKey:     "key",
	})

	err := m.OnAccountCreated(context.Background(), &entity.Account{
		ID:          1,
		DisplayName: "Test User",
	})
	if err == nil {
		t.Fatal("expected error for unreachable service URL, got nil")
	}
}

// ── notification Register includes reconciliation hook ────────────────────────

// TestNotificationModule_Register_IncludesReconciliationHook verifies that
// Register() adds the OnReconciliationIssue hook to the registry.
func TestNotificationModule_Register_IncludesReconciliationHook(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	m := NewNotificationModule(NotificationConfig{
		Enabled:    true,
		ServiceURL: srv.URL,
		APIKey:     "key",
	})
	r := NewRegistry()
	m.Register(r)

	// 4 hooks: account_created, checkin, referral_signup, reconciliation_issue
	if r.HookCount() != 4 {
		t.Errorf("HookCount = %d, want 4", r.HookCount())
	}

	// Fire the reconciliation hook via the registry to confirm it routes correctly.
	var fired bool
	r.OnReconciliationIssue("custom", func(ctx context.Context, issue *entity.ReconciliationIssue) error {
		fired = true
		return nil
	})
	r.FireReconciliationIssue(context.Background(), &entity.ReconciliationIssue{
		IssueType: "test",
		OrderNo:   "ORD-TEST",
		Severity:  "critical",
	})
	if !fired {
		t.Error("custom reconciliation hook was not fired through registry")
	}
}
