package app

import (
	"context"
	"fmt"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/tracing"
)

// SubscriptionService manages the lifecycle of product subscriptions.
type SubscriptionService struct {
	subs            subscriptionStore
	plans           planStore
	entitlements    *EntitlementService
	gracePeriodDays int
}

func NewSubscriptionService(subs subscriptionStore, plans planStore, ents *EntitlementService, gracePeriodDays int) *SubscriptionService {
	if gracePeriodDays <= 0 {
		gracePeriodDays = 3
	}
	return &SubscriptionService{subs: subs, plans: plans, entitlements: ents, gracePeriodDays: gracePeriodDays}
}

// Activate creates or renews a subscription and syncs entitlements.
func (s *SubscriptionService) Activate(ctx context.Context, accountID int64, productID string, planID int64, paymentMethod, externalSubID string) (*entity.Subscription, error) {
	ctx, span := tracing.Tracer("lurus-platform").Start(ctx, "subscription.activate")
	defer span.End()

	plan, err := s.plans.GetPlanByID(ctx, planID)
	if err != nil || plan == nil {
		return nil, fmt.Errorf("plan %d not found", planID)
	}

	// Expire any existing live subscription
	existing, err := s.subs.GetActive(ctx, accountID, productID)
	if err != nil {
		return nil, fmt.Errorf("check existing: %w", err)
	}
	if existing != nil {
		existing.Status = entity.SubStatusExpired
		if err := s.subs.Update(ctx, existing); err != nil {
			return nil, fmt.Errorf("expire old sub: %w", err)
		}
	}

	now := time.Now().UTC()
	sub := &entity.Subscription{
		AccountID:     accountID,
		ProductID:     productID,
		PlanID:        planID,
		Status:        entity.SubStatusActive,
		StartedAt:     &now,
		PaymentMethod: paymentMethod,
		ExternalSubID: externalSubID,
	}

	// Calculate expiry based on billing cycle
	expiry := calculateExpiry(now, plan.BillingCycle)
	sub.ExpiresAt = expiry

	if err := s.subs.Create(ctx, sub); err != nil {
		return nil, fmt.Errorf("create subscription: %w", err)
	}
	if err := s.entitlements.SyncFromSubscription(ctx, sub); err != nil {
		// Non-fatal: entitlements will be retried asynchronously
		_ = err
	}
	return sub, nil
}

// Expire marks a subscription as expired and enters the grace period.
func (s *SubscriptionService) Expire(ctx context.Context, subID int64) error {
	ctx, span := tracing.Tracer("lurus-platform").Start(ctx, "subscription.expire")
	defer span.End()

	sub, err := s.subs.GetByID(ctx, subID)
	if err != nil || sub == nil {
		return fmt.Errorf("subscription %d not found", subID)
	}
	grace := time.Now().UTC().Add(time.Duration(s.gracePeriodDays) * 24 * time.Hour)
	sub.Status = entity.SubStatusGrace
	sub.GraceUntil = &grace
	if err := s.subs.Update(ctx, sub); err != nil {
		return err
	}
	return nil
}

// EndGrace transitions a grace-period subscription to expired and resets entitlements.
func (s *SubscriptionService) EndGrace(ctx context.Context, subID int64) error {
	ctx, span := tracing.Tracer("lurus-platform").Start(ctx, "subscription.end_grace")
	defer span.End()

	sub, err := s.subs.GetByID(ctx, subID)
	if err != nil || sub == nil {
		return fmt.Errorf("subscription %d not found", subID)
	}
	sub.Status = entity.SubStatusExpired
	if err := s.subs.Update(ctx, sub); err != nil {
		return err
	}
	return s.entitlements.ResetToFree(ctx, sub.AccountID, sub.ProductID)
}

// Cancel disables auto-renew and marks subscription as cancelled.
func (s *SubscriptionService) Cancel(ctx context.Context, accountID int64, productID string) error {
	sub, err := s.subs.GetActive(ctx, accountID, productID)
	if err != nil || sub == nil {
		return fmt.Errorf("no active subscription for product %s", productID)
	}
	sub.Status = entity.SubStatusCancelled
	sub.AutoRenew = false
	return s.subs.Update(ctx, sub)
}

// GetActive returns the live subscription for an account+product.
func (s *SubscriptionService) GetActive(ctx context.Context, accountID int64, productID string) (*entity.Subscription, error) {
	return s.subs.GetActive(ctx, accountID, productID)
}

// ListByAccount returns all subscriptions for an account.
func (s *SubscriptionService) ListByAccount(ctx context.Context, accountID int64) ([]entity.Subscription, error) {
	return s.subs.ListByAccount(ctx, accountID)
}

// GetByID returns a subscription by its ID.
func (s *SubscriptionService) GetByID(ctx context.Context, id int64) (*entity.Subscription, error) {
	return s.subs.GetByID(ctx, id)
}

// UpdateRenewalState persists renewal attempt counter and next retry time.
func (s *SubscriptionService) UpdateRenewalState(ctx context.Context, subID int64, attempts int, nextAt *time.Time) error {
	return s.subs.UpdateRenewalState(ctx, subID, attempts, nextAt)
}

// ListActiveExpired returns active subscriptions past their expires_at.
func (s *SubscriptionService) ListActiveExpired(ctx context.Context) ([]entity.Subscription, error) {
	return s.subs.ListActiveExpired(ctx)
}

// ListGraceExpired returns grace-period subscriptions past their grace_until.
func (s *SubscriptionService) ListGraceExpired(ctx context.Context) ([]entity.Subscription, error) {
	return s.subs.ListGraceExpired(ctx)
}

// calculateExpiry returns the expiry time for a given billing cycle.
// Month-based cycles clamp to the last day of the target month to avoid
// overflow (e.g. Jan 31 + 1 month → Feb 28, not March 3).
func calculateExpiry(from time.Time, cycle string) *time.Time {
	var t time.Time
	switch cycle {
	case entity.BillingCycleWeekly:
		t = from.AddDate(0, 0, 7)
	case entity.BillingCycleMonthly:
		t = addMonthsClamped(from, 1)
	case entity.BillingCycleQuarterly:
		t = addMonthsClamped(from, 3)
	case entity.BillingCycleYearly:
		t = from.AddDate(1, 0, 0)
	case entity.BillingCycleForever, entity.BillingCycleOneTime:
		return nil // no expiry
	default:
		t = addMonthsClamped(from, 1) // default monthly
	}
	return &t
}

// addMonthsClamped adds n months and clamps to the last valid day of the target month.
// Example: Jan 31 + 1 month → Feb 28 (not March 3).
func addMonthsClamped(t time.Time, months int) time.Time {
	y, m, d := t.Date()
	targetMonth := time.Month(int(m) + months)
	targetYear := y
	// Normalize month overflow (e.g. month 13 → Jan next year)
	for targetMonth > 12 {
		targetMonth -= 12
		targetYear++
	}
	// Clamp day to last day of target month
	lastDay := daysInMonth(targetYear, targetMonth)
	if d > lastDay {
		d = lastDay
	}
	return time.Date(targetYear, targetMonth, d, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Location())
}

// daysInMonth returns the number of days in the given month/year.
func daysInMonth(year int, month time.Month) int {
	// time.Date normalises day=0 to the last day of the previous month
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}
