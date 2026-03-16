package handler

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/hanmahong5-arch/lurus-platform/modules/notification/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/modules/notification/internal/domain/entity"
	"github.com/hanmahong5-arch/lurus-platform/modules/notification/internal/pkg/metrics"
)

// alertmanagerPayload is the top-level webhook payload from Alertmanager.
type alertmanagerPayload struct {
	Version           string            `json:"version"`
	GroupKey          string            `json:"groupKey"`
	TruncatedAlerts   int               `json:"truncatedAlerts"`
	Status            string            `json:"status"` // "firing" | "resolved"
	Receiver          string            `json:"receiver"`
	GroupLabels       map[string]string `json:"groupLabels"`
	CommonLabels      map[string]string `json:"commonLabels"`
	CommonAnnotations map[string]string `json:"commonAnnotations"`
	ExternalURL       string            `json:"externalURL"`
	Alerts            []alertmanagerAlert `json:"alerts"`
}

// alertmanagerAlert is a single alert inside the payload.
type alertmanagerAlert struct {
	Status       string            `json:"status"` // "firing" | "resolved"
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     time.Time         `json:"startsAt"`
	EndsAt       time.Time         `json:"endsAt"`
	GeneratorURL string            `json:"generatorURL"`
	Fingerprint  string            `json:"fingerprint"`
}

// AlertmanagerWebhook receives webhook payloads from Alertmanager and converts
// them into notification dispatches (email to admin + in-app for admin account).
// POST /webhooks/alertmanager
func (h *NotificationHandler) AlertmanagerWebhook(c *gin.Context) {
	var payload alertmanagerPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid alertmanager payload: " + err.Error()})
		return
	}

	if len(payload.Alerts) == 0 {
		c.JSON(http.StatusOK, gin.H{"ok": true, "processed": 0})
		return
	}

	processed := 0
	for _, alert := range payload.Alerts {
		metrics.AlertmanagerWebhooksTotal.WithLabelValues(alert.Status).Inc()
		if err := h.processAlert(c, alert); err != nil {
			slog.Error("failed to process alertmanager alert",
				"fingerprint", alert.Fingerprint,
				"alertname", alert.Labels["alertname"],
				"err", err,
			)
			continue
		}
		processed++
	}

	slog.Info("alertmanager webhook processed",
		"total", len(payload.Alerts),
		"processed", processed,
		"group_status", payload.Status,
	)

	c.JSON(http.StatusOK, gin.H{"ok": true, "processed": processed})
}

// processAlert converts one Alertmanager alert into a notification dispatch.
func (h *NotificationHandler) processAlert(c *gin.Context, alert alertmanagerAlert) error {
	alertName := alert.Labels["alertname"]
	severity := alert.Labels["severity"]
	namespace := alert.Labels["namespace"]
	summary := alert.Annotations["summary"]
	description := alert.Annotations["description"]

	// Build human-readable title and body
	statusEmoji := "🔥"
	if alert.Status == "resolved" {
		statusEmoji = "✅"
	}

	title := fmt.Sprintf("[%s %s] %s", strings.ToUpper(severity), statusEmoji, alertName)
	if len(title) > 200 {
		title = title[:197] + "..."
	}

	var body strings.Builder
	body.WriteString(fmt.Sprintf("Status: %s\n", alert.Status))
	if namespace != "" {
		body.WriteString(fmt.Sprintf("Namespace: %s\n", namespace))
	}
	if summary != "" {
		body.WriteString(fmt.Sprintf("Summary: %s\n", summary))
	}
	if description != "" {
		body.WriteString(fmt.Sprintf("Detail: %s\n", description))
	}
	if !alert.StartsAt.IsZero() {
		body.WriteString(fmt.Sprintf("Started: %s\n", alert.StartsAt.Format(time.RFC3339)))
	}

	// Determine channels: critical → email + in_app, warning → in_app only
	channels := []entity.Channel{entity.ChannelInApp}
	if severity == "critical" {
		channels = append(channels, entity.ChannelEmail)
	}

	// Use fingerprint as event_id for idempotency (append status to differentiate firing/resolved)
	eventID := fmt.Sprintf("am_%s_%s", alert.Fingerprint, alert.Status)

	// account_id=0 represents the system/admin account
	return h.svc.Send(c.Request.Context(), app.SendRequest{
		AccountID: 0,
		EventType: "system.alert." + alert.Status,
		EventID:   eventID,
		Channels:  channels,
		Vars: map[string]string{
			"alertname":   alertName,
			"severity":    severity,
			"namespace":   namespace,
			"summary":     summary,
			"description": description,
			"status":      alert.Status,
		},
		EmailAddr: h.adminEmail,
	})
}
