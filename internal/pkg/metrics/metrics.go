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

	// ── QR primitive (v2) metrics ──────────────────────────────────────────
	// See docs/qr-primitive.md §Metrics and deploy/grafana/dashboards/qr-primitive.json.

	qrSessionsCreatedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "qr_sessions_created_total",
			Help:      "Total QR sessions successfully created, partitioned by action.",
		},
		[]string{"action"},
	)

	qrConfirmedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "qr_confirmed_total",
			Help:      "Total QR sessions successfully confirmed by the APP, partitioned by action.",
		},
		[]string{"action"},
	)

	qrExpiredTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "qr_expired_total",
			Help:      "Total QR session lookups that 404'd due to TTL expiry or never-existing id (from status or confirm endpoints).",
		},
	)

	qrSignatureRejectedTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "qr_signature_rejected_total",
			Help:      "Total confirm requests rejected for invalid or expired HMAC signature. Spikes likely indicate tampering attempts or APP/backend secret mismatch.",
		},
	)

	qrConfirmLatency = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "qr_confirm_latency_seconds",
			Help:      "End-to-end latency of the /api/v2/qr/:id/confirm handler, partitioned by action.",
			// Confirm is mostly Redis IO — use tight buckets.
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5},
		},
		[]string{"action"},
	)

	qrLegacySignaturesTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "qr_legacy_signatures_total",
			Help:      "Total QR confirm calls that fell back to the pre-B5 (timestamp-less) signature path. Used to monitor the legacy deprecation window — spikes near removal date should block the cutover.",
		},
	)

	// Rate limiter fallback: counts every request routed through the local
	// in-process token bucket because Redis was unreachable. A non-zero
	// rate indicates a Redis outage and should page; sustained firing of
	// this counter is also the signal that the fallback's 2x quota is
	// actually protecting the service instead of fail-open letting
	// everything through.
	ratelimitFallbackEngagedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "ratelimit_fallback_engaged_total",
			Help:      "Total rate-limit checks served by the in-process fallback bucket because Redis was unreachable.",
		},
		[]string{"scope"}, // scope: "ip" | "user"
	)

	// QR long-poll concurrency: current in-flight long-poll goroutines
	// and cumulative rejections due to the max-inflight semaphore being
	// saturated. Gauge + counter pair mirrors the usual "queue depth" /
	// "drop rate" shape so a saturation alert can be built from either.
	qrPollsInflight = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "qr_polls_inflight",
			Help:      "Number of /api/v2/qr/:id/status long-poll requests currently holding a semaphore slot.",
		},
	)

	qrPollsRejectedOverloadTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "qr_polls_rejected_overload_total",
			Help:      "Total /api/v2/qr/:id/status requests rejected with 503 because the long-poll semaphore was saturated.",
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

// RecordQRSessionCreated records a successful QR session creation.
func RecordQRSessionCreated(action string) {
	qrSessionsCreatedTotal.WithLabelValues(action).Inc()
}

// RecordQRConfirmed records a successful confirm (pending → confirmed transition).
func RecordQRConfirmed(action string) {
	qrConfirmedTotal.WithLabelValues(action).Inc()
}

// RecordQRExpired records a status/confirm lookup that 404'd (TTL expired or id never existed).
func RecordQRExpired() {
	qrExpiredTotal.Inc()
}

// RecordQRSignatureRejected records a confirm request that failed HMAC / timestamp validation.
func RecordQRSignatureRejected() {
	qrSignatureRejectedTotal.Inc()
}

// RecordQRConfirmLatency records the wall-clock latency of a confirm handler call.
func RecordQRConfirmLatency(action string, elapsed time.Duration) {
	qrConfirmLatency.WithLabelValues(action).Observe(elapsed.Seconds())
}

// RecordQRLegacySignature records a confirm request that took the pre-B5
// (timestamp-less) HMAC verification path. Incremented regardless of whether
// the legacy signature itself validated — the counter measures *usage* of
// the deprecated format so we can decide when it is safe to remove.
func RecordQRLegacySignature() {
	qrLegacySignaturesTotal.Inc()
}

// RecordRateLimitFallbackEngaged increments when a rate-limit check ran
// against the in-process fallback bucket (Redis was unreachable).
// scope: "ip" | "user".
func RecordRateLimitFallbackEngaged(scope string) {
	ratelimitFallbackEngagedTotal.WithLabelValues(scope).Inc()
}

// IncQRPollsInflight marks that a new long-poll started holding a slot.
func IncQRPollsInflight() {
	qrPollsInflight.Inc()
}

// DecQRPollsInflight marks that a long-poll released its slot.
func DecQRPollsInflight() {
	qrPollsInflight.Dec()
}

// RecordQRPollRejectedOverload increments when a long-poll was refused
// because the concurrency semaphore was full.
func RecordQRPollRejectedOverload() {
	qrPollsRejectedOverloadTotal.Inc()
}
