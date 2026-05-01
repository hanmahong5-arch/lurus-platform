package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/hanmahong5-arch/lurus-platform/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// AccountAdminHandler exposes admin-only operations on accounts that
// fall outside the normal AccountHandler surface. Today this is only
// DeleteRequest (Phase 4 / Sprint 1A — GDPR-grade purge); future
// admin actions on accounts (force-suspend, force-merge) would land
// here next to it.
//
// Mounted under /admin/v1/accounts/* with admin-JWT middleware. The
// underlying executor is registered separately on QRHandler at boot;
// when missing, DeleteRequest returns 501 with a clear "delete flow
// not wired" message rather than silently 503-ing.
type AccountAdminHandler struct {
	accounts *app.AccountService
	qr       *QRHandler
}

// NewAccountAdminHandler wires the handler. accounts is required for
// the pre-flight target lookup. qr may be nil — DeleteRequest gates
// at 501 in that case, same as AppsAdmin's WithDeleteFlow pattern.
func NewAccountAdminHandler(accounts *app.AccountService) *AccountAdminHandler {
	return &AccountAdminHandler{accounts: accounts}
}

// WithDeleteFlow wires the QR handler. Chainable. Required for
// DeleteRequest to leave its 501 gate.
func (h *AccountAdminHandler) WithDeleteFlow(qr *QRHandler) *AccountAdminHandler {
	h.qr = qr
	return h
}

// deleteRequestResponse is the JSON shape returned to the admin Web UI
// after a successful DeleteRequest. Mirrors AppsAdmin's deleteRequest
// shape so the frontend QR-render component can be reused.
type accountDeleteRequestResponse struct {
	ID        string `json:"id"`
	QRPayload string `json:"qr_payload"`
	ExpiresAt string `json:"expires_at"`
	ExpiresIn int    `json:"expires_in"`
	AccountID int64  `json:"account_id"`
}

// DeleteRequest — POST /admin/v1/accounts/:id/delete-request
//
// Mints a delegate-action QR session for "purge this account". The
// caller is already authenticated as an admin by AdminAuth on the
// /admin/v1 router group. No DB cascade happens here — the audit row
// is created at QR mint AND re-validated by the executor on confirm,
// so the destructive cascade only runs after the boss biometric-
// confirms on the APP via /api/v2/qr/:id/confirm.
//
// Pre-flight checks:
//   - target account exists                  → 404 if not
//   - target account is not already deleted  → 200 idempotent if so
//     (matches the executor's
//     ErrAccountAlreadyPurged
//     handling — both ends
//     agree on the no-op
//     shape)
func (h *AccountAdminHandler) DeleteRequest(c *gin.Context) {
	if h.qr == nil {
		respondError(c, http.StatusNotImplemented, "delete_flow_not_wired",
			"QR-delegate delete flow is not configured on this deployment")
		return
	}
	callerID, ok := requireAccountID(c)
	if !ok {
		return
	}
	accountID, ok := parsePathInt64(c, "id", "Account ID")
	if !ok {
		return
	}

	a, err := h.accounts.GetByID(c.Request.Context(), accountID)
	if err != nil {
		respondInternalError(c, "AccountAdmin.DeleteRequest.lookup", err)
		return
	}
	if a == nil {
		respondNotFound(c, "Account")
		return
	}
	// Idempotent short-circuit — the desired end-state already holds
	// so there's nothing for the boss to confirm. Surface a 200 with
	// no QR payload so the admin UI can render "already deleted"
	// without firing a doomed scan flow.
	if a.Status == entity.AccountStatusDeleted {
		c.JSON(http.StatusOK, gin.H{
			"already_deleted": true,
			"account_id":      accountID,
		})
		return
	}

	session, err := h.qr.CreateDelegateSessionWithParams(c.Request.Context(), callerID, QRDelegateParams{
		Op:        qrDelegateOpDeleteAccount,
		AccountID: accountID,
	})
	if err != nil {
		if errors.Is(err, ErrUnsupportedDelegateOp) {
			respondError(c, http.StatusNotImplemented, "delete_flow_not_wired",
				"delete_account delegate executor is not registered on this deployment")
			return
		}
		respondInternalError(c, "AccountAdmin.DeleteRequest.create", err)
		return
	}
	slog.InfoContext(c.Request.Context(), "account_admin.delete_requested",
		"account_id", accountID, "initiator", callerID, "session_id", session.ID)

	c.JSON(http.StatusOK, accountDeleteRequestResponse{
		ID:        session.ID,
		QRPayload: session.QRPayload,
		ExpiresAt: session.ExpiresAt.Format(time.RFC3339),
		ExpiresIn: session.ExpiresIn,
		AccountID: accountID,
	})
}
