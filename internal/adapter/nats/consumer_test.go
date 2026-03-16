package nats

import (
	"context"
	"encoding/json"
	"testing"

	natsgo "github.com/nats-io/nats.go"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/event"
)

// TestConsumer_HandleLLMUsage_MalformedJSON verifies that unmarshal errors are returned.
func TestConsumer_HandleLLMUsage_MalformedJSON(t *testing.T) {
	// Consumer with nil vip is safe as long as we don't reach the RecalculateFromWallet call.
	c := &Consumer{vip: nil}
	msg := &natsgo.Msg{Data: []byte("not valid json")}

	err := c.handleLLMUsage(context.Background(), msg)
	if err == nil {
		t.Error("expected error for malformed JSON, got nil")
	}
}

// TestConsumer_HandleLLMUsage_ZeroAccountID verifies that messages with AccountID <= 0 are silently ignored.
func TestConsumer_HandleLLMUsage_ZeroAccountID(t *testing.T) {
	c := &Consumer{vip: nil}
	payload, _ := json.Marshal(event.LLMUsageReportedPayload{
		AccountID:  0,
		AmountCNY:  10.0,
		TokensUsed: 1000,
	})
	msg := &natsgo.Msg{Data: payload}

	err := c.handleLLMUsage(context.Background(), msg)
	if err != nil {
		t.Errorf("expected nil for AccountID=0, got: %v", err)
	}
}

// TestConsumer_HandleLLMUsage_NegativeAccountID verifies that negative AccountID is also ignored.
func TestConsumer_HandleLLMUsage_NegativeAccountID(t *testing.T) {
	c := &Consumer{vip: nil}
	payload, _ := json.Marshal(event.LLMUsageReportedPayload{
		AccountID:  -1,
		AmountCNY:  5.0,
		TokensUsed: 500,
	})
	msg := &natsgo.Msg{Data: payload}

	err := c.handleLLMUsage(context.Background(), msg)
	if err != nil {
		t.Errorf("expected nil for AccountID=-1, got: %v", err)
	}
}
