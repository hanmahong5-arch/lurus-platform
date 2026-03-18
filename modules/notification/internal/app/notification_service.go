// Package app contains use-case orchestration for the notification service.
package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"github.com/hanmahong5-arch/lurus-platform/modules/notification/internal/adapter/repo"
	"github.com/hanmahong5-arch/lurus-platform/modules/notification/internal/adapter/sender"
	"github.com/hanmahong5-arch/lurus-platform/modules/notification/internal/domain/entity"
	"github.com/hanmahong5-arch/lurus-platform/modules/notification/internal/pkg/metrics"
)

const (
	maxRetries     = 3
	retryBaseDelay = 1 * time.Second
	retryRedisKey  = "notif:retry_queue"
)

// NotificationService orchestrates notification creation, template resolution, and dispatch.
type NotificationService struct {
	notifRepo *repo.NotificationRepo
	tmplRepo  *repo.TemplateRepo
	prefRepo  *repo.PreferenceRepo
	email     sender.Sender
	fcm       sender.Sender
	ws        *sender.WSSender
	hub       *sender.Hub
	rdb       *redis.Client
}

// NewNotificationService creates a NotificationService with all dependencies.
func NewNotificationService(
	notifRepo *repo.NotificationRepo,
	tmplRepo *repo.TemplateRepo,
	prefRepo *repo.PreferenceRepo,
	emailSender sender.Sender,
	fcmSender sender.Sender,
	wsSender *sender.WSSender,
	hub *sender.Hub,
) *NotificationService {
	return &NotificationService{
		notifRepo: notifRepo,
		tmplRepo:  tmplRepo,
		prefRepo:  prefRepo,
		email:     emailSender,
		fcm:       fcmSender,
		ws:        wsSender,
		hub:       hub,
	}
}

// SetRedis sets the Redis client for retry queue management.
// Called after construction to avoid circular dependencies.
func (s *NotificationService) SetRedis(rdb *redis.Client) {
	s.rdb = rdb
}

// SendRequest is the input for creating and dispatching a notification.
type SendRequest struct {
	AccountID int64
	EventType string
	EventID   string
	Channels  []entity.Channel // which channels to dispatch to
	Vars      map[string]string // template variable substitutions
	EmailAddr string // recipient email (for email channel)
}

// Send creates and dispatches a notification across requested channels.
// Idempotent: if a notification for the same event_id + channel already exists, it skips.
func (s *NotificationService) Send(ctx context.Context, req SendRequest) error {
	for _, ch := range req.Channels {
		// Idempotency check
		if req.EventID != "" {
			if _, err := s.notifRepo.FindByEventID(ctx, req.EventID, ch); err == nil {
				slog.Debug("notification already exists, skipping",
					"event_id", req.EventID, "channel", ch)
				continue
			}
		}

		// Check user preference
		enabled, err := s.prefRepo.IsChannelEnabled(ctx, req.AccountID, ch)
		if err != nil {
			slog.Warn("preference check failed, defaulting to enabled",
				"account_id", req.AccountID, "channel", ch, "err", err)
			enabled = true
		}
		if !enabled {
			slog.Debug("channel disabled by user preference",
				"account_id", req.AccountID, "channel", ch)
			continue
		}

		// Resolve template
		title, body, priority := s.resolveTemplate(ctx, req.EventType, ch, req.Vars)

		// Create notification record
		notif := &entity.Notification{
			AccountID: req.AccountID,
			Channel:   ch,
			Category:  categoryFromEvent(req.EventType),
			Title:     title,
			Body:      body,
			Priority:  priority,
			Status:    entity.StatusPending,
			EventType: req.EventType,
			EventID:   req.EventID,
		}

		if err := s.notifRepo.Create(ctx, notif); err != nil {
			return fmt.Errorf("create notification for channel %s: %w", ch, err)
		}

		// Dispatch asynchronously (best effort)
		go s.dispatch(context.Background(), notif, req.EmailAddr, 0)
	}
	return nil
}

// retryItem represents a notification pending retry in the Redis sorted set.
type retryItem struct {
	NotificationID int64  `json:"id"`
	EmailAddr      string `json:"email_addr"`
	Attempt        int    `json:"attempt"`
}

// dispatch sends the notification through its channel and updates status.
// On failure, queues for retry with exponential backoff instead of immediately marking as failed.
func (s *NotificationService) dispatch(ctx context.Context, notif *entity.Notification, emailAddr string, attempt int) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	start := time.Now()
	ch := string(notif.Channel)

	var err error
	msg := sender.Message{
		Subject:  notif.Title,
		Body:     notif.Body,
		Priority: string(notif.Priority),
		Metadata: map[string]string{
			"account_id":      strconv.FormatInt(notif.AccountID, 10),
			"notification_id": strconv.FormatInt(notif.ID, 10),
		},
	}

	switch notif.Channel {
	case entity.ChannelEmail:
		msg.To = emailAddr
		if s.email != nil {
			err = s.email.Send(ctx, msg)
		}
	case entity.ChannelFCM:
		if s.fcm != nil {
			err = s.fcm.Send(ctx, msg)
		}
	case entity.ChannelInApp:
		if s.ws != nil {
			err = s.ws.Send(ctx, msg)
		}
	}

	metrics.NotificationDispatchDuration.WithLabelValues(ch).Observe(time.Since(start).Seconds())

	if err != nil {
		if attempt < maxRetries-1 {
			// Queue for retry with exponential backoff: 1s, 4s, 16s.
			s.scheduleRetry(notif, emailAddr, attempt+1)
			metrics.NotificationsSentTotal.WithLabelValues(ch, "retrying").Inc()
			_ = s.notifRepo.UpdateStatus(ctx, notif.ID, entity.StatusRetrying)
			return
		}

		// Max retries exhausted — mark as failed with reason.
		metrics.NotificationsSentTotal.WithLabelValues(ch, "failed").Inc()
		slog.Error("notification dispatch failed after retries",
			"id", notif.ID,
			"channel", notif.Channel,
			"attempt", attempt+1,
			"err", err,
		)
		metadata, _ := json.Marshal(map[string]string{
			"failure_reason": err.Error(),
			"attempts":       strconv.Itoa(attempt + 1),
		})
		_ = s.notifRepo.UpdateStatusWithMetadata(ctx, notif.ID, entity.StatusFailed, string(metadata))
		return
	}

	metrics.NotificationsSentTotal.WithLabelValues(ch, "success").Inc()
	_ = s.notifRepo.UpdateStatus(ctx, notif.ID, entity.StatusSent)
}

