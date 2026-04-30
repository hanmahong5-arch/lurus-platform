package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/hanmahong5-arch/lurus-platform/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// reasonTextMaxRunes is the max user-supplied reason length, in runes
// (NOT bytes — Chinese characters are 3 bytes each but one rune). 500
// matches the public API contract; longer text is truncated, NOT
// rejected, because the user is mid-destructive-flow and we don't
// fail-blame them on a length nit.
const reasonTextMaxRunes = 500

// AccountSelfDeleteHandler exposes the user-facing destructive flow at
// POST /api/v1/account/me/delete-request. Sibling to AccountAdminHandler
// (which mints a QR-delegate session for support staff to act on a
// user's behalf) — this handler accepts the user's own JWT and registers
// a soft delete request with a 30-day cooling-off period.
//
// Mounted under /api/v1/account/me/* with the standard JWT middleware
// (account_id is resolved by the auth layer; we never trust a body-
// supplied id).
type AccountSelfDeleteHandler struct {
	requests *app.AccountDeleteRequestService
}

// NewAccountSelfDeleteHandler wires the handler. requests is required;
// passing nil panics at boot via the requireService check, surfacing a
// wiring mistake before the first request rather than at runtime.
func NewAccountSelfDeleteHandler(requests *app.AccountDeleteRequestService) *AccountSelfDeleteHandler {
	if requests == nil {
		// Same gate-pattern as the admin handler's WithDeleteFlow nil
		// check, but here the dependency is unconditionally required —
		// no caller should construct the handler without the service.
		// Surface the misuse loudly.
		panic("handler: AccountSelfDeleteHandler requires non-nil AccountDeleteRequestService")
	}
	return &AccountSelfDeleteHandler{requests: requests}
}

// selfDeleteRequestBody is the JSON request shape. All fields optional;
// reason is validated against the closed enum at the handler, reason_text
// is truncated.
type selfDeleteRequestBody struct {
	Reason     string `json:"reason"`
	ReasonText string `json:"reason_text"`
}

// selfDeleteResponse is the JSON success shape. request_id is a stringified
// int64 to match the typical Lutu APP TypeScript-side `string | number`
// tolerance and to keep parity with how /admin/v1/.../delete-request
// returns its session id as a string.
type selfDeleteResponse struct {
	RequestID       string `json:"request_id"`
	Status          string `json:"status"`
	CoolingOffUntil string `json:"cooling_off_until"`
	AlreadyDeleted  bool   `json:"already_deleted,omitempty"`
}

// Submit — POST /api/v1/account/me/delete-request
//
// Resolves the account from the JWT subject (set by the auth
// middleware), validates the optional reason payload, and registers a
// pending delete request. Idempotent: re-submitting while a request is
// already pending returns 200 with the existing request id rather than
// creating a duplicate.
func (h *AccountSelfDeleteHandler) Submit(c *gin.Context) {
	accountID, ok := requireAccountID(c)
	if !ok {
		return
	}

	var body selfDeleteRequestBody
	// Optional body: an empty POST is legitimate (user just wants to
	// register intent without a reason). Only fail when the body is
	// present but malformed.
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&body); err != nil {
			handleBindError(c, err)
			return
		}
	}

	// Reason enum validation. Empty reason is allowed.
	if !entity.IsValidAccountDeleteReason(body.Reason) {
		respondValidationError(c, "Invalid reason value", map[string]string{
			"reason": "Must be one of: no_longer_using, privacy_concern, experience_issue, found_alternative, other",
		})
		return
	}

	// reason_text gets truncated, not rejected. Counting runes (not
	// bytes) so a Chinese-only message is held to 500 characters as a
	// user perceives them, not 500 bytes.
	if utf8.RuneCountInString(body.ReasonText) > reasonTextMaxRunes {
		body.ReasonText = truncateRunes(body.ReasonText, reasonTextMaxRunes)
	}

	result, err := h.requests.RequestSelfDelete(c.Request.Context(), app.SelfDeleteRequest{
		AccountID:  accountID,
		Reason:     body.Reason,
		ReasonText: body.ReasonText,
	})
	if err != nil {
		switch {
		case errors.Is(err, app.ErrAccountAlreadyDeleted):
			// Idempotent terminal: account already in Deleted status.
			// Return 200 so the APP renders "already_deleted" rather
			// than firing an alarming toast.
			slog.InfoContext(c.Request.Context(), "account_self_delete.already_deleted",
				"account_id", accountID, "request_id", c.GetString("request_id"))
			c.JSON(http.StatusOK, selfDeleteResponse{
				Status:         "already_deleted",
				AlreadyDeleted: true,
			})
			return
		case errors.Is(err, app.ErrAccountHasActiveSubscription):
			// Business rule: the user must cancel their active sub
			// before deleting the account. Map to 409 with an action
			// hint so the APP can surface a "Go to subscriptions" link.
			respondConflictWithAction(c,
				"Please cancel your active subscription before deleting your account.",
				ErrorAction{Type: "link", Label: "Manage subscription", URL: "/subscriptions"},
			)
			return
		case errors.Is(err, app.ErrDeleteRequestPending):
			// Race-loser path that didn't manage to re-read the row.
			// Treat as 200 idempotent without the body — the APP can
			// re-fetch via GET /api/v1/account/me to confirm.
			c.JSON(http.StatusOK, selfDeleteResponse{
				Status: entity.AccountDeleteRequestStatusPending,
			})
			return
		}
		respondInternalError(c, "AccountSelfDelete.Submit", err)
		return
	}

	if result.Idempotent {
		slog.InfoContext(c.Request.Context(), "account_self_delete.idempotent",
			"account_id", accountID,
			"request_id", result.RequestID,
			"trace_id", c.GetString("request_id"))
	} else {
		slog.InfoContext(c.Request.Context(), "account_self_delete.created",
			"account_id", accountID,
			"request_id", result.RequestID,
			"reason", body.Reason,
			"cooling_off_until", result.CoolingOffUntil.Format(time.RFC3339),
			"trace_id", c.GetString("request_id"))
	}

	c.JSON(http.StatusOK, selfDeleteResponse{
		RequestID:       strconv.FormatInt(result.RequestID, 10),
		Status:          result.Status,
		CoolingOffUntil: result.CoolingOffUntil.Format(time.RFC3339),
	})
}

// truncateRunes returns the first n runes of s. Used in place of a
// byte-slice [:n] which would split a multi-byte UTF-8 sequence and
// produce invalid output. n is assumed positive — caller guards.
func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n])
}
