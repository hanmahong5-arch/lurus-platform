package temporal

import (
	"context"
	"fmt"
	"log/slog"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"

	"github.com/hanmahong5-arch/lurus-platform/internal/temporal/activities"
	"github.com/hanmahong5-arch/lurus-platform/internal/temporal/workflows"
)

// WorkerDeps holds all dependencies injected into Temporal activities.
type WorkerDeps struct {
	SubActivities          *activities.SubscriptionActivities
	WalletActivities       *activities.WalletActivities
	EventActivities        *activities.EventActivities
	QueryActivities        *activities.QueryActivities
	NotificationActivities *activities.NotificationActivities
}

// Worker wraps a Temporal worker with registered workflows and activities.
type Worker struct {
	w worker.Worker
}

// NewWorker creates a Temporal worker and registers all workflows and activities.
func NewWorker(c client.Client, deps WorkerDeps) *Worker {
	w := worker.New(c, activities.TaskQueue, worker.Options{
		MaxConcurrentActivityExecutionSize:     10,
		MaxConcurrentWorkflowTaskExecutionSize: 10,
	})

	// Register workflows
	w.RegisterWorkflow(workflows.SubscriptionRenewalWorkflow)
	w.RegisterWorkflow(workflows.PaymentCompletionWorkflow)
	w.RegisterWorkflow(workflows.SubscriptionLifecycleWorkflow)
	w.RegisterWorkflow(workflows.ExpiryScannerWorkflow)

	// Register activities with struct methods (Temporal auto-discovers exported methods)
	w.RegisterActivity(deps.SubActivities)
	w.RegisterActivity(deps.WalletActivities)
	w.RegisterActivity(deps.EventActivities)
	w.RegisterActivity(deps.QueryActivities)
	if deps.NotificationActivities != nil {
		w.RegisterActivity(deps.NotificationActivities)
	}

	return &Worker{w: w}
}

// Run starts the Temporal worker and blocks until ctx is cancelled.
func (tw *Worker) Run(ctx context.Context) error {
	slog.Info("temporal: worker starting", "task_queue", activities.TaskQueue)

	errCh := make(chan error, 1)
	go func() {
		errCh <- tw.w.Run(worker.InterruptCh())
	}()

	select {
	case <-ctx.Done():
		tw.w.Stop()
		slog.Info("temporal: worker stopped")
		return nil
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("temporal worker: %w", err)
		}
		return nil
	}
}
