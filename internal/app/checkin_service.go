package app

import (
	"context"
	"errors"
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

// CheckinHookFunc is called after a successful check-in to trigger notifications.
type CheckinHookFunc func(ctx context.Context, accountID int64, streak int)

// CheckinService orchestrates daily check-in operations.
type CheckinService struct {
	checkins    checkinStore
	wallets     walletStore
	onCheckin   CheckinHookFunc
}

// NewCheckinService creates a new CheckinService.
func NewCheckinService(checkins checkinStore, wallets walletStore) *CheckinService {
	return &CheckinService{checkins: checkins, wallets: wallets}
}

// SetOnCheckinHook sets the post-checkin hook (typically wired to module.Registry.FireCheckin).
func (s *CheckinService) SetOnCheckinHook(fn CheckinHookFunc) {
	s.onCheckin = fn
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

// ErrCheckinAlreadyToday is the typed error every layer (repo / service /
// handler) returns when the (account_id, checkin_date) unique constraint
// rejects an INSERT. Caller branches on errors.Is to render a friendly
// message instead of a DB-level "duplicate key" surface.
//
// Defined here (not in a separate errors.go) because it's the ONLY
// app-layer sentinel anyone needs from this service — premature
// generalisation would just add files for no caller win.
var ErrCheckinAlreadyToday = errors.New("checkin: already checked in today")

// DoCheckin performs a daily check-in for the account.
//
// Concurrency model: the previous read-then-write pattern (GetByAccountAndDate
// then Create) was a TOCTOU race — two concurrent callers would BOTH pass
// the read check then both attempt Create, and only the DB-level unique
// constraint kept us from double-crediting the wallet. The race window
// was small but real and caught by TestCheckinService_DoCheckin_Concurrent.
//
// Fixed by collapsing into a single atomic insert with ON CONFLICT DO
// NOTHING on (account_id, checkin_date). The repo signals "the row was
// already there" via ErrCheckinAlreadyToday. We branch on that and
// return cleanly WITHOUT crediting the wallet — eliminating the
// double-credit class of bugs.
//
// Note: CountConsecutive runs BEFORE the insert. That's safe because the
// streak count is monotonic up to today's insertion — a concurrent
// successful insert by another goroutine for the SAME account would mean
// only one of us wins the OnConflict race, and we'd return early on the
// loser side without crediting. The winner sees a valid (read-then-insert)
// streak count for itself. (Different accounts are never racing on the
// same row.)
func (s *CheckinService) DoCheckin(ctx context.Context, accountID int64) (*CheckinResult, error) {
	today := time.Now().Format("2006-01-02")

	// Count consecutive days (including yesterday). Reads only — concurrent
	// callers reading the same value is fine.
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	consecutiveDays, err := s.checkins.CountConsecutive(ctx, accountID, yesterday)
	if err != nil {
		return nil, fmt.Errorf("checkin: count consecutive: %w", err)
	}
	// Today's checkin makes it consecutive+1.
	consecutiveDays++

	reward := calculateCheckinReward(consecutiveDays)

	checkin := &entity.Checkin{
		AccountID:   accountID,
		CheckinDate: today,
		RewardType:  "credits",
		RewardValue: reward,
	}
	// Atomic insert-or-noop. Repo returns ErrCheckinAlreadyToday when
	// the row already existed (DB unique constraint enforced).
	if err := s.checkins.Create(ctx, checkin); err != nil {
		if errors.Is(err, ErrCheckinAlreadyToday) {
			return nil, ErrCheckinAlreadyToday
		}
		return nil, fmt.Errorf("checkin: create record: %w", err)
	}

	// We won the race for today. Credit the wallet. (Concurrent caller
	// for the same account would have returned ErrCheckinAlreadyToday
	// above and never reached here, so no double-credit.)
	tx, err := s.wallets.Credit(ctx, accountID, reward,
		entity.TxTypeCheckinReward,
		fmt.Sprintf("Daily checkin reward (day %d streak)", consecutiveDays),
		"checkin", fmt.Sprintf("%d", checkin.ID), "")
	if err != nil {
		return nil, fmt.Errorf("checkin: credit wallet: %w", err)
	}

	// Fire post-checkin hooks (notification, etc.) — non-blocking.
	if s.onCheckin != nil {
		go s.onCheckin(ctx, accountID, consecutiveDays)
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
