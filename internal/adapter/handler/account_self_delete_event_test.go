package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/event"
)

// spyPublisher captures every IdentityEvent passed to Publish so tests
// can assert on subject, payload shape, and call count. failErr makes
// every Publish call fail with that error — used to verify the
// handler's "publish failure does not affect 200" contract.
type spyPublisher struct {
	mu      sync.Mutex
	events  []*event.IdentityEvent
	failErr error
}

func (s *spyPublisher) Publish(_ context.Context, ev *event.IdentityEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failErr != nil {
		return s.failErr
	}
	s.events = append(s.events, ev)
	return nil
}

func (s *spyPublisher) snapshot() []*event.IdentityEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*event.IdentityEvent, len(s.events))
	copy(out, s.events)
	return out
}

func (s *spyPublisher) waitForEvent(t *testing.T, timeout time.Duration) *event.IdentityEvent {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if got := s.snapshot(); len(got) > 0 {
			return got[0]
		}
		time.Sleep(10 * time.Millisecond)
	}
	return nil
}

// ── happy path: event emitted on fresh insert ──────────────────────

func TestAccountSelfDelete_PublishesEventOnInsert(t *testing.T) {
	h, as, _ := setupSelfDeleteHandler(t)
	pub := &spyPublisher{}
	h = h.WithPublisher(pub)

	acct := as.seed(entity.Account{
		ZitadelSub: "sub-pub-1",
		Email:      "pub1@x.com",
		Status:     entity.AccountStatusActive,
	})
	w := postSelfDelete(h, acct.ID, map[string]any{
		"reason":      "no_longer_using",
		"reason_text": "test",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d (body=%s); want 200", w.Code, w.Body.String())
	}

	ev := pub.waitForEvent(t, 1*time.Second)
	if ev == nil {
		t.Fatal("publisher did not receive event within timeout")
	}
	if ev.EventType != event.SubjectAccountDeleteRequested {
		t.Errorf("subject = %q; want %q", ev.EventType, event.SubjectAccountDeleteRequested)
	}
	if ev.AccountID != acct.ID {
		t.Errorf("account_id = %d; want %d", ev.AccountID, acct.ID)
	}

	var payload event.AccountDeleteRequestedPayload
	if err := json.Unmarshal(ev.Payload, &payload); err != nil {
		t.Fatalf("payload unmarshal: %v", err)
	}
	if payload.RequestID == 0 {
		t.Error("request_id missing from payload")
	}
	if payload.Reason != "no_longer_using" {
		t.Errorf("reason = %q; want %q", payload.Reason, "no_longer_using")
	}
	if payload.CoolingOffUntil == "" {
		t.Error("cooling_off_until empty in payload")
	}
}

// ── insert failure: NO publish ────────────────────────────────────

func TestAccountSelfDelete_NoPublishOnInsertFailure(t *testing.T) {
	h, as, store := setupSelfDeleteHandler(t)
	pub := &spyPublisher{}
	h = h.WithPublisher(pub)

	acct := as.seed(entity.Account{
		ZitadelSub: "sub-pub-fail",
		Email:      "pubfail@x.com",
		Status:     entity.AccountStatusActive,
	})
	store.failOnce = true
	store.failErr = errors.New("simulated db outage")

	w := postSelfDelete(h, acct.ID, map[string]any{"reason": "other"})
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d; want 500", w.Code)
	}
	// Brief wait — if a publish were going to happen, it would have
	// already been queued by now.
	time.Sleep(50 * time.Millisecond)
	if got := pub.snapshot(); len(got) != 0 {
		t.Errorf("publisher saw %d events on insert failure; want 0", len(got))
	}
}

// ── publish failure: handler still returns 200 ────────────────────

func TestAccountSelfDelete_PublishFailure_DoesNotFailHandler(t *testing.T) {
	h, as, _ := setupSelfDeleteHandler(t)
	pub := &spyPublisher{failErr: errors.New("nats down")}
	h = h.WithPublisher(pub)

	acct := as.seed(entity.Account{
		ZitadelSub: "sub-pubfail",
		Email:      "pubfail2@x.com",
		Status:     entity.AccountStatusActive,
	})
	w := postSelfDelete(h, acct.ID, map[string]any{"reason": "privacy_concern"})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 even though publish failed", w.Code)
	}
}

// ── idempotent re-submit: NO duplicate event ──────────────────────

func TestAccountSelfDelete_IdempotentResubmit_NoDoublePublish(t *testing.T) {
	h, as, _ := setupSelfDeleteHandler(t)
	pub := &spyPublisher{}
	h = h.WithPublisher(pub)

	acct := as.seed(entity.Account{
		ZitadelSub: "sub-idem-pub",
		Email:      "idempub@x.com",
		Status:     entity.AccountStatusActive,
	})
	w1 := postSelfDelete(h, acct.ID, map[string]any{"reason": "other"})
	if w1.Code != http.StatusOK {
		t.Fatalf("first call status = %d", w1.Code)
	}
	w2 := postSelfDelete(h, acct.ID, map[string]any{"reason": "other"})
	if w2.Code != http.StatusOK {
		t.Fatalf("second call status = %d", w2.Code)
	}
	// Wait for any goroutined publishes to land.
	time.Sleep(100 * time.Millisecond)
	if got := pub.snapshot(); len(got) != 1 {
		t.Errorf("publisher saw %d events; want 1 (idempotent re-submit must not re-fire)", len(got))
	}
}
