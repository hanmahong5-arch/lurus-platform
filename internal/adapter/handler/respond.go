package handler

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
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

// handleBindError sends a user-friendly validation error without exposing
// internal struct field names or JSON unmarshaling details.
func handleBindError(c *gin.Context, err error) {
	msg := "Invalid request body"
	// Extract the most relevant part of binding validation errors.
	if strings.Contains(err.Error(), "required") {
		msg = "Missing required fields"
	} else if strings.Contains(err.Error(), "cannot unmarshal") {
		msg = "Invalid field type in request body"
	}
	respondBadRequest(c, msg)
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
