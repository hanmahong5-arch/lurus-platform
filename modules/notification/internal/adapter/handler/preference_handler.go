package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/hanmahong5-arch/lurus-platform/modules/notification/internal/app"
)

// PreferenceHandler exposes notification preference APIs.
type PreferenceHandler struct {
	svc *app.PreferenceService
}

// NewPreferenceHandler creates a PreferenceHandler.
func NewPreferenceHandler(svc *app.PreferenceService) *PreferenceHandler {
	return &PreferenceHandler{svc: svc}
}

// Get returns all channel preferences for the authenticated account.
// GET /api/v1/notifications/preferences
func (h *PreferenceHandler) Get(c *gin.Context) {
	accountID := c.GetInt64("account_id")
	if accountID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing account_id"})
		return
	}

	prefs, err := h.svc.GetByAccount(c.Request.Context(), accountID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get preferences"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"preferences": prefs})
}

// Update batch-updates channel preferences for the authenticated account.
// PUT /api/v1/notifications/preferences
//
// Request body: [{"channel":"email","enabled":false}, ...]
func (h *PreferenceHandler) Update(c *gin.Context) {
	accountID := c.GetInt64("account_id")
	if accountID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing account_id"})
		return
	}

	var updates []app.PreferenceUpdate
	if err := c.ShouldBindJSON(&updates); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}
	if len(updates) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "at least one preference update required"})
		return
	}

	if err := h.svc.BatchUpdate(c.Request.Context(), accountID, updates); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}
