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

const namespace = "lurus_platform"

// Registered Prometheus metrics.
var (
	httpRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "http_requests_total",
			Help:      "Total number of HTTP requests, partitioned by method, route, and status.",
		},
		[]string{"method", "route", "status"},
	)

	httpRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "http_request_duration_seconds",
			Help:      "HTTP request latency histogram by method and route.",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"method", "route"},
	)

	webhookTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "webhook_events_total",
			Help:      "Total webhook events processed, by provider and result.",
		},
		[]string{"provider", "result"}, // result: success | duplicate | error | invalid_signature
	)

	cacheOps = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "cache_operations_total",
			Help:      "Cache get operations, partitioned by result (hit/miss/error).",
		},
		[]string{"result"},
	)

	walletOpsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "wallet_operations_total",
			Help:      "Total wallet operations, by operation type and result.",
		},
		[]string{"operation", "result"},
	)

	walletAmountTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "wallet_operation_amount_cny_total",
			Help:      "Cumulative CNY amount processed by wallet operations.",
		},
		[]string{"operation"},
	)

	paymentOrderTransitions = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "payment_order_transitions_total",
			Help:      "Payment order state transitions.",
		},
		[]string{"from_status", "to_status", "order_type", "provider"},
	)

	subscriptionTransitions = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "subscription_transitions_total",
			Help:      "Subscription lifecycle state transitions.",
		},
		[]string{"from_status", "to_status", "product_id"},
	)

	refundOpsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "refund_operations_total",
			Help:      "Total refund operations, by action and result.",
		},
		[]string{"action", "result"},
	)

	reconciliationIssuesFound = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "reconciliation_issues_found_total",
			Help:      "Total reconciliation issues detected by the worker.",
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
func RecordCacheHit() { cacheOps.WithLabelValues("hit").Inc() }

// RecordCacheMiss increments the cache miss counter.
func RecordCacheMiss() { cacheOps.WithLabelValues("miss").Inc() }

// RecordCacheError increments the cache error counter.
func RecordCacheError() { cacheOps.WithLabelValues("error").Inc() }

// RecordWalletOperation records a wallet operation outcome.
// operation: "topup" | "debit" | "credit"
// result: "success" | "error"
func RecordWalletOperation(operation, result string) {
	walletOpsTotal.WithLabelValues(operation, result).Inc()
}

// RecordWalletAmount accumulates the CNY amount for a wallet operation.
func RecordWalletAmount(operation string, amountCNY float64) {
	walletAmountTotal.WithLabelValues(operation).Add(amountCNY)
}

// RecordPaymentOrderTransition records a payment order state transition.
func RecordPaymentOrderTransition(fromStatus, toStatus, orderType, provider string) {
	paymentOrderTransitions.WithLabelValues(fromStatus, toStatus, orderType, provider).Inc()
}

// RecordSubscriptionTransition records a subscription lifecycle state transition.
func RecordSubscriptionTransition(fromStatus, toStatus, productID string) {
	subscriptionTransitions.WithLabelValues(fromStatus, toStatus, productID).Inc()
}

// RecordRefundOperation records a refund operation outcome.
// action: "request" | "approve" | "reject"
// result: "success" | "error"
func RecordRefundOperation(action, result string) {
	refundOpsTotal.WithLabelValues(action, result).Inc()
}

// RecordReconciliationIssues records the number of issues found in a single tick.
func RecordReconciliationIssues(count int) {
	reconciliationIssuesFound.Add(float64(count))
}
