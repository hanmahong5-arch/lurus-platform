package app

import (
	"testing"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/modules/notification/internal/domain/entity"
)

func TestSubstituteVars(t *testing.T) {
	tests := []struct {
		name string
		text string
		vars map[string]string
		want string
	}{
		{
			name: "no vars",
			text: "Hello world",
			vars: nil,
			want: "Hello world",
		},
		{
			name: "single var",
			text: "Hello {{name}}",
			vars: map[string]string{"name": "Alice"},
			want: "Hello Alice",
		},
		{
			name: "multiple vars",
			text: "{{action}} signal on {{symbol}}",
			vars: map[string]string{"action": "BUY", "symbol": "AAPL"},
			want: "BUY signal on AAPL",
		},
		{
			name: "unmatched var stays",
			text: "{{unknown}} text",
			vars: map[string]string{"other": "val"},
			want: "{{unknown}} text",
		},
		{
			name: "repeated var",
			text: "{{x}} and {{x}}",
			vars: map[string]string{"x": "Y"},
			want: "Y and Y",
		},
		{
			name: "empty vars map",
			text: "{{a}}",
			vars: map[string]string{},
			want: "{{a}}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := substituteVars(tt.text, tt.vars)
			if got != tt.want {
				t.Errorf("substituteVars() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCategoryFromEvent(t *testing.T) {
	tests := []struct {
		eventType string
		want      string
	}{
		{"identity.account.created", "account"},
		{"identity.subscription.activated", "subscription"},
		{"lucrum.strategy.triggered", "strategy"},
		{"lucrum.risk.alert", "risk"},
		{"llm.quota.threshold", "quota"},
		{"llm.quota.50", "quota"},
		{"llm.quota.100", "quota"},
		{"simple", "general"},
		{"", "general"},
	}

	for _, tt := range tests {
		t.Run(tt.eventType, func(t *testing.T) {
			got := categoryFromEvent(tt.eventType)
			if got != tt.want {
				t.Errorf("categoryFromEvent(%q) = %q, want %q", tt.eventType, got, tt.want)
			}
		})
	}
}

func TestRetryConstants(t *testing.T) {
	if maxRetries != 3 {
		t.Errorf("maxRetries = %d, want 3", maxRetries)
	}
	if retryBaseDelay != 1*time.Second {
		t.Errorf("retryBaseDelay = %v, want 1s", retryBaseDelay)
	}
	if retryRedisKey != "notif:retry_queue" {
		t.Errorf("retryRedisKey = %q, want %q", retryRedisKey, "notif:retry_queue")
	}
}

func TestIsValidChannel(t *testing.T) {
	valid := []entity.Channel{entity.ChannelInApp, entity.ChannelEmail, entity.ChannelFCM}
	for _, ch := range valid {
		if !isValidChannel(ch) {
			t.Errorf("isValidChannel(%q) = false, want true", ch)
		}
	}

	invalid := []entity.Channel{"sms", "slack", ""}
	for _, ch := range invalid {
		if isValidChannel(ch) {
			t.Errorf("isValidChannel(%q) = true, want false", ch)
		}
	}
}