// scheduleRetry queues a failed notification for retry with exponential backoff.
// Uses Redis sorted set with score = next retry timestamp.
func (s *NotificationService) scheduleRetry(notif *entity.Notification, emailAddr string, attempt int) {
	if s.rdb == nil {
		// No Redis — fallback to immediate goroutine retry with sleep.
		delay := retryBaseDelay * time.Duration(1<<(2*uint(attempt-1)))
		go func() {
			time.Sleep(delay)
			s.dispatch(context.Background(), notif, emailAddr, attempt)
		}()
		return
	}

	// Exponential backoff: 1s, 4s, 16s.
	delay := retryBaseDelay * time.Duration(1<<(2*uint(attempt-1)))
	nextRetry := time.Now().Add(delay)

	item, _ := json.Marshal(retryItem{
		NotificationID: notif.ID,
		EmailAddr:      emailAddr,
		Attempt:        attempt,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.rdb.ZAdd(ctx, retryRedisKey, redis.Z{
		Score:  float64(nextRetry.Unix()),
		Member: string(item),
	}).Err(); err != nil {
		slog.Error("failed to queue retry",
			"notification_id", notif.ID,
			"err", err,
		)
	}
}

// StartRetryWorker launches a background goroutine that processes the retry queue.
// It polls Redis every second for due retries and re-dispatches them.
func (s *NotificationService) StartRetryWorker(ctx context.Context) {
	if s.rdb == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.processRetryQueue(ctx)
			}
		}
	}()
}

func (s *NotificationService) processRetryQueue(ctx context.Context) {
	now := float64(time.Now().Unix())

	// Fetch items due for retry (score <= now).
	results, err := s.rdb.ZRangeByScoreWithScores(ctx, retryRedisKey, &redis.ZRangeBy{
		Min:   "-inf",
		Max:   strconv.FormatFloat(now, 'f', 0, 64),
		Count: 10, // process up to 10 retries per tick
	}).Result()
	if err != nil {
		return
	}

	for _, z := range results {
		member := z.Member.(string)

		// Remove from queue first (claim the item).
		removed, err := s.rdb.ZRem(ctx, retryRedisKey, member).Result()
		if err != nil || removed == 0 {
			continue // another worker claimed it
		}

		var item retryItem
		if err := json.Unmarshal([]byte(member), &item); err != nil {
			slog.Error("invalid retry queue item", "err", err)
			continue
		}

		notif, err := s.notifRepo.GetByID(ctx, item.NotificationID)
		if err != nil {
			slog.Error("retry: notification not found",
				"id", item.NotificationID,
				"err", err)
			continue
		}

		go s.dispatch(context.Background(), notif, item.EmailAddr, item.Attempt)
	}
}

// resolveTemplate looks up a template for the event/channel combo.
// Falls back to the event type as title if no template exists.
func (s *NotificationService) resolveTemplate(ctx context.Context, eventType string, ch entity.Channel, vars map[string]string) (string, string, entity.Priority) {
	tmpl, err := s.tmplRepo.FindByEventAndChannel(ctx, eventType, ch)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			// Fallback: use event type as title
			return eventType, eventType, entity.PriorityNormal
		}
		slog.Warn("template lookup failed", "event_type", eventType, "channel", ch, "err", err)
		return eventType, eventType, entity.PriorityNormal
	}

	title := substituteVars(tmpl.Title, vars)
	body := substituteVars(tmpl.Body, vars)
	return title, body, tmpl.Priority
}

// substituteVars replaces {{key}} placeholders in text with values from vars.
func substituteVars(text string, vars map[string]string) string {
	for k, v := range vars {
		text = strings.ReplaceAll(text, "{{"+k+"}}", v)
	}
	return text
}

// categoryFromEvent extracts a category from an event type string.
// e.g. "identity.account.created" -> "account"
func categoryFromEvent(eventType string) string {
	parts := strings.Split(eventType, ".")
	if len(parts) >= 2 {
		return parts[1]
	}
	return "general"
}

// ListByAccount returns paginated notifications for an account.
func (s *NotificationService) ListByAccount(ctx context.Context, accountID int64, limit, offset int) ([]entity.Notification, int64, error) {
	return s.notifRepo.ListByAccount(ctx, accountID, limit, offset)
}

// CountUnread returns unread in-app notification count.
func (s *NotificationService) CountUnread(ctx context.Context, accountID int64) (int64, error) {
	return s.notifRepo.CountUnread(ctx, accountID)
}

// MarkRead marks a single notification as read.
func (s *NotificationService) MarkRead(ctx context.Context, id, accountID int64) error {
	return s.notifRepo.MarkRead(ctx, id, accountID)
}

// MarkAllRead marks all unread notifications as read.
func (s *NotificationService) MarkAllRead(ctx context.Context, accountID int64) (int64, error) {
	return s.notifRepo.MarkAllRead(ctx, accountID)
}
