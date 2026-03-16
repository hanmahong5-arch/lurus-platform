package handler

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hanmahong5-arch/lurus-platform/internal/app"
)

// RefundHandler handles refund-related HTTP endpoints.
type RefundHandler struct {
	refunds *app.RefundService
}

// NewRefundHandler creates a new RefundHandler.
func NewRefundHandler(refunds *app.RefundService) *RefundHandler {
	return &RefundHandler{refunds: refunds}
}

// RequestRefund creates a refund request for a paid order.
// POST /api/v1/refunds
func (h *RefundHandler) RequestRefund(c *gin.Context) {
	accountID := mustAccountID(c)
	var req struct {
		OrderNo string `json:"order_no" binding:"required"`
		Reason  string `json:"reason"   binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	r, err := h.refunds.RequestRefund(c.Request.Context(), accountID, req.OrderNo, req.Reason)
	if err != nil {
		slog.Warn("refund/request: failed",
			"account_id", accountID, "order_no", req.OrderNo, "err", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, r)
}

// ListRefunds returns paginated refunds for the current user.
// GET /api/v1/refunds
func (h *RefundHandler) ListRefunds(c *gin.Context) {
	accountID := mustAccountID(c)
	page, pageSize := mustPageParams(c)

	list, total, err := h.refunds.ListByAccount(c.Request.Context(), accountID, page, pageSize)
	if err != nil {
		slog.Error("refund/list: failed", "account_id", accountID, "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list refunds"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list, "total": total})
}

// GetRefund returns a single refund by its refund number.
// IDOR is enforced: only the owning account may retrieve the refund.
// GET /api/v1/refunds/:refund_no
func (h *RefundHandler) GetRefund(c *gin.Context) {
	accountID := mustAccountID(c)
	refundNo := c.Param("refund_no")

	r, err := h.refunds.GetByNo(c.Request.Context(), accountID, refundNo)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "refund not found"})
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
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.refunds.Approve(c.Request.Context(), refundNo, req.ReviewerID, req.ReviewNote); err != nil {
		slog.Warn("refund/admin-approve: failed", "refund_no", refundNo, "err", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.refunds.Reject(c.Request.Context(), refundNo, req.ReviewerID, req.ReviewNote); err != nil {
		slog.Warn("refund/admin-reject: failed", "refund_no", refundNo, "err", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"rejected": true})
}
