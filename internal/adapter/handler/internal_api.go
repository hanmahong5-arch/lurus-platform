package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hanmahong5-arch/lurus-platform/internal/adapter/payment"
	"github.com/hanmahong5-arch/lurus-platform/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/auth"
)

// validateSessionTokenFn is a package-level reference to auth.ValidateSessionToken
// so InternalHandler can validate tokens without importing the auth middleware directly.
var validateSessionTokenFn = auth.ValidateSessionToken

// InternalHandler serves /internal/v1/* endpoints for service-to-service calls.
type InternalHandler struct {
	accounts      *app.AccountService
	subs          *app.SubscriptionService
	entitlements  *app.EntitlementService
	vip           *app.VIPService
	overview      *app.OverviewService
	wallet        *app.WalletService
	referral      *app.ReferralService
	sessionSecret string
	epay          *payment.EpayProvider
	stripe        *payment.StripeProvider
	creem         *payment.CreemProvider
}

func NewInternalHandler(
	accounts *app.AccountService,
	subs *app.SubscriptionService,
	ents *app.EntitlementService,
	vip *app.VIPService,
	overview *app.OverviewService,
	wallet *app.WalletService,
	referral *app.ReferralService,
	sessionSecret string,
) *InternalHandler {
	return &InternalHandler{
		accounts:      accounts,
		subs:          subs,
		entitlements:  ents,
		vip:           vip,
		overview:      overview,
		wallet:        wallet,
		referral:      referral,
		sessionSecret: sessionSecret,
	}
}

// WithPaymentProviders sets payment providers for checkout resolution.
func (h *InternalHandler) WithPaymentProviders(epay *payment.EpayProvider, stripe *payment.StripeProvider, creem *payment.CreemProvider) *InternalHandler {
	h.epay = epay
	h.stripe = stripe
	h.creem = creem
	return h
}

// GetAccountByZitadelSub looks up an account by Zitadel OIDC sub.
// GET /internal/v1/accounts/by-zitadel-sub/:sub
func (h *InternalHandler) GetAccountByZitadelSub(c *gin.Context) {
	sub := c.Param("sub")
	a, err := h.accounts.GetByZitadelSub(c.Request.Context(), sub)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "lookup failed"})
		return
	}
	if a == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
		return
	}
	c.JSON(http.StatusOK, a)
}

