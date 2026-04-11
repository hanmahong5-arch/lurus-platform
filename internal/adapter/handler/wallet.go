package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/hanmahong5-arch/lurus-platform/internal/adapter/payment"
	"github.com/hanmahong5-arch/lurus-platform/internal/app"
)

const (
	minTopupCNY = 1.0      // minimum topup amount in CNY
	maxTopupCNY = 100000.0 // maximum single topup amount in CNY
)

// WalletHandler handles wallet and topup endpoints.
type WalletHandler struct {
	wallets  *app.WalletService
	payments *payment.Registry
}

func NewWalletHandler(wallets *app.WalletService, payments *payment.Registry) *WalletHandler {
	return &WalletHandler{wallets: wallets, payments: payments}
}

// GetWallet returns the current user's wallet balance and VIP info.
// GET /api/v1/wallet
func (h *WalletHandler) GetWallet(c *gin.Context) {
	accountID, ok := requireAccountID(c)
	if !ok {
		return
	}
	wallet, err := h.wallets.GetWallet(c.Request.Context(), accountID)
	if err != nil {
		respondInternalError(c, "wallet.get", err)
		return
	}
	c.JSON(http.StatusOK, wallet)
}

// ListTransactions returns paginated wallet transaction history.
// GET /api/v1/wallet/transactions
func (h *WalletHandler) ListTransactions(c *gin.Context) {
	accountID, ok := requireAccountID(c)
	if !ok {
		return
	}
	page, pageSize := parsePagination(c)
	list, total, err := h.wallets.ListTransactions(c.Request.Context(), accountID, page, pageSize)
	if err != nil {
		respondInternalError(c, "wallet.list_transactions", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list, "total": total})
}

// Redeem applies a redemption code to the user's wallet.
// POST /api/v1/wallet/redeem
func (h *WalletHandler) Redeem(c *gin.Context) {
	accountID, ok := requireAccountID(c)
	if !ok {
		return
	}
	var req struct {
		Code string `json:"code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		handleBindError(c, err)
		return
	}
	if err := h.wallets.Redeem(c.Request.Context(), accountID, req.Code); err != nil {
		errMsg := err.Error()
		switch {
		case strings.Contains(errMsg, "usage limit"):
			respondRichError(c, http.StatusBadRequest, ErrorBody{
				Code:    "code_exhausted",
				Message: "This redemption code has reached its usage limit",
				Fields:  map[string]string{"code": "This code can no longer be used"},
			})
		case strings.Contains(errMsg, "expired"):
			respondRichError(c, http.StatusBadRequest, ErrorBody{
				Code:    "code_expired",
				Message: "This redemption code has expired",
				Fields:  map[string]string{"code": "This code has expired and is no longer valid"},
			})
		default:
			respondRichError(c, http.StatusBadRequest, ErrorBody{
				Code:    "invalid_code",
				Message: "Invalid redemption code",
				Fields:  map[string]string{"code": "Please check the code and try again"},
			})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"redeemed": true, "message": "Code redeemed successfully"})
}

// TopupInfo returns available payment methods for topup.
// GET /api/v1/wallet/topup/info
func (h *WalletHandler) TopupInfo(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"payment_methods": h.payments.ListMethods()})
}

// CreateTopup creates a payment order and returns the checkout URL.
// POST /api/v1/wallet/topup
func (h *WalletHandler) CreateTopup(c *gin.Context) {
	accountID, ok := requireAccountID(c)
	if !ok {
		return
	}
	var req struct {
		AmountCNY     float64 `json:"amount_cny"     binding:"required,gt=0"`
		PaymentMethod string  `json:"payment_method" binding:"required"`
		ReturnURL     string  `json:"return_url"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		handleBindError(c, err)
		return
	}

	if req.AmountCNY < minTopupCNY {
		respondError(c, http.StatusBadRequest, ErrCodeInvalidParameter,
			"Minimum topup amount is 1.00 CNY")
		return
	}
	if req.AmountCNY > maxTopupCNY {
		respondError(c, http.StatusBadRequest, ErrCodeInvalidParameter,
			"Maximum topup amount is 100,000 CNY per transaction")
		return
	}
	if !h.payments.HasMethod(req.PaymentMethod) {
		respondError(c, http.StatusBadRequest, ErrCodeInvalidParameter,
			"Unsupported payment method")
		return
	}

	order, err := h.wallets.CreateTopup(c.Request.Context(), accountID, req.AmountCNY, req.PaymentMethod)
	if err != nil {
		respondInternalError(c, "wallet.create_topup", err)
		return
	}

	returnURL := req.ReturnURL
	if returnURL == "" {
		returnURL = "/topup/result"
	}

	payURL, externalID, err := h.payments.Checkout(c.Request.Context(), order, returnURL)
	if err != nil {
		var pe *payment.ProviderNotAvailableError
		if errors.As(err, &pe) {
			respondError(c, http.StatusBadRequest, ErrCodeInvalidParameter, pe.Error())
			return
		}
		respondInternalError(c, "wallet.checkout", err)
		return
	}

	if externalID != "" {
		order.ExternalID = externalID
		_ = h.wallets.UpdatePaymentOrder(c.Request.Context(), order)
	}

	c.JSON(http.StatusCreated, gin.H{
		"order_no": order.OrderNo,
		"pay_url":  payURL,
	})
}

