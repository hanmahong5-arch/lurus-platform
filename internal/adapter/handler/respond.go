package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
)

// ── Unified error response ──────────────────────────────────────────────────

// Error codes — machine-readable identifiers for client-side error handling.
const (
	ErrCodeInvalidRequest      = "invalid_request"
	ErrCodeInvalidParameter    = "invalid_parameter"
	ErrCodeUnauthorized        = "unauthorized"
	ErrCodeForbidden           = "forbidden"
	ErrCodeNotFound            = "not_found"
	ErrCodeConflict            = "conflict"
	ErrCodeInsufficientBalance = "insufficient_balance"
	ErrCodeRateLimited         = "rate_limited"
	ErrCodeInternal            = "internal_error"
)

// respondError sends a structured JSON error response.
// Always use this instead of raw gin.H{"error": ...} for consistency.
func respondError(c *gin.Context, httpStatus int, code, message string) {
	c.JSON(httpStatus, gin.H{
		"error":   code,
		"message": message,
	})
}

// respondBadRequest is a shorthand for 400 errors.
func respondBadRequest(c *gin.Context, message string) {
	respondError(c, http.StatusBadRequest, ErrCodeInvalidRequest, message)
}

// respondNotFound is a shorthand for 404 errors.
func respondNotFound(c *gin.Context, resource string) {
	respondError(c, http.StatusNotFound, ErrCodeNotFound, resource+" not found")
}

// respondUnauthorized is a shorthand for 401 errors.
func respondUnauthorized(c *gin.Context) {
	respondError(c, http.StatusUnauthorized, ErrCodeUnauthorized, "Authentication required")
}

// respondInternalError logs the original error and sends a generic 500 response.
// Never exposes internal error details to the client.
func respondInternalError(c *gin.Context, context string, err error) {
	slog.Error("internal error", "handler", context, "err", err,
		"request_id", c.GetString("request_id"))
	respondError(c, http.StatusInternalServerError, ErrCodeInternal, "An internal error occurred")
}

// ── Request helpers ─────────────────────────────────────────────────────────

// requireAccountID extracts the authenticated account ID from the Gin context.
// Returns 0 and sends a 401 response if the user is not authenticated.
// All public API handlers should use this instead of the old mustAccountID.
func requireAccountID(c *gin.Context) (int64, bool) {
	raw, exists := c.Get("account_id")
	if !exists {
		respondUnauthorized(c)
		return 0, false
	}
	id, ok := raw.(int64)
	if !ok || id == 0 {
		respondUnauthorized(c)
		return 0, false
	}
	return id, true
}

// handleBindError parses Gin validation errors and sends field-level feedback.
//
// For validator.ValidationErrors (struct tag validation), it extracts each failing
// field name and maps the validation tag to a human-readable message, so the
// frontend can highlight the specific input that needs attention.
//
// For JSON syntax errors, it returns a generic message without leaking internal
// struct field names or unmarshaling details.
func handleBindError(c *gin.Context, err error) {
	// Try to extract field-level validation errors from go-playground/validator.
	var ve validator.ValidationErrors
	if errors.As(err, &ve) {
		fields := make(map[string]string, len(ve))
		for _, fe := range ve {
			// fe.Field() is the Go struct field name; convert to JSON field name
			// using the json tag convention (lowercase first letter or json tag).
			fieldName := toJSONFieldName(fe.Field())
			fields[fieldName] = validationTagToMessage(fe.Tag(), fe.Param(), fieldName)
		}
		respondValidationError(c, "Please fix the issues below", fields)
		return
	}

	// JSON parse error — don't expose internals.
	msg := "Invalid request body"
	errStr := err.Error()
	if strings.Contains(errStr, "cannot unmarshal") {
		msg = "Invalid field type in request body"
	} else if strings.Contains(errStr, "request body too large") {
		respondError(c, http.StatusRequestEntityTooLarge, "payload_too_large",
			"Request body is too large")
		return
	}
	respondBadRequest(c, msg)
}

// toJSONFieldName converts a Go struct field name (e.g. "AmountCNY") to its
// likely JSON counterpart (e.g. "amount_cny") using simple heuristics.
// For most Gin bindings, the json tag is already lowercase/snake_case.
func toJSONFieldName(goField string) string {
	// Simple camelCase → snake_case conversion.
	var result strings.Builder
	for i, r := range goField {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				result.WriteByte('_')
			}
			result.WriteRune(r + 32) // toLower
		} else {
			result.WriteRune(r)
		}
	}
	return result.String()
}

// validationTagToMessage maps go-playground/validator tags to user-friendly messages.
func validationTagToMessage(tag, param, field string) string {
	switch tag {
	case "required":
		return "This field is required"
	case "email":
		return "Please enter a valid email address"
	case "min":
		return "Must be at least " + param + " characters"
	case "max":
		return "Must be at most " + param + " characters"
	case "gt":
		return "Must be greater than " + param
	case "gte":
		return "Must be at least " + param
	case "lt":
		return "Must be less than " + param
	case "lte":
		return "Must be at most " + param
	case "oneof":
		return "Must be one of: " + param
	case "url":
		return "Please enter a valid URL"
	case "uuid":
		return "Must be a valid UUID"
	case "numeric":
		return "Must be a number"
	case "alphanum":
		return "Must contain only letters and numbers"
	default:
		return "Invalid value"
	}
}

// parsePathInt64 parses a URL path parameter as int64.
// Returns 0 and sends a 400 response if parsing fails.
func parsePathInt64(c *gin.Context, param, label string) (int64, bool) {
	raw := c.Param(param)
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		respondError(c, http.StatusBadRequest, ErrCodeInvalidParameter,
			label+" must be a positive integer")
		return 0, false
	}
	return id, true
}

// parsePagination extracts page and page_size from query parameters with
// safe defaults and range clamping.
func parsePagination(c *gin.Context) (page, pageSize int) {
	page = 1
	pageSize = 20
	if p := c.Query("page"); p != "" {
		if v, err := strconv.Atoi(p); err == nil && v > 0 {
			page = v
		}
	}
	if ps := c.Query("page_size"); ps != "" {
		if v, err := strconv.Atoi(ps); err == nil && v > 0 {
			pageSize = v
		}
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}
