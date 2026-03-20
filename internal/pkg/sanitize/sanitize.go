// Package sanitize provides utilities for cleaning user input before logging,
// preventing log injection attacks and sensitive data leakage.
//
// Borrowed from OpenClaw's log sanitization patterns (ws-connection.ts).
package sanitize

import (
	"regexp"
	"strings"
	"unicode"
)

const (
	// MaxLogValueLen is the maximum length of a sanitized log value.
	// Longer values are truncated with "..." suffix.
	MaxLogValueLen = 256
)

// sensitivePatterns matches field names that should be masked in logs.
var sensitivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)password`),
	regexp.MustCompile(`(?i)secret`),
	regexp.MustCompile(`(?i)token$`),
	regexp.MustCompile(`(?i)api.?key`),
	regexp.MustCompile(`(?i)authorization`),
	regexp.MustCompile(`(?i)cookie`),
}

// sensitiveWhitelist contains field names that match sensitive patterns
// but are NOT actually sensitive (e.g. "maxTokens" is a config value).
var sensitiveWhitelist = map[string]bool{
	"maxTokens":       true,
	"maxOutputTokens": true,
	"tokenLimit":      true,
	"tokenCount":      true,
}

// IsSensitiveField reports whether a field name likely contains sensitive data.
// Uses regex pattern matching with a whitelist for known false positives.
func IsSensitiveField(fieldName string) bool {
	if sensitiveWhitelist[fieldName] {
		return false
	}
	for _, p := range sensitivePatterns {
		if p.MatchString(fieldName) {
			return true
		}
	}
	return false
}

// MaskSecret replaces a secret value with a masked representation,
// preserving the first 4 characters for debugging.
func MaskSecret(value string) string {
	if len(value) <= 4 {
		return "****"
	}
	return value[:4] + strings.Repeat("*", min(len(value)-4, 8))
}

// LogValue cleans a string value for safe inclusion in structured logs.
// Removes control characters, collapses whitespace, and truncates to MaxLogValueLen.
// Prevents log injection attacks where user input contains \r\n to forge log entries.
func LogValue(s string) string {
	if s == "" {
		return ""
	}

	// Remove control characters (prevent log injection).
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.IsControl(r) && r != '\t' {
			b.WriteRune(' ')
		} else {
			b.WriteRune(r)
		}
	}
	cleaned := strings.Join(strings.Fields(b.String()), " ")

	if len(cleaned) <= MaxLogValueLen {
		return cleaned
	}
	// Truncate at a rune boundary.
	runes := []rune(cleaned)
	if len(runes) > MaxLogValueLen {
		return string(runes[:MaxLogValueLen]) + "..."
	}
	return cleaned
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
