package nats

import (
	"context"
	"encoding/json"
	"testing"

	natsgo "github.com/nats-io/nats.go"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/event"
)

// mockJetStreamPublisher extends panicJetStream with configurable AddStream behavior.
type mockJetStreamPublisher struct {
	natsgo.JetStreamContext
	published    []publishedMsg
	publishErr   error
	addStreamErr error
	streamInfo   *natsgo.StreamInfo
	streamInfoErr error
}

func (m *mockJetStreamPublisher) Publish(subj string, data []byte, opts ...natsgo.PubOpt) (*natsgo.PubAck, error) {
	if m.publishErr != nil {
		return nil, m.publishErr
	}
	m.published = append(m.published, publishedMsg{subject: subj, data: data})
	return &natsgo.PubAck{Stream: event.StreamIdentityEvents, Sequence: uint64(len(m.published))}, nil
}

func (m *mockJetStreamPublisher) AddStream(cfg *natsgo.StreamConfig, opts ...natsgo.JSOpt) (*natsgo.StreamInfo, error) {
	return nil, m.addStreamErr
}

func (m *mockJetStreamPublisher) StreamInfo(stream string, opts ...natsgo.JSOpt) (*natsgo.StreamInfo, error) {
	if m.streamInfoErr != nil {
		return nil, m.streamInfoErr
	}
	if m.streamInfo != nil {
		return m.streamInfo, nil
	}
	return &natsgo.StreamInfo{}, nil
}

// newTestPublisherFromJS creates a Publisher directly from a JetStreamContext mock,
// bypassing the natsgo.Conn requirement. Used to test Publisher.Publish paths.
func newTestPublisherFromJS(js natsgo.JetStreamContext) *Publisher {
	return &Publisher{js: js}
}

// TestPublisher_Publish_SubjectPreserved verifies the published subject matches EventType.
func TestPublisher_Publish_SubjectPreserved(t *testing.T) {
	mock := &mockJetStreamPublisher{}
	p := newTestPublisherFromJS(mock)

	ev, _ := event.NewEvent(event.SubjectSubscriptionExpired, 5, "L-0000000005", "pro", nil)
	if err := p.Publish(context.Background(), ev); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if len(mock.published) != 1 {
		t.Fatalf("expected 1 message, got %d", len(mock.published))
	}
	if mock.published[0].subject != event.SubjectSubscriptionExpired {
		t.Errorf("subject = %q, want %q", mock.published[0].subject, event.SubjectSubscriptionExpired)
	}
}

// TestPublisher_Publish_PayloadFields verifies structured payload fields are encoded.
func TestPublisher_Publish_PayloadFields(t *testing.T) {
	mock := &mockJetStreamPublisher{}
	p := newTestPublisherFromJS(mock)

	sub := event.SubscriptionActivatedPayload{
		SubscriptionID: 99,
		PlanCode:       "pro-monthly",
		ExpiresAt:      "2026-12-31T00:00:00Z",
	}
	ev, _ := event.NewEvent(event.SubjectSubscriptionActivated, 10, "L-0000000010", "pro", sub)
	if err := p.Publish(context.Background(), ev); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	var decoded event.IdentityEvent
	if err := json.Unmarshal(mock.published[0].data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.ProductID != "pro" {
		t.Errorf("ProductID = %q, want pro", decoded.ProductID)
	}

	var subPayload event.SubscriptionActivatedPayload
	if err := json.Unmarshal(decoded.Payload, &subPayload); err != nil {
		t.Fatalf("unmarshal sub payload: %v", err)
	}
	if subPayload.PlanCode != "pro-monthly" {
		t.Errorf("PlanCode = %q, want pro-monthly", subPayload.PlanCode)
	}
}

// TestPublisher_Publish_EntitlementUpdated verifies EntitlementUpdatedPayload encoding.
func TestPublisher_Publish_EntitlementUpdated(t *testing.T) {
	mock := &mockJetStreamPublisher{}
	p := newTestPublisherFromJS(mock)

	payload := event.EntitlementUpdatedPayload{Keys: []string{"gpt-4o", "claude-3-5"}}
	ev, _ := event.NewEvent(event.SubjectEntitlementUpdated, 3, "L-0000000003", "", payload)
	if err := p.Publish(context.Background(), ev); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	var decoded event.IdentityEvent
	_ = json.Unmarshal(mock.published[0].data, &decoded)
	var ep event.EntitlementUpdatedPayload
	_ = json.Unmarshal(decoded.Payload, &ep)
	if len(ep.Keys) != 2 {
		t.Errorf("Keys length = %d, want 2", len(ep.Keys))
	}
}

// TestPublisher_Publish_VIPLevelChanged verifies VIPLevelChangedPayload encoding.
func TestPublisher_Publish_VIPLevelChanged(t *testing.T) {
	mock := &mockJetStreamPublisher{}
	p := newTestPublisherFromJS(mock)

	payload := event.VIPLevelChangedPayload{OldLevel: 1, NewLevel: 3}
	ev, _ := event.NewEvent(event.SubjectVIPLevelChanged, 2, "L-0000000002", "", payload)
	if err := p.Publish(context.Background(), ev); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	var decoded event.IdentityEvent
	_ = json.Unmarshal(mock.published[0].data, &decoded)
	var vp event.VIPLevelChangedPayload
	_ = json.Unmarshal(decoded.Payload, &vp)
	if vp.NewLevel != 3 {
		t.Errorf("NewLevel = %d, want 3", vp.NewLevel)
	}
}

// TestPublisher_Publish_LurusIDPreserved verifies the LurusID field round-trips.
func TestPublisher_Publish_LurusIDPreserved(t *testing.T) {
	mock := &mockJetStreamPublisher{}
	p := newTestPublisherFromJS(mock)

	ev, _ := event.NewEvent(event.SubjectTopupCompleted, 8, "L-0000000008", "", nil)
	_ = p.Publish(context.Background(), ev)

	var decoded event.IdentityEvent
	_ = json.Unmarshal(mock.published[0].data, &decoded)
	if decoded.LurusID != "L-0000000008" {
		t.Errorf("LurusID = %q, want L-0000000008", decoded.LurusID)
	}
}
