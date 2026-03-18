package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	otelgin "go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"

	"github.com/hanmahong5-arch/lurus-platform/modules/notification/internal/pkg/auth"
)

var (
	httpRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "lurus_notification",
			Name:      "http_requests_total",
			Help:      "Total HTTP requests by method, route, and status.",
		},
		[]string{"method", "route", "status"},
	)
	httpRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "lurus_notification",
			Name:      "http_request_duration_seconds",
			Help:      "HTTP request latency by method and route.",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"method", "route"},
	)
)

// Deps holds all handler dependencies for router construction.
type Deps struct {
	Notifications   *NotificationHandler
	Templates       *TemplateHandler
	Preferences     *PreferenceHandler
	Devices         *DeviceHandler
	JWT             *auth.Middleware
	InternalKey     string
	WebhookSecret   string // shared secret for alertmanager webhook (empty = no auth)
	OtelServiceName string // service name for OTel tracing (default: lurus-notification)
}

// BuildRouter creates the Gin engine with all routes.
func BuildRouter(deps Deps) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	// HTTP RED metrics middleware (Prometheus)
	r.Use(httpMetricsMiddleware())

	// OTel distributed tracing middleware
	svcName := deps.OtelServiceName
	if svcName == "" {
		svcName = "lurus-notification"
	}
	r.Use(otelgin.Middleware(svcName))

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

	// User-facing API (protected by JWT authentication)
	api := r.Group("/api/v1/notifications")
	if deps.JWT != nil {
		api.Use(deps.JWT.Auth())
	} else {
		// Fallback for dev/test without JWT configured.
		api.Use(devAccountIDMiddleware())
	}
	{
		api.GET("", deps.Notifications.List)
		api.GET("/unread", deps.Notifications.Unread)
		api.POST("/:id/read", deps.Notifications.MarkRead)
		api.POST("/read-all", deps.Notifications.MarkAllRead)
		api.GET("/ws", deps.Notifications.WebSocket)

		// Preference management
		if deps.Preferences != nil {
			api.GET("/preferences", deps.Preferences.Get)
			api.PUT("/preferences", deps.Preferences.Update)
		}

		// Device token management (FCM push)
		if deps.Devices != nil {
			api.POST("/devices", deps.Devices.Register)
			api.DELETE("/devices/:token", deps.Devices.Unregister)
		}
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

// httpMetricsMiddleware records RED metrics for every HTTP request.
func httpMetricsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		status := strconv.Itoa(c.Writer.Status())
		elapsed := time.Since(start).Seconds()

		httpRequestsTotal.WithLabelValues(c.Request.Method, route, status).Inc()
		httpRequestDuration.WithLabelValues(c.Request.Method, route).Observe(elapsed)
	}
}

// devAccountIDMiddleware extracts account_id from X-Account-ID header.
// Used only when SESSION_SECRET is not configured (dev/test environments).
func devAccountIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		idStr := c.GetHeader("X-Account-ID")
		if idStr == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing X-Account-ID header (dev mode)"})
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
