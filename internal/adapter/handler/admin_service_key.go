package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hanmahong5-arch/lurus-platform/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// serviceKeyAdmin defines the contract for service key admin operations.
type serviceKeyAdmin interface {
	Create(ctx context.Context, key *entity.ServiceAPIKey) error
	ListAll(ctx context.Context) ([]entity.ServiceAPIKey, error)
	Revoke(ctx context.Context, id int64) error
}

// AdminServiceKeyHandler manages service API keys via admin endpoints.
type AdminServiceKeyHandler struct {
	repo serviceKeyAdmin
}

// NewAdminServiceKeyHandler creates the handler.
func NewAdminServiceKeyHandler(svc serviceKeyAdmin) *AdminServiceKeyHandler {
	return &AdminServiceKeyHandler{repo: svc}
}

// CreateServiceKey generates a new scoped API key for a service.
// POST /admin/v1/service-keys
//
// Request:  { "service_name": "forge", "description": "...", "scopes": ["account:read","entitlement"], "rate_limit_rpm": 500 }
// Response: { "id": 1, "key": "sk-forge-a1b2c3d4e5f6...", "key_prefix": "sk-forge", "service_name": "forge", "scopes": [...] }
//
// The raw key is returned ONCE. It cannot be retrieved after this call.
func (h *AdminServiceKeyHandler) CreateServiceKey(c *gin.Context) {
	var req struct {
		ServiceName  string   `json:"service_name"  binding:"required"`
		Description  string   `json:"description"`
		Scopes       []string `json:"scopes"        binding:"required,min=1"`
		RateLimitRPM int      `json:"rate_limit_rpm"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		handleBindError(c, err)
		return
	}

	// Validate scopes.
	validScopes := make(map[string]bool)
	for _, s := range entity.AllScopes() {
		validScopes[s] = true
	}
	for _, s := range req.Scopes {
		if !validScopes[s] {
			respondValidationError(c, "Invalid scope", map[string]string{
				"scopes": "Unknown scope: " + s + ". Valid: account:read, account:write, wallet:read, wallet:debit, wallet:credit, entitlement, checkout",
			})
			return
		}
	}

	if req.RateLimitRPM <= 0 {
		req.RateLimitRPM = 1000
	}

	// Generate random key: sk-<service>-<32 hex chars>
	rawBytes := make([]byte, 24)
	if _, err := rand.Read(rawBytes); err != nil {
		respondInternalError(c, "admin.create_service_key", err)
		return
	}
	rawKey := "sk-" + req.ServiceName + "-" + hex.EncodeToString(rawBytes)
	keyHash := app.HashKey(rawKey)
	keyPrefix := rawKey[:min(len(rawKey), 12)]

	key := &entity.ServiceAPIKey{
		KeyHash:      keyHash,
		KeyPrefix:    keyPrefix,
		ServiceName:  req.ServiceName,
		Description:  req.Description,
		Scopes:       entity.StringList(req.Scopes),
		RateLimitRPM: req.RateLimitRPM,
		Status:       entity.ServiceKeyActive,
	}

	if err := h.repo.Create(c.Request.Context(), key); err != nil {
		respondInternalError(c, "admin.create_service_key", err)
		return
	}

	// Return the raw key ONCE — it cannot be retrieved after this.
	c.JSON(http.StatusCreated, gin.H{
		"id":             key.ID,
		"key":            rawKey,
		"key_prefix":     keyPrefix,
		"service_name":   req.ServiceName,
		"scopes":         req.Scopes,
		"rate_limit_rpm": req.RateLimitRPM,
		"message":        "Save this key now. It cannot be retrieved again.",
	})
}

// ListServiceKeys returns all active service keys (without hashes).
// GET /admin/v1/service-keys
func (h *AdminServiceKeyHandler) ListServiceKeys(c *gin.Context) {
	keys, err := h.repo.ListAll(c.Request.Context())
	if err != nil {
		respondInternalError(c, "admin.list_service_keys", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"keys": keys})
}

// RevokeServiceKey permanently revokes a service key.
// DELETE /admin/v1/service-keys/:id
func (h *AdminServiceKeyHandler) RevokeServiceKey(c *gin.Context) {
	id, ok := parsePathInt64(c, "id", "Service key ID")
	if !ok {
		return
	}
	if err := h.repo.Revoke(c.Request.Context(), id); err != nil {
		respondInternalError(c, "admin.revoke_service_key", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"revoked": true, "message": "Key has been permanently revoked"})
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
