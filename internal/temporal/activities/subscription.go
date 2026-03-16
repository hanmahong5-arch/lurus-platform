// Package activities provides Temporal activity implementations that wrap
// existing lurus-platform service methods.
package activities

import (
	"context"
	"fmt"

	"github.com/hanmahong5-arch/lurus-platform/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

const (
	// TaskQueue is the single task queue used by all lurus-platform workflows and activities.
	TaskQueue = "lurus-platform-tasks"
)

// SubscriptionActivities wraps SubscriptionService for Temporal.
type SubscriptionActivities struct {
	Subs *app.SubscriptionService
}

// ActivateInput is the input for the Activate activity.
type ActivateInput struct {
	AccountID     int64
	ProductID     string
	PlanID        int64
	PaymentMethod string
	ExternalSubID string
}

// ActivateOutput is the result of a successful activation.
type ActivateOutput struct {
	SubscriptionID int64
	ExpiresAt      string // RFC3339
	PlanCode       string
}

// Activate creates or renews a subscription. Wraps SubscriptionService.Activate.
func (a *SubscriptionActivities) Activate(ctx context.Context, in ActivateInput) (*ActivateOutput, error) {
	sub, err := a.Subs.Activate(ctx, in.AccountID, in.ProductID, in.PlanID, in.PaymentMethod, in.ExternalSubID)
	if err != nil {
		return nil, fmt.Errorf("activate subscription: %w", err)
	}
	out := &ActivateOutput{SubscriptionID: sub.ID}
	if sub.ExpiresAt != nil {
		out.ExpiresAt = sub.ExpiresAt.Format("2006-01-02T15:04:05Z07:00")
	}
	return out, nil
}

// GetSubscription fetches a subscription by ID.
func (a *SubscriptionActivities) GetSubscription(ctx context.Context, subID int64) (*entity.Subscription, error) {
	return a.Subs.GetByID(ctx, subID)
}

// ResetRenewalState resets renewal_attempts and next_renewal_at after successful renewal.
func (a *SubscriptionActivities) ResetRenewalState(ctx context.Context, subID int64) error {
	return a.Subs.UpdateRenewalState(ctx, subID, 0, nil)
}

// Expire transitions a subscription from active to grace period.
func (a *SubscriptionActivities) Expire(ctx context.Context, subID int64) error {
	return a.Subs.Expire(ctx, subID)
}

// EndGrace transitions a grace-period subscription to expired and resets entitlements.
func (a *SubscriptionActivities) EndGrace(ctx context.Context, subID int64) error {
	return a.Subs.EndGrace(ctx, subID)
}

// SubscriptionSummary is a serializable subset of entity.Subscription for workflow use.
type SubscriptionSummary struct {
	ID        int64
	AccountID int64
	ProductID string
	PlanID    int64
	Status    string
	AutoRenew bool
}

// ListActiveExpired returns active subscriptions past their expires_at.
func (a *SubscriptionActivities) ListActiveExpired(ctx context.Context) ([]SubscriptionSummary, error) {
	subs, err := a.Subs.ListActiveExpired(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active expired: %w", err)
	}
	return toSummaries(subs), nil
}

// ListGraceExpired returns grace-period subscriptions past their grace_until.
func (a *SubscriptionActivities) ListGraceExpired(ctx context.Context) ([]SubscriptionSummary, error) {
	subs, err := a.Subs.ListGraceExpired(ctx)
	if err != nil {
		return nil, fmt.Errorf("list grace expired: %w", err)
	}
	return toSummaries(subs), nil
}

func toSummaries(subs []entity.Subscription) []SubscriptionSummary {
	out := make([]SubscriptionSummary, len(subs))
	for i, s := range subs {
		out[i] = SubscriptionSummary{
			ID: s.ID, AccountID: s.AccountID, ProductID: s.ProductID,
			PlanID: s.PlanID, Status: s.Status, AutoRenew: s.AutoRenew,
		}
	}
	return out
}