// ListOrders returns paginated payment orders for the current user.
// GET /api/v1/wallet/orders
func (h *WalletHandler) ListOrders(c *gin.Context) {
	accountID, ok := requireAccountID(c)
	if !ok {
		return
	}
	page, pageSize := parsePagination(c)
	list, total, err := h.wallets.ListOrders(c.Request.Context(), accountID, page, pageSize)
	if err != nil {
		respondInternalError(c, "wallet.list_orders", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list, "total": total})
}

// GetOrder returns a specific payment order by order number.
// GET /api/v1/wallet/orders/:order_no
func (h *WalletHandler) GetOrder(c *gin.Context) {
	accountID, ok := requireAccountID(c)
	if !ok {
		return
	}
	orderNo := c.Param("order_no")
	order, err := h.wallets.GetOrderByNo(c.Request.Context(), accountID, orderNo)
	if err != nil {
		respondNotFound(c, "Order")
		return
	}
	c.JSON(http.StatusOK, order)
}

// AdminAdjustWallet allows admin to manually credit/debit a wallet.
// POST /admin/v1/accounts/:id/wallet/adjust
func (h *WalletHandler) AdminAdjustWallet(c *gin.Context) {
	id, ok := parsePathInt64(c, "id", "Account ID")
	if !ok {
		return
	}
	var req struct {
		Amount      float64 `json:"amount"      binding:"required"`
		Description string  `json:"description" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		handleBindError(c, err)
		return
	}

	ctx := c.Request.Context()
	var err error
	if req.Amount > 0 {
		_, err = h.wallets.Credit(ctx, id, req.Amount, "admin_credit", req.Description, "admin", "admin_adjust", "")
	} else if req.Amount < 0 {
		_, err = h.wallets.Debit(ctx, id, -req.Amount, "admin_debit", req.Description, "admin", "admin_adjust", "")
	}
	if err != nil {
		if strings.Contains(err.Error(), "insufficient") {
			respondRichError(c, http.StatusPaymentRequired, ErrorBody{
				Code:    ErrCodeInsufficientBalance,
				Message: "Insufficient wallet balance for this adjustment",
				Actions: []ErrorAction{
					{Type: "link", Label: "Top up wallet", URL: "/wallet/topup"},
				},
			})
			return
		}
		respondInternalError(c, "wallet.admin_adjust", err)
		return
	}

	wallet, err := h.wallets.GetWallet(ctx, id)
	if err != nil {
		respondInternalError(c, "wallet.admin_adjust.get", err)
		return
	}
	c.JSON(http.StatusOK, wallet)
}

// AdminListReconciliationIssues returns paginated reconciliation issues.
// GET /admin/v1/reconciliation/issues?status=open&page=1&page_size=20
func (h *WalletHandler) AdminListReconciliationIssues(c *gin.Context) {
	status := c.Query("status")
	page, pageSize := parsePagination(c)
	list, total, err := h.wallets.ListReconciliationIssues(c.Request.Context(), status, page, pageSize)
	if err != nil {
		respondInternalError(c, "reconciliation.list", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list, "total": total})
}

// AdminResolveReconciliationIssue marks an issue as resolved or ignored.
// POST /admin/v1/reconciliation/issues/:id/resolve
func (h *WalletHandler) AdminResolveReconciliationIssue(c *gin.Context) {
	id, ok := parsePathInt64(c, "id", "Issue ID")
	if !ok {
		return
	}
	var req struct {
		Status     string `json:"status"     binding:"required,oneof=resolved ignored"`
		Resolution string `json:"resolution" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		handleBindError(c, err)
		return
	}
	if err := h.wallets.ResolveReconciliationIssue(c.Request.Context(), id, req.Status, req.Resolution); err != nil {
		respondInternalError(c, "reconciliation.resolve", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"resolved": true})
}

// providerError wraps payment.ProviderNotAvailableError for backward compatibility in tests.
type providerError = payment.ProviderNotAvailableError
