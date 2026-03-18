package activities

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// PlanStore is the interface for looking up product plans.
type PlanStore interface {
	GetPlanByID(ctx context.Context, id int64) (*entity.ProductPlan, error)
}

// QueryActivities provides read-only database queries as Temporal activities.
type QueryActivities struct {
	Plans PlanStore
}

// GetPlanByIDOutput holds plan details needed by the renewal workflow.
type GetPlanByIDOutput struct {
	PlanID   int64
	Code     string
	PriceCNY float64
}

// GetPlanByID fetches plan details for pricing.
func (a *QueryActivities) GetPlanByID(ctx context.Context, planID int64) (*GetPlanByIDOutput, error) {
	plan, err := a.Plans.GetPlanByID(ctx, planID)
	if err != nil {
		slog.Warn("activity/get-plan: failed", "plan_id", planID, "err", err)
		return nil, fmt.Errorf("get plan %d: %w", planID, err)
	}
	if plan == nil {
		slog.Warn("activity/get-plan: not found", "plan_id", planID)
		return nil, fmt.Errorf("plan %d not found", planID)
	}
	return &GetPlanByIDOutput{
		PlanID:   plan.ID,
		Code:     plan.Code,
		PriceCNY: plan.PriceCNY,
	}, nil
}
