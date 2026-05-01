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

	qrDelegateConfirmsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "qr_delegate_confirms_total",
			Help:      "Total confirmed QR-delegate ops, partitioned by op type and outcome. result=success|failed; success means the executor returned no error, failed includes both ErrUnsupportedDelegateOp and any cascade failure. Op type is the canonical op string (delete_oidc_app|delete_account|approve_refund|...) — adding a new op needs no metric definition change, just a new label value.",
		},
		[]string{"op", "result"},
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

	// Counts long-polls that degraded from the Pub/Sub wait to the legacy
	// 1s polling loop because Redis Pub/Sub subscribe failed. A non-zero
	// rate is an early warning that Pub/Sub on the broker is degraded.
	qrPollFallbackTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "qr_poll_fallback_total",
			Help:      "Total long-poll requests that fell back to the 1s polling loop after Pub/Sub subscribe failed.",
		},
	)

	// App registry reconciler: per-environment outcome counter. Labels
	// stay stable and low-cardinality (handful of enum values) so this
	// is safe to use for alerting rules.
	appRegistryReconcileTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "app_registry_reconcile_total",
			Help:      "App registry reconcile results per (app, env) pair. Outcomes: noop, secret_updated, rollout_triggered, oidc_ensure_failed, secret_write_failed, rollout_failed, project_ensure_failed, skipped_disabled, rotation_succeeded, rotation_zitadel_failed, rotation_secret_write_failed, rotation_rollout_failed, rotation_state_read_failed, rotation_state_write_failed, rotation_lookup_failed.",
		},
		[]string{"outcome"},
	)

	// OIDC client_secret rotations are tracked separately from the
	// reconcile-outcome counter because the (app, env, trigger) split
	// is the natural grouping for an alerting / dashboard query — e.g.
	// "alert when an app hasn't auto-rotated in 100 days" or "spike of
	// manual rotations in 5 min". Trigger label is "auto" when fired
	// from the periodic reconciler, "manual" when an operator hits the
	// /admin/v1/apps/:name/:env/rotate-secret endpoint.
	//
	// Steady-state rate is roughly 1/(interval_days * apps) for auto,
	// plus operator-driven manual events. A persistent zero rate on a
	// confidential app is itself a signal that rotation is misconfigured.
	oidcSecretRotatedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "oidc_secret_rotated_total",
			Help:      "OIDC client_secret rotations grouped by trigger (manual|auto). Sustained activity should be roughly 1/(interval_days * apps) for auto, plus operator-driven manual events.",
		},
		[]string{"app", "env", "trigger"},
	)

	// newapi_sync (C.2 NewAPI quota mirror) operation counter — single
	// metric covers all three entry points (account create / topup credit
	// / llm-token issue) so dashboards and alerts can pivot on `op` while
	// keeping a stable, low-cardinality series count.
	//
	// `op` is one of:
	//   account_provisioned — OnAccountCreated (4c hook)
	//   topup_synced        — OnTopupCompleted (4d hook)
	//   llm_token_issued    — EnsureUserLLMToken (4e endpoint)
	//
	// `result` is one of:
	//   success | skipped | duplicate | error
	//
	// Specifically, "duplicate" is reserved for topup_synced events that
	// the deduper caught — the count is also the count of double-credits
	// avoided. Sustained non-zero suggests JetStream redelivery is firing
	// (probably fine) or a malformed publisher (probably not).
	newapiSyncOpsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "newapi_sync_ops_total",
			Help:      "newapi_sync operation outcomes by op and result. op∈{account_provisioned,topup_synced,llm_token_issued}; result∈{success,skipped,duplicate,error}.",
		},
		[]string{"op", "result"},
	)

	// hookOutcomeTotal tracks every async lifecycle hook invocation
	// (P1-9). Pivot on `event` (account_created / plan_changed / …) +
	// `hook` (mail / notification / newapi_sync / …) + `result`:
	//   succeeded_first_try | retry_succeeded | dlq |
	//   replay_succeeded    | replay_failed
	// `dlq` is the alerting signal — a non-zero rate means hooks are
	// permanently failing and operator intervention is needed.
	hookOutcomeTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "hook_outcomes_total",
			Help:      "Module hook invocation outcomes by event, hook name, and result. result∈{succeeded_first_try,retry_succeeded,dlq,replay_succeeded,replay_failed}.",
		},
		[]string{"event", "hook", "result"},
	)

	// hookDLQDepth is the live count of unresolved DLQ rows. Updated
	// by a periodic reconciler tick; safe to alert on >0 with a long
	// for-duration so blips don't page.
	hookDLQDepth = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "hook_dlq_pending",
			Help:      "Current count of pending hook failures in module.hook_failures.",
		},
	)

	// credentialAgeDays tracks how stale each platform-privileged
	// credential is. Cardinality is bounded by the explicit list in
	// app.DefaultTrackedCredentials (currently 2 series) so this is
	// safe to alert on directly. See docs/runbooks/credential-rotation.md
	// — these are the platform privileged tokens (Zitadel PAT / NewAPI
	// admin) that need rotation discipline before going prod, and the
	// gauge plus the soft (>90d) / hard (>180d) Prometheus rules in
	// docs/observability/alerts.md are the only operator visibility
	// into rotation drift until the rotation worker ships.
	credentialAgeDays = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "credential_age_days",
			Help:      "Age of tracked credentials in days. Alert when >90 (soft) or >180 (hard) — these are platform privileged tokens (Zitadel PAT / NewAPI admin) that need rotation discipline before going prod.",
		},
		[]string{"name"}, // zitadel_pat | newapi_admin_token
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