// UpsertAccount creates or updates an account from a Zitadel webhook payload.
// Supports optional referrer_aff_code to link the account to a referrer on first creation.
// POST /internal/v1/accounts/upsert
func (h *InternalHandler) UpsertAccount(c *gin.Context) {
	var req struct {
		ZitadelSub      string `json:"zitadel_sub"       binding:"required"`
		Email           string `json:"email"             binding:"required"`
		DisplayName     string `json:"display_name"`
		AvatarURL       string `json:"avatar_url"`
		ReferrerAffCode string `json:"referrer_aff_code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx := c.Request.Context()
	a, err := h.accounts.UpsertByZitadelSub(ctx, req.ZitadelSub, req.Email, req.DisplayName, req.AvatarURL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Link referrer on first account creation (aff_code lookup).
	if req.ReferrerAffCode != "" && a.ReferrerID == nil {
		referrer, rerr := h.accounts.GetByAffCode(ctx, req.ReferrerAffCode)
		if rerr == nil && referrer != nil && referrer.ID != a.ID {
			referrerID := referrer.ID
			a.ReferrerID = &referrerID
			if uerr := h.accounts.Update(ctx, a); uerr == nil {
				// Fire signup reward — non-critical, ignore error.
				_ = h.referral.OnSignup(ctx, a.ID, referrer.ID)
			}
		}
	}

	c.JSON(http.StatusOK, a)
}

// GetEntitlements returns entitlements for an account+product (Redis-cached).
// GET /internal/v1/accounts/:id/entitlements/:product_id
func (h *InternalHandler) GetEntitlements(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid account id"})
		return
	}
	productID := c.Param("product_id")
	em, err := h.entitlements.Get(c.Request.Context(), id, productID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get entitlements"})
		return
	}
	if em == nil {
		em = map[string]string{"plan_code": "free"}
	}
	c.JSON(http.StatusOK, em)
}

// GetSubscription returns the active subscription for an account+product.
// GET /internal/v1/accounts/:id/subscription/:product_id
func (h *InternalHandler) GetSubscription(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid account id"})
		return
	}
	productID := c.Param("product_id")
	sub, err := h.subs.GetActive(c.Request.Context(), id, productID)
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

// GetAccountByOAuth looks up an account by OAuth provider and provider_id.
// GET /internal/v1/accounts/by-oauth/:provider/:provider_id
func (h *InternalHandler) GetAccountByOAuth(c *gin.Context) {
	provider := c.Param("provider")
	providerID := c.Param("provider_id")
	a, err := h.accounts.GetByOAuthBinding(c.Request.Context(), provider, providerID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "lookup failed"})
		return
	}
	if a == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
		return
	}
	c.JSON(http.StatusOK, a)
}

// GetAccountOverview returns the aggregated overview for a given account ID.
// GET /internal/v1/accounts/:id/overview?product_id=<pid>
func (h *InternalHandler) GetAccountOverview(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid account id"})
		return
	}
	productID := c.Query("product_id")
	ov, err := h.overview.Get(c.Request.Context(), id, productID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get overview"})
		return
	}
	c.JSON(http.StatusOK, ov)
}

// ReportUsage receives LLM usage reports from lurus-api for VIP accumulation.
// POST /internal/v1/usage/report
func (h *InternalHandler) ReportUsage(c *gin.Context) {
	var req struct {
		AccountID int64   `json:"account_id" binding:"required"`
		AmountCNY float64 `json:"amount_cny" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	_ = h.vip.RecalculateFromWallet(c.Request.Context(), req.AccountID)
	c.JSON(http.StatusOK, gin.H{"accepted": true})
}

// DebitWallet deducts LB from an account wallet (e.g. AI quota overage).
// POST /internal/v1/accounts/:id/wallet/debit
func (h *InternalHandler) DebitWallet(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid account id"})
		return
	}
	var req struct {
		Amount      float64 `json:"amount"      binding:"required,gt=0"`
		Type        string  `json:"type"        binding:"required"`
		ProductID   string  `json:"product_id"`
		Description string  `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	tx, err := h.wallet.Debit(c.Request.Context(), id, req.Amount, req.Type, req.Description, "internal_debit", "", req.ProductID)
	if err != nil {
		// Insufficient balance returns a structured error
		c.JSON(http.StatusBadRequest, gin.H{"error": "insufficient_balance"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "balance_after": tx.BalanceAfter})
}

// GetAccountByEmail looks up an account by email address.
// GET /internal/v1/accounts/by-email/:email
func (h *InternalHandler) GetAccountByEmail(c *gin.Context) {
	email := c.Param("email")
	a, err := h.accounts.GetByEmail(c.Request.Context(), email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "lookup failed"})
		return
	}
	if a == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
		return
	}
	c.JSON(http.StatusOK, a)
}

// GetAccountByPhone looks up an account by phone number.
// GET /internal/v1/accounts/by-phone/:phone
func (h *InternalHandler) GetAccountByPhone(c *gin.Context) {
	phone := c.Param("phone")
	a, err := h.accounts.GetByPhone(c.Request.Context(), phone)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "lookup failed"})
		return
	}
	if a == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
		return
	}
	c.JSON(http.StatusOK, a)
}

// GetWalletBalance returns the wallet balance for an account (quick lookup).
// GET /internal/v1/accounts/:id/wallet/balance
func (h *InternalHandler) GetWalletBalance(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid account id"})
		return
	}
	w, err := h.wallet.GetBalance(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "balance lookup failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"balance": w.Balance, "frozen": w.Frozen})
}

// ValidateSession validates a lurus session token and returns the associated account.
// POST /internal/v1/accounts/validate-session
func (h *InternalHandler) ValidateSession(c *gin.Context) {
	var req struct {
		Token string `json:"token" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if h.sessionSecret == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "session validation not configured"})
		return
	}
	accountID, err := validateSessionTokenFn(req.Token, h.sessionSecret)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid session token"})
		return
	}
	a, err := h.accounts.GetByID(c.Request.Context(), accountID)
	if err != nil || a == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
		return
	}
	c.JSON(http.StatusOK, a)
}

