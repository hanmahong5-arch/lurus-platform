package event_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/event"
)

func TestNewEvent(t *testing.T) {
	payload := map[string]string{"plan_code": "pro"}
	ev, err := event.NewEvent(event.SubjectSubscriptionActivated, 42, "LU0000042", "llm-api", payload)
	if err != nil {
		t.Fatalf("NewEvent returned error: %v", err)
	}
	if ev.EventID == "" {
		t.Error("EventID should not be empty")
	}
	if ev.EventType != event.SubjectSubscriptionActivated {
		t.Errorf("EventType=%q, want %q", ev.EventType, event.SubjectSubscriptionActivated)
	}
	if ev.AccountID != 42 {
		t.Errorf("AccountID=%d, want 42", ev.AccountID)
	}
	if ev.LurusID != "LU0000042" {
		t.Errorf("LurusID=%q, want LU0000042", ev.LurusID)
	}
	if ev.ProductID != "llm-api" {
		t.Errorf("ProductID=%q, want llm-api", ev.ProductID)
	}
	if ev.OccurredAt.IsZero() {
		t.Error("OccurredAt should not be zero")
	}
	// Payload must be valid JSON
	var decoded map[string]string
	if err := json.Unmarshal(ev.Payload, &decoded); err != nil {
		t.Errorf("Payload not valid JSON: %v", err)
	}
	if decoded["plan_code"] != "pro" {
		t.Errorf("Payload plan_code=%q, want pro", decoded["plan_code"])
	}
}

func TestNewEvent_UUIDUnique(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 50; i++ {
		ev, err := event.NewEvent(event.SubjectAccountCreated, int64(i), "", "", nil)
		if err != nil {
			t.Fatalf("NewEvent error: %v", err)
		}
		if seen[ev.EventID] {
			t.Errorf("duplicate EventID %q", ev.EventID)
		}
		seen[ev.EventID] = true
	}
}

func TestNewEvent_OccurredAtRecent(t *testing.T) {
	before := time.Now().Add(-time.Second)
	ev, _ := event.NewEvent(event.SubjectTopupCompleted, 1, "LU0000001", "", nil)
	after := time.Now().Add(time.Second)
	if ev.OccurredAt.Before(before) || ev.OccurredAt.After(after) {
		t.Errorf("OccurredAt=%v out of expected range [%v, %v]", ev.OccurredAt, before, after)
	}
}

func TestSubjectConstants(t *testing.T) {
	subjects := []string{
		event.SubjectAccountCreated,
		event.SubjectSubscriptionActivated,
		event.SubjectSubscriptionExpired,
		event.SubjectTopupCompleted,
		event.SubjectEntitlementUpdated,
		event.SubjectVIPLevelChanged,
		event.SubjectLLMUsageReported,
	}
	for _, s := range subjects {
		if s == "" {
			t.Errorf("empty subject constant")
		}
	}
}

// TestNewEvent_MarshalError verifies that an unmarshalable payload returns an error.
func TestNewEvent_MarshalError(t *testing.T) {
	// channels cannot be marshaled to JSON.
	_, err := event.NewEvent("test.event", 1, "", "", make(chan int))
	if err == nil {
		t.Error("expected marshal error for channel payload, got nil")
	}
}
