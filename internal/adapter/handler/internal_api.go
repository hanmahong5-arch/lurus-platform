package handler

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hanmahong5-arch/lurus-platform/internal/adapter/payment"
	"github.com/hanmahong5-arch/lurus-platform/internal/adapter/repo"
	"github.com/hanmahong5-arch/lurus-platform/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/auth"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/lurusapi"
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
	preferences   *repo.PreferenceRepo
	referral      *app.ReferralService
	plans         *app.ProductService
	sessionSecret string
	payments      *payment.Registry
	lurusAPI      *lurusapi.Client
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

// WithProductService sets the product/plan service for subscription checkout.
func (h *InternalHandler) WithProductService(ps *app.ProductService) *InternalHandler {
	h.plans = ps
	return h
}

// WithLurusAPI sets the lurus-api client for currency exchange.
func (h *InternalHandler) WithLurusAPI(c *lurusapi.Client) *InternalHandler {
	h.lurusAPI = c
	return h
}

// WithPayments sets the payment provider registry for checkout resolution.
func (h *InternalHandler) WithPayments(r *payment.Registry) *InternalHandler {
	h.payments = r
	return h
}

// GetAccountByZitadelSub looks up an account by Zitadel OIDC sub.
// GET /internal/v1/accounts/by-zitadel-sub/:sub
func (h *InternalHandler) GetAccountByZitadelSub(c *gin.Context) {
	if !requireScope(c, "account:read") {
		return
	}
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

// GetAccountByID looks up an account by its internal numeric ID.
// GET /internal/v1/accounts/:id
func (h *InternalHandler) GetAccountByID(c *gin.Context) {
	if !requireScope(c, "account:read") {
		return
	}
	id, ok := parsePathInt64(c, "id", "Account ID")
	if !ok {
		return
	}
	a, err := h.accounts.GetByID(c.Request.Context(), id)
	if err != nil {
		respondInternalError(c, "internal.get_account_by_id", err)
		return
	}
	if a == nil {
		respondNotFound(c, "Account")
		return
	}
	c.JSON(http.StatusOK, a)
}

// UpsertAccount creates or updates an account from a Zitadel webhook payload.
// Supports optional referrer_aff_code to link the account to a referrer on first creation.
// POST /internal/v1/accounts/upsert
func (h *InternalHandler) UpsertAccount(c *gin.Context) {
	if !requireScope(c, "account:write") {
		return
	}
	var req struct {
		ZitadelSub      string `json:"zitadel_sub"       binding:"required"`
		Email           string `json:"email"             binding:"required"`
		DisplayName     string `json:"display_name"`
		AvatarURL       string `json:"avatar_url"`
		ReferrerAffCode string `json:"referrer_aff_code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		handleBindError(c, err)
		return
	}
	ctx := c.Request.Context()
	a, err := h.accounts.UpsertByZitadelSub(ctx, req.ZitadelSub, req.Email, req.DisplayName, req.AvatarURL)
	if err != nil {
		respondInternalError(c, "handler", err)
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
	if !requireScope(c, "entitlement") {
		return
	}
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
	if !requireScope(c, "entitlement") {
		return
	}
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
	if !requireScope(c, "account:read") {
		return
	}
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
	if !requireScope(c, "account:read") {
		return
	}
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
	if !requireScope(c, "wallet:read") {
		return
	}
	var req struct {
		AccountID int64   `json:"account_id" binding:"required"`
		AmountCNY float64 `json:"amount_cny" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		handleBindError(c, err)
		return
	}
	_ = h.vip.RecalculateFromWallet(c.Request.Context(), req.AccountID)
	c.JSON(http.StatusOK, gin.H{"accepted": true})
}

// DebitWallet deducts LB from an account wallet (e.g. AI quota overage).
// POST /internal/v1/accounts/:id/wallet/debit
func (h *InternalHandler) DebitWallet(c *gin.Context) {
	if !requireScope(c, "wallet:debit") {
		return
	}
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
		handleBindError(c, err)
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
	if !requireScope(c, "account:read") {
		return
	}
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
	if !requireScope(c, "account:read") {
		return
	}
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
	if !requireScope(c, "wallet:read") {
		return
	}
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
	if w == nil {
		c.JSON(http.StatusOK, gin.H{"balance": 0.0, "frozen": 0.0})
		return
	}
	c.JSON(http.StatusOK, gin.H{"balance": w.Balance, "frozen": w.Frozen})
}

// ValidateSession validates a lurus session token and returns the associated account.
// POST /internal/v1/accounts/validate-session
func (h *InternalHandler) ValidateSession(c *gin.Context) {
	if !requireScope(c, "account:read") {
		return
	}
	var req struct {
		Token string `json:"token" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		handleBindError(c, err)
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
	if !requireScope(c, "wallet:read") {
		return
	}
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
		handleBindError(c, err)
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
	if !requireScope(c, "checkout") {
		return
	}
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
		handleBindError(c, err)
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
	if h.payments == nil || !h.payments.HasMethod(req.PaymentMethod) {
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
	payURL, externalID, err := h.payments.Checkout(c.Request.Context(), order, returnURL)
	if err != nil {
		respondBadRequest(c, "Invalid request")
		return
	}

	if externalID != "" {
		order.ExternalID = externalID
	}
	order.PayURL = payURL
	_ = h.wallet.UpdatePaymentOrder(c.Request.Context(), order)

	c.JSON(http.StatusCreated, gin.H{
		"order_no":   order.OrderNo,
		"pay_url":    payURL,
		"status":     order.Status,
		"expires_at": order.ExpiresAt,
	})
}

// GetCheckoutStatus returns the status of a checkout order.
// GET /internal/v1/checkout/:order_no/status
func (h *InternalHandler) GetCheckoutStatus(c *gin.Context) {
	if !requireScope(c, "checkout") {
		return
	}
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
	if !requireScope(c, "checkout") {
		return
	}
	var methods []payment.MethodInfo
	if h.payments != nil {
		methods = h.payments.ListMethods()
	}
	c.JSON(http.StatusOK, gin.H{"payment_methods": methods})
}

// PreAuthorize creates a pre-authorization hold on a wallet.
// POST /internal/v1/accounts/:id/wallet/pre-authorize
func (h *InternalHandler) PreAuthorize(c *gin.Context) {
	if !requireScope(c, "wallet:debit") {
		return
	}
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
		handleBindError(c, err)
		return
	}

	ttl := 10 * time.Minute
	if req.TTLSeconds > 0 {
		ttl = time.Duration(req.TTLSeconds) * time.Second
	}

	pa, err := h.wallet.PreAuthorize(c.Request.Context(), id, req.Amount, req.ProductID, req.ReferenceID, req.Description, ttl)
	if err != nil {
		respondBadRequest(c, "Invalid request")
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
	if !requireScope(c, "wallet:debit") {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid preauth id"})
		return
	}
	var req struct {
		ActualAmount float64 `json:"actual_amount" binding:"required,gte=0"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		handleBindError(c, err)
		return
	}

	pa, err := h.wallet.SettlePreAuth(c.Request.Context(), id, req.ActualAmount)
	if err != nil {
		respondBadRequest(c, "Invalid request")
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
	if !requireScope(c, "wallet:debit") {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid preauth id"})
		return
	}

	pa, err := h.wallet.ReleasePreAuth(c.Request.Context(), id)
	if err != nil {
		respondBadRequest(c, "Invalid request")
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"preauth_id": pa.ID,
		"status":     pa.Status,
		"amount":     pa.Amount,
	})
}

// ExchangeLucToLut converts platform credits (LuCoin/LUC) to API credits (Lute/LUT).
// Two-phase operation: (1) debit wallet, (2) call lurus-api to credit Lute.
// If phase 2 fails, phase 1 is rolled back (wallet credit).
//
// POST /internal/v1/accounts/:id/currency/exchange
func (h *InternalHandler) ExchangeLucToLut(c *gin.Context) {
	if !requireScope(c, "wallet:debit") {
		return
	}
	if h.lurusAPI == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "currency exchange not configured"})
		return
	}

	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid account id"})
		return
	}

	var req struct {
		Amount         float64 `json:"amount"          binding:"required,gt=0"` // LUC amount
		LurusUserID    int     `json:"lurus_user_id"   binding:"required"`      // User ID in lurus-api
		IdempotencyKey string  `json:"idempotency_key" binding:"required"`      // Dedup key
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		handleBindError(c, err)
		return
	}

	if req.Amount > 100000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "amount exceeds maximum (100,000 LUC)"})
		return
	}

	ctx := c.Request.Context()

	// Get VIP level for bonus calculation
	vipLevel := 0
	vipInfo, err := h.vip.Get(ctx, accountID)
	if err == nil && vipInfo != nil {
		vipLevel = int(vipInfo.Level)
	}

	// Phase 1: Debit wallet (LUC)
	refID := "cex:" + req.IdempotencyKey
	debitTx, err := h.wallet.Debit(ctx, accountID, req.Amount,
		entity.TxTypeCurrencyExchange,
		fmt.Sprintf("Exchange %.4f LUC to Lute (user=%d)", req.Amount, req.LurusUserID),
		"currency_exchange", refID, "lurus_api")
	if err != nil {
		slog.WarnContext(ctx, "currency exchange: wallet debit failed",
			"account_id", accountID, "amount", req.Amount, "error", err)
		// P1-10: standard envelope is {error: <snake_code>, message: <text>}.
		// Removed redundant "error_code" UPPERCASE field — clients keying off
		// "error" already have a stable machine code.
		respondError(c, http.StatusBadRequest, ErrCodeInsufficientBalance,
			"Not enough LuCoin balance for this exchange")
		return
	}

	// Phase 2: Call lurus-api to credit Lute
	exchangeResp, err := h.lurusAPI.ExchangeLucToLut(ctx, &lurusapi.ExchangeRequest{
		UserID:          req.LurusUserID,
		LucAmount:       req.Amount,
		VIPLevel:        vipLevel,
		ReferenceID:     fmt.Sprintf("platform-tx-%d", debitTx.ID),
		PlatformOrderNo: refID,
		Note:            fmt.Sprintf("Platform account %d exchange", accountID),
	})
	if err != nil {
		// Rollback: credit back the debited amount
		slog.ErrorContext(ctx, "currency exchange: lurus-api call failed, rolling back wallet debit",
			"account_id", accountID, "debit_tx_id", debitTx.ID, "error", err)
		// P2-6: original err is already in slog above; keep TX description
		// generic so the user-visible wallet ledger doesn't leak upstream
		// error text.
		_, rollbackErr := h.wallet.Credit(ctx, accountID, req.Amount,
			entity.TxTypeRefund,
			fmt.Sprintf("Rollback currency exchange (debit_tx=%d): upstream API error", debitTx.ID),
			"currency_exchange_rollback", refID, "lurus_api")
		if rollbackErr != nil {
			slog.ErrorContext(ctx, "currency exchange: CRITICAL rollback failed",
				"account_id", accountID, "debit_tx_id", debitTx.ID, "rollback_error", rollbackErr)
		}
		// P1-10: unified envelope. Code stays "upstream_failed" (machine-readable);
		// the human message tells the user the wallet was refunded.
		respondError(c, http.StatusBadGateway, ErrCodeUpstreamFailed,
			"Failed to credit Lute to API account. Wallet has been refunded.")
		return
	}

	slog.InfoContext(ctx, "currency exchange completed",
		"account_id", accountID, "luc_amount", req.Amount,
		"lut_amount", exchangeResp.LutAmount, "vip_level", vipLevel)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"exchange_id":    exchangeResp.ExchangeID,
			"luc_amount":     req.Amount,
			"lut_amount":     exchangeResp.LutAmount,
			"exchange_rate":  exchangeResp.ExchangeRate,
			"vip_level":      vipLevel,
			"vip_bonus":      exchangeResp.VIPBonus,
			"wallet_balance": debitTx.BalanceAfter,
			"lut_balance":    exchangeResp.UserBalance,
			"lut_balance_cn": exchangeResp.BalanceCN,
		},
	})
}

