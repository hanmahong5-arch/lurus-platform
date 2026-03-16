// Package metrics exposes Prometheus instrumentation for lurus-platform.
package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Registered Prometheus metrics.
var (
	httpRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "lurus_identity",
			Name:      "http_requests_total",
			Help:      "Total number of HTTP requests, partitioned by method, route, and status.",
		},
		[]string{"method", "route", "status"},
	)

	httpRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "lurus_identity",
			Name:      "http_request_duration_seconds",
			Help:      "HTTP request latency histogram by method and route.",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"method", "route"},
	)

	webhookTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "lurus_identity",
			Name:      "webhook_events_total",
			Help:      "Total webhook events processed, by provider and result.",
		},
		[]string{"provider", "result"}, // result: success | duplicate | error
	)

	cacheHits = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "lurus_identity",
			Name:      "cache_operations_total",
			Help:      "Cache get operations, partitioned by result (hit/miss/error).",
		},
		[]string{"result"},
	)

	cronRunsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "lurus_identity",
			Name:      "cron_runs_total",
			Help:      "Total cron job executions, by job name and result.",
		},
		[]string{"job", "result"},
	)

	subscriptionExpiredTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "lurus_identity",
			Name:      "subscriptions_expired_total",
			Help:      "Total subscriptions transitioned to expired state by the cron job.",
		},
	)
)

// Handler returns the standard Prometheus /metrics HTTP handler.
func Handler() http.Handler {
	return promhttp.Handler()
}

// HTTPMiddleware returns a Gin middleware that records request count and latency.
func HTTPMiddleware() gin.HandlerFunc {
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

// RecordWebhookEvent records a webhook processing outcome.
// provider: "epay" | "stripe" | "creem"
// result: "success" | "duplicate" | "error" | "invalid_signature"
func RecordWebhookEvent(provider, result string) {
	webhookTotal.WithLabelValues(provider, result).Inc()
}

// RecordCacheHit increments the cache hit counter.
func RecordCacheHit() { cacheHits.WithLabelValues("hit").Inc() }

// RecordCacheMiss increments the cache miss counter.
func RecordCacheMiss() { cacheHits.WithLabelValues("miss").Inc() }

// RecordCacheError increments the cache error counter.
func RecordCacheError() { cacheHits.WithLabelValues("error").Inc() }

// RecordCronRun records a cron job execution outcome.
func RecordCronRun(job, result string) { cronRunsTotal.WithLabelValues(job, result).Inc() }

// RecordSubscriptionExpired increments the expiry counter.
func RecordSubscriptionExpired() { subscriptionExpiredTotal.Inc() }
