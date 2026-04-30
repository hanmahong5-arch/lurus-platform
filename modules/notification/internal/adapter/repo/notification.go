// Package repo provides PostgreSQL repositories for the notification service.
package repo

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/hanmahong5-arch/lurus-platform/modules/notification/internal/domain/entity"
)

// NotificationRepo persists notifications to PostgreSQL.
type NotificationRepo struct {
	db *gorm.DB
}

// NewNotificationRepo creates a new NotificationRepo.
func NewNotificationRepo(db *gorm.DB) *NotificationRepo {
	return &NotificationRepo{db: db}
}

// Create inserts a new notification record.
func (r *NotificationRepo) Create(ctx context.Context, n *entity.Notification) error {
	if err := r.db.WithContext(ctx).Create(n).Error; err != nil {
		return fmt.Errorf("create notification: %w", err)
	}
	return nil
}

// ListByAccount retrieves notifications for a given account, ordered by creation time desc.
func (r *NotificationRepo) ListByAccount(ctx context.Context, accountID int64, limit, offset int) ([]entity.Notification, int64, error) {
	var items []entity.Notification
	var total int64

	q := r.db.WithContext(ctx).Model(&entity.Notification{}).Where("account_id = ?", accountID)
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count notifications: %w", err)
	}
	if err := q.Order("created_at DESC").Limit(limit).Offset(offset).Find(&items).Error; err != nil {
		return nil, 0, fmt.Errorf("list notifications: %w", err)
	}
	return items, total, nil
}

// ListFilter narrows the notification list by source/category/unread.
// Empty Source/Category mean "no filter on that field".
type ListFilter struct {
	Source     string
	Category   string
	UnreadOnly bool
}

// ListByAccountFiltered is like ListByAccount but supports source/category/unread filters.
// Filter strings are matched verbatim against the indexed columns; the caller is
// responsible for any normalization. Both count and slice queries share the same
// WHERE clause so total reflects the visible page set.
func (r *NotificationRepo) ListByAccountFiltered(ctx context.Context, accountID int64, f ListFilter, limit, offset int) ([]entity.Notification, int64, error) {
	var items []entity.Notification
	var total int64

	q := r.db.WithContext(ctx).Model(&entity.Notification{}).Where("account_id = ?", accountID)
	if f.Source != "" {
		q = q.Where("source = ?", f.Source)
	}
	if f.Category != "" {
		q = q.Where("category = ?", f.Category)
	}
	if f.UnreadOnly {
		q = q.Where("channel = ? AND read_at IS NULL", entity.ChannelInApp)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count notifications (filtered): %w", err)
	}
	if err := q.Order("created_at DESC").Limit(limit).Offset(offset).Find(&items).Error; err != nil {
		return nil, 0, fmt.Errorf("list notifications (filtered): %w", err)
	}
	return items, total, nil
}

// CountUnreadBySource groups unread in-app notifications by source.
// Always returns a non-nil map, possibly empty.
func (r *NotificationRepo) CountUnreadBySource(ctx context.Context, accountID int64) (map[string]int64, error) {
	type row struct {
		Source string
		Count  int64
	}
	var rows []row
	err := r.db.WithContext(ctx).Model(&entity.Notification{}).
		Select("source, COUNT(*) AS count").
		Where("account_id = ? AND channel = ? AND read_at IS NULL", accountID, entity.ChannelInApp).
		Group("source").
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("count unread by source: %w", err)
	}
	out := make(map[string]int64, len(rows))
	for _, r := range rows {
		if r.Source == "" {
			continue
		}
		out[r.Source] = r.Count
	}
	return out, nil
}

// CountUnreadByCategory groups unread in-app notifications by category.
// Always returns a non-nil map, possibly empty.
func (r *NotificationRepo) CountUnreadByCategory(ctx context.Context, accountID int64) (map[string]int64, error) {
	type row struct {
		Category string
		Count    int64
	}
	var rows []row
	err := r.db.WithContext(ctx).Model(&entity.Notification{}).
		Select("category, COUNT(*) AS count").
		Where("account_id = ? AND channel = ? AND read_at IS NULL", accountID, entity.ChannelInApp).
		Group("category").
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("count unread by category: %w", err)
	}
	out := make(map[string]int64, len(rows))
	for _, r := range rows {
		if r.Category == "" {
			continue
		}
		out[r.Category] = r.Count
	}
	return out, nil
}

// CountUnread returns the number of unread notifications for an account.
func (r *NotificationRepo) CountUnread(ctx context.Context, accountID int64) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&entity.Notification{}).
		Where("account_id = ? AND channel = ? AND read_at IS NULL", accountID, entity.ChannelInApp).
		Count(&count).Error
	if err != nil {
		return 0, fmt.Errorf("count unread: %w", err)
	}
	return count, nil
}

// MarkRead marks a single notification as read.
func (r *NotificationRepo) MarkRead(ctx context.Context, id, accountID int64) error {
	now := time.Now().UTC()
	result := r.db.WithContext(ctx).Model(&entity.Notification{}).
		Where("id = ? AND account_id = ? AND read_at IS NULL", id, accountID).
		Updates(map[string]any{
			"read_at": now,
			"status":  entity.StatusRead,
		})
	if result.Error != nil {
		return fmt.Errorf("mark read: %w", result.Error)
	}
	return nil
}

// MarkAllRead marks all unread in-app notifications as read for an account.
func (r *NotificationRepo) MarkAllRead(ctx context.Context, accountID int64) (int64, error) {
	now := time.Now().UTC()
	result := r.db.WithContext(ctx).Model(&entity.Notification{}).
		Where("account_id = ? AND channel = ? AND read_at IS NULL", accountID, entity.ChannelInApp).
		Updates(map[string]any{
			"read_at": now,
			"status":  entity.StatusRead,
		})
	if result.Error != nil {
		return 0, fmt.Errorf("mark all read: %w", result.Error)
	}
	return result.RowsAffected, nil
}

// UpdateStatus updates the delivery status for a notification.
func (r *NotificationRepo) UpdateStatus(ctx context.Context, id int64, status entity.Status) error {
	updates := map[string]any{"status": status}
	if status == entity.StatusSent {
		now := time.Now().UTC()
		updates["sent_at"] = now
	}
	if err := r.db.WithContext(ctx).Model(&entity.Notification{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return fmt.Errorf("update status: %w", err)
	}
	return nil
}

// UpdateStatusWithMetadata updates the delivery status and appends metadata (e.g. failure reason).
func (r *NotificationRepo) UpdateStatusWithMetadata(ctx context.Context, id int64, status entity.Status, metadata string) error {
	updates := map[string]any{"status": status, "metadata": metadata}
	if status == entity.StatusSent {
		now := time.Now().UTC()
		updates["sent_at"] = now
	}
	if err := r.db.WithContext(ctx).Model(&entity.Notification{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return fmt.Errorf("update status with metadata: %w", err)
	}
	return nil
}

// GetByID retrieves a single notification by ID.
func (r *NotificationRepo) GetByID(ctx context.Context, id int64) (*entity.Notification, error) {
	var n entity.Notification
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&n).Error; err != nil {
		return nil, fmt.Errorf("get notification by id: %w", err)
	}
	return &n, nil
}

// FindByEventID checks if a notification for this event already exists (idempotency).
func (r *NotificationRepo) FindByEventID(ctx context.Context, eventID string, channel entity.Channel) (*entity.Notification, error) {
	var n entity.Notification
	err := r.db.WithContext(ctx).Where("event_id = ? AND channel = ?", eventID, channel).First(&n).Error
	if err != nil {
		return nil, err
	}
	return &n, nil
}
