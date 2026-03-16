package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/hanmahong5-arch/lurus-platform/modules/notification/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/modules/notification/internal/domain/entity"
)

// internalNotifyRequest is the JSON body for POST /internal/v1/notify.
type internalNotifyRequest struct {
	AccountID int64             `json:"account_id" binding:"required"`
	EventType string            `json:"event_type" binding:"required"`
	EventID   string            `json:"event_id"`
	Channels  []entity.Channel  `json:"channels" binding:"required"`
	Vars      map[string]string `json:"vars"`
	EmailAddr string            `json:"email_addr"`
}

// InternalNotify allows other services to send notifications via HTTP.
// POST /internal/v1/notify
func (h *NotificationHandler) InternalNotify(c *gin.Context) {
	var req internalNotifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	if err := h.svc.Send(c.Request.Context(), app.SendRequest{
		AccountID: req.AccountID,
		EventType: req.EventType,
		EventID:   req.EventID,
		Channels:  req.Channels,
		Vars:      req.Vars,
		EmailAddr: req.EmailAddr,
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to send notification"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}
