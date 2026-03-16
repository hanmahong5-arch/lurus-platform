package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

const (
	// bulkGenerateMaxCount is the maximum number of redemption codes per batch request.
	bulkGenerateMaxCount = 1000
	// codeLength is the number of random bytes used to produce each 8-char code.
	codeLength = 4
)

// Referral reward rates (in LB = CNY).
// Based on target paid CAC ≤ ¥90:
//   - Pro annual LTV: ¥199×12 = ¥2388; 6-month retention LTV ≈ ¥716
//   - Max referral cost target: 12% of 6-month LTV = ¥86
//   - 5+10+30+(199×5%×6) ≈ 104 LB per successful Pro annual referral
const (
	RewardSignup            = 5.0
	RewardFirstTopup        = 10.0
	RewardFirstSubscription = 30.0 // Raised from 20 — justification above
	RewardRenewalRate       = 0.05 // 5% royalty on subscription amount for first 6 renewals
)

// ReferralService processes referral chain reward events, stats queries, and bulk code generation.
type ReferralService struct {
	accounts     accountStore
	wallets      walletStore
	redemptions  redemptionCodeStore
	stats        referralStatsStore  // optional; nil when not wired
	rewardEvents rewardEventStore    // optional; nil when not wired (dedup disabled)
}

// NewReferralService creates a ReferralService without bulk-code support (legacy path).
func NewReferralService(accounts accountStore, wallets walletStore) *ReferralService {
	return &ReferralService{accounts: accounts, wallets: wallets}
}

// NewReferralServiceWithCodes creates a ReferralService with bulk-code support.
func NewReferralServiceWithCodes(accounts accountStore, wallets walletStore, redemptions redemptionCodeStore) *ReferralService {
	return &ReferralService{accounts: accounts, wallets: wallets, redemptions: redemptions}
}

// WithStats attaches a stats store to the service and returns it for chaining.
func (s *ReferralService) WithStats(stats referralStatsStore) *ReferralService {
	s.stats = stats
	return s
}

// WithRewardEvents attaches a reward event store for deduplication and returns for chaining.
func (s *ReferralService) WithRewardEvents(store rewardEventStore) *ReferralService {
	s.rewardEvents = store
	return s
}

// GetStats returns aggregated referral statistics for a referrer account.
// Returns (0, 0, nil) when no stats store is configured.
func (s *ReferralService) GetStats(ctx context.Context, referrerAccountID int64) (totalReferrals int, totalRewardedLB float64, err error) {
	if s.stats == nil {
		return 0, 0, nil
	}
	return s.stats.GetReferralStats(ctx, referrerAccountID)
}

// OnSignup awards a signup bonus to the referrer.
func (s *ReferralService) OnSignup(ctx context.Context, refereeID, referrerID int64) error {
	return s.reward(ctx, referrerID, refereeID, entity.ReferralEventSignup, RewardSignup)
}

// OnFirstTopup awards a topup bonus to the referrer.
func (s *ReferralService) OnFirstTopup(ctx context.Context, refereeID, referrerID int64) error {
	return s.reward(ctx, referrerID, refereeID, entity.ReferralEventFirstTopup, RewardFirstTopup)
}

// OnFirstSubscription awards a subscription bonus to the referrer.
func (s *ReferralService) OnFirstSubscription(ctx context.Context, refereeID, referrerID int64) error {
	return s.reward(ctx, referrerID, refereeID, entity.ReferralEventFirstSubscription, RewardFirstSubscription)
}

// OnRenewal awards renewal royalty to the referrer (5% of subscription amount, capped at 6 renewals).
// renewalCount is the current renewal number (1-indexed). No reward issued after 6 renewals.
// Uses "renewal_royalty_N" as eventType so each round gets its own UNIQUE slot.
func (s *ReferralService) OnRenewal(ctx context.Context, refereeID, referrerID int64, amountCNY float64, renewalCount int) error {
	if renewalCount > 6 {
		return nil // Royalty window exhausted
	}
	rewardLB := amountCNY * RewardRenewalRate
	if rewardLB <= 0 {
		return nil
	}
	eventType := fmt.Sprintf("renewal_royalty_%d", renewalCount)
	return s.reward(ctx, referrerID, refereeID, eventType, rewardLB)
}

