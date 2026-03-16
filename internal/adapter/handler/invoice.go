package handler

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/hanmahong5-arch/lurus-platform/internal/app"
)

// InvoiceHandler handles invoice-related HTTP endpoints.
type InvoiceHandler struct {
	invoices *app.InvoiceService
}

// NewInvoiceHandler creates a new InvoiceHandler.
func NewInvoiceHandler(invoices *app.InvoiceService) *InvoiceHandler {
	return &InvoiceHandler{invoices: invoices}
}

// GenerateInvoice creates or retrieves an invoice for a paid payment order.
// Idempotent: calling multiple times with the same order_no returns the same invoice.
// POST /api/v1/invoices
func (h *InvoiceHandler) GenerateInvoice(c *gin.Context) {
	accountID := mustAccountID(c)
	var req struct {
		OrderNo string `json:"order_no" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	inv, err := h.invoices.Generate(c.Request.Context(), accountID, req.OrderNo)
	if err != nil {
		slog.Warn("invoice/generate: failed",
			"account_id", accountID, "order_no", req.OrderNo, "err", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, inv)
}

// ListInvoices returns paginated invoices for the current user.
// GET /api/v1/invoices
func (h *InvoiceHandler) ListInvoices(c *gin.Context) {
	accountID := mustAccountID(c)
	page, pageSize := mustPageParams(c)

	list, total, err := h.invoices.ListByAccount(c.Request.Context(), accountID, page, pageSize)
	if err != nil {
		slog.Error("invoice/list: failed", "account_id", accountID, "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list invoices"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list, "total": total})
}

// GetInvoice returns a single invoice by its invoice number.
// IDOR is enforced: only the owning account may retrieve the invoice.
// GET /api/v1/invoices/:invoice_no
func (h *InvoiceHandler) GetInvoice(c *gin.Context) {
	accountID := mustAccountID(c)
	invoiceNo := c.Param("invoice_no")

	inv, err := h.invoices.GetByNo(c.Request.Context(), accountID, invoiceNo)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "invoice not found"})
		return
	}
	c.JSON(http.StatusOK, inv)
}

// AdminList returns paginated invoices across all accounts.
// Optional query param account_id filters by account.
// GET /admin/v1/invoices
func (h *InvoiceHandler) AdminList(c *gin.Context) {
	var filterAccountID int64
	if raw := c.Query("account_id"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid account_id"})
			return
		}
		filterAccountID = id
	}
	page, pageSize := mustPageParams(c)

	list, total, err := h.invoices.AdminList(c.Request.Context(), filterAccountID, page, pageSize)
	if err != nil {
		slog.Error("invoice/admin-list: failed", "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list invoices"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list, "total": total})
}

// mustPageParams reads and normalises page/page_size query params.
func mustPageParams(c *gin.Context) (page, pageSize int) {
	page, _ = strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ = strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	return
}
