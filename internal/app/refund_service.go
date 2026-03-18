package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/event"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/metrics"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/tracing"
)

const (
	// refundWindowDays is the maximum age of a paid order eligible for refund.
	refundWindowDays = 7
	// refundPublishTimeout is the deadline for a best-effort NATS publish after approval.
	refundPublishTimeout = 5 * time.Second
)

// RefundPublisher is the subset of the NATS publisher required by RefundService.
type RefundPublisher interface {
	Publish(ctx context.Context, ev *event.IdentityEvent) error
}

// refundOutboxWriter is the subset of the outbox repository needed by RefundService.
type refundOutboxWriter interface {
	Insert(ctx context.Context, ev *event.IdentityEvent) error
}

// subscriptionCanceller allows RefundService to cancel the subscription tied to a refunded order.
type subscriptionCanceller interface {
	Cancel(ctx context.Context, accountID int64, productID string) error
}

// RefundService orchestrates the refund request and approval workflow.
type RefundService struct {
	refunds   refundStore
	wallets   walletStore
	publisher RefundPublisher
	outbox    refundOutboxWriter
	subCancel subscriptionCanceller // optional; nil when not wired
}

// NewRefundService creates a new RefundService.
func NewRefundService(refunds refundStore, wallets walletStore, publisher RefundPublisher, outbox refundOutboxWriter) *RefundService {
	return &RefundService{refunds: refunds, wallets: wallets, publisher: publisher, outbox: outbox}
}

// WithSubscriptionCanceller attaches a subscription canceller and returns for chaining.
func (s *RefundService) WithSubscriptionCanceller(c subscriptionCanceller) *RefundService {
	s.subCancel = c
	return s
}

// RequestRefund creates a new refund request for a paid order.
// Rules enforced:
//   - The order must be paid and belong to accountID (IDOR).
//   - The order must have been created within the last refundWindowDays.
//   - No pending or approved refund may exist for the same order.
func (s *RefundService) RequestRefund(ctx context.Context, accountID int64, orderNo, reason string) (*entity.Refund, error) {
	ctx, span := tracing.Tracer("lurus-platform").Start(ctx, "refund.request")
	defer span.End()
	span.SetAttributes(
		attribute.Int64("account.id", accountID),
		attribute.String("order.no", orderNo),
	)

	// Fetch the order directly; we do not use WalletService.GetOrderByNo to avoid
	// the IDOR obscure-error behaviour — we want distinct error messages internally.
	order, err := s.wallets.GetPaymentOrderByNo(ctx, orderNo)
	if err != nil {
		return nil, fmt.Errorf("get order: %w", err)
	}
	if order == nil || order.AccountID != accountID {
		return nil, errors.New("order not found")
	}
	if order.Status != entity.OrderStatusPaid {
		return nil, errors.New("refund requires a paid order")
	}

	// Enforce the 7-day refund window.
	cutoff := time.Now().UTC().AddDate(0, 0, -refundWindowDays)
	if order.CreatedAt.Before(cutoff) {
		return nil, fmt.Errorf("refund window of %d days has expired", refundWindowDays)
	}

	// Prevent duplicate in-flight refunds.
	pending, err := s.refunds.GetPendingByOrderNo(ctx, orderNo)
	if err != nil {
		return nil, fmt.Errorf("check pending refund: %w", err)
	}
	if pending != nil {
		return nil, errors.New("a refund is already in progress for this order")
	}

	r := &entity.Refund{
		RefundNo:  generateRefundNo(),
		AccountID: accountID,
		OrderNo:   orderNo,
		AmountCNY: order.AmountCNY,
		Reason:    reason,
		Status:    entity.RefundStatusPending,
	}
	if err := s.refunds.Create(ctx, r); err != nil {
		metrics.RecordRefundOperation("request", "error")
		return nil, fmt.Errorf("create refund: %w", err)
	}
	slog.Info("refund/request", "refund_no", r.RefundNo, "account_id", accountID, "order_no", orderNo, "amount_cny", r.AmountCNY)
	metrics.RecordRefundOperation("request", "success")
	return r, nil
}

// GetByNo returns a refund by its number, enforcing IDOR.
func (s *RefundService) GetByNo(ctx context.Context, accountID int64, refundNo string) (*entity.Refund, error) {
	r, err := s.refunds.GetByRefundNo(ctx, refundNo)
	if err != nil {
		return nil, fmt.Errorf("get refund: %w", err)
	}
	if r == nil || r.AccountID != accountID {
		return nil, errors.New("refund not found")
	}
	return r, nil
}

// ListByAccount returns paginated refunds for an account.
func (s *RefundService) ListByAccount(ctx context.Context, accountID int64, page, pageSize int) ([]entity.Refund, int64, error) {
	return s.refunds.ListByAccount(ctx, accountID, page, pageSize)
}

// Approve transitions a pending refund to approved, credits the wallet, then marks it completed.
// Workflow: pending → approved (DB, conditional) → credit wallet → completed (DB) → publish NATS event.
// The conditional UPDATE prevents double-refund on concurrent admin approvals.
func (s *RefundService) Approve(ctx context.Context, refundNo, reviewerID, reviewNote string) error {
	ctx, span := tracing.Tracer("lurus-platform").Start(ctx, "refund.approve")
	defer span.End()
	span.SetAttributes(attribute.String("refund.no", refundNo))

	r, err := s.refunds.GetByRefundNo(ctx, refundNo)
	if err != nil {
		return fmt.Errorf("get refund: %w", err)
	}
	if r == nil {
		return errors.New("refund not found")
	}
	if r.Status != entity.RefundStatusPending {
		return fmt.Errorf("refund is not in pending state (current: %s)", r.Status)
	}

	// Conditional transition: only pending→approved succeeds; concurrent calls get an error.
	now := time.Now().UTC()
	if err := s.refunds.UpdateStatus(ctx, refundNo,
		string(entity.RefundStatusPending), string(entity.RefundStatusApproved),
		reviewNote, reviewerID, &now); err != nil {
		return fmt.Errorf("update refund status to approved: %w", err)
	}

	// Credit the wallet (1 Credit = 1 CNY).
	_, err = s.wallets.Credit(ctx,
		r.AccountID,
		r.AmountCNY,
		entity.TxTypeRefund,
		fmt.Sprintf("Refund approved: %s", r.RefundNo),
		"refund",
		r.RefundNo,
		"",
	)
	if err != nil {
		// Log but do not roll back; the refund record is already approved.
		// A background reconciliation job can retry the credit.
		slog.Error("refund/approve: credit wallet failed",
			"refund_no", refundNo,
			"account_id", r.AccountID,
			"amount", r.AmountCNY,
			"err", err,
		)
		return fmt.Errorf("credit wallet: %w", err)
	}

	// Transition to completed.
	completedAt := time.Now().UTC()
	if err := s.refunds.MarkCompleted(ctx, refundNo, completedAt); err != nil {
		return fmt.Errorf("mark refund completed: %w", err)
	}

	// If the refunded order was a subscription, cancel it (best-effort, non-fatal).
	if s.subCancel != nil {
		order, _ := s.wallets.GetPaymentOrderByNo(ctx, r.OrderNo)
		if order != nil && order.OrderType == "subscription" && order.ProductID != "" {
			if cancelErr := s.subCancel.Cancel(ctx, r.AccountID, order.ProductID); cancelErr != nil {
				slog.Warn("refund/approve: cancel subscription after refund failed",
					"refund_no", refundNo, "product_id", order.ProductID, "err", cancelErr)
			}
		}
	}

	// Best-effort NATS event publish.
	s.publishRefundCompleted(r)

	slog.Info("refund/approve", "refund_no", refundNo, "account_id", r.AccountID, "amount_cny", r.AmountCNY, "reviewer_id", reviewerID)
	span.SetAttributes(attribute.Float64("amount.cny", r.AmountCNY))
	metrics.RecordRefundOperation("approve", "success")
	return nil
}

// Reject transitions a pending refund to rejected.
func (s *RefundService) Reject(ctx context.Context, refundNo, reviewerID, reviewNote string) error {
	ctx, span := tracing.Tracer("lurus-platform").Start(ctx, "refund.reject")
	defer span.End()
	span.SetAttributes(attribute.String("refund.no", refundNo))

	r, err := s.refunds.GetByRefundNo(ctx, refundNo)
	if err != nil {
		return fmt.Errorf("get refund: %w", err)
	}
	if r == nil {
		return errors.New("refund not found")
	}
	if r.Status != entity.RefundStatusPending {
		return fmt.Errorf("refund is not in pending state (current: %s)", r.Status)
	}

	now := time.Now().UTC()
	if err := s.refunds.UpdateStatus(ctx, refundNo,
		string(entity.RefundStatusPending), string(entity.RefundStatusRejected),
		reviewNote, reviewerID, &now); err != nil {
		return fmt.Errorf("update refund status to rejected: %w", err)
	}
	slog.Info("refund/reject", "refund_no", refundNo, "account_id", r.AccountID, "reviewer_id", reviewerID)
	metrics.RecordRefundOperation("reject", "success")
	return nil
}

// publishRefundCompleted writes the refund event to the outbox for reliable delivery.
// Falls back to direct NATS publish if the outbox is unavailable.
func (s *RefundService) publishRefundCompleted(r *entity.Refund) {
	payload := map[string]any{
		"refund_no":  r.RefundNo,
		"order_no":   r.OrderNo,
		"amount_cny": r.AmountCNY,
	}
	ev, err := event.NewEvent("identity.refund.completed", r.AccountID, "", "", payload)
	if err != nil {
		slog.Error("refund/publish: build event", "refund_no", r.RefundNo, "err", err)
		return
	}

	// Primary path: write to outbox (relay will publish to NATS).
	if s.outbox != nil {
		if err := s.outbox.Insert(context.Background(), ev); err != nil {
			slog.Error("refund/publish: outbox insert failed, falling back to direct publish",
				"refund_no", r.RefundNo, "err", err)
		} else {
			return
		}
	}

	// Fallback: direct NATS publish (best-effort).
	if s.publisher == nil {
		return
	}
	pubCtx, cancel := context.WithTimeout(context.Background(), refundPublishTimeout)
	defer cancel()
	if err := s.publisher.Publish(pubCtx, ev); err != nil {
		slog.Error("refund/publish: publish event", "refund_no", r.RefundNo, "err", err)
	}
}

// generateRefundNo returns a unique refund number with prefix "LR" and nanosecond timestamp.
func generateRefundNo() string {
	return fmt.Sprintf("LR%X", time.Now().UnixNano())
}
