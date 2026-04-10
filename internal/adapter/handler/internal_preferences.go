package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/hanmahong5-arch/lurus-platform/internal/adapter/repo"
)

// SyncPreferences upserts user preferences for a given namespace.
// POST /internal/v1/preferences/sync
// Body: { "account_id": 123, "namespace": "creator", "data": { ... } }
func (h *InternalHandler) SyncPreferences(c *gin.Context) {
	if !requireScope(c, "preference:write") {
		return
	}
	var req struct {
		AccountID int64           `json:"account_id" binding:"required"`
		Namespace string          `json:"namespace"  binding:"required"`
		Data      json.RawMessage `json:"data"       binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		handleBindError(c, err)
		return
	}

	ctx := c.Request.Context()
	pref, err := h.preferences.Upsert(ctx, req.AccountID, req.Namespace, req.Data)
	if err != nil {
		respondInternalError(c, "SyncPreferences", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    pref,
	})
}

// GetPreferences returns user preferences for a given namespace.
// GET /internal/v1/preferences/:account_id?namespace=creator
func (h *InternalHandler) GetPreferences(c *gin.Context) {
	if !requireScope(c, "preference:read") {
		return
	}
	accountID, err := strconv.ParseInt(c.Param("account_id"), 10, 64)
	if err != nil {
		respondBadRequest(c, "invalid account_id")
		return
	}
	namespace := c.DefaultQuery("namespace", "creator")

	ctx := c.Request.Context()
	pref, err := h.preferences.Get(ctx, accountID, namespace)
	if err != nil {
		respondInternalError(c, "GetPreferences", err)
		return
	}
	if pref == nil {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data":    nil,
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    pref,
	})
}

// WithPreferenceRepo sets the preference repository.
func (h *InternalHandler) WithPreferenceRepo(r *repo.PreferenceRepo) *InternalHandler {
	h.preferences = r
	return h
}