// GetBillingSummary returns an aggregated billing overview for an account.
// GET /internal/v1/accounts/:id/billing-summary
func (h *InternalHandler) GetBillingSummary(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid account id"})
		return
	}
	summary, err := h.wallet.GetBillingSummary(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "billing summary lookup failed"})
		return
	}
	c.JSON(http.StatusOK, summary)
}

// CreditWallet adds LB to an account wallet (admin-only, e.g. marketplace author revenue).
// POST /admin/v1/accounts/:id/wallet/credit
func (h *InternalHandler) CreditWallet(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid account id"})
		return
	}
	var req struct {
		Amount      float64 `json:"amount"      binding:"required,gt=0"`
		Type        string  `json:"type"        binding:"required"`
		ProductID   string  `json:"product_id"`
		Description string  `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	tx, err := h.wallet.Credit(c.Request.Context(), id, req.Amount, req.Type, req.Description, "internal_credit", "", req.ProductID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "credit failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "balance_after": tx.BalanceAfter})
}

// CreateCheckout creates a checkout session for a cross-service topup.
// POST /internal/v1/checkout/create
func (h *InternalHandler) CreateCheckout(c *gin.Context) {
	var req struct {
		AccountID      int64   `json:"account_id"      binding:"required"`
		AmountCNY      float64 `json:"amount_cny"      binding:"required,gt=0"`
		PaymentMethod  string  `json:"payment_method"  binding:"required"`
		SourceService  string  `json:"source_service"  binding:"required"`
		IdempotencyKey string  `json:"idempotency_key"`
		ReturnURL      string  `json:"return_url"`
		TTLSeconds     int     `json:"ttl_seconds"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.AmountCNY < minTopupCNY {
		c.JSON(http.StatusBadRequest, gin.H{"error": "amount_cny must be at least 1.00"})
		return
	}
	if req.AmountCNY > maxTopupCNY {
		c.JSON(http.StatusBadRequest, gin.H{"error": "amount_cny exceeds maximum allowed per transaction"})
		return
	}
	if !validPaymentMethods[req.PaymentMethod] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported payment method"})
		return
	}

	ttl := 30 * time.Minute
	if req.TTLSeconds > 0 {
		ttl = time.Duration(req.TTLSeconds) * time.Second
	}

	order, err := h.wallet.CreateCheckoutSession(c.Request.Context(),
		req.AccountID, req.AmountCNY, req.PaymentMethod, req.SourceService, req.IdempotencyKey, ttl)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create checkout session"})
		return
	}

	// Resolve payment URL via provider.
	returnURL := req.ReturnURL
	if returnURL == "" {
		returnURL = "/topup/result"
	}
	payURL, externalID, err := h.resolveCheckout(c, order, returnURL)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if externalID != "" {
		order.ExternalID = externalID
	}
	order.PayURL = payURL
	_ = h.wallet.UpdatePaymentOrder(c.Request.Context(), order)

	c.JSON(http.StatusCreated, gin.H{
		"order_no": order.OrderNo,
		"pay_url":  payURL,
		"status":   order.Status,
		"expires_at": order.ExpiresAt,
	})
}

