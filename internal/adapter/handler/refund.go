package handler

import (
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
			"not found":            {http.StatusNotFound, "Order not found or does not belong to your account"},
			"requires a paid":      {http.StatusBadRequest, "Refunds can only be requested for paid orders"},
			"window":               {http.StatusBadRequest, "The refund window for this order has expired"},
			"already in progress":  {http.StatusConflict, "A refund is already in progress for this order"},
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
