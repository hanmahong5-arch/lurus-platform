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
	"alipay":        true,
	"alipay_qr":     true,
	"alipay_wap":    true,
	"wechat_native": true,
	"wechat_h5":     true,
	"wechat_jsapi":  true,
	"epay_alipay":   true,
	"epay_wechat":   true,
	"stripe":        true,
	"creem":         true,
}

// WalletHandler handles wallet and topup endpoints.
type WalletHandler struct {
	wallets   *app.WalletService
	epay      *payment.EpayProvider
	stripe    *payment.StripeProvider
	creem     *payment.CreemProvider
	alipay    *payment.AlipayProvider
	wechatPay *payment.WechatPayProvider
}

func NewWalletHandler(
	wallets *app.WalletService,
	epay *payment.EpayProvider,
	stripe *payment.StripeProvider,
	creem *payment.CreemProvider,
) *WalletHandler {
	return &WalletHandler{wallets: wallets, epay: epay, stripe: stripe, creem: creem}
}

// WithAlipayProvider sets the direct Alipay provider.
func (h *WalletHandler) WithAlipayProvider(p *payment.AlipayProvider) *WalletHandler {
	h.alipay = p
	return h
}

// WithWechatPayProvider sets the direct WeChat Pay provider.
func (h *WalletHandler) WithWechatPayProvider(p *payment.WechatPayProvider) *WalletHandler {
	h.wechatPay = p
	return h
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
	methods := make([]gin.H, 0, 8)
	// Direct Alipay (preferred over Epay gateway).
	if h.alipay != nil {
		methods = append(methods,
			gin.H{"id": "alipay", "name": "支付宝", "provider": "alipay", "type": "redirect"},
			gin.H{"id": "alipay_qr", "name": "支付宝 (扫码)", "provider": "alipay", "type": "qr"},
			gin.H{"id": "alipay_wap", "name": "支付宝 (手机)", "provider": "alipay", "type": "redirect"},
		)
	} else if h.epay != nil {
		methods = append(methods,
			gin.H{"id": "epay_alipay", "name": "支付宝", "provider": "epay", "type": "qr"},
		)
	}
	// Direct WeChat Pay (preferred over Epay gateway).
	if h.wechatPay != nil {
		methods = append(methods,
			gin.H{"id": "wechat_native", "name": "微信支付 (扫码)", "provider": "wechat", "type": "qr"},
			gin.H{"id": "wechat_h5", "name": "微信支付 (H5)", "provider": "wechat", "type": "redirect"},
		)
	} else if h.epay != nil {
		methods = append(methods,
			gin.H{"id": "epay_wechat", "name": "微信支付", "provider": "epay", "type": "qr"},
		)
	}
	if h.stripe != nil {
		methods = append(methods, gin.H{"id": "stripe", "name": "信用卡 (Stripe)", "provider": "stripe", "type": "redirect"})
	}
	if h.creem != nil {
		methods = append(methods, gin.H{"id": "creem", "name": "Creem", "provider": "creem", "type": "redirect"})
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

// resolveCheckout routes the order to the correct payment provider.
func (h *WalletHandler) resolveCheckout(ctx context.Context, order *entity.PaymentOrder, returnURL string) (payURL, externalID string, err error) {
	switch order.PaymentMethod {
	case "alipay", "alipay_qr", "alipay_wap":
		if h.alipay == nil {
			return "", "", errProviderDisabled("alipay")
		}
		return h.alipay.CreateCheckout(ctx, order, returnURL)
	case "wechat_native", "wechat_h5", "wechat_jsapi":
		if h.wechatPay == nil {
			return "", "", errProviderDisabled("wechat")
		}
		return h.wechatPay.CreateCheckout(ctx, order, returnURL)
	case "epay_alipay", "epay_wxpay", "epay_wechat":
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
