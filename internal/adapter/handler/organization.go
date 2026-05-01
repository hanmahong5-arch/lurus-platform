package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/hanmahong5-arch/lurus-platform/internal/app"
)

// OrganizationHandler handles organization, membership, API key, and org wallet endpoints.
type OrganizationHandler struct {
	svc *app.OrganizationService
}

func NewOrganizationHandler(svc *app.OrganizationService) *OrganizationHandler {
	return &OrganizationHandler{svc: svc}
}

// Create registers a new organization owned by the authenticated user.
// POST /api/v1/organizations
func (h *OrganizationHandler) Create(c *gin.Context) {
	accountID, ok := requireAccountID(c)
	if !ok {
		return
	}
	var req struct {
		Name string `json:"name" binding:"required"`
		Slug string `json:"slug" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		handleBindError(c, err)
		return
	}
	org, err := h.svc.Create(c.Request.Context(), req.Name, req.Slug, accountID)
	if err != nil {
		respondBadRequest(c, "Invalid request")
		return
	}
	c.JSON(http.StatusCreated, org)
}

// ListMine returns all organizations the authenticated user belongs to.
// GET /api/v1/organizations
func (h *OrganizationHandler) ListMine(c *gin.Context) {
	accountID, ok := requireAccountID(c)
	if !ok {
		return
	}
	orgs, err := h.svc.ListMine(c.Request.Context(), accountID)
	if err != nil {
		respondInternalError(c, "org.list_mine", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": orgs})
}

// Get returns a single organization. The caller must be a member.
// GET /api/v1/organizations/:id
func (h *OrganizationHandler) Get(c *gin.Context) {
	accountID, ok := requireAccountID(c)
	if !ok {
		return
	}
	id, ok := parsePathInt64(c, "id", "Organization ID")
	if !ok {
		return
	}
	org, err := h.svc.Get(c.Request.Context(), id, accountID)
	if err != nil {
		respondError(c, http.StatusForbidden, ErrCodeForbidden, "Permission denied")
		return
	}
	if org == nil {
		respondNotFound(c, "Organization")
		return
	}
	c.JSON(http.StatusOK, org)
}

// AddMember adds a user to the organization. Caller must be owner or admin.
// POST /api/v1/organizations/:id/members
func (h *OrganizationHandler) AddMember(c *gin.Context) {
	accountID, ok := requireAccountID(c)
	if !ok {
		return
	}
	orgID, ok := parsePathInt64(c, "id", "Organization ID")
	if !ok {
		return
	}
	var req struct {
		AccountID int64  `json:"account_id" binding:"required"`
		Role      string `json:"role"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		handleBindError(c, err)
		return
	}
	role := req.Role
	if role == "" {
		role = "member"
	}
	if err := h.svc.AddMember(c.Request.Context(), orgID, accountID, req.AccountID, role); err != nil {
		respondError(c, http.StatusForbidden, ErrCodeForbidden, "Permission denied")
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// RemoveMember removes a user from the organization. Caller must be owner or admin.
// DELETE /api/v1/organizations/:id/members/:uid
func (h *OrganizationHandler) RemoveMember(c *gin.Context) {
	accountID, ok := requireAccountID(c)
	if !ok {
		return
	}
	orgID, ok := parsePathInt64(c, "id", "Organization ID")
	if !ok {
		return
	}
	targetID, ok := parsePathInt64(c, "uid", "Account ID")
	if !ok {
		return
	}
	if err := h.svc.RemoveMember(c.Request.Context(), orgID, accountID, targetID); err != nil {
		respondBadRequest(c, "Invalid request")
		return
	}
	c.Status(http.StatusNoContent)
}

// ListAPIKeys returns API keys for an organization (key hashes are never exposed).
// GET /api/v1/organizations/:id/api-keys
func (h *OrganizationHandler) ListAPIKeys(c *gin.Context) {
	orgID, ok := parsePathInt64(c, "id", "Organization ID")
	if !ok {
		return
	}
	keys, err := h.svc.ListAPIKeys(c.Request.Context(), orgID)
	if err != nil {
		respondInternalError(c, "org.list_api_keys", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": keys})
}

// CreateAPIKey generates a new org API key. The raw key is returned only once.
// POST /api/v1/organizations/:id/api-keys
func (h *OrganizationHandler) CreateAPIKey(c *gin.Context) {
	accountID, ok := requireAccountID(c)
	if !ok {
		return
	}
	orgID, ok := parsePathInt64(c, "id", "Organization ID")
	if !ok {
		return
	}
	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		handleBindError(c, err)
		return
	}
	rawKey, key, err := h.svc.CreateAPIKey(c.Request.Context(), orgID, accountID, req.Name)
	if err != nil {
		respondBadRequest(c, "Invalid request")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"raw_key": rawKey, "key": key})
}

// RevokeAPIKey revokes an org API key. Caller must be owner or admin.
// DELETE /api/v1/organizations/:id/api-keys/:kid
func (h *OrganizationHandler) RevokeAPIKey(c *gin.Context) {
	accountID, ok := requireAccountID(c)
	if !ok {
		return
	}
	orgID, ok := parsePathInt64(c, "id", "Organization ID")
	if !ok {
		return
	}
	keyID, ok := parsePathInt64(c, "kid", "API key ID")
	if !ok {
		return
	}
	if err := h.svc.RevokeAPIKey(c.Request.Context(), orgID, accountID, keyID); err != nil {
		respondBadRequest(c, "Invalid request")
		return
	}
	c.Status(http.StatusNoContent)
}

// GetWallet returns the organization's shared token wallet.
// GET /api/v1/organizations/:id/wallet
func (h *OrganizationHandler) GetWallet(c *gin.Context) {
	orgID, ok := parsePathInt64(c, "id", "Organization ID")
	if !ok {
		return
	}
	wallet, err := h.svc.GetWallet(c.Request.Context(), orgID)
	if err != nil {
		respondInternalError(c, "org.get_wallet", err)
		return
	}
	c.JSON(http.StatusOK, wallet)
}

// ResolveAPIKey resolves a raw org API key to the owning organization.
// Called by internal services (e.g. lurus-api) to authenticate org API key requests.
// POST /internal/v1/orgs/resolve-api-key
func (h *OrganizationHandler) ResolveAPIKey(c *gin.Context) {
	var req struct {
		RawKey string `json:"raw_key" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		handleBindError(c, err)
		return
	}
	org, err := h.svc.ResolveAPIKey(c.Request.Context(), req.RawKey)
	if err != nil {
		respondError(c, http.StatusUnauthorized, ErrCodeUnauthorized,
			"Invalid or revoked API key")
		return
	}
	c.JSON(http.StatusOK, org)
}

// AdminList returns a paginated list of all organizations.
// GET /admin/v1/organizations
func (h *OrganizationHandler) AdminList(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if limit < 1 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	orgs, err := h.svc.ListAll(c.Request.Context(), limit, offset)
	if err != nil {
		respondInternalError(c, "org.admin_list", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": orgs})
}

// AdminUpdateStatus updates an organization's status (active | suspended).
// PATCH /admin/v1/organizations/:id
func (h *OrganizationHandler) AdminUpdateStatus(c *gin.Context) {
	id, ok := parsePathInt64(c, "id", "Organization ID")
	if !ok {
		return
	}
	var req struct {
		Status string `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		handleBindError(c, err)
		return
	}
	if req.Status != "active" && req.Status != "suspended" {
		respondError(c, http.StatusBadRequest, ErrCodeInvalidParameter,
			"status must be active or suspended")
		return
	}
	if err := h.svc.UpdateStatus(c.Request.Context(), id, req.Status); err != nil {
		respondBadRequest(c, "Invalid request")
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
