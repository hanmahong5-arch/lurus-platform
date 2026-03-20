package handler

import (
	"encoding/base64"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hanmahong5-arch/lurus-platform/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/auth"
)

// AdminConfigHandler serves /admin/v1/settings endpoints and the public QR code endpoint.
type AdminConfigHandler struct {
	cfg *app.AdminConfigService
}

// NewAdminConfigHandler creates the handler.
func NewAdminConfigHandler(cfg *app.AdminConfigService) *AdminConfigHandler {
	return &AdminConfigHandler{cfg: cfg}
}

// ListSettings returns all settings; secret values are masked.
// GET /admin/v1/settings
func (h *AdminConfigHandler) ListSettings(c *gin.Context) {
	settings, err := h.cfg.LoadAll(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load settings"})
		return
	}
	out := make([]map[string]any, 0, len(settings))
	for _, st := range settings {
		v := st.Value
		if st.IsSecret && v != "" {
			v = "••••••••"
		}
		out = append(out, map[string]any{
			"key":        st.Key,
			"value":      v,
			"is_secret":  st.IsSecret,
			"updated_by": st.UpdatedBy,
			"updated_at": st.UpdatedAt,
		})
	}
	c.JSON(http.StatusOK, gin.H{"settings": out})
}

// UpdateSettings batch-updates one or more config items.
// PUT /admin/v1/settings
// Body: {"settings": [{"key": "epay_key", "value": "secret123"}, ...]}
func (h *AdminConfigHandler) UpdateSettings(c *gin.Context) {
	var req struct {
		Settings []struct {
			Key   string `json:"key"   binding:"required"`
			Value string `json:"value"`
		} `json:"settings" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		handleBindError(c, err)
		return
	}

	claims := auth.GetClaims(c)
	updatedBy := "admin"
	if claims != nil && claims.Email != "" {
		updatedBy = claims.Email
	}

	for _, item := range req.Settings {
		if err := h.cfg.Set(c.Request.Context(), item.Key, item.Value, updatedBy); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "save failed for key: " + item.Key})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"updated": len(req.Settings)})
}

// UploadQRCode stores a QR code image (as base64) in admin settings.
// POST /admin/v1/settings/qrcode
// Body: {"type": "alipay"|"wechat"|"channel", "image_base64": "<base64>"}
func (h *AdminConfigHandler) UploadQRCode(c *gin.Context) {
	var req struct {
		Type        string `json:"type"         binding:"required"`
		ImageBase64 string `json:"image_base64" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		handleBindError(c, err)
		return
	}

	var key string
	switch req.Type {
	case "alipay":
		key = "qr_static_alipay"
	case "wechat":
		key = "qr_static_wechat"
	case "channel":
		key = "qr_channel_promo"
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "type must be alipay, wechat, or channel"})
		return
	}

	// Validate that the payload is valid base64.
	if _, err := base64.StdEncoding.DecodeString(req.ImageBase64); err != nil {
		if _, err2 := base64.RawStdEncoding.DecodeString(req.ImageBase64); err2 != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid base64 image data"})
			return
		}
	}

	claims := auth.GetClaims(c)
	updatedBy := "admin"
	if claims != nil && claims.Email != "" {
		updatedBy = claims.Email
	}

	if err := h.cfg.Set(c.Request.Context(), key, req.ImageBase64, updatedBy); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save QR code"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"key": key,
		"url": "/api/v1/public/qrcode/" + req.Type,
	})
}

// GetPublicQRCode serves the stored QR code image (no auth required).
// GET /api/v1/public/qrcode/:type
func (h *AdminConfigHandler) GetPublicQRCode(c *gin.Context) {
	t := c.Param("type")
	var key string
	switch t {
	case "alipay":
		key = "qr_static_alipay"
	case "wechat":
		key = "qr_static_wechat"
	case "channel":
		key = "qr_channel_promo"
	default:
		c.JSON(http.StatusNotFound, gin.H{"error": "unknown QR type"})
		return
	}

	val, err := h.cfg.Get(c.Request.Context(), key)
	if err != nil || val == "" {
		c.Status(http.StatusNoContent)
		return
	}

	// Decode base64 → binary image.
	imgData, err := base64.StdEncoding.DecodeString(val)
	if err != nil {
		imgData, err = base64.RawStdEncoding.DecodeString(val)
		if err != nil {
			c.Status(http.StatusNoContent)
			return
		}
	}

	// Detect image type from magic bytes.
	contentType := "image/png"
	if len(imgData) >= 2 && imgData[0] == 0xFF && imgData[1] == 0xD8 {
		contentType = "image/jpeg"
	}
	c.Data(http.StatusOK, contentType, imgData)
}
