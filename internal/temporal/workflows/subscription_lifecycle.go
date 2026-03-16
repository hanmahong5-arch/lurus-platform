package workflows

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/event"
	"github.com/hanmahong5-arch/lurus-platform/internal/temporal/activities"

)

// LifecycleInput is the input for SubscriptionLifecycleWorkflow.
type LifecycleInput struct {
	SubscriptionID int64
	AccountID      int64
	ProductID      string
	PlanID         int64
	ExpiresAt      time.Time // zero = no expiry (forever plan)
	GraceDays      int
	AutoRenew      bool
	PaymentMethod  string
	ExternalSubID  string
}

// SubscriptionLifecycleWorkflow manages a single subscription's full lifecycle:
//   - Sends reminder emails at 7, 3, 1 days before expiry
//   - On expiry: triggers auto-renewal OR enters grace period
//   - After grace period: permanently expires and resets entitlements
//
// Workflow ID: "lifecycle:{SubscriptionID}" — one per subscription, idempotent start.
// Replaces: cron/expiry.go + cron/notification.go + their Redis locks/dedup.
func SubscriptionLifecycleWorkflow(ctx workflow.Context, in LifecycleInput) error {
	if in.ExpiresAt.IsZero() {
		// Forever plan — no lifecycle management needed.
		return nil
	}

	actOpts := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    2 * time.Minute,
			MaximumAttempts:    5,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, actOpts)

	// --- Phase 1: Reminder emails ---
	reminderDays := []int{7, 3, 1}
	for _, days := range reminderDays {
		reminderTime := in.ExpiresAt.Add(-time.Duration(days) * 24 * time.Hour)
		sleepDur := reminderTime.Sub(workflow.Now(ctx))
		if sleepDur <= 0 {
			continue // already past this reminder point
		}

		if err := workflow.Sleep(ctx, sleepDur); err != nil {
			return err // workflow cancelled
		}

		// Verify subscription is still active before sending reminder.
		sub, err := getSubStatus(ctx, in.SubscriptionID)
		if err != nil || sub.Status != entity.SubStatusActive {
			return nil // cancelled, renewed, or already expired
		}

		_ = workflow.ExecuteActivity(ctx, "SendExpiryReminder", activities.SendReminderInput{
			AccountID:      in.AccountID,
			SubscriptionID: in.SubscriptionID,
			ProductID:      in.ProductID,
			DaysLeft:       days,
			ExpiresAt:      in.ExpiresAt.UTC().Format("2006-01-02 15:04 UTC"),
		}).Get(ctx, nil)
	}

	// --- Phase 2: Wait for expiry ---
	sleepUntilExpiry := in.ExpiresAt.Sub(workflow.Now(ctx))
	if sleepUntilExpiry > 0 {
		if err := workflow.Sleep(ctx, sleepUntilExpiry); err != nil {
			return err
		}
	}

	// Re-check: subscription might have been cancelled or renewed while sleeping.
	sub, err := getSubStatus(ctx, in.SubscriptionID)
	if err != nil {
		return fmt.Errorf("get subscription: %w", err)
	}
	if sub.Status != entity.SubStatusActive {
		return nil // already handled
	}

	// --- Phase 3: Auto-renew or enter grace ---
	if in.AutoRenew {
		// Start renewal workflow as a child with ABANDON policy so it outlives this workflow.
		renewOpts := workflow.ChildWorkflowOptions{
			WorkflowID: fmt.Sprintf("renewal:%d", in.SubscriptionID),
			TaskQueue:  activities.TaskQueue,
		}
		renewCtx := workflow.WithChildOptions(ctx, renewOpts)
		renewFut := workflow.ExecuteChildWorkflow(renewCtx, SubscriptionRenewalWorkflow, RenewalInput{
			SubscriptionID: in.SubscriptionID,
			AccountID:      in.AccountID,
			ProductID:      in.ProductID,
			PlanID:         in.PlanID,
			PaymentMethod:  in.PaymentMethod,
			ExternalSubID:  in.ExternalSubID,
		})

		// Wait for renewal result — if it succeeds, new lifecycle will be started by the new subscription.
		if err := renewFut.Get(ctx, nil); err == nil {
			return nil // renewal succeeded; new subscription has its own lifecycle
		}
		// Renewal failed (e.g. insufficient funds) — fall through to grace period.
	}

	// Enter grace period.
	if err := workflow.ExecuteActivity(ctx, "Expire", in.SubscriptionID).Get(ctx, nil); err != nil {
		return fmt.Errorf("expire subscription: %w", err)
	}
	_ = workflow.ExecuteActivity(ctx, "PublishToNATS", activities.PublishEventInput{
		Subject:   event.SubjectSubscriptionExpired,
		AccountID: in.AccountID,
		ProductID: in.ProductID,
		Payload: map[string]any{
			"subscription_id": in.SubscriptionID,
			"phase":           "grace_entered",
		},
	}).Get(ctx, nil)

	// --- Phase 4: Grace period ---
	graceDuration := time.Duration(in.GraceDays) * 24 * time.Hour
	if err := workflow.Sleep(ctx, graceDuration); err != nil {
		return err
	}

	// Re-check: might have renewed during grace.
	sub, err = getSubStatus(ctx, in.SubscriptionID)
	if err != nil {
		return fmt.Errorf("get subscription after grace: %w", err)
	}
	if sub.Status != entity.SubStatusGrace {
		return nil // renewed during grace
	}

	// Permanent expiry.
	if err := workflow.ExecuteActivity(ctx, "EndGrace", in.SubscriptionID).Get(ctx, nil); err != nil {
		return fmt.Errorf("end grace: %w", err)
	}
	_ = workflow.ExecuteActivity(ctx, "PublishToNATS", activities.PublishEventInput{
		Subject:   event.SubjectSubscriptionExpired,
		AccountID: in.AccountID,
		ProductID: in.ProductID,
		Payload: map[string]any{
			"subscription_id": in.SubscriptionID,
			"phase":           "expired_downgraded",
		},
	}).Get(ctx, nil)

	return nil
}

// getSubStatus is a helper to fetch subscription and return its summary.
func getSubStatus(ctx workflow.Context, subID int64) (*entity.Subscription, error) {
	var sub entity.Subscription
	if err := workflow.ExecuteActivity(ctx, "GetSubscription", subID).Get(ctx, &sub); err != nil {
		return nil, err
	}
	return &sub, nil
}
