package repo

import (
	"context"
	"fmt"
	"strings"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"gorm.io/gorm"
)

// ReferralRepo provides read-only statistics over referral reward events.
type ReferralRepo struct {
	db *gorm.DB
}

// NewReferralRepo creates a new ReferralRepo.
func NewReferralRepo(db *gorm.DB) *ReferralRepo {
	return &ReferralRepo{db: db}
}

// GetReferralStats returns the total number of distinct referees and total LB rewarded
// for the given referrer account, based on the billing.wallet_transactions ledger.
//
// Each referral reward entry has type='referral_reward' and reference_id='referee:<id>'.
// Counting DISTINCT reference_id gives unique referees even when multiple events fire
// per referee (signup + first_topup + first_subscription).
func (r *ReferralRepo) GetReferralStats(ctx context.Context, referrerAccountID int64) (totalReferrals int, totalRewardedLB float64, err error) {
	var result struct {
		TotalReferrals  int     `gorm:"column:total_referrals"`
		TotalRewardedLB float64 `gorm:"column:total_rewarded_lb"`
	}
	err = r.db.WithContext(ctx).Raw(`
		SELECT
			COUNT(DISTINCT reference_id) AS total_referrals,
			COALESCE(SUM(amount), 0)     AS total_rewarded_lb
		FROM billing.wallet_transactions
		WHERE account_id = ? AND type = 'referral_reward'
	`, referrerAccountID).Scan(&result).Error
	if err != nil {
		return 0, 0, fmt.Errorf("query referral stats: %w", err)
	}
	return result.TotalReferrals, result.TotalRewardedLB, nil
}

// CreateRewardEvent inserts a referral reward event.
// Returns (true, nil) on success, (false, nil) if UNIQUE constraint violated (idempotent).
func (r *ReferralRepo) CreateRewardEvent(ctx context.Context, ev *entity.ReferralRewardEvent) (bool, error) {
	if err := r.db.WithContext(ctx).Create(ev).Error; err != nil {
		// Check for unique violation (PostgreSQL error code 23505).
		if strings.Contains(err.Error(), "uq_referral_reward_event") ||
			strings.Contains(err.Error(), "23505") {
			return false, nil
		}
		return false, fmt.Errorf("insert reward event: %w", err)
	}
	return true, nil
}
