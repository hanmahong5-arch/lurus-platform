// Package metrics provides Prometheus instrumentation for lurus-platform-notification.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// NotificationsSentTotal counts notifications dispatched by channel and status.
	NotificationsSentTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "lurus_notification",
			Name:      "sent_total",
			Help:      "Total notifications dispatched, by channel and result.",
		},
		[]string{"channel", "result"}, // result: success | failed
	)

	// NotificationDispatchDuration measures dispatch latency per channel.
	NotificationDispatchDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "lurus_notification",
			Name:      "dispatch_duration_seconds",
			Help:      "Notification dispatch latency by channel.",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"channel"},
	)

	// WebSocketConnections tracks current active WebSocket connections.
	WebSocketConnections = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "lurus_notification",
			Name:      "websocket_connections",
			Help:      "Current number of active WebSocket connections.",
		},
	)

	// NATSEventsTotal counts NATS events consumed by subject and result.
	NATSEventsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "lurus_notification",
			Name:      "nats_events_total",
			Help:      "Total NATS events consumed, by subject and result.",
		},
		[]string{"subject", "result"}, // result: success | error | skip
	)

	// AlertmanagerWebhooksTotal counts alertmanager webhook invocations.
	AlertmanagerWebhooksTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "lurus_notification",
			Name:      "alertmanager_webhooks_total",
			Help:      "Total alertmanager webhook invocations by status.",
		},
		[]string{"alert_status"}, // firing | resolved
	)
)
