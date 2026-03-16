package workflows

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/event"
	"github.com/hanmahong5-arch/lurus-platform/internal/temporal/activities"

)

// PaymentInput is the input for PaymentCompletionWorkflow.
type PaymentInput struct {
	OrderNo  string
	Provider string // "epay" | "stripe" | "creem"
}

// PaymentCompletionWorkflow handles post-payment processing:
//  1. MarkOrderPaid — transition order pending→paid (idempotent)
//  2. If subscription: SubscriptionActivate
//  3. PublishToNATS — emit completion event
//
// Workflow ID: "payment:{OrderNo}" ensures double-webhook = single execution.
// If activation fails after payment, Temporal retries until success or
// human intervention via Temporal UI.
func PaymentCompletionWorkflow(ctx workflow.Context, in PaymentInput) error {
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

	// Step 1: Mark order as paid (idempotent — safe on retry)
	var order activities.MarkOrderPaidOutput
	if err := workflow.ExecuteActivity(ctx, "MarkOrderPaid", in.OrderNo).Get(ctx, &order); err != nil {
		return fmt.Errorf("mark order paid: %w", err)
	}

	// Step 2: Handle based on order type
	switch order.OrderType {
	case "subscription":
		if order.PlanID == 0 || order.ProductID == "" {
			// Subscription order without plan/product — nothing to activate
			break
		}
		activateIn := activities.ActivateInput{
			AccountID:     order.AccountID,
			ProductID:     order.ProductID,
			PlanID:        order.PlanID,
			PaymentMethod: order.PaymentMethod,
			ExternalSubID: order.ExternalID,
		}
		var activateOut activities.ActivateOutput
		if err := workflow.ExecuteActivity(ctx, "Activate", activateIn).Get(ctx, &activateOut); err != nil {
			return fmt.Errorf("activate subscription: %w", err)
		}

		// Publish subscription activated event
		evIn := activities.PublishEventInput{
			Subject:   event.SubjectSubscriptionActivated,
			AccountID: order.AccountID,
			ProductID: order.ProductID,
			Payload: map[string]any{
				"subscription_id": activateOut.SubscriptionID,
				"plan_id":         order.PlanID,
				"plan_code":       activateOut.PlanCode,
				"expires_at":      activateOut.ExpiresAt,
				"payment_method":  order.PaymentMethod,
				"provider":        in.Provider,
			},
		}
		_ = workflow.ExecuteActivity(ctx, "PublishToNATS", evIn).Get(ctx, nil)

		// Start lifecycle workflow for the new subscription (fire and forget).
		if activateOut.ExpiresAt != "" {
			expiresAt, _ := time.Parse(time.RFC3339, activateOut.ExpiresAt)
			if !expiresAt.IsZero() {
				lifecycleOpts := workflow.ChildWorkflowOptions{
					WorkflowID: fmt.Sprintf("lifecycle:%d", activateOut.SubscriptionID),
					TaskQueue:  activities.TaskQueue,
				}
				lifecycleCtx := workflow.WithChildOptions(ctx, lifecycleOpts)
				workflow.ExecuteChildWorkflow(lifecycleCtx, SubscriptionLifecycleWorkflow, LifecycleInput{
					SubscriptionID: activateOut.SubscriptionID,
					AccountID:      order.AccountID,
					ProductID:      order.ProductID,
					PlanID:         order.PlanID,
					ExpiresAt:      expiresAt,
					GraceDays:      3, // default; matches GRACE_PERIOD_DAYS
					AutoRenew:      false,
					PaymentMethod:  order.PaymentMethod,
					ExternalSubID:  order.ExternalID,
				})
			}
		}

	case "topup":
		// Topup wallet credit already handled inside MarkOrderPaid.
		// Just publish the event.
		evIn := activities.PublishEventInput{
			Subject:   event.SubjectTopupCompleted,
			AccountID: order.AccountID,
			Payload: map[string]any{
				"order_no":      order.OrderNo,
				"amount_cny":    order.AmountCNY,
				"credits_added": order.AmountCNY,
				"provider":      in.Provider,
			},
		}
		_ = workflow.ExecuteActivity(ctx, "PublishToNATS", evIn).Get(ctx, nil)
	}

	return nil
}
