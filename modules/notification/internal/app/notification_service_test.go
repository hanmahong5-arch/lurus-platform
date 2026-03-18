package app

import (
	"testing"
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
		{"lucrum.strategy.triggered", "strategy"},
		{"llm.quota.threshold", "quota"},
		{"simple", "general"},
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
