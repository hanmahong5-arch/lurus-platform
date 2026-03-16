package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/hanmahong5-arch/lurus-platform/internal/app"
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

// CreditWallet adds LB to an account wallet (e.g. marketplace author revenue).
// POST /internal/v1/accounts/:id/wallet/credit
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
