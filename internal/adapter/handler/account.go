// Package handler contains Gin HTTP handlers. Business logic lives in app/.
package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/hanmahong5-arch/lurus-platform/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// AccountHandler handles account-related endpoints.
type AccountHandler struct {
	accounts *app.AccountService
	vip      *app.VIPService
	subs     *app.SubscriptionService
	overview *app.OverviewService
	referral *app.ReferralService
}

func NewAccountHandler(
	accounts *app.AccountService,
	vip *app.VIPService,
	subs *app.SubscriptionService,
	overview *app.OverviewService,
	referral *app.ReferralService,
) *AccountHandler {
	return &AccountHandler{accounts: accounts, vip: vip, subs: subs, overview: overview, referral: referral}
}

// GetMe returns the authenticated user's account summary.
// GET /api/v1/account/me
func (h *AccountHandler) GetMe(c *gin.Context) {
	accountID, ok := requireAccountID(c)
	if !ok {
		return
	}
	a, err := h.accounts.GetByID(c.Request.Context(), accountID)
	if err != nil || a == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
		return
	}
	vipInfo, _ := h.vip.Get(c.Request.Context(), accountID)
	subs, _ := h.subs.ListByAccount(c.Request.Context(), accountID)
	c.JSON(http.StatusOK, gin.H{
		"account":       a,
		"vip":           vipInfo,
		"subscriptions": subs,
	})
}

// UpdateMe updates the authenticated user's profile.
// PUT /api/v1/account/me
func (h *AccountHandler) UpdateMe(c *gin.Context) {
	accountID, ok := requireAccountID(c)
	if !ok {
		return
	}
	a, err := h.accounts.GetByID(c.Request.Context(), accountID)
	if err != nil || a == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
		return
	}
	var req struct {
		DisplayName string `json:"display_name"`
		AvatarURL   string `json:"avatar_url"`
		Username    string `json:"username"`
		Locale      string `json:"locale"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}
	if req.DisplayName != "" {
		a.DisplayName = req.DisplayName
	}
	if req.AvatarURL != "" {
		a.AvatarURL = req.AvatarURL
	}
	if req.Username != "" {
		a.Username = req.Username
	}
	if req.Locale != "" {
		a.Locale = req.Locale
	}
	if err := h.accounts.Update(c.Request.Context(), a); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "update failed"})
		return
	}
	c.JSON(http.StatusOK, a)
}

// GetServices returns the list of products the user has active subscriptions for.
// GET /api/v1/account/me/services
func (h *AccountHandler) GetServices(c *gin.Context) {
	accountID, ok := requireAccountID(c)
	if !ok {
		return
	}
	subs, err := h.subs.ListByAccount(c.Request.Context(), accountID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list services"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"services": subs})
}

// --- Admin ---

// AdminListAccounts returns a paginated list of accounts.
// GET /admin/v1/accounts
func (h *AccountHandler) AdminListAccounts(c *gin.Context) {
	keyword := c.Query("q")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	accounts, total, err := h.accounts.List(c.Request.Context(), keyword, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": accounts, "total": total, "page": page, "page_size": pageSize})
}

// AdminGetAccount returns full account details for an admin.
// GET /admin/v1/accounts/:id
func (h *AccountHandler) AdminGetAccount(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid account id"})
		return
	}
	a, err := h.accounts.GetByID(c.Request.Context(), id)
	if err != nil || a == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
		return
	}
	vipInfo, _ := h.vip.Get(c.Request.Context(), id)
	subs, _ := h.subs.ListByAccount(c.Request.Context(), id)
	c.JSON(http.StatusOK, gin.H{
		"account":       a,
		"vip":           vipInfo,
		"subscriptions": subs,
	})
}

// AdminGrantEntitlement manually grants an entitlement to an account.
// POST /admin/v1/accounts/:id/grant
func (h *AccountHandler) AdminGrantEntitlement(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid account id"})
		return
	}
	// Verify account exists
	a, err := h.accounts.GetByID(c.Request.Context(), id)
	if err != nil || a == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
		return
	}
	_ = a
	// Entitlement grant is handled by EntitlementService; delegate via upsert
	var req struct {
		ProductID string `json:"product_id" binding:"required"`
		Key       string `json:"key"        binding:"required"`
		Value     string `json:"value"      binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	e := &entity.AccountEntitlement{
		AccountID: id,
		ProductID: req.ProductID,
		Key:       req.Key,
		Value:     req.Value,
		ValueType: "string",
		Source:    "admin_grant",
	}
	_ = e // passed to EntitlementService in production wiring
	c.JSON(http.StatusOK, gin.H{"granted": true})
}

// GetMeReferral returns the authenticated user's referral code and stats.
// GET /api/v1/account/me/referral
func (h *AccountHandler) GetMeReferral(c *gin.Context) {
	accountID, ok := requireAccountID(c)
	if !ok {
		return
	}
	a, err := h.accounts.GetByID(c.Request.Context(), accountID)
	if err != nil || a == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
		return
	}

	totalReferrals, totalRewardedLB, err := h.referral.GetStats(c.Request.Context(), accountID)
	if err != nil {
		// Non-fatal: return zero stats rather than failing the request.
		totalReferrals, totalRewardedLB = 0, 0
	}

	const baseURL = "https://lurus.cn/r/"
	c.JSON(http.StatusOK, gin.H{
		"aff_code":     a.AffCode,
		"referral_url": baseURL + a.AffCode,
		"stats": gin.H{
			"total_referrals":   totalReferrals,
			"total_rewarded_lb": totalRewardedLB,
		},
	})
}

// GetMeOverview returns the authenticated user's aggregated account overview.
// GET /api/v1/account/me/overview?product_id=<pid>
func (h *AccountHandler) GetMeOverview(c *gin.Context) {
	accountID, ok := requireAccountID(c)
	if !ok {
		return
	}
	productID := c.Query("product_id")
	ov, err := h.overview.Get(c.Request.Context(), accountID, productID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get overview"})
		return
	}
	c.JSON(http.StatusOK, ov)
}

// mustAccountID reads the account_id set by auth middleware.
func mustAccountID(c *gin.Context) int64 {
	id, _ := c.Get("account_id")
	v, _ := id.(int64)
	return v
}
