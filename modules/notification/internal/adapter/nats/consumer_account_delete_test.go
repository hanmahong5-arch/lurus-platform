package nats

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/modules/notification/internal/domain/entity"
	"github.com/hanmahong5-arch/lurus-platform/modules/notification/internal/pkg/event"
)

// makeDeleteRequestedEnvelope is a helper that produces a JSON
// envelope-bytes blob for the buildAccountDeleteRequestedSend tests.
// Marshal failures are returned as a t.Fatal-style panic because the
// inputs are test-fixed and can never legitimately fail to marshal.
func makeDeleteRequestedEnvelope(t *testing.T, accountID int64, payload any) []byte {
	t.Helper()
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	envelope := event.IdentityEvent{
		EventID:    "evt-test-1",
		EventType:  event.SubjectAccountDeleteRequested,
		AccountID:  accountID,
		LurusID:    "LU0000000001",
		Payload:    rawPayload,
		OccurredAt: time.Now().UTC(),
	}
	out, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	return out
}

func TestBuildAccountDeleteRequestedSend_HappyPath(t *testing.T) {
	deadline := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	body := makeDeleteRequestedEnvelope(t, 42, event.AccountDeleteRequestedPayload{
		RequestID:       777,
		Reason:          "no_longer_using",
		CoolingOffUntil: deadline.Format(time.RFC3339),
	})

	req, ok := buildAccountDeleteRequestedSend(body)
	if !ok {
		t.Fatal("happy path returned ok=false")
	}
	if req.AccountID != 42 {
		t.Errorf("AccountID = %d; want 42", req.AccountID)
	}
	if req.EventType != event.SubjectAccountDeleteRequested {
		t.Errorf("EventType = %q; want %q", req.EventType, event.SubjectAccountDeleteRequested)
	}
	if req.Title != "您已提交注销请求" {
		t.Errorf("Title = %q; want %q", req.Title, "您已提交注销请求")
	}
	wantBody := "您的账号注销将于 2026-05-30 生效，期间登录可取消。"
	if req.Body != wantBody {
		t.Errorf("Body = %q; want %q", req.Body, wantBody)
	}
	if req.Priority != entity.PriorityNormal {
		t.Errorf("Priority = %q; want %q", req.Priority, entity.PriorityNormal)
	}
	if req.Category != "account.delete_requested" {
		t.Errorf("Category = %q; want %q", req.Category, "account.delete_requested")
	}
	if len(req.Channels) != 1 || req.Channels[0] != entity.ChannelInApp {
		t.Errorf("Channels = %v; want [in_app]", req.Channels)
	}
	if got := req.Payload["request_id"]; got != int64(777) {
		t.Errorf("Payload.request_id = %v (%T); want 777 (int64)", got, got)
	}
}

func TestBuildAccountDeleteRequestedSend_MissingOptionalReason(t *testing.T) {
	deadline := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	body := makeDeleteRequestedEnvelope(t, 7, event.AccountDeleteRequestedPayload{
		RequestID: 1,
		// Reason intentionally omitted.
		CoolingOffUntil: deadline.Format(time.RFC3339),
	})
	req, ok := buildAccountDeleteRequestedSend(body)
	if !ok {
		t.Fatal("missing-reason returned ok=false; want ok=true (reason is optional)")
	}
	if !strings.Contains(req.Body, "2026-06-01") {
		t.Errorf("Body = %q; want it to contain the formatted date", req.Body)
	}
}

func TestBuildAccountDeleteRequestedSend_CorruptEnvelope_Skipped(t *testing.T) {
	_, ok := buildAccountDeleteRequestedSend([]byte("not-json"))
	if ok {
		t.Error("corrupt envelope returned ok=true; want skip")
	}
}

func TestBuildAccountDeleteRequestedSend_NoAccount_Skipped(t *testing.T) {
	body := makeDeleteRequestedEnvelope(t, 0, event.AccountDeleteRequestedPayload{
		CoolingOffUntil: time.Now().UTC().Format(time.RFC3339),
	})
	_, ok := buildAccountDeleteRequestedSend(body)
	if ok {
		t.Error("account_id=0 returned ok=true; want skip")
	}
}

func TestBuildAccountDeleteRequestedSend_CorruptPayload_Skipped(t *testing.T) {
	// Hand-craft an envelope whose inner Payload is type-wrong (a JSON
	// object where an int64 field is encoded as a string). The outer
	// envelope is valid JSON so we exercise the payload-unmarshal
	// branch specifically (not the envelope-unmarshal branch).
	innerPayload := json.RawMessage(`{"request_id": "not-an-int", "cooling_off_until": "2026-05-30T00:00:00Z"}`)
	envelope := event.IdentityEvent{
		EventID:    "evt-corrupt",
		EventType:  event.SubjectAccountDeleteRequested,
		AccountID:  9,
		Payload:    innerPayload,
		OccurredAt: time.Now().UTC(),
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	_, ok := buildAccountDeleteRequestedSend(body)
	if ok {
		t.Error("corrupt payload returned ok=true; want skip")
	}
}

func TestBuildAccountDeleteRequestedSend_BadDate_FallsBackToRaw(t *testing.T) {
	body := makeDeleteRequestedEnvelope(t, 11, event.AccountDeleteRequestedPayload{
		RequestID:       1,
		CoolingOffUntil: "not-a-date",
	})
	req, ok := buildAccountDeleteRequestedSend(body)
	if !ok {
		t.Fatal("bad-date returned ok=false; want graceful fallback")
	}
	if !strings.Contains(req.Body, "not-a-date") {
		t.Errorf("Body = %q; want it to contain the raw fallback string", req.Body)
	}
}
