package repo

import (
	"context"
	"errors"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"gorm.io/gorm"
)

// CheckinRepo provides persistence operations for daily check-ins.
type CheckinRepo struct {
	db *gorm.DB
}

// NewCheckinRepo creates a new CheckinRepo.
func NewCheckinRepo(db *gorm.DB) *CheckinRepo {
	return &CheckinRepo{db: db}
}

// Create inserts a new checkin record. Returns an error if the daily unique constraint is violated.
func (r *CheckinRepo) Create(ctx context.Context, c *entity.Checkin) error {
	return r.db.WithContext(ctx).Create(c).Error
}

// GetByAccountAndDate returns the checkin for the given account and date, or nil if not found.
func (r *CheckinRepo) GetByAccountAndDate(ctx context.Context, accountID int64, date string) (*entity.Checkin, error) {
	var c entity.Checkin
	err := r.db.WithContext(ctx).
		Where("account_id = ? AND checkin_date = ?", accountID, date).
		First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// ListByAccountAndMonth returns all checkins for an account in a given month (format: yyyy-MM).
func (r *CheckinRepo) ListByAccountAndMonth(ctx context.Context, accountID int64, yearMonth string) ([]entity.Checkin, error) {
	var checkins []entity.Checkin
	err := r.db.WithContext(ctx).
		Where("account_id = ? AND checkin_date LIKE ?", accountID, yearMonth+"%").
		Order("checkin_date ASC").
		Find(&checkins).Error
	return checkins, err
}

// CountConsecutive counts the number of consecutive check-in days up to (and including) the given date.
func (r *CheckinRepo) CountConsecutive(ctx context.Context, accountID int64, date string) (int, error) {
	var checkins []entity.Checkin
	err := r.db.WithContext(ctx).
		Where("account_id = ? AND checkin_date <= ?", accountID, date).
		Order("checkin_date DESC").
		Limit(30). // Look back at most 30 days
		Find(&checkins).Error
	if err != nil {
		return 0, err
	}
	if len(checkins) == 0 {
		return 0, nil
	}
	// Count consecutive days backwards from the target date.
	count := 0
	for i, c := range checkins {
		if i == 0 {
			if c.CheckinDate != date {
				return 0, nil
			}
			count = 1
			continue
		}
		// Check if the previous checkin was exactly one day before.
		prevDate := checkins[i-1].CheckinDate
		expectedDate := subtractDay(prevDate)
		if c.CheckinDate != expectedDate {
			break
		}
		count++
	}
	return count, nil
}

// subtractDay subtracts one day from a yyyy-MM-dd date string.
func subtractDay(dateStr string) string {
	t, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return ""
	}
	return t.AddDate(0, 0, -1).Format("2006-01-02")
}
