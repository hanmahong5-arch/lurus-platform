package handler

import (
	"net/http"
	"strconv"
	"strings"

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
	accountID, ok := requireAccountID(c)
	if !ok {
		return
	}
	var req struct {
		OrderNo string `json:"order_no" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		handleBindError(c, err)
		return
	}

	inv, err := h.invoices.Generate(c.Request.Context(), accountID, req.OrderNo)
	if err != nil {
		classifyBusinessError(c, "invoice.generate", err, map[string]errorMapping{
			"not found":        {http.StatusNotFound, "Order not found or does not belong to your account"},
			"only be generated": {http.StatusBadRequest, "Invoices can only be generated for paid orders"},
		})
		return
	}
	c.JSON(http.StatusOK, inv)
}

// ListInvoices returns paginated invoices for the current user.
// GET /api/v1/invoices
func (h *InvoiceHandler) ListInvoices(c *gin.Context) {
	accountID, ok := requireAccountID(c)
	if !ok {
		return
	}
	page, pageSize := parsePagination(c)

	list, total, err := h.invoices.ListByAccount(c.Request.Context(), accountID, page, pageSize)
	if err != nil {
		respondInternalError(c, "invoice.list", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list, "total": total})
}

// GetInvoice returns a single invoice by its invoice number.
// IDOR is enforced: only the owning account may retrieve the invoice.
// GET /api/v1/invoices/:invoice_no
func (h *InvoiceHandler) GetInvoice(c *gin.Context) {
	accountID, ok := requireAccountID(c)
	if !ok {
		return
	}
	invoiceNo := c.Param("invoice_no")

	inv, err := h.invoices.GetByNo(c.Request.Context(), accountID, invoiceNo)
	if err != nil {
		respondNotFound(c, "Invoice")
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
			respondError(c, http.StatusBadRequest, ErrCodeInvalidParameter, "Invalid account_id parameter")
			return
		}
		filterAccountID = id
	}
	page, pageSize := parsePagination(c)

	list, total, err := h.invoices.AdminList(c.Request.Context(), filterAccountID, page, pageSize)
	if err != nil {
		respondInternalError(c, "invoice.admin_list", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list, "total": total})
}

// ── Error classification helper ─────────────────────────────────────────────

// errorMapping maps a business error keyword to an HTTP status and user message.
type errorMapping struct {
	status  int
	message string
}

// classifyBusinessError inspects err.Error() for known business keywords and
// sends the appropriate HTTP response. Falls back to 500 for unknown errors.
// This bridges the gap between app-layer fmt.Errorf strings and proper HTTP responses
// without requiring app-layer changes.
func classifyBusinessError(c *gin.Context, handler string, err error, mappings map[string]errorMapping) {
	errMsg := err.Error()
	for keyword, m := range mappings {
		if strings.Contains(errMsg, keyword) {
			respondError(c, m.status, ErrCodeInvalidRequest, m.message)
			return
		}
	}
	respondInternalError(c, handler, err)
}
