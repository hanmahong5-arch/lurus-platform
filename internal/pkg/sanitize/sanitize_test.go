package sanitize

import (
	"strings"
	"testing"
)

func TestIsSensitiveField(t *testing.T) {
	tests := []struct {
		field string
		want  bool
	}{
		{"password", true},
		{"Password", true},
		{"user_password", true},
		{"secret", true},
		{"clientSecret", true},
		{"apiKey", true},
		{"api_key", true},
		{"authorization", true},
		{"cookie", true},
		{"sessionToken", true},
		// Whitelist exceptions.
		{"maxTokens", false},
		{"maxOutputTokens", false},
		{"tokenLimit", false},
		{"tokenCount", false},
		// Non-sensitive fields.
		{"username", false},
		{"email", false},
		{"accountId", false},
		{"amount", false},
	}
	for _, tc := range tests {
		t.Run(tc.field, func(t *testing.T) {
			if got := IsSensitiveField(tc.field); got != tc.want {
				t.Errorf("IsSensitiveField(%q) = %v, want %v", tc.field, got, tc.want)
			}
		})
	}
}

func TestMaskSecret(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", "****"},
		{"abc", "****"},
		{"abcd", "****"},
		{"sk-1234567890", "sk-1********"},
		{"ghp_xxxxxxxxxxxxx", "ghp_********"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := MaskSecret(tc.input)
			if got != tc.want {
				t.Errorf("MaskSecret(%q) = %q, want %q", tc.input, got, tc.want)
			}
			// Verify never returns the original value.
			if got == tc.input && tc.input != "" {
				t.Error("MaskSecret should never return the original value")
			}
		})
	}
}

func TestLogValue_RemovesControlCharacters(t *testing.T) {
	input := "normal\r\ninjected log line\x00null byte"
	got := LogValue(input)
	if strings.Contains(got, "\r") || strings.Contains(got, "\n") || strings.Contains(got, "\x00") {
		t.Errorf("LogValue should remove control chars, got %q", got)
	}
}

func TestLogValue_CollapsesWhitespace(t *testing.T) {
	input := "too   many    spaces"
	got := LogValue(input)
	if got != "too many spaces" {
		t.Errorf("LogValue = %q, want 'too many spaces'", got)
	}
}

func TestLogValue_Truncates(t *testing.T) {
	input := strings.Repeat("a", 500)
	got := LogValue(input)
	if len(got) > MaxLogValueLen+3 { // +3 for "..."
		t.Errorf("LogValue should truncate to %d, got len=%d", MaxLogValueLen, len(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Error("truncated value should end with '...'")
	}
}

func TestLogValue_Empty(t *testing.T) {
	if got := LogValue(""); got != "" {
		t.Errorf("LogValue('') = %q, want empty", got)
	}
}

func TestLogValue_PreservesTab(t *testing.T) {
	got := LogValue("a\tb")
	if !strings.Contains(got, "\t") && !strings.Contains(got, "a b") {
		// Tab may be collapsed to space by Fields()
		t.Errorf("LogValue should preserve or collapse tabs, got %q", got)
	}
}

func TestLogValue_UnicodeRune(t *testing.T) {
	input := "你好世界 Hello"
	got := LogValue(input)
	if got != "你好世界 Hello" {
		t.Errorf("LogValue = %q, want original", got)
	}
}
