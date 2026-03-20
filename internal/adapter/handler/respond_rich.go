package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// ── Rich Error Response — designed for frontend form UX ─────────────────────
//
// Every error response tells the user:
//   1. WHAT went wrong (machine-readable code + human message)
//   2. WHERE it went wrong (which form field, if applicable)
//   3. WHAT TO DO about it (suggested actions with labels and URLs)
//
// Frontend maps field errors directly to inline validation messages,
// and renders actions as clickable buttons/links below the form.

// RichError is the top-level error envelope.
// All error responses should use this structure for consistency.
type RichError struct {
	Error ErrorBody `json:"error"`
}

// ErrorBody contains the structured error details.
type ErrorBody struct {
	// Code is a machine-readable error identifier for client-side switching.
	// Examples: "validation_error", "conflict", "unauthorized", "rate_limited"
	Code string `json:"code"`

	// Message is a user-friendly summary in the user's language.
	Message string `json:"message"`

	// Fields maps form field names to per-field error messages.
	// Only present for validation errors. Null/omitted fields have no error.
	// Frontend should highlight these fields and show the message inline.
	Fields map[string]string `json:"fields,omitempty"`

	// Actions are suggested next steps the user can take.
	// Frontend renders these as buttons or links below the error message.
	Actions []ErrorAction `json:"actions,omitempty"`

	// RetryAfterMs hints when the user can retry (for rate-limited responses).
	RetryAfterMs int64 `json:"retry_after_ms,omitempty"`
}

// ErrorAction represents a suggested user action.
type ErrorAction struct {
	// Type: "link" (navigate), "retry" (re-submit), "dismiss" (close)
	Type string `json:"type"`
	// Label is the button/link text.
	Label string `json:"label"`
	// URL is the navigation target (for "link" type).
	URL string `json:"url,omitempty"`
}

// ── Builder helpers ─────────────────────────────────────────────────────────

// respondRichError sends a structured error response with full UX context.
func respondRichError(c *gin.Context, status int, body ErrorBody) {
	c.JSON(status, RichError{Error: body})
}

// respondValidationError sends a 400 with per-field error details.
// The frontend highlights each field with its specific error message.
func respondValidationError(c *gin.Context, message string, fields map[string]string) {
	respondRichError(c, http.StatusBadRequest, ErrorBody{
		Code:    "validation_error",
		Message: message,
		Fields:  fields,
	})
}

// respondConflictWithAction sends a 409 with a suggested navigation action.
// Example: "Email already registered" → action: "Sign in instead"
func respondConflictWithAction(c *gin.Context, message string, actions ...ErrorAction) {
	respondRichError(c, http.StatusConflict, ErrorBody{
		Code:    "conflict",
		Message: message,
		Actions: actions,
	})
}

// respondRateLimitedRich sends a 429 with retry guidance.
func respondRateLimitedRich(c *gin.Context, message string, retryAfterMs int64) {
	respondRichError(c, http.StatusTooManyRequests, ErrorBody{
		Code:         "rate_limited",
		Message:      message,
		RetryAfterMs: retryAfterMs,
	})
}

// ── Common actions ──────────────────────────────────────────────────────────

// ActionGoToLogin creates a "sign in" navigation action.
func ActionGoToLogin() ErrorAction {
	return ErrorAction{Type: "link", Label: "Already have an account? Sign in", URL: "/login"}
}

// ActionGoToRegister creates a "sign up" navigation action.
func ActionGoToRegister() ErrorAction {
	return ErrorAction{Type: "link", Label: "Don't have an account? Sign up", URL: "/register"}
}

// ActionGoToForgotPassword creates a "forgot password" navigation action.
func ActionGoToForgotPassword() ErrorAction {
	return ErrorAction{Type: "link", Label: "Forgot your password?", URL: "/forgot-password"}
}

// ActionRetry creates a "try again" action.
func ActionRetry() ErrorAction {
	return ErrorAction{Type: "retry", Label: "Try again"}
}