// GetCurrencyInfo returns the three-tier currency system configuration from lurus-api.
// GET /internal/v1/currency/info
func (h *InternalHandler) GetCurrencyInfo(c *gin.Context) {
	if !requireScope(c, "wallet:read") {
		return
	}
	if h.lurusAPI == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "currency service not configured"})
		return
	}

	info, err := h.lurusAPI.GetCurrencyInfo(c.Request.Context())
	if err != nil {
		respondInternalError(c, "currency.info", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": info})
}

// InternalSubscriptionCheckout initiates a subscription purchase on behalf of a user.
// Services like Lucrum and Creator call this instead of forwarding user JWTs.
// POST /internal/v1/subscriptions/checkout
func (h *InternalHandler) InternalSubscriptionCheckout(c *gin.Context) {
	if !requireScope(c, "checkout") {
		return
	}
	if h.plans == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "product service not configured"})
		return
	}

	var req struct {
		AccountID     int64  `json:"account_id"      binding:"required"`
		ProductID     string `json:"product_id"       binding:"required"`
		PlanCode      string `json:"plan_code"        binding:"required"`
		BillingCycle  string `json:"billing_cycle"    binding:"required"`
		PaymentMethod string `json:"payment_method"   binding:"required"`
		ReturnURL     string `json:"return_url"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		handleBindError(c, err)
		return
	}

	ctx := c.Request.Context()

	// Resolve plan_code + billing_cycle to a concrete plan.
	plans, err := h.plans.ListPlans(ctx, req.ProductID)
	if err != nil {
		respondInternalError(c, "internal.subscription_checkout.list_plans", err)
		return
	}
	var matched *entity.ProductPlan
	for i := range plans {
		if plans[i].Code == req.PlanCode && plans[i].BillingCycle == req.BillingCycle {
			matched = &plans[i]
			break
		}
	}
	if matched == nil {
		respondNotFound(c, "Plan matching code="+req.PlanCode+" cycle="+req.BillingCycle)
		return
	}

	returnURL := req.ReturnURL
	if returnURL == "" {
		returnURL = "/subscriptions"
	}

	// Wallet payment: debit balance and activate immediately.
	if req.PaymentMethod == "wallet" {
		if matched.PriceCNY > 0 {
			if _, err := h.wallet.Debit(ctx, req.AccountID, matched.PriceCNY,
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
		sub, err := h.subs.Activate(ctx, req.AccountID, req.ProductID, matched.ID, req.PaymentMethod, "")
		if err != nil {
			// Compensate: refund already-debited amount if activation fails.
			if matched.PriceCNY > 0 {
				_, creditErr := h.wallet.Credit(ctx, req.AccountID, matched.PriceCNY,
					"subscription_payment_refund",
					"Subscription activation failed, auto-refund",
					"subscription", "", req.ProductID)
				if creditErr != nil {
					slog.Error("CRITICAL: internal subscription checkout compensation failed",
						"account_id", req.AccountID, "amount", matched.PriceCNY,
						"activate_err", err, "credit_err", creditErr)
				}
			}
			respondInternalError(c, "internal.subscription_checkout.activate", err)
			return
		}
		c.JSON(http.StatusCreated, gin.H{"subscription": sub})
		return
	}

	// External payment: create order and return checkout URL.
	order := &entity.PaymentOrder{
		AccountID:     req.AccountID,
		OrderType:     "subscription",
		ProductID:     req.ProductID,
		PlanID:        &matched.ID,
		AmountCNY:     matched.PriceCNY,
		Currency:      "CNY",
		PaymentMethod: req.PaymentMethod,
		Status:        entity.OrderStatusPending,
	}
	if err := h.wallet.CreateSubscriptionOrder(ctx, order); err != nil {
		respondInternalError(c, "internal.subscription_checkout.create_order", err)
		return
	}

	payURL, externalID, err := h.payments.Checkout(c.Request.Context(), order, returnURL)
	if err != nil {
		var pe *payment.ProviderNotAvailableError
		if errors.As(err, &pe) {
			respondError(c, http.StatusBadRequest, ErrCodeInvalidParameter, pe.Error())
			return
		}
		respondInternalError(c, "internal.subscription_checkout.resolve", err)
		return
	}
	if externalID != "" {
		order.ExternalID = externalID
		if err := h.wallet.UpdatePaymentOrder(ctx, order); err != nil {
			slog.Warn("internal.subscription_checkout: failed to save external_id (non-fatal)",
				"order_no", order.OrderNo, "external_id", externalID, "err", err)
		}
	}

	c.JSON(http.StatusCreated, gin.H{
		"order_no": order.OrderNo,
		"pay_url":  payURL,
	})
}

// GetPaymentProviderStatus returns the circuit breaker state of all payment providers.
// GET /internal/v1/payment/providers
func (h *InternalHandler) GetPaymentProviderStatus(c *gin.Context) {
	if !requireScope(c, "checkout") {
		return
	}
	if h.payments == nil {
		c.JSON(http.StatusOK, gin.H{"providers": []any{}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"providers": h.payments.ProviderStatuses()})
}

// InternalListWalletTransactions returns wallet transaction history for an account.
// POST /internal/v1/accounts/:id/wallet/transactions
func (h *InternalHandler) InternalListWalletTransactions(c *gin.Context) {
	if !requireScope(c, "wallet:read") {
		return
	}
	id, ok := parsePathInt64(c, "id", "Account ID")
	if !ok {
		return
	}
	page, pageSize := parsePagination(c)
	list, total, err := h.wallet.ListTransactions(c.Request.Context(), id, page, pageSize)
	if err != nil {
		respondInternalError(c, "internal.wallet.list_transactions", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list, "total": total})
}
