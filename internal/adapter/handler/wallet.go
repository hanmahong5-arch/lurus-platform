package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"

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
		msg := "Invalid or expired redemption code"
		if strings.Contains(err.Error(), "usage limit") {
			msg = "This code has reached its usage limit"
		} else if strings.Contains(err.Error(), "expired") {
			msg = "This code has expired"
		} else if strings.Contains(err.Error(), "invalid") {
			msg = "Invalid redemption code"
		}
		respondBadRequest(c, msg)
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
	if !validPaymentMethods[req.PaymentMethod] {
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

	payURL, externalID, err := h.resolveCheckout(c.Request.Context(), order, returnURL)
	if err != nil {
		var pe *providerError
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
			respondError(c, http.StatusPaymentRequired, ErrCodeInsufficientBalance,
				"Insufficient wallet balance for this adjustment")
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
	return "Payment provider not available: " + e.name
}