// BulkGenerateCodes generates count unique redemption codes in a single batch.
// count must be in [1, 1000]. Each code is 8 uppercase alphanumeric characters.
// The returned slice is in the same order as the generated codes.
func (s *ReferralService) BulkGenerateCodes(
	ctx context.Context,
	productID, planCode string,
	durationDays int,
	expiresAt *time.Time,
	notes string,
	count int,
) ([]entity.RedemptionCode, error) {
	if count < 1 || count > bulkGenerateMaxCount {
		return nil, fmt.Errorf("count must be between 1 and %d, got %d", bulkGenerateMaxCount, count)
	}
	if s.redemptions == nil {
		return nil, fmt.Errorf("redemption code store not configured")
	}

	codes := make([]entity.RedemptionCode, 0, count)
	seen := make(map[string]struct{}, count)

	for len(codes) < count {
		code, err := generateCode()
		if err != nil {
			return nil, fmt.Errorf("generate code: %w", err)
		}
		if _, dup := seen[code]; dup {
			continue // retry on collision
		}
		seen[code] = struct{}{}
		rc := entity.RedemptionCode{
			Code:         code,
			ProductID:    productID,
			RewardType:   "subscription_trial",
			RewardValue:  float64(durationDays),
			MaxUses:      1,
			ExpiresAt:    expiresAt,
			BatchID:      "",
			RewardMetadata: []byte(`{"plan_code":"` + planCode + `","duration_days":` + fmt.Sprintf("%d", durationDays) + `}`),
		}
		// notes stored in description field via RewardMetadata or via Code notes workaround;
		// entity has no Notes field, so we embed notes into BatchID as a readable prefix.
		if notes != "" {
			rc.BatchID = notes
		}
		codes = append(codes, rc)
	}

	if err := s.redemptions.BulkCreate(ctx, codes); err != nil {
		return nil, fmt.Errorf("bulk create codes: %w", err)
	}
	return codes, nil
}

// generateCode returns a random 8-character uppercase alphanumeric code.
func generateCode() (string, error) {
	b := make([]byte, codeLength)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("rand read: %w", err)
	}
	raw := strings.ToUpper(hex.EncodeToString(b)) // 8 hex chars
	return raw, nil
}

func (s *ReferralService) reward(ctx context.Context, referrerID, refereeID int64, eventType string, amount float64) error {
	referrer, err := s.accounts.GetByID(ctx, referrerID)
	if err != nil || referrer == nil {
		return fmt.Errorf("referrer %d not found", referrerID)
	}

	// Dedup via UNIQUE constraint: insert event first, skip if duplicate.
	if s.rewardEvents != nil {
		ev := &entity.ReferralRewardEvent{
			ReferrerID:    referrerID,
			RefereeID:     refereeID,
			EventType:     eventType,
			RewardCredits: amount,
			Status:        "credited",
		}
		created, err := s.rewardEvents.CreateRewardEvent(ctx, ev)
		if err != nil {
			return fmt.Errorf("record reward event: %w", err)
		}
		if !created {
			return nil // Duplicate reward, skip idempotently.
		}
	}

	if _, err := s.wallets.GetOrCreate(ctx, referrerID); err != nil {
		return fmt.Errorf("ensure referrer wallet: %w", err)
	}
	_, err = s.wallets.Credit(ctx, referrerID, amount,
		entity.TxTypeReferralReward,
		fmt.Sprintf("推荐奖励 — %s", eventType),
		"referral_event",
		fmt.Sprintf("referee:%d", refereeID),
		"")
	return err
}