// GetCheckoutStatus returns the status of a checkout order.
// GET /internal/v1/checkout/:order_no/status
func (h *InternalHandler) GetCheckoutStatus(c *gin.Context) {
	orderNo := c.Param("order_no")
	order, err := h.wallet.GetCheckoutStatus(c.Request.Context(), orderNo)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"order_no":   order.OrderNo,
		"status":     order.Status,
		"amount_cny": order.AmountCNY,
		"pay_url":    order.PayURL,
		"paid_at":    order.PaidAt,
		"expires_at": order.ExpiresAt,
	})
}

// GetPaymentMethods returns the list of available payment methods.
// GET /internal/v1/payment-methods
func (h *InternalHandler) GetPaymentMethods(c *gin.Context) {
	methods := make([]gin.H, 0, 4)
	if h.epay != nil {
		methods = append(methods,
			gin.H{"id": "epay_alipay", "name": "支付宝", "provider": "epay", "type": "qr"},
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

// PreAuthorize creates a pre-authorization hold on a wallet.
// POST /internal/v1/accounts/:id/wallet/pre-authorize
func (h *InternalHandler) PreAuthorize(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid account id"})
		return
	}
	var req struct {
		Amount      float64 `json:"amount"       binding:"required,gt=0"`
		ProductID   string  `json:"product_id"   binding:"required"`
		ReferenceID string  `json:"reference_id"`
		Description string  `json:"description"`
		TTLSeconds  int     `json:"ttl_seconds"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ttl := 10 * time.Minute
	if req.TTLSeconds > 0 {
		ttl = time.Duration(req.TTLSeconds) * time.Second
	}

	pa, err := h.wallet.PreAuthorize(c.Request.Context(), id, req.Amount, req.ProductID, req.ReferenceID, req.Description, ttl)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"preauth_id": pa.ID,
		"amount":     pa.Amount,
		"expires_at": pa.ExpiresAt,
		"status":     pa.Status,
	})
}

// SettlePreAuth settles a pre-authorization with the actual charge amount.
// POST /internal/v1/wallet/pre-auth/:id/settle
func (h *InternalHandler) SettlePreAuth(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid preauth id"})
		return
	}
	var req struct {
		ActualAmount float64 `json:"actual_amount" binding:"required,gte=0"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	pa, err := h.wallet.SettlePreAuth(c.Request.Context(), id, req.ActualAmount)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"preauth_id":    pa.ID,
		"status":        pa.Status,
		"held_amount":   pa.Amount,
		"actual_amount": pa.ActualAmount,
		"settled_at":    pa.SettledAt,
	})
}

// ReleasePreAuth releases a pre-authorization, unfreezing the hold.
// POST /internal/v1/wallet/pre-auth/:id/release
func (h *InternalHandler) ReleasePreAuth(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid preauth id"})
		return
	}

	pa, err := h.wallet.ReleasePreAuth(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"preauth_id": pa.ID,
		"status":     pa.Status,
		"amount":     pa.Amount,
	})
}

// resolveCheckout routes the order to the correct payment provider.
func (h *InternalHandler) resolveCheckout(c *gin.Context, order *entity.PaymentOrder, returnURL string) (payURL, externalID string, err error) {
	switch order.PaymentMethod {
	case "epay_alipay", "epay_wxpay", "epay_wechat":
		if h.epay == nil {
			return "", "", &providerError{name: "epay"}
		}
		return h.epay.CreateCheckout(c.Request.Context(), order, returnURL)
	case "stripe":
		if h.stripe == nil {
			return "", "", &providerError{name: "stripe"}
		}
		return h.stripe.CreateCheckout(c.Request.Context(), order, returnURL)
	case "creem":
		if h.creem == nil {
			return "", "", &providerError{name: "creem"}
		}
		return h.creem.CreateCheckout(c.Request.Context(), order, returnURL)
	default:
		return "", "", &providerError{name: order.PaymentMethod}
	}
}
