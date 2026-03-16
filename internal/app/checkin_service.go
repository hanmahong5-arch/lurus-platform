package app

import (
	"context"
	"fmt"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// Checkin reward configuration (in LB = CNY).
const (
	checkinBaseReward       = 0.10 // Base daily reward: 0.10 LB
	checkinStreakBonusStep   = 7    // Every 7 consecutive days, multiply reward
	checkinStreakBonusFactor = 1.5  // 1.5x multiplier for each 7-day streak tier
	checkinMaxReward        = 1.0  // Maximum daily reward cap
)

// checkinStore defines persistence operations for check-ins.
type checkinStore interface {
	Create(ctx context.Context, c *entity.Checkin) error
	GetByAccountAndDate(ctx context.Context, accountID int64, date string) (*entity.Checkin, error)
	ListByAccountAndMonth(ctx context.Context, accountID int64, yearMonth string) ([]entity.Checkin, error)
	CountConsecutive(ctx context.Context, accountID int64, date string) (int, error)
}

// CheckinStatus represents the current checkin status for a user.
type CheckinStatus struct {
	CheckedInToday  bool              `json:"checked_in_today"`
	ConsecutiveDays int               `json:"consecutive_days"`
	MonthCheckins   []entity.Checkin  `json:"month_checkins"`
}

// CheckinResult represents the result of a successful checkin.
type CheckinResult struct {
	RewardValue     float64 `json:"reward_value"`
	RewardType      string  `json:"reward_type"`
	ConsecutiveDays int     `json:"consecutive_days"`
	WalletBalance   float64 `json:"wallet_balance"`
}

// CheckinService orchestrates daily check-in operations.
type CheckinService struct {
	checkins checkinStore
	wallets  walletStore
}

// NewCheckinService creates a new CheckinService.
func NewCheckinService(checkins checkinStore, wallets walletStore) *CheckinService {
	return &CheckinService{checkins: checkins, wallets: wallets}
}

// GetStatus returns the check-in status for the current month.
func (s *CheckinService) GetStatus(ctx context.Context, accountID int64) (*CheckinStatus, error) {
	now := time.Now()
	today := now.Format("2006-01-02")
	yearMonth := now.Format("2006-01")

	// Check if already checked in today.
	todayCheckin, err := s.checkins.GetByAccountAndDate(ctx, accountID, today)
	if err != nil {
		return nil, fmt.Errorf("checkin status: lookup today: %w", err)
	}

	// Get consecutive days (up to yesterday if not checked in today, or today if checked in).
	var consecutiveDays int
	if todayCheckin != nil {
		consecutiveDays, err = s.checkins.CountConsecutive(ctx, accountID, today)
	} else {
		yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")
		consecutiveDays, err = s.checkins.CountConsecutive(ctx, accountID, yesterday)
	}
	if err != nil {
		return nil, fmt.Errorf("checkin status: count consecutive: %w", err)
	}

	// Get all checkins for the current month.
	monthCheckins, err := s.checkins.ListByAccountAndMonth(ctx, accountID, yearMonth)
	if err != nil {
		return nil, fmt.Errorf("checkin status: list month: %w", err)
	}

	return &CheckinStatus{
		CheckedInToday:  todayCheckin != nil,
		ConsecutiveDays: consecutiveDays,
		MonthCheckins:   monthCheckins,
	}, nil
}

// DoCheckin performs a daily check-in for the account.
func (s *CheckinService) DoCheckin(ctx context.Context, accountID int64) (*CheckinResult, error) {
	today := time.Now().Format("2006-01-02")

	// Check if already checked in today.
	existing, err := s.checkins.GetByAccountAndDate(ctx, accountID, today)
	if err != nil {
		return nil, fmt.Errorf("checkin: lookup today: %w", err)
	}
	if existing != nil {
		return nil, fmt.Errorf("checkin: already checked in today")
	}

	// Count consecutive days (including yesterday).
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	consecutiveDays, err := s.checkins.CountConsecutive(ctx, accountID, yesterday)
	if err != nil {
		return nil, fmt.Errorf("checkin: count consecutive: %w", err)
	}
	// Today's checkin makes it consecutive+1.
	consecutiveDays++

	// Calculate reward based on streak.
	reward := calculateCheckinReward(consecutiveDays)

	// Create checkin record.
	checkin := &entity.Checkin{
		AccountID:   accountID,
		CheckinDate: today,
		RewardType:  "credits",
		RewardValue: reward,
	}
	if err := s.checkins.Create(ctx, checkin); err != nil {
		return nil, fmt.Errorf("checkin: create record: %w", err)
	}

	// Credit wallet.
	tx, err := s.wallets.Credit(ctx, accountID, reward,
		entity.TxTypeCheckinReward,
		fmt.Sprintf("Daily checkin reward (day %d streak)", consecutiveDays),
		"checkin", fmt.Sprintf("%d", checkin.ID), "")
	if err != nil {
		return nil, fmt.Errorf("checkin: credit wallet: %w", err)
	}

	return &CheckinResult{
		RewardValue:     reward,
		RewardType:      "credits",
		ConsecutiveDays: consecutiveDays,
		WalletBalance:   tx.BalanceAfter,
	}, nil
}

// calculateCheckinReward computes the reward based on consecutive days.
func calculateCheckinReward(consecutiveDays int) float64 {
	// Base reward with streak multiplier.
	streakTier := consecutiveDays / checkinStreakBonusStep
	multiplier := 1.0
	for i := 0; i < streakTier; i++ {
		multiplier *= checkinStreakBonusFactor
	}
	reward := checkinBaseReward * multiplier

	// Cap at maximum.
	if reward > checkinMaxReward {
		reward = checkinMaxReward
	}
	return reward
}
