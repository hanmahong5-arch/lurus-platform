package workflows

import (
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"github.com/hanmahong5-arch/lurus-platform/internal/temporal/activities"
)

// setup creates a test workflow environment with all activities and workflows registered.
func setup() *testsuite.TestWorkflowEnvironment {
	s := testsuite.WorkflowTestSuite{}
	env := s.NewTestWorkflowEnvironment()
	env.RegisterActivity(&activities.SubscriptionActivities{})
	env.RegisterActivity(&activities.WalletActivities{})
	env.RegisterActivity(&activities.EventActivities{})
	env.RegisterActivity(&activities.QueryActivities{})
	env.RegisterActivity(&activities.NotificationActivities{})
	env.RegisterWorkflow(SubscriptionRenewalWorkflow)
	env.RegisterWorkflow(PaymentCompletionWorkflow)
	env.RegisterWorkflow(SubscriptionLifecycleWorkflow)
	env.RegisterWorkflow(ExpiryScannerWorkflow)
	return env
}

// --- SubscriptionRenewalWorkflow ---

func TestSubscriptionRenewalWorkflow_HappyPath(t *testing.T) {
	env := setup()

	env.OnActivity("GetPlanByID", mock.Anything, int64(10)).Return(&activities.GetPlanByIDOutput{
		PlanID: 10, Code: "pro-monthly", PriceCNY: 29.9,
	}, nil)
	env.OnActivity("Debit", mock.Anything, mock.MatchedBy(func(in activities.DebitInput) bool {
		return in.AccountID == 1 && in.Amount == 29.9
	})).Return(&activities.DebitOutput{TransactionID: 100}, nil)
	env.OnActivity("Activate", mock.Anything, mock.MatchedBy(func(in activities.ActivateInput) bool {
		return in.AccountID == 1 && in.PlanID == 10
	})).Return(&activities.ActivateOutput{SubscriptionID: 50, ExpiresAt: "2026-04-17T00:00:00Z", PlanCode: "pro-monthly"}, nil)
	env.OnActivity("ResetRenewalState", mock.Anything, int64(5)).Return(nil)
	env.OnActivity("PublishToNATS", mock.Anything, mock.Anything).Return(nil)

	env.ExecuteWorkflow(SubscriptionRenewalWorkflow, RenewalInput{
		SubscriptionID: 5, AccountID: 1, ProductID: "lucrum",
		PlanID: 10, PaymentMethod: "wallet",
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
}

func TestSubscriptionRenewalWorkflow_DebitFails(t *testing.T) {
	env := setup()

	env.OnActivity("GetPlanByID", mock.Anything, mock.Anything).Return(&activities.GetPlanByIDOutput{
		PlanID: 10, Code: "pro", PriceCNY: 29.9,
	}, nil)
	env.OnActivity("Debit", mock.Anything, mock.Anything).Return(nil, errInsufficientFunds)
	env.OnActivity("PublishToNATS", mock.Anything, mock.Anything).Return(nil)

	env.ExecuteWorkflow(SubscriptionRenewalWorkflow, RenewalInput{
		SubscriptionID: 5, AccountID: 1, ProductID: "lucrum", PlanID: 10,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
	require.Contains(t, env.GetWorkflowError().Error(), "wallet debit")
}

func TestSubscriptionRenewalWorkflow_ActivateFails_Refund(t *testing.T) {
	env := setup()

	env.OnActivity("GetPlanByID", mock.Anything, mock.Anything).Return(&activities.GetPlanByIDOutput{
		PlanID: 10, Code: "pro", PriceCNY: 29.9,
	}, nil)
	env.OnActivity("Debit", mock.Anything, mock.Anything).Return(&activities.DebitOutput{TransactionID: 100}, nil)
	env.OnActivity("Activate", mock.Anything, mock.Anything).Return(nil, errActivationFailed)
	env.OnActivity("Credit", mock.Anything, mock.MatchedBy(func(in activities.CreditInput) bool {
		return in.Amount == 29.9 && in.TxType == "subscription_renewal_refund"
	})).Return(nil)
	env.OnActivity("PublishToNATS", mock.Anything, mock.Anything).Return(nil)

	env.ExecuteWorkflow(SubscriptionRenewalWorkflow, RenewalInput{
		SubscriptionID: 5, AccountID: 1, ProductID: "lucrum", PlanID: 10,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
	require.Contains(t, env.GetWorkflowError().Error(), "funds refunded")
}

// --- PaymentCompletionWorkflow ---

func TestPaymentCompletionWorkflow_Topup(t *testing.T) {
	env := setup()

	env.OnActivity("MarkOrderPaid", mock.Anything, "ORD-002").Return(&activities.MarkOrderPaidOutput{
		OrderNo: "ORD-002", AccountID: 2, OrderType: "topup",
		AmountCNY: 100.0, PaymentMethod: "epay",
	}, nil)
	env.OnActivity("PublishToNATS", mock.Anything, mock.Anything).Return(nil)

	env.ExecuteWorkflow(PaymentCompletionWorkflow, PaymentInput{
		OrderNo: "ORD-002", Provider: "epay",
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
}

func TestPaymentCompletionWorkflow_SubscriptionNoPlan(t *testing.T) {
	env := setup()

	env.OnActivity("MarkOrderPaid", mock.Anything, "ORD-003").Return(&activities.MarkOrderPaidOutput{
		OrderNo: "ORD-003", AccountID: 3, OrderType: "subscription",
		ProductID: "", PlanID: 0,
	}, nil)

	env.ExecuteWorkflow(PaymentCompletionWorkflow, PaymentInput{
		OrderNo: "ORD-003", Provider: "creem",
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
}

func TestPaymentCompletionWorkflow_Subscription(t *testing.T) {
	env := setup()

	env.OnActivity("MarkOrderPaid", mock.Anything, "ORD-001").Return(&activities.MarkOrderPaidOutput{
		OrderNo: "ORD-001", AccountID: 1, OrderType: "subscription",
		ProductID: "lucrum", PlanID: 10, AmountCNY: 29.9,
		PaymentMethod: "stripe", ExternalID: "sub_ext_1",
	}, nil)
	env.OnActivity("Activate", mock.Anything, mock.Anything).Return(&activities.ActivateOutput{
		SubscriptionID: 50, ExpiresAt: "2026-04-17T00:00:00Z", PlanCode: "pro",
	}, nil)
	env.OnActivity("PublishToNATS", mock.Anything, mock.Anything).Return(nil)
	// Child lifecycle workflow will run — mock its activities
	env.OnActivity("GetSubscription", mock.Anything, mock.Anything).Return(&entity.Subscription{
		ID: 50, Status: entity.SubStatusActive,
	}, nil).Maybe()
	env.OnActivity("SendExpiryReminder", mock.Anything, mock.Anything).Return(nil).Maybe()
	env.OnActivity("Expire", mock.Anything, mock.Anything).Return(nil).Maybe()
	env.OnActivity("EndGrace", mock.Anything, mock.Anything).Return(nil).Maybe()

	env.ExecuteWorkflow(PaymentCompletionWorkflow, PaymentInput{
		OrderNo: "ORD-001", Provider: "stripe",
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
}

// --- SubscriptionLifecycleWorkflow ---

func TestSubscriptionLifecycleWorkflow_ForeverPlan(t *testing.T) {
	env := setup()

	env.ExecuteWorkflow(SubscriptionLifecycleWorkflow, LifecycleInput{
		SubscriptionID: 1, AccountID: 1, ProductID: "lucrum",
		ExpiresAt: time.Time{},
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
}

func TestSubscriptionLifecycleWorkflow_AlreadyCancelled(t *testing.T) {
	env := setup()

	expiresAt := time.Now().Add(10 * time.Millisecond)

	env.OnActivity("GetSubscription", mock.Anything, int64(1)).Return(&entity.Subscription{
		ID: 1, Status: entity.SubStatusCancelled,
	}, nil)
	env.OnActivity("PublishToNATS", mock.Anything, mock.Anything).Return(nil).Maybe()

	env.ExecuteWorkflow(SubscriptionLifecycleWorkflow, LifecycleInput{
		SubscriptionID: 1, AccountID: 1, ProductID: "lucrum",
		PlanID: 10, ExpiresAt: expiresAt, GraceDays: 3, AutoRenew: false,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
}

// --- ExpiryScannerWorkflow ---

func TestExpiryScannerWorkflow_WithGraceExpired(t *testing.T) {
	env := setup()

	env.OnActivity("ListActiveExpired", mock.Anything).Return([]activities.SubscriptionSummary{}, nil)
	env.OnActivity("ListGraceExpired", mock.Anything).Return([]activities.SubscriptionSummary{
		{ID: 2, AccountID: 20, ProductID: "api", PlanID: 3},
	}, nil)
	env.OnActivity("EndGrace", mock.Anything, int64(2)).Return(nil)
	env.OnActivity("PublishToNATS", mock.Anything, mock.Anything).Return(nil)
	env.OnActivity("ExpireStalePendingOrders", mock.Anything).Return(int64(0), nil)
	env.OnActivity("ExpireStalePreAuths", mock.Anything).Return(int64(0), nil)

	env.ExecuteWorkflow(ExpiryScannerWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
}

func TestExpiryScannerWorkflow_EmptyLists(t *testing.T) {
	env := setup()

	env.OnActivity("ListActiveExpired", mock.Anything).Return([]activities.SubscriptionSummary{}, nil)
	env.OnActivity("ListGraceExpired", mock.Anything).Return([]activities.SubscriptionSummary{}, nil)
	env.OnActivity("ExpireStalePendingOrders", mock.Anything).Return(int64(0), nil)
	env.OnActivity("ExpireStalePreAuths", mock.Anything).Return(int64(0), nil)

	env.ExecuteWorkflow(ExpiryScannerWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
}

// --- itoa helper ---

func TestItoa(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0"},
		{1, "1"},
		{123, "123"},
		{-42, "-42"},
		{9999999999, "9999999999"},
	}
	for _, tt := range tests {
		require.Equal(t, tt.want, itoa(tt.in), "itoa(%d)", tt.in)
	}
}

var (
	errInsufficientFunds = &testError{msg: "insufficient funds"}
	errActivationFailed  = &testError{msg: "activation failed"}
)

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }
