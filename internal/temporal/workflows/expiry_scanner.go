package workflows

import (
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/event"
	"github.com/hanmahong5-arch/lurus-platform/internal/temporal/activities"

)

// ExpiryScannerWorkflow is a migration bridge that catches up pre-Temporal
// subscriptions that don't have lifecycle workflows.
//
// Intended to run as a Temporal Cron Schedule (every 1 hour).
// Once all old subscriptions have expired, this scanner becomes a no-op.
//
// Also handles stale pending order cleanup (previously in cron/expiry.go Phase 3).
func ExpiryScannerWorkflow(ctx workflow.Context) error {
	actOpts := workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, actOpts)

	// Phase 1: Active subscriptions past their expires_at.
	// Start lifecycle workflows for each (idempotent via workflow ID).
	var activeExpired []activities.SubscriptionSummary
	if err := workflow.ExecuteActivity(ctx, "ListActiveExpired").Get(ctx, &activeExpired); err != nil {
		// Non-fatal: log via activity error, continue to next phase.
		_ = err
	} else {
		for _, sub := range activeExpired {
			startLifecycleForSub(ctx, sub, 3) // default grace days
		}
	}

	// Phase 2: Grace-period subscriptions past their grace_until.
	// These don't need lifecycle workflows — just expire them directly.
	var graceExpired []activities.SubscriptionSummary
	if err := workflow.ExecuteActivity(ctx, "ListGraceExpired").Get(ctx, &graceExpired); err != nil {
		_ = err
	} else {
		for _, sub := range graceExpired {
			_ = workflow.ExecuteActivity(ctx, "EndGrace", sub.ID).Get(ctx, nil)
			_ = workflow.ExecuteActivity(ctx, "PublishToNATS", activities.PublishEventInput{
				Subject:   event.SubjectSubscriptionExpired,
				AccountID: sub.AccountID,
				ProductID: sub.ProductID,
				Payload: map[string]any{
					"subscription_id": sub.ID,
					"phase":           "expired_downgraded",
				},
			}).Get(ctx, nil)
		}
	}

	// Phase 3: Expire stale pending payment orders (>24h).
	_ = workflow.ExecuteActivity(ctx, "ExpireStalePendingOrders").Get(ctx, nil)

	// Phase 4: Expire stale pre-authorizations past their deadline and unfreeze held balance.
	_ = workflow.ExecuteActivity(ctx, "ExpireStalePreAuths").Get(ctx, nil)

	return nil
}

// startLifecycleForSub starts a SubscriptionLifecycleWorkflow for a subscription
// that was already past its expiry (catch-up mode). The lifecycle workflow will
// immediately proceed to grace/expiry since ExpiresAt is in the past.
func startLifecycleForSub(ctx workflow.Context, sub activities.SubscriptionSummary, graceDays int) {
	childOpts := workflow.ChildWorkflowOptions{
		WorkflowID: "lifecycle:" + itoa(sub.ID),
		TaskQueue:  activities.TaskQueue,
	}
	childCtx := workflow.WithChildOptions(ctx, childOpts)
	// Fire and forget — don't wait for lifecycle completion.
	workflow.ExecuteChildWorkflow(childCtx, SubscriptionLifecycleWorkflow, LifecycleInput{
		SubscriptionID: sub.ID,
		AccountID:      sub.AccountID,
		ProductID:      sub.ProductID,
		PlanID:         sub.PlanID,
		ExpiresAt:      time.Now(), // already expired; lifecycle will skip timers and proceed immediately
		GraceDays:      graceDays,
		AutoRenew:      sub.AutoRenew,
	})
}

// itoa is a simple int64 to string without importing strconv in workflow code.
func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
