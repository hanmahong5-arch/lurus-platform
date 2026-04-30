package event

import (
	"encoding/json"
	"testing"
)

// Each new consumer (C.3) parses an envelope + payload off the wire. These
// tests pin the JSON shape so a producer rename in PSI/LLM/Lucrum publishers
// fails fast in unit tests rather than on stage.

func TestPSIEvent_Unmarshal(t *testing.T) {
	raw := []byte(`{
		"event_id": "psi-evt-1",
		"event_type": "psi.order.approval_needed",
		"account_id": 42,
		"workspace_id": 7,
		"payload": {"order_id":1001,"order_no":"PO-001","amount_cny":1234.5,"submitted_by":"alice"},
		"occurred_at": "2026-04-30T10:00:00Z"
	}`)

	var ev PSIEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		t.Fatalf("PSIEvent unmarshal: %v", err)
	}
	if ev.EventID != "psi-evt-1" || ev.AccountID != 42 || ev.WorkspaceID != 7 {
		t.Fatalf("PSIEvent envelope wrong: %+v", ev)
	}

	var payload PSIOrderApprovalPayload
	if err := json.Unmarshal(ev.Payload, &payload); err != nil {
		t.Fatalf("PSIOrderApprovalPayload unmarshal: %v", err)
	}
	if payload.OrderNo != "PO-001" || payload.AmountCNY != 1234.5 || payload.SubmittedBy != "alice" {
		t.Fatalf("psi order payload wrong: %+v", payload)
	}
}

func TestPSIEvent_NoAccount(t *testing.T) {
	raw := []byte(`{"event_id":"x","event_type":"psi.inventory.redline","account_id":0,"payload":{}}`)
	var ev PSIEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if ev.AccountID > 0 {
		t.Fatalf("expected account_id <= 0, got %d", ev.AccountID)
	}
}

func TestPSIInventoryRedlinePayload_Unmarshal(t *testing.T) {
	raw := []byte(`{"sku":"SKU-1","sku_name":"Widget","on_hand":3,"threshold":10}`)
	var p PSIInventoryRedlinePayload
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.SKU != "SKU-1" || p.SKUName != "Widget" || p.OnHand != 3 || p.Threshold != 10 {
		t.Fatalf("payload wrong: %+v", p)
	}
}

func TestPSIPaymentReceivedPayload_Unmarshal(t *testing.T) {
	raw := []byte(`{"payment_id":99,"amount_cny":500.0,"payer_name":"bob"}`)
	var p PSIPaymentReceivedPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.PaymentID != 99 || p.AmountCNY != 500.0 || p.PayerName != "bob" {
		t.Fatalf("payload wrong: %+v", p)
	}
}

func TestLLMEvent_Unmarshal(t *testing.T) {
	raw := []byte(`{
		"event_id": "llm-1",
		"event_type": "llm.image.generated",
		"account_id": 7,
		"payload": {"job_id":"job-x","image_url":"https://x/y.png","prompt":"a cat"},
		"occurred_at": "2026-04-30T10:00:00Z"
	}`)
	var ev LLMEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		t.Fatalf("LLMEvent unmarshal: %v", err)
	}
	if ev.AccountID != 7 || ev.EventType != "llm.image.generated" {
		t.Fatalf("envelope wrong: %+v", ev)
	}
	var p LLMImageGeneratedPayload
	if err := json.Unmarshal(ev.Payload, &p); err != nil {
		t.Fatalf("payload unmarshal: %v", err)
	}
	if p.JobID != "job-x" || p.ImageURL != "https://x/y.png" || p.Prompt != "a cat" {
		t.Fatalf("payload wrong: %+v", p)
	}
}

func TestLLMUsageMilestonePayload_Unmarshal(t *testing.T) {
	raw := []byte(`{"period":"day","tokens_used":12345,"milestone":"daily-10k"}`)
	var p LLMUsageMilestonePayload
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.Period != "day" || p.TokensUsed != 12345 || p.Milestone != "daily-10k" {
		t.Fatalf("payload wrong: %+v", p)
	}
}

func TestLucrumAdvisorOutputPayload_Unmarshal(t *testing.T) {
	raw := []byte(`{"advisor_id":"a1","advisor_name":"AlphaBot","symbol":"AAPL","summary":"buy"}`)
	var p LucrumAdvisorOutputPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.AdvisorName != "AlphaBot" || p.Symbol != "AAPL" || p.Summary != "buy" {
		t.Fatalf("payload wrong: %+v", p)
	}
}

func TestLucrumMarketEventPayload_Unmarshal(t *testing.T) {
	raw := []byte(`{"symbol":"BTC","headline":"halving","severity":"info"}`)
	var p LucrumMarketEventPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.Symbol != "BTC" || p.Severity != "info" {
		t.Fatalf("payload wrong: %+v", p)
	}
}

func TestVIPLevelChangedPayload_Unmarshal(t *testing.T) {
	raw := []byte(`{"level":"gold","old_level":"silver"}`)
	var p VIPLevelChangedPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.Level != "gold" || p.OldLevel != "silver" {
		t.Fatalf("payload wrong: %+v", p)
	}
}

func TestStreamConstants(t *testing.T) {
	if StreamPSIEvents != "PSI_EVENTS" {
		t.Errorf("StreamPSIEvents = %q, want PSI_EVENTS", StreamPSIEvents)
	}
}

func TestSubjectConstants(t *testing.T) {
	cases := []struct {
		got, want string
	}{
		{SubjectPSIOrderApprovalNeeded, "psi.order.approval_needed"},
		{SubjectPSIInventoryRedline, "psi.inventory.redline"},
		{SubjectPSIPaymentReceived, "psi.payment.received"},
		{SubjectLLMImageGenerated, "llm.image.generated"},
		{SubjectLLMUsageMilestone, "llm.usage.milestone"},
		{SubjectLucrumAdvisorOutput, "lucrum.advisor.output"},
		{SubjectLucrumMarketEvent, "lucrum.market.event"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("subject = %q, want %q", c.got, c.want)
		}
	}
}
