package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hanmahong5-arch/lurus-platform/internal/adapter/repo"
	"github.com/hanmahong5-arch/lurus-platform/internal/app"
)

// RefundHandler handles refund-related HTTP endpoints.
type RefundHandler struct {
	refunds *app.RefundService
	// qr is the QR-delegate session minter used by the admin
	// QR-approve flow. Optional: when nil the AdminQRApprove
	// endpoint stays at 501 so a half-wired deployment is loud.
	qr        *QRHandler
	auditRepo *repo.AuditEventRepo
}

// NewRefundHandler creates a new RefundHandler.
func NewRefundHandler(refunds *app.RefundService) *RefundHandler {
	return &RefundHandler{refunds: refunds}
}

// WithQRApprove wires the QR-delegate session minter so the boss
// can biometric-approve large refunds from his APP. Chainable; safe
// to call with nil to leave the endpoint gated.
func (h *RefundHandler) WithQRApprove(qr *QRHandler) *RefundHandler {
	h.qr = qr
	return h
}

// WithAuditRepo wires the persistent audit-events sink. Chainable;
// nil-safe.
func (h *RefundHandler) WithAuditRepo(r *repo.AuditEventRepo) *RefundHandler {
	h.auditRepo = r
	return h
}

// RequestRefund creates a refund request for a paid order.
// POST /api/v1/refunds
func (h *RefundHandler) RequestRefund(c *gin.Context) {
	accountID, ok := requireAccountID(c)
	if !ok {
		return
	}
	var req struct {
		OrderNo string `json:"order_no" binding:"required"`
		Reason  string `json:"reason"   binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		handleBindError(c, err)
		return
	}

	r, err := h.refunds.RequestRefund(c.Request.Context(), accountID, req.OrderNo, req.Reason)
	if err != nil {
		classifyBusinessError(c, "refund.request", err, map[string]errorMapping{
			"not found":           {http.StatusNotFound, "Order not found or does not belong to your account"},
			"requires a paid":     {http.StatusBadRequest, "Refunds can only be requested for paid orders"},
			"window":              {http.StatusBadRequest, "The refund window for this order has expired"},
			"already in progress": {http.StatusConflict, "A refund is already in progress for this order"},
		})
		return
	}
	c.JSON(http.StatusCreated, r)
}

// ListRefunds returns paginated refunds for the current user.
// GET /api/v1/refunds
func (h *RefundHandler) ListRefunds(c *gin.Context) {
	accountID, ok := requireAccountID(c)
	if !ok {
		return
	}
	page, pageSize := parsePagination(c)

	list, total, err := h.refunds.ListByAccount(c.Request.Context(), accountID, page, pageSize)
	if err != nil {
		respondInternalError(c, "refund.list", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list, "total": total})
}

// GetRefund returns a single refund by its refund number.
// IDOR is enforced: only the owning account may retrieve the refund.
// GET /api/v1/refunds/:refund_no
func (h *RefundHandler) GetRefund(c *gin.Context) {
	accountID, ok := requireAccountID(c)
	if !ok {
		return
	}
	refundNo := c.Param("refund_no")

	r, err := h.refunds.GetByNo(c.Request.Context(), accountID, refundNo)
	if err != nil {
		respondNotFound(c, "Refund")
		return
	}
	c.JSON(http.StatusOK, r)
}

// AdminApprove approves a pending refund and credits the account wallet.
// POST /admin/v1/refunds/:refund_no/approve
func (h *RefundHandler) AdminApprove(c *gin.Context) {
	refundNo := c.Param("refund_no")
	var req struct {
		ReviewerID string `json:"reviewer_id" binding:"required"`
		ReviewNote string `json:"review_note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		handleBindError(c, err)
		return
	}

	if err := h.refunds.Approve(c.Request.Context(), refundNo, req.ReviewerID, req.ReviewNote); err != nil {
		classifyBusinessError(c, "refund.approve", err, map[string]errorMapping{
			"not found":   {http.StatusNotFound, "Refund not found"},
			"not pending": {http.StatusConflict, "Refund is no longer in pending state"},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"approved": true})
}

// AdminReject rejects a pending refund.
// POST /admin/v1/refunds/:refund_no/reject
func (h *RefundHandler) AdminReject(c *gin.Context) {
	refundNo := c.Param("refund_no")
	var req struct {
		ReviewerID string `json:"reviewer_id" binding:"required"`
		ReviewNote string `json:"review_note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		handleBindError(c, err)
		return
	}

	if err := h.refunds.Reject(c.Request.Context(), refundNo, req.ReviewerID, req.ReviewNote); err != nil {
		classifyBusinessError(c, "refund.reject", err, map[string]errorMapping{
			"not found":   {http.StatusNotFound, "Refund not found"},
			"not pending": {http.StatusConflict, "Refund is no longer in pending state"},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"rejected": true})
}

// qrApproveResponse mirrors the QR session shape so the admin Web UI
// can render a QR immediately after the mint without an extra
// round-trip. The Web side typically polls /api/v2/qr/:id/status to
// learn when the boss biometric-confirmed.
type qrApproveResponse struct {
	ID        string `json:"id"`
	QRPayload string `json:"qr_payload"`
	ExpiresAt string `json:"expires_at"`
	ExpiresIn int    `json:"expires_in"`
	RefundNo  string `json:"refund_no"`
}

// AdminQRApprove — POST /admin/v1/refunds/:refund_no/qr-approve
//
// Mints a QR-delegate session for the approve_refund op. The CS rep
// is already authenticated as an admin by AdminAuth; the destructive
// step (RefundService.Approve) only runs after the boss scans + a
// biometric step-up on the APP.
//
// Threshold enforcement is intentionally not done here — the policy
// "amounts > X must use QR" lives in the admin Web UI (which only
// surfaces the QR-approve button for refunds above the threshold).
// Backend stays simple: any pending refund can be QR-approved.
//
// Why a separate endpoint instead of overloading /approve: the
// existing /approve runs synchronously and credits immediately; this
// path defers to the boss. Keeping them distinct lets clients
// choose explicitly and keeps the audit trail unambiguous.
func (h *RefundHandler) AdminQRApprove(c *gin.Context) {
	if h.qr == nil {
		respondError(c, http.StatusNotImplemented, "qr_approve_not_wired",
			"QR-delegate refund approval is not configured on this deployment")
		return
	}
	callerID, ok := requireAccountID(c)
	if !ok {
		return
	}
	refundNo := c.Param("refund_no")
	if refundNo == "" {
		respondError(c, http.StatusBadRequest, ErrCodeInvalidRequest,
			"refund_no path param is required")
		return
	}

	session, err := h.qr.CreateDelegateSessionWithParams(c.Request.Context(), callerID, QRDelegateParams{
		Op:       qrDelegateOpApproveRefund,
		RefundNo: refundNo,
	})
	if err != nil {
		if errors.Is(err, ErrUnsupportedDelegateOp) {
			respondError(c, http.StatusNotImplemented, "qr_approve_not_wired",
				"approve_refund delegate executor is not registered on this deployment")
			return
		}
		respondInternalError(c, "Refund.AdminQRApprove", err)
		return
	}
	slog.InfoContext(c.Request.Context(), "refund.qr_approve_requested",
		"refund_no", refundNo, "initiator", callerID, "session_id", session.ID)

	emitAudit(c, h.auditRepo, "refund.qr_approve_request", auditEmitResultSuccess,
		int64Ptr(callerID), nil, "refund",
		map[string]string{"refund_no": refundNo, "session_id": session.ID}, "")

	c.JSON(http.StatusOK, qrApproveResponse{
		ID:        session.ID,
		QRPayload: session.QRPayload,
		ExpiresAt: session.ExpiresAt.Format(time.RFC3339),
		ExpiresIn: session.ExpiresIn,
		RefundNo:  refundNo,
	})
}
