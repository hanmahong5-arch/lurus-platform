package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/hanmahong5-arch/lurus-platform/modules/notification/internal/app"
)

// DeviceHandler exposes device token management APIs for FCM push notifications.
type DeviceHandler struct {
	svc *app.DeviceService
}

// NewDeviceHandler creates a DeviceHandler.
func NewDeviceHandler(svc *app.DeviceService) *DeviceHandler {
	return &DeviceHandler{svc: svc}
}

// registerDeviceRequest is the JSON body for POST /api/v1/notifications/devices.
type registerDeviceRequest struct {
	Platform string `json:"platform" binding:"required"` // "ios" or "android"
	Token    string `json:"token" binding:"required"`
}

// Register registers a new device token for push notifications.
// POST /api/v1/notifications/devices
func (h *DeviceHandler) Register(c *gin.Context) {
	accountID := c.GetInt64("account_id")
	if accountID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing account_id"})
		return
	}

	var req registerDeviceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	if req.Platform != "ios" && req.Platform != "android" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "platform must be 'ios' or 'android'"})
		return
	}

	if err := h.svc.RegisterToken(c.Request.Context(), accountID, req.Platform, req.Token); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to register device"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Unregister removes a device token.
// DELETE /api/v1/notifications/devices/:token
func (h *DeviceHandler) Unregister(c *gin.Context) {
	accountID := c.GetInt64("account_id")
	if accountID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing account_id"})
		return
	}

	token := c.Param("token")
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "token is required"})
		return
	}

	if err := h.svc.UnregisterToken(c.Request.Context(), accountID, token); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to unregister device"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}