// RecordQRDelegateConfirm records the outcome of a confirmed
// QR-delegate op, partitioned by op type. result must be "success"
// or "failed" so dashboards can chart per-op approval rates.
//
// Why a separate metric from qr_confirmed_total: that counter labels
// only by action (login|join_org|delegate). For ops dashboards we
// need per-op visibility — "did delete_account fail at the cascade
// step today?" — without redoing the whole metric.
func RecordQRDelegateConfirm(op, result string) {
	qrDelegateConfirmsTotal.WithLabelValues(op, result).Inc()
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

// RecordQRPollFallback increments when a long-poll degraded from the
// Pub/Sub wait to the 1s polling loop (Redis Pub/Sub subscribe failure).
func RecordQRPollFallback() {
	qrPollFallbackTotal.Inc()
}

// RecordAppRegistryReconcile increments the reconciler outcome counter
// for a single (app, env) unit of work. `outcome` must come from the
// fixed vocabulary documented in the metric help text.
func RecordAppRegistryReconcile(outcome string) {
	appRegistryReconcileTotal.WithLabelValues(outcome).Inc()
}

// RecordOIDCSecretRotation increments the OIDC rotation counter for a
// single completed rotation. trigger must be "auto" (fired by the
// periodic reconciler) or "manual" (admin endpoint).
func RecordOIDCSecretRotation(app, env, trigger string) {
	oidcSecretRotatedTotal.WithLabelValues(app, env, trigger).Inc()
}

// RecordHookOutcome increments the hook outcomes counter (P1-9).
// Safe to call from any goroutine; counter ops are atomic.
func RecordHookOutcome(event, hook, result string) {
	hookOutcomeTotal.WithLabelValues(event, hook, result).Inc()
}

// SetHookDLQDepth updates the live DLQ depth gauge. Set from the
// periodic reconciler tick.
func SetHookDLQDepth(depth int64) {
	hookDLQDepth.Set(float64(depth))
}

// RecordCredentialAgeDays publishes the age (in days) of a tracked
// credential. The gauge is overwritten on each tick — the latest
// value is always the current age. `name` must be a stable
// low-cardinality identifier matching app.TrackedCredential.Name
// (currently "zitadel_pat" or "newapi_admin_token").
func RecordCredentialAgeDays(name string, days float64) {
	credentialAgeDays.WithLabelValues(name).Set(days)
}

// RecordNewAPISyncOp increments the newapi_sync operation counter.
//
// `op` ∈ {"account_provisioned","topup_synced","llm_token_issued"} —
// each newapi_sync entry point. `result` ∈ {"success","skipped","duplicate","error"}:
//
//   - success    happy path; the operation reached NewAPI and recorded its work
//   - skipped    correctly bypassed (e.g. account not yet mapped, amount ≤ 0)
//   - duplicate  dedup caught a JetStream redelivery (only relevant to topup);
//     count = how often we saved the user from double-credit
//   - error      anything else; should also create a log entry. Sustained
//     non-zero error rate is the alerting signal.
//
// Cardinality is bounded (3×4 = 12 series total) so safe for high-rate
// dashboards and Prometheus federation.
func RecordNewAPISyncOp(op, result string) {
	newapiSyncOpsTotal.WithLabelValues(op, result).Inc()
}
