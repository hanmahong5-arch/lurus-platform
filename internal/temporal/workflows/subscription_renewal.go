// Package workflows contains Temporal workflow definitions for lurus-platform.
package workflows

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/event"
	"github.com/hanmahong5-arch/lurus-platform/internal/temporal/activities"
)

// RenewalInput is the input for SubscriptionRenewalWorkflow.
type RenewalInput struct {
	SubscriptionID int64
	AccountID      int64
	ProductID      string
	PlanID         int64
	PaymentMethod  string
	ExternalSubID  string
}

// SubscriptionRenewalWorkflow implements the subscription auto-renewal saga.
//
// Steps:
//  1. GetPlanByID — fetch pricing
//  2. WalletDebit — charge the wallet
//  3. SubscriptionActivate — create new subscription cycle
//  4. ResetRenewalState — clear retry counters
//  5. PublishToNATS — emit renewal_success event
//
// If step 3 fails after step 2, Temporal guarantees the compensation (wallet credit/refund)
// will execute. This eliminates the CRITICAL money-lost scenario in the old cron implementation.
func SubscriptionRenewalWorkflow(ctx workflow.Context, in RenewalInput) error {
	// Activity options for short DB/service calls
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

	// Step 1: Get plan pricing
	var plan activities.GetPlanByIDOutput
	if err := workflow.ExecuteActivity(ctx, "GetPlanByID", in.PlanID).Get(ctx, &plan); err != nil {
		return fmt.Errorf("get plan: %w", err)
	}

	// Step 2: Debit wallet
	orderRef := fmt.Sprintf("renewal:sub:%d", in.SubscriptionID)
	debitIn := activities.DebitInput{
		AccountID: in.AccountID,
		Amount:    plan.PriceCNY,
		TxType:    "subscription_renewal",
		Desc:      fmt.Sprintf("Auto-renewal for plan %s", plan.Code),
		RefType:   "subscription",
		RefID:     orderRef,
		ProductID: in.ProductID,
	}

	var debitOut activities.DebitOutput
	if err := workflow.ExecuteActivity(ctx, "Debit", debitIn).Get(ctx, &debitOut); err != nil {
		// Debit failed (insufficient funds, etc.) — no money moved, safe to return.
		publishRenewalFailedEvent(ctx, in, plan.Code, err.Error())
		return fmt.Errorf("wallet debit: %w", err)
	}

	// --- SAGA COMPENSATION SETUP ---
	// From this point, money has been deducted. If anything fails below,
	// we must refund. Temporal guarantees this compensation executes.
	compensationCtx, _ := workflow.NewDisconnectedContext(ctx)
	compensationCtx = workflow.WithActivityOptions(compensationCtx, actOpts)

	// Step 3: Activate new subscription cycle
	activateIn := activities.ActivateInput{
		AccountID:     in.AccountID,
		ProductID:     in.ProductID,
		PlanID:        in.PlanID,
		PaymentMethod: in.PaymentMethod,
		ExternalSubID: in.ExternalSubID,
	}

	var activateOut activities.ActivateOutput
	if err := workflow.ExecuteActivity(ctx, "Activate", activateIn).Get(ctx, &activateOut); err != nil {
		// Activation failed — COMPENSATE by refunding the debit.
		refundRef := fmt.Sprintf("refund:renewal:sub:%d", in.SubscriptionID)
		creditIn := activities.CreditInput{
			AccountID: in.AccountID,
			Amount:    plan.PriceCNY,
			TxType:    "subscription_renewal_refund",
			Desc:      fmt.Sprintf("Renewal refund: activation failed for plan %s", plan.Code),
			RefType:   "subscription",
			RefID:     refundRef,
			ProductID: in.ProductID,
		}
		// Use disconnected context so compensation runs even if workflow is cancelled.
		if creditErr := workflow.ExecuteActivity(compensationCtx, "Credit", creditIn).Get(compensationCtx, nil); creditErr != nil {
			workflow.GetLogger(ctx).Error("CRITICAL: saga compensation (wallet refund) failed — manual intervention required",
				"subscription_id", in.SubscriptionID,
				"account_id", in.AccountID,
				"amount", plan.PriceCNY,
				"credit_error", creditErr,
				"activate_error", err,
			)
			return temporal.NewNonRetryableApplicationError(
				fmt.Sprintf("activation failed AND refund failed for sub %d, amount %.2f: activate=%v, refund=%v",
					in.SubscriptionID, plan.PriceCNY, err, creditErr),
				"COMPENSATION_FAILED",
				creditErr,
			)
		}

		publishRenewalFailedEvent(ctx, in, plan.Code, err.Error())
		return fmt.Errorf("activate (funds refunded): %w", err)
	}

	// Step 4: Reset renewal state on the original subscription row
	if err := workflow.ExecuteActivity(ctx, "ResetRenewalState", in.SubscriptionID).Get(ctx, nil); err != nil {
		workflow.GetLogger(ctx).Error("reset renewal state failed (non-critical, subscription already activated)",
			"subscription_id", in.SubscriptionID,
			"error", err,
		)
	}

	// Step 5: Publish success event
	publishRenewalSuccessEvent(ctx, in, plan.Code, activateOut)

	return nil
}

// publishRenewalSuccessEvent emits a subscription.activated event to NATS.
func publishRenewalSuccessEvent(ctx workflow.Context, in RenewalInput, planCode string, out activities.ActivateOutput) {
	evIn := activities.PublishEventInput{
		Subject:   event.SubjectSubscriptionActivated,
		AccountID: in.AccountID,
		ProductID: in.ProductID,
		Payload: map[string]any{
			"subscription_id": out.SubscriptionID,
			"plan_id":         in.PlanID,
			"plan_code":       planCode,
			"event":           "renewal_success",
		},
	}
	_ = workflow.ExecuteActivity(ctx, "PublishToNATS", evIn).Get(ctx, nil)
}

// publishRenewalFailedEvent emits a subscription.expired event to NATS.
func publishRenewalFailedEvent(ctx workflow.Context, in RenewalInput, planCode, reason string) {
	evIn := activities.PublishEventInput{
		Subject:   event.SubjectSubscriptionExpired,
		AccountID: in.AccountID,
		ProductID: in.ProductID,
		Payload: map[string]any{
			"subscription_id": in.SubscriptionID,
			"plan_id":         in.PlanID,
			"plan_code":       planCode,
			"event":           "renewal_failed",
			"reason":          reason,
		},
	}
	_ = workflow.ExecuteActivity(ctx, "PublishToNATS", evIn).Get(ctx, nil)
}
