package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/hanmahong5-arch/lurus-platform/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/internal/module/ops"
)

// RefundApprover is the subset of RefundService the executor needs.
// Declared as an interface so unit tests can supply an in-memory
// fake without standing up the full refund/wallet/outbox stack —
// matches the QROrgService / AccountPurgeOrchestrator pattern used
// elsewhere in this package.
type RefundApprover interface {
	Approve(ctx context.Context, refundNo, reviewerID, reviewNote string) error
}

// RefundApproveExecutor implements QRDelegateExecutor + ops.DelegateOp
// for the approve_refund operation (Phase 4 / Sprint 3A).
//
// Why a QR-confirmed approval path
//
// Direct admin approval already exists (RefundHandler.AdminApprove).
// This executor exists for the case where a customer-service rep can
// raise a refund request but the actual approval needs the boss's
// biometric sign-off — large-amount refunds, suspicious patterns, or
// any policy where "approve under threshold = direct, over threshold
// = QR" applies. The threshold check itself lives in the mint
// endpoint, not here; this executor's contract is "given a refund_no
// that the boss has biometric-confirmed, approve it".
//
// Risk classification: warn rather than destructive — a wrongly-
// approved refund can be re-debited and re-issued; nothing here is
// permanently lost.
type RefundApproveExecutor struct {
	refunds RefundApprover
}

// NewRefundApproveExecutor wires the executor. refunds may be nil
// at construction time (tests) but ExecuteDelegate will then return
// an error rather than panic.
func NewRefundApproveExecutor(refunds RefundApprover) *RefundApproveExecutor {
	return &RefundApproveExecutor{refunds: refunds}
}

// Compile-time guarantees:
//   - the production type *app.RefundService satisfies RefundApprover
//   - this executor satisfies both the QR dispatch contract and the
//     ops catalogue's DelegateOp metadata contract
var (
	_ RefundApprover = (*app.RefundService)(nil)
	_ ops.DelegateOp = (*RefundApproveExecutor)(nil)
)

// SupportedOps returns the delegate ops this executor handles.
func (e *RefundApproveExecutor) SupportedOps() []string {
	return []string{qrDelegateOpApproveRefund}
}

// ExecuteDelegate runs RefundService.Approve once the boss has
// biometric-confirmed on his APP. callerID is the boss's account id;
// it is recorded as the reviewer so the refund's audit trail
// captures who actually signed off — not the CS rep who minted the
// QR.
func (e *RefundApproveExecutor) ExecuteDelegate(ctx context.Context, params QRDelegateParams, callerID int64) error {
	if params.Op != qrDelegateOpApproveRefund {
		return fmt.Errorf("%w: %q", ErrUnsupportedDelegateOp, params.Op)
	}
	if e.refunds == nil {
		return errors.New("refund_approve: refunds service not wired")
	}
	if params.RefundNo == "" {
		return errors.New("refund_approve: missing refund_no")
	}
	reviewer := strconv.FormatInt(callerID, 10)
	reviewNote := "Approved via QR-delegate biometric confirmation"
	if err := e.refunds.Approve(ctx, params.RefundNo, reviewer, reviewNote); err != nil {
		return fmt.Errorf("refund_approve: approve %s: %w", params.RefundNo, err)
	}
	slog.InfoContext(ctx, "refund_approve.completed",
		"refund_no", params.RefundNo, "approver", callerID)
	return nil
}

// ── ops.Op metadata ────────────────────────────────────────────────────────

// Type identifies this executor's op in the QR delegate dispatch
// table. Must match the SupportedOps entry exactly.
func (e *RefundApproveExecutor) Type() string { return qrDelegateOpApproveRefund }

// Description is the one-line English summary surfaced in admin UIs
// and audit logs.
func (e *RefundApproveExecutor) Description() string {
	return "Approve a pending refund and credit the user's wallet under boss biometric sign-off"
}

// RiskLevel — refund approval is financially significant but not
// destructive: a wrongly-approved refund can be reversed by debiting
// the account back. APP UI should still require biometric step-up
// (warn level does), but the colour palette can be amber rather
// than red.
func (e *RefundApproveExecutor) RiskLevel() ops.RiskLevel { return ops.RiskWarn }

// IsDestructive — false; refund approval is reversible.
func (e *RefundApproveExecutor) IsDestructive() bool { return false }
