package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/hanmahong5-arch/lurus-platform/internal/adapter/payment"
	"github.com/hanmahong5-arch/lurus-platform/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

const (
	minTopupCNY = 1.0      // minimum topup amount in CNY
	maxTopupCNY = 100000.0 // maximum single topup amount in CNY
)

// validPaymentMethods is the set of accepted payment method identifiers.
var validPaymentMethods = map[string]bool{
	"epay_alipay": true,
	"epay_wechat": true,
	"stripe":      true,
	"creem":       true,
}

// WalletHandler handles wallet and topup endpoints.
type WalletHandler struct {
	wallets *app.WalletService
	epay    *payment.EpayProvider
	stripe  *payment.StripeProvider
	creem   *payment.CreemProvider
}

func NewWalletHandler(
	wallets *app.WalletService,
	epay *payment.EpayProvider,
	stripe *payment.StripeProvider,
	creem *payment.CreemProvider,
) *WalletHandler {
	return &WalletHandler{wallets: wallets, epay: epay, stripe: stripe, creem: creem}
}

// GetWallet returns the current user's wallet balance and VIP info.
// GET /api/v1/wallet
func (h *WalletHandler) GetWallet(c *gin.Context) {
	accountID := mustAccountID(c)
	wallet, err := h.wallets.GetWallet(c.Request.Context(), accountID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "wallet lookup failed"})
		return
	}
	c.JSON(http.StatusOK, wallet)
}

