package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hanmahong5-arch/lurus-platform/internal/adapter/payment"
	"github.com/hanmahong5-arch/lurus-platform/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// SubscriptionHandler handles subscription lifecycle endpoints.
type SubscriptionHandler struct {
	subs      *app.SubscriptionService
	plans     *app.ProductService
	wallets   *app.WalletService
	epay      *payment.EpayProvider
	stripe    *payment.StripeProvider
	creem     *payment.CreemProvider
	alipay    *payment.AlipayProvider
	wechatPay *payment.WechatPayProvider
}

func NewSubscriptionHandler(
	subs *app.SubscriptionService,
	plans *app.ProductService,
	wallets *app.WalletService,
	epay *payment.EpayProvider,
	stripe *payment.StripeProvider,
	creem *payment.CreemProvider,
) *SubscriptionHandler {
	return &SubscriptionHandler{subs: subs, plans: plans, wallets: wallets, epay: epay, stripe: stripe, creem: creem}
}

// WithAlipayProvider sets the direct Alipay provider.
func (h *SubscriptionHandler) WithAlipayProvider(p *payment.AlipayProvider) *SubscriptionHandler {
	h.alipay = p
	return h
}

// WithWechatPayProvider sets the direct WeChat Pay provider.
func (h *SubscriptionHandler) WithWechatPayProvider(p *payment.WechatPayProvider) *SubscriptionHandler {
	h.wechatPay = p
	return h
}

// ListSubscriptions returns all subscriptions for the current user.
// GET /api/v1/subscriptions
func (h *SubscriptionHandler) ListSubscriptions(c *gin.Context) {
	accountID, ok := requireAccountID(c)
	if !ok {
		return
	}
	list, err := h.subs.ListByAccount(c.Request.Context(), accountID)
	if err != nil {
		respondInternalError(c, "subscription.list", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"subscriptions": list})
}

// GetSubscription returns the active subscription for a specific product.
// GET /api/v1/subscriptions/:product_id
func (h *SubscriptionHandler) GetSubscription(c *gin.Context) {
	accountID, ok := requireAccountID(c)
	if !ok {
		return
	}
	productID := c.Param("product_id")
	sub, err := h.subs.GetActive(c.Request.Context(), accountID, productID)
	if err != nil {
		respondInternalError(c, "subscription.get_active", err)
		return
	}
	if sub == nil {
		respondNotFound(c, "Active subscription")
		return
	}
	c.JSON(http.StatusOK, sub)
}

// Checkout initiates a subscription purchase.
// POST /api/v1/subscriptions/checkout
// Body: { product_id, plan_id, payment_method, return_url }
func (h *SubscriptionHandler) Checkout(c *gin.Context) {
	accountID, ok := requireAccountID(c)
	if !ok {
		return
	}
	var req struct {
		ProductID     string `json:"product_id"     binding:"required"`
		PlanID        int64  `json:"plan_id"        binding:"required"`
		PaymentMethod string `json:"payment_method" binding:"required"`
		ReturnURL     string `json:"return_url"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		handleBindError(c, err)
		return
	}

	returnURL := req.ReturnURL
	if returnURL == "" {
		returnURL = "/subscriptions"
	}

	ctx := c.Request.Context()

	// Wallet payment: debit balance and activate immediately.
	if req.PaymentMethod == "wallet" {
		plan, err := h.plans.GetPlanByID(ctx, req.PlanID)
		if err != nil || plan == nil {
			respondNotFound(c, "Plan")
			return
		}
		if plan.PriceCNY > 0 {
			if _, err := h.wallets.Debit(ctx, accountID, plan.PriceCNY,
				entity.TxTypeSubscription,
				"订阅 "+req.ProductID+" 套餐",
				"subscription", "", req.ProductID); err != nil {
				respondRichError(c, http.StatusPaymentRequired, ErrorBody{
					Code:    ErrCodeInsufficientBalance,
					Message: "Insufficient wallet balance for this subscription",
					Actions: []ErrorAction{
						{Type: "link", Label: "Top up wallet first", URL: "/wallet/topup"},
						{Type: "link", Label: "Try another payment method", URL: ""},
					},
				})
				return
			}
		}
		sub, err := h.subs.Activate(ctx, accountID, req.ProductID, req.PlanID, req.PaymentMethod, "")
		if err != nil {
			// Compensate: refund already-debited amount if activation fails.
			if plan.PriceCNY > 0 {
				_, creditErr := h.wallets.Credit(ctx, accountID, plan.PriceCNY,
					"subscription_payment_refund",
					"Subscription activation failed, auto-refund",
					"subscription", "", req.ProductID)
				if creditErr != nil {
					slog.Error("CRITICAL: subscription checkout compensation failed",
						"account_id", accountID, "amount", plan.PriceCNY,
						"activate_err", err, "credit_err", creditErr)
				}
			}
			respondInternalError(c, "subscription.activate", err)
			return
		}
		c.JSON(http.StatusCreated, gin.H{"subscription": sub})
		return
	}

	// External payment: create order and return checkout URL.
	plan, err := h.plans.GetPlanByID(ctx, req.PlanID)
	if err != nil || plan == nil {
		respondNotFound(c, "Plan")
		return
	}

	order := &entity.PaymentOrder{
		AccountID:     accountID,
		OrderType:     "subscription",
		ProductID:     req.ProductID,
		PlanID:        &req.PlanID,
		AmountCNY:     plan.PriceCNY,
		Currency:      "CNY",
		PaymentMethod: req.PaymentMethod,
		Status:        entity.OrderStatusPending,
	}
	if err := h.wallets.CreateSubscriptionOrder(ctx, order); err != nil {
		respondInternalError(c, "subscription.create_order", err)
		return
	}

	payURL, externalID, err := h.resolveCheckout(ctx, order, returnURL)
	if err != nil {
		var pe *providerError
		if errors.As(err, &pe) {
			respondError(c, http.StatusBadRequest, ErrCodeInvalidParameter, pe.Error())
			return
		}
		respondInternalError(c, "subscription.checkout", err)
		return
	}
	if externalID != "" {
		order.ExternalID = externalID
		if err := h.wallets.UpdatePaymentOrder(ctx, order); err != nil {
			slog.Warn("subscription.checkout: failed to save external_id (non-fatal)",
				"order_no", order.OrderNo, "external_id", externalID, "err", err)
		}
	}

	c.JSON(http.StatusCreated, gin.H{
		"order_no": order.OrderNo,
		"pay_url":  payURL,
	})
}

// CancelSubscription disables auto-renew.
// POST /api/v1/subscriptions/:product_id/cancel
func (h *SubscriptionHandler) CancelSubscription(c *gin.Context) {
	accountID, ok := requireAccountID(c)
	if !ok {
		return
	}
	productID := c.Param("product_id")
	if err := h.subs.Cancel(c.Request.Context(), accountID, productID); err != nil {
		classifyBusinessError(c, "subscription.cancel", err, map[string]errorMapping{
			"no active": {http.StatusNotFound, "No active subscription found for this product"},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"cancelled": true})
}

// resolveCheckout routes the order to the correct payment provider.
func (h *SubscriptionHandler) resolveCheckout(ctx context.Context, order *entity.PaymentOrder, returnURL string) (string, string, error) {
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
