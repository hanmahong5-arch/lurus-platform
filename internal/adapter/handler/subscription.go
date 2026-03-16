package handler

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hanmahong5-arch/lurus-platform/internal/adapter/payment"
	"github.com/hanmahong5-arch/lurus-platform/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// SubscriptionHandler handles subscription lifecycle endpoints.
type SubscriptionHandler struct {
	subs    *app.SubscriptionService
	plans   *app.ProductService
	wallets *app.WalletService
	epay    *payment.EpayProvider
	stripe  *payment.StripeProvider
	creem   *payment.CreemProvider
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

// ListSubscriptions returns all subscriptions for the current user.
// GET /api/v1/subscriptions
func (h *SubscriptionHandler) ListSubscriptions(c *gin.Context) {
	accountID := mustAccountID(c)
	list, err := h.subs.ListByAccount(c.Request.Context(), accountID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list subscriptions"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"subscriptions": list})
}

// GetSubscription returns the active subscription for a specific product.
// GET /api/v1/subscriptions/:product_id
func (h *SubscriptionHandler) GetSubscription(c *gin.Context) {
	accountID := mustAccountID(c)
	productID := c.Param("product_id")
	sub, err := h.subs.GetActive(c.Request.Context(), accountID, productID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "lookup failed"})
		return
	}
	if sub == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no active subscription"})
		return
	}
	c.JSON(http.StatusOK, sub)
}

// Checkout initiates a subscription purchase.
// POST /api/v1/subscriptions/checkout
// Body: { product_id, plan_id, payment_method, return_url }
func (h *SubscriptionHandler) Checkout(c *gin.Context) {
	accountID := mustAccountID(c)
	var req struct {
		ProductID     string `json:"product_id"     binding:"required"`
		PlanID        int64  `json:"plan_id"        binding:"required"`
		PaymentMethod string `json:"payment_method" binding:"required"`
		ReturnURL     string `json:"return_url"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	returnURL := req.ReturnURL
	if returnURL == "" {
		returnURL = "/subscriptions"
	}

	// Wallet payment: debit balance and activate immediately
	if req.PaymentMethod == "wallet" {
		plan, err := h.plans.GetPlanByID(c.Request.Context(), req.PlanID)
		if err != nil || plan == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "plan not found"})
			return
		}
		if plan.PriceCNY > 0 {
			if _, err := h.wallets.Debit(c.Request.Context(), accountID, plan.PriceCNY,
				entity.TxTypeSubscription,
				"订阅 "+req.ProductID+" 套餐",
				"subscription", "", req.ProductID); err != nil {
				c.JSON(http.StatusPaymentRequired, gin.H{"error": err.Error()})
				return
			}
		}
		sub, err := h.subs.Activate(c.Request.Context(), accountID, req.ProductID, req.PlanID, req.PaymentMethod, "")
		if err != nil {
			// Compensate: refund already-debited amount if activation fails.
			if plan.PriceCNY > 0 {
				_, creditErr := h.wallets.Credit(c.Request.Context(), accountID, plan.PriceCNY,
					"subscription_payment_refund",
					"Subscription activation failed, auto-refund",
					"subscription", "", req.ProductID)
				if creditErr != nil {
					slog.Error("CRITICAL: subscription checkout compensation failed",
						"account_id", accountID, "amount", plan.PriceCNY,
						"activate_err", err, "credit_err", creditErr)
				}
			}
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusCreated, gin.H{"subscription": sub})
		return
	}

	// External payment: create order and return checkout URL
	plan, err := h.plans.GetPlanByID(c.Request.Context(), req.PlanID)
	if err != nil || plan == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "plan not found"})
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
	// OrderNo is set inside CreateSubscriptionOrder
	if err := h.wallets.CreateSubscriptionOrder(c.Request.Context(), order); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "create order failed"})
		return
	}

	payURL, externalID, err := h.resolveCheckout(c.Request.Context(), order, returnURL)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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

// CancelSubscription disables auto-renew.
// POST /api/v1/subscriptions/:product_id/cancel
func (h *SubscriptionHandler) CancelSubscription(c *gin.Context) {
	accountID := mustAccountID(c)
	productID := c.Param("product_id")
	if err := h.subs.Cancel(c.Request.Context(), accountID, productID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"cancelled": true})
}

// resolveCheckout routes the order to the correct payment provider.
func (h *SubscriptionHandler) resolveCheckout(ctx context.Context, order *entity.PaymentOrder, returnURL string) (string, string, error) {
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
