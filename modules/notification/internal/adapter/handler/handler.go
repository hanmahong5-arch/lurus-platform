// Package handler provides HTTP handlers for the notification service.
package handler

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/hanmahong5-arch/lurus-platform/modules/notification/internal/adapter/sender"
	"github.com/hanmahong5-arch/lurus-platform/modules/notification/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/modules/notification/internal/domain/entity"
)

// NotificationHandler exposes notification APIs.
type NotificationHandler struct {
	svc        *app.NotificationService
	hub        *sender.Hub
	adminEmail string
}

// NewNotificationHandler creates a NotificationHandler.
func NewNotificationHandler(svc *app.NotificationService, hub *sender.Hub, adminEmail string) *NotificationHandler {
	return &NotificationHandler{svc: svc, hub: hub, adminEmail: adminEmail}
}

// List returns paginated notifications for the authenticated account.
// GET /api/v1/notifications?limit=20&offset=0
func (h *NotificationHandler) List(c *gin.Context) {
	accountID := c.GetInt64("account_id")
	if accountID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing account_id"})
		return
	}

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	items, total, err := h.svc.ListByAccount(c.Request.Context(), accountID, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list notifications"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"items": items,
		"total": total,
	})
}

// Unread returns the unread count.
// GET /api/v1/notifications/unread
func (h *NotificationHandler) Unread(c *gin.Context) {
	accountID := c.GetInt64("account_id")
	if accountID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing account_id"})
		return
	}

	count, err := h.svc.CountUnread(c.Request.Context(), accountID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to count unread"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"unread": count})
}

// MarkRead marks a single notification as read.
// POST /api/v1/notifications/:id/read
func (h *NotificationHandler) MarkRead(c *gin.Context) {
	accountID := c.GetInt64("account_id")
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid notification ID"})
		return
	}

	if err := h.svc.MarkRead(c.Request.Context(), id, accountID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to mark as read"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// MarkAllRead marks all unread notifications as read.
// POST /api/v1/notifications/read-all
func (h *NotificationHandler) MarkAllRead(c *gin.Context) {
	accountID := c.GetInt64("account_id")
	if accountID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing account_id"})
		return
	}

	affected, err := h.svc.MarkAllRead(c.Request.Context(), accountID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to mark all as read"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"affected": affected})
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // handled by auth middleware before reaching here
	},
}

// WebSocket upgrades to WebSocket for real-time notification push.
// GET /api/v1/notifications/ws
func (h *NotificationHandler) WebSocket(c *gin.Context) {
	accountID := c.GetInt64("account_id")
	if accountID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing account_id"})
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		slog.Error("websocket upgrade failed", "err", err)
		return
	}

	h.hub.Register(accountID, conn)
	defer func() {
		h.hub.Unregister(accountID, conn)
		conn.Close()
	}()

	// Send initial unread count
	unread, _ := h.svc.CountUnread(c.Request.Context(), accountID)
	h.hub.Broadcast(accountID, sender.WSMessage{
		Type:   "unread_count",
		Unread: unread,
	})

	// Keep the connection alive; read messages are ignored (write-only push).
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			break
		}
	}
}

// TemplateHandler exposes admin template management APIs.
type TemplateHandler struct {
	svc *app.TemplateService
}

// NewTemplateHandler creates a TemplateHandler.
func NewTemplateHandler(svc *app.TemplateService) *TemplateHandler {
	return &TemplateHandler{svc: svc}
}

// List returns all templates.
// GET /admin/v1/templates
func (h *TemplateHandler) List(c *gin.Context) {
	items, err := h.svc.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list templates"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// Upsert creates or updates a template.
// POST /admin/v1/templates
func (h *TemplateHandler) Upsert(c *gin.Context) {
	var t entity.Template
	if err := c.ShouldBindJSON(&t); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}
	if t.EventType == "" || t.Channel == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "event_type and channel are required"})
		return
	}
	if err := h.svc.Upsert(c.Request.Context(), &t); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to upsert template"})
		return
	}
	c.JSON(http.StatusOK, t)
}

// Delete removes a template by ID.
// DELETE /admin/v1/templates/:id
func (h *TemplateHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid template ID"})
		return
	}
	if err := h.svc.Delete(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete template"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