// ListTransactions returns paginated wallet transaction history.
// GET /api/v1/wallet/transactions
func (h *WalletHandler) ListTransactions(c *gin.Context) {
	accountID := mustAccountID(c)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	list, total, err := h.wallets.ListTransactions(c.Request.Context(), accountID, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list transactions"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list, "total": total})
}

// Redeem applies a redemption code to the user's wallet.
// POST /api/v1/wallet/redeem
func (h *WalletHandler) Redeem(c *gin.Context) {
	accountID := mustAccountID(c)
	var req struct {
		Code string `json:"code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.wallets.Redeem(c.Request.Context(), accountID, req.Code); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"redeemed": true})
}

// TopupInfo returns available payment methods for topup.
// GET /api/v1/wallet/topup/info
func (h *WalletHandler) TopupInfo(c *gin.Context) {
	methods := make([]gin.H, 0, 4)
	if h.epay != nil {
		methods = append(methods,
			gin.H{"id": "epay_alipay", "name": "支付宝", "provider": "epay"},
			gin.H{"id": "epay_wxpay", "name": "微信支付", "provider": "epay"},
		)
	}
	if h.stripe != nil {
		methods = append(methods, gin.H{"id": "stripe", "name": "信用卡 (Stripe)", "provider": "stripe"})
	}
	if h.creem != nil {
		methods = append(methods, gin.H{"id": "creem", "name": "Creem", "provider": "creem"})
	}
	c.JSON(http.StatusOK, gin.H{"payment_methods": methods})
}

// CreateTopup creates a payment order and returns the checkout URL.
// POST /api/v1/wallet/topup
func (h *WalletHandler) CreateTopup(c *gin.Context) {
	accountID := mustAccountID(c)
	var req struct {
		AmountCNY     float64 `json:"amount_cny"     binding:"required,gt=0"`
		PaymentMethod string  `json:"payment_method" binding:"required"`
		ReturnURL     string  `json:"return_url"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate topup amount bounds.
	if req.AmountCNY < minTopupCNY {
		c.JSON(http.StatusBadRequest, gin.H{"error": "amount_cny must be at least 1.00"})
		return
	}
	if req.AmountCNY > maxTopupCNY {
		c.JSON(http.StatusBadRequest, gin.H{"error": "amount_cny exceeds maximum allowed per transaction"})
		return
	}

	// Validate payment method against known providers.
	if !validPaymentMethods[req.PaymentMethod] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported payment method"})
		return
	}

	order, err := h.wallets.CreateTopup(c.Request.Context(), accountID, req.AmountCNY, req.PaymentMethod)
	if err != nil {
		slog.Error("wallet/create-topup: create order failed", "account_id", accountID, "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create payment order"})
		return
	}

	returnURL := req.ReturnURL
	if returnURL == "" {
		returnURL = "/topup/result"
	}

	payURL, externalID, err := h.resolveCheckout(c.Request.Context(), order, returnURL)
	if err != nil {
		// providerError is a user-visible error (provider disabled/misconfigured).
		var pe *providerError
		if errors.As(err, &pe) {
			c.JSON(http.StatusBadRequest, gin.H{"error": pe.Error()})
			return
		}
		slog.Error("wallet/create-topup: checkout failed", "account_id", accountID, "order_no", order.OrderNo, "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "payment checkout failed"})
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
	accountID := mustAccountID(c)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	list, total, err := h.wallets.ListOrders(c.Request.Context(), accountID, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list orders"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list, "total": total})
}

// GetOrder returns a specific payment order by order number.
// GET /api/v1/wallet/orders/:order_no
func (h *WalletHandler) GetOrder(c *gin.Context) {
	accountID := mustAccountID(c)
	orderNo := c.Param("order_no")
	order, err := h.wallets.GetOrderByNo(c.Request.Context(), accountID, orderNo)
	if err != nil {
		// Return 404 for both not-found and ownership mismatch (prevents enumeration).
		// Internal errors are logged but never surfaced to the caller.
		slog.Warn("wallet/get-order: order not found or not owned", "account_id", accountID, "order_no", orderNo, "err", err)
		c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
		return
	}
	c.JSON(http.StatusOK, order)
}

// AdminAdjustWallet allows admin to manually credit/debit a wallet.
// POST /admin/v1/accounts/:id/wallet/adjust
func (h *WalletHandler) AdminAdjustWallet(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid account id"})
		return
	}
	var req struct {
		Amount      float64 `json:"amount"      binding:"required"`
		Description string  `json:"description" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	if req.Amount > 0 {
		_, err = h.wallets.Credit(ctx, id, req.Amount, "admin_credit", req.Description, "admin", "admin_adjust", "")
	} else if req.Amount < 0 {
		_, err = h.wallets.Debit(ctx, id, -req.Amount, "admin_debit", req.Description, "admin", "admin_adjust", "")
	}
	if err != nil {
		// "insufficient balance" is a user-meaningful error; DB errors must not leak.
		slog.Error("wallet/admin-adjust: adjust failed", "account_id", id, "amount", req.Amount, "err", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "wallet adjustment failed"})
		return
	}

	wallet, err := h.wallets.GetWallet(ctx, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "adjustment failed"})
		return
	}
	c.JSON(http.StatusOK, wallet)
}

// resolveCheckout routes the order to the correct payment provider.
func (h *WalletHandler) resolveCheckout(ctx context.Context, order *entity.PaymentOrder, returnURL string) (payURL, externalID string, err error) {
	switch order.PaymentMethod {
	case "epay_alipay", "epay_wxpay":
		if h.epay == nil {
			return "", "", errProviderDisabled("epay")
		}
		return h.epay.CreateCheckout(ctx, order, returnURL)
	case "stripe":
		if h.stripe == nil {
			return "", "", errProviderDisabled("stripe")
		}
		return h.stripe.CreateCheckout(ctx, order, returnURL)
	case "creem":
		if h.creem == nil {
			return "", "", errProviderDisabled("creem")
		}
		return h.creem.CreateCheckout(ctx, order, returnURL)
	default:
		return "", "", errProviderDisabled(order.PaymentMethod)
	}
}

func errProviderDisabled(name string) error {
	return &providerError{name: name}
}

type providerError struct{ name string }

func (e *providerError) Error() string {
	return "payment provider not available: " + e.name
}
