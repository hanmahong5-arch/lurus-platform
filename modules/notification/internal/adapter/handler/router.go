package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Deps holds all handler dependencies for router construction.
type Deps struct {
	Notifications *NotificationHandler
	Templates     *TemplateHandler
	InternalKey   string
	WebhookSecret string // shared secret for alertmanager webhook (empty = no auth)
}

// BuildRouter creates the Gin engine with all routes.
func BuildRouter(deps Deps) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	// Health check
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Prometheus metrics
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// Alertmanager webhook (cluster-internal, optional shared secret)
	webhooks := r.Group("/webhooks")
	if deps.WebhookSecret != "" {
		webhooks.Use(webhookSecretAuth(deps.WebhookSecret))
	}
	{
		webhooks.POST("/alertmanager", deps.Notifications.AlertmanagerWebhook)
	}

	// Internal API (service-to-service, protected by bearer key)
	internal := r.Group("/internal/v1")
	internal.Use(internalKeyAuth(deps.InternalKey))
	{
		// Other services can send notifications via internal API
		internal.POST("/notify", deps.Notifications.InternalNotify)
	}

	// User-facing API (protected by account_id from JWT middleware)
	// NOTE: JWT middleware to be added when integrating with auth system.
	// For now, account_id is expected in header X-Account-ID for testing.
	api := r.Group("/api/v1/notifications")
	api.Use(tempAccountIDMiddleware())
	{
		api.GET("", deps.Notifications.List)
		api.GET("/unread", deps.Notifications.Unread)
		api.POST("/:id/read", deps.Notifications.MarkRead)
		api.POST("/read-all", deps.Notifications.MarkAllRead)
		api.GET("/ws", deps.Notifications.WebSocket)
	}

	// Admin API (template management, protected by internal key for now)
	admin := r.Group("/admin/v1")
	admin.Use(internalKeyAuth(deps.InternalKey))
	{
		admin.GET("/templates", deps.Templates.List)
		admin.POST("/templates", deps.Templates.Upsert)
		admin.DELETE("/templates/:id", deps.Templates.Delete)
	}

	return r
}

// internalKeyAuth validates the bearer token matches the INTERNAL_API_KEY.
func internalKeyAuth(key string) gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		if auth == "" || auth != "Bearer "+key {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid or missing internal API key"})
			return
		}
		c.Next()
	}
}

// webhookSecretAuth validates a shared secret in the Authorization header.
// Used for cluster-internal webhooks (e.g. Alertmanager).
func webhookSecretAuth(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		if auth == "" || auth != "Bearer "+secret {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid webhook secret"})
			return
		}
		c.Next()
	}
}

// tempAccountIDMiddleware extracts account_id from X-Account-ID header.
// This is a temporary middleware for testing; in production, JWT middleware
// will set account_id from Zitadel claims.
func tempAccountIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		idStr := c.GetHeader("X-Account-ID")
		if idStr == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing X-Account-ID header"})
			return
		}
		var accountID int64
		for _, ch := range idStr {
			if ch < '0' || ch > '9' {
				c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid X-Account-ID"})
				return
			}
			accountID = accountID*10 + int64(ch-'0')
		}
		c.Set("account_id", accountID)
		c.Next()
	}
}
