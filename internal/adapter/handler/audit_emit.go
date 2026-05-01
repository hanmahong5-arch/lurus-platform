package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/hanmahong5-arch/lurus-platform/internal/adapter/repo"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// auditEmitTimeout bounds the audit Save call so a wedged DB cannot
// stretch the user-visible response. The audit row is best-effort —
// missing it is logged at WARN and the underlying op proceeds.
const auditEmitTimeout = 2 * time.Second

// auditEmitResultSuccess / auditEmitResultFailed are the canonical
// values for AuditEvent.Result. The repo layer accepts any string but
// downstream filters in the admin dashboard sort by these two.
const (
	auditEmitResultSuccess = "success"
	auditEmitResultFailed  = "failed"
)

// emitAudit best-effort writes one audit row.
//
// Contract:
//   - No-op when r is nil. Lets handlers chain WithAuditRepo without
//     requiring every test to wire a repo.
//   - Save failures log at WARN and return; they NEVER block the
//     caller's response. The slog warn line carries enough context to
//     reconstruct the missing row from logs.
//   - actor / target IDs are pointers so 0 means "no actor" rather
//     than "actor 0". Pass nil for ops that don't have them.
//   - params is the op-specific JSON blob (e.g. {"app":"...", "env":"..."}).
//     Marshal failures fall back to the empty object so the emit still
//     succeeds — losing op detail beats losing the audit row entirely.
func emitAudit(c *gin.Context, r *repo.AuditEventRepo, op string, result string, actor, target *int64, targetKind string, params any, errMsg string) {
	if r == nil {
		return
	}
	if op == "" || result == "" {
		slog.Warn("audit_event: emit skipped — op/result empty",
			"op", op, "result", result)
		return
	}

	rawParams, mErr := json.Marshal(params)
	if mErr != nil || len(rawParams) == 0 {
		rawParams = json.RawMessage("{}")
	}

	row := &entity.AuditEvent{
		Op:         op,
		ActorID:    actor,
		TargetID:   target,
		TargetKind: targetKind,
		Params:     rawParams,
		Result:     result,
		Error:      errMsg,
		IP:         c.ClientIP(),
		UserAgent:  c.Request.UserAgent(),
		OccurredAt: time.Now().UTC(),
		RequestID:  c.GetString("request_id"),
	}

	// Detach from the request context so a client disconnect or a 30s
	// request-timeout cannot orphan the audit write.
	saveCtx, cancel := context.WithTimeout(context.Background(), auditEmitTimeout)
	defer cancel()
	if err := r.Save(saveCtx, row); err != nil {
		slog.Warn("audit_event: save failed",
			"op", op, "result", result, "target_kind", targetKind,
			"err", err, "request_id", row.RequestID)
	}
}

// emitAuditCtx is the executor-side variant for delegate dispatch
// (qr_handler.confirmDelegate calls into ExecuteDelegate without a
// *gin.Context). callerID is the boss who biometric-confirmed.
func emitAuditCtx(ctx context.Context, r *repo.AuditEventRepo, op string, result string, actor, target *int64, targetKind string, params any, errMsg string) {
	if r == nil {
		return
	}
	if op == "" || result == "" {
		return
	}

	rawParams, mErr := json.Marshal(params)
	if mErr != nil || len(rawParams) == 0 {
		rawParams = json.RawMessage("{}")
	}

	row := &entity.AuditEvent{
		Op:         op,
		ActorID:    actor,
		TargetID:   target,
		TargetKind: targetKind,
		Params:     rawParams,
		Result:     result,
		Error:      errMsg,
		OccurredAt: time.Now().UTC(),
	}

	saveCtx, cancel := context.WithTimeout(context.Background(), auditEmitTimeout)
	defer cancel()
	if err := r.Save(saveCtx, row); err != nil {
		slog.WarnContext(ctx, "audit_event: save failed",
			"op", op, "result", result, "target_kind", targetKind, "err", err)
	}
}

// int64Ptr is a tiny helper for the optional-int64 audit fields.
func int64Ptr(v int64) *int64 { return &v }

// actorIDFromContext returns the authenticated admin's account_id as a
// pointer, or nil when none is set. Distinct from requireAccountID:
// this is for audit emit at success paths where the caller has
// already passed authorisation, so we never want to abort with 401 —
// just record what we have.
func actorIDFromContext(c *gin.Context) *int64 {
	raw, ok := c.Get("account_id")
	if !ok {
		return nil
	}
	id, ok := raw.(int64)
	if !ok || id == 0 {
		return nil
	}
	return &id
}
