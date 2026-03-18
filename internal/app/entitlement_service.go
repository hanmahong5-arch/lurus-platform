package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"

	"go.opentelemetry.io/otel/attribute"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/metrics"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/tracing"
)

// EntitlementService computes and caches per-account product entitlements.
type EntitlementService struct {
	subRepo  subscriptionStore
	planRepo planStore
	cache    entitlementCache
}

func NewEntitlementService(sub subscriptionStore, plan planStore, c entitlementCache) *EntitlementService {
	return &EntitlementService{subRepo: sub, planRepo: plan, cache: c}
}

// Get returns entitlements for an account+product, serving from cache when available.
func (s *EntitlementService) Get(ctx context.Context, accountID int64, productID string) (map[string]string, error) {
	ctx, span := tracing.Tracer("lurus-platform").Start(ctx, "entitlement.get")
	defer span.End()

	if em, err := s.cache.Get(ctx, accountID, productID); err == nil && em != nil {
		span.SetAttributes(attribute.Bool("cache.hit", true))
		metrics.RecordCacheHit()
		return em, nil
	}
	span.SetAttributes(attribute.Bool("cache.hit", false))
	metrics.RecordCacheMiss()
	return s.Refresh(ctx, accountID, productID)
}

// Refresh re-computes entitlements from DB and updates cache.
func (s *EntitlementService) Refresh(ctx context.Context, accountID int64, productID string) (map[string]string, error) {
	rows, err := s.subRepo.GetEntitlements(ctx, accountID, productID)
	if err != nil {
		return nil, fmt.Errorf("get entitlements: %w", err)
	}
	em := make(map[string]string, len(rows))
	for _, r := range rows {
		em[r.Key] = r.Value
	}
	// Ensure plan_code is always present
	if _, ok := em["plan_code"]; !ok {
		em["plan_code"] = "free"
	}
	_ = s.cache.Set(ctx, accountID, productID, em)
	return em, nil
}

// SyncFromSubscription derives entitlements from a live subscription's plan features
// and writes them to account_entitlements (upsert), then refreshes the cache.
func (s *EntitlementService) SyncFromSubscription(ctx context.Context, sub *entity.Subscription) error {
	plan, err := s.planRepo.GetPlanByID(ctx, sub.PlanID)
	if err != nil || plan == nil {
		return fmt.Errorf("plan %d not found: %w", sub.PlanID, err)
	}

	// Parse JSONB features map
	var features map[string]any
	if err := json.Unmarshal(plan.Features, &features); err != nil {
		return fmt.Errorf("parse plan features: %w", err)
	}

	// Always write plan_code
	entries := map[string]string{
		"plan_code": plan.Code,
	}
	for k, v := range features {
		entries[k] = anyToString(v)
	}

	src := "subscription"
	srcRef := fmt.Sprintf("%d", sub.ID)
	for k, v := range entries {
		vt := inferValueType(v)
		e := &entity.AccountEntitlement{
			AccountID: sub.AccountID,
			ProductID: sub.ProductID,
			Key:       k,
			Value:     v,
			ValueType: vt,
			Source:    src,
			SourceRef: srcRef,
			ExpiresAt: sub.ExpiresAt,
		}
		if err := s.subRepo.UpsertEntitlement(ctx, e); err != nil {
			return fmt.Errorf("upsert entitlement %s: %w", k, err)
		}
	}
	_ = s.cache.Invalidate(ctx, sub.AccountID, sub.ProductID)
	slog.Info("entitlement/sync", "account_id", sub.AccountID, "product_id", sub.ProductID, "plan_code", plan.Code, "keys_synced", len(entries))
	return nil
}

// ResetToFree replaces all entitlements for account+product with only plan_code=free.
func (s *EntitlementService) ResetToFree(ctx context.Context, accountID int64, productID string) error {
	if err := s.subRepo.DeleteEntitlements(ctx, accountID, productID); err != nil {
		return err
	}
	e := &entity.AccountEntitlement{
		AccountID: accountID,
		ProductID: productID,
		Key:       "plan_code",
		Value:     "free",
		ValueType: "string",
		Source:    "system",
	}
	if err := s.subRepo.UpsertEntitlement(ctx, e); err != nil {
		return err
	}
	_ = s.cache.Invalidate(ctx, accountID, productID)
	slog.Info("entitlement/reset-to-free", "account_id", accountID, "product_id", productID)
	return nil
}

func anyToString(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case bool:
		if val {
			return "true"
		}
		return "false"
	case float64:
		if val == float64(int64(val)) {
			return strconv.FormatInt(int64(val), 10)
		}
		return strconv.FormatFloat(val, 'f', -1, 64)
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

func inferValueType(v string) string {
	if v == "true" || v == "false" {
		return "boolean"
	}
	if _, err := strconv.ParseInt(v, 10, 64); err == nil {
		return "integer"
	}
	if _, err := strconv.ParseFloat(v, 64); err == nil {
		return "decimal"
	}
	return "string"
}
