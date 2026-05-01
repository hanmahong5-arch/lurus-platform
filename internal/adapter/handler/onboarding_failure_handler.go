package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/hanmahong5-arch/lurus-platform/internal/adapter/repo"
	"github.com/hanmahong5-arch/lurus-platform/internal/module"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/metrics"
)

// OnboardingFailureHandler exposes the dead-letter queue for module hook
// failures (P1-9). Mounted under /admin/v1/onboarding-failures with the
// admin-JWT middleware. Surfaces the previously-silent failure surface
// and gives operators a one-click replay path.
type OnboardingFailureHandler struct {
	repo      *repo.HookFailureRepo
	registry  *module.Registry
	auditRepo *repo.AuditEventRepo
}

// WithAuditRepo wires the persistent audit-events sink. Chainable;
// nil-safe.
func (h *OnboardingFailureHandler) WithAuditRepo(r *repo.AuditEventRepo) *OnboardingFailureHandler {
	h.auditRepo = r
	return h
}

// NewOnboardingFailureHandler wires the handler. Both dependencies are
// required; main.go gates the route mount on (registry != nil && repo != nil).
func NewOnboardingFailureHandler(r *repo.HookFailureRepo, reg *module.Registry) *OnboardingFailureHandler {
	return &OnboardingFailureHandler{repo: r, registry: reg}
}

// List — GET /admin/v1/onboarding-failures
//
// Query params:
//
//	pending  — "true" / "false" (default true)
//	page     — 1-indexed (default 1)
//	page_size — capped at 100 (default 20)
//
// Response:
//
//	{ "data": HookFailure[], "total": int, "pending_depth": int }
//
// `pending_depth` is the live DLQ depth (regardless of filter) so the UI
// can show a "12 alerts" badge in the nav even when filtering by date.
func (h *OnboardingFailureHandler) List(c *gin.Context) {
	pendingOnly := c.DefaultQuery("pending", "true") != "false"
	page, pageSize := parsePagination(c)
	offset := (page - 1) * pageSize

	rows, total, err := h.repo.List(c.Request.Context(), pendingOnly, pageSize, offset)
	if err != nil {
		respondInternalError(c, "onboarding_failures.list", err)
		return
	}
	depth, derr := h.repo.PendingDepth(c.Request.Context())
	if derr != nil {
		// Soft failure — list still serves; depth shows -1 so the UI
		// can render "?" rather than block.
		slog.WarnContext(c.Request.Context(), "onboarding_failures: pending depth lookup failed",
			"err", derr, "request_id", c.GetString("request_id"))
		depth = -1
	} else {
		metrics.SetHookDLQDepth(depth)
	}

	c.JSON(http.StatusOK, gin.H{
		"data":          rows,
		"total":         total,
		"pending_depth": depth,
	})
}

// Replay — POST /admin/v1/onboarding-failures/:id/replay
//
// Re-fetches a fresh account snapshot via the registry's account
// fetcher, looks up the named hook, and re-invokes it. Outcomes:
//
//	200 {"replayed":true}                   — hook succeeded; row stamped
//	200 {"replayed":true, "skipped":true}   — account purged since failure;
//	                                          row stamped (no longer pending)
//	409 already_replayed                    — row was replayed previously
//	502 hook_replay_failed                  — hook still fails; row's
//	                                          attempts++ and last_failed_at
//	                                          refreshed for the next try
//	404 not_found                           — id doesn't match any row
//	501 replay_unsupported                  — event type doesn't support
//	                                          replay (reconciliation_issue)
func (h *OnboardingFailureHandler) Replay(c *gin.Context) {
	id, ok := parsePathInt64(c, "id", "Failure ID")
	if !ok {
		return
	}
	ctx := c.Request.Context()

	row, err := h.repo.GetByID(ctx, id)
	if err != nil {
		respondInternalError(c, "onboarding_failures.replay.lookup", err)
		return
	}
	if row == nil {
		respondNotFound(c, "Hook failure")
		return
	}
	if row.ReplayedAt != nil {
		respondError(c, http.StatusConflict, "already_replayed",
			"This failure was already replayed at "+row.ReplayedAt.Format(time.RFC3339))
		return
	}

	replayErr := h.registry.Replay(ctx, row)
	switch {
	case replayErr == nil:
		// Hook succeeded — stamp the row.
		if err := h.repo.MarkReplayed(ctx, id, time.Now().UTC()); err != nil {
			respondInternalError(c, "onboarding_failures.replay.mark", err)
			return
		}
		metrics.RecordHookOutcome(row.Event, row.HookName, "replay_succeeded")
		slog.InfoContext(ctx, "onboarding_failures.replay.succeeded",
			"id", id, "event", row.Event, "hook", row.HookName,
			"account_id", row.AccountID, "request_id", c.GetString("request_id"))
		emitAudit(c, h.auditRepo, "hook.replay", auditEmitResultSuccess,
			actorIDFromContext(c), int64Ptr(id), "hook_failure",
			map[string]any{"event": row.Event, "hook": row.HookName, "account_id": row.AccountID}, "")
		c.JSON(http.StatusOK, gin.H{"replayed": true})

	case errors.Is(replayErr, module.ErrAccountAlreadyGone):
		// Account purged — mark the row replayed so it stops showing
		// as pending. Operators see "skipped: account purged" in the
		// response and can confirm the cleanup.
		if err := h.repo.MarkReplayed(ctx, id, time.Now().UTC()); err != nil {
			respondInternalError(c, "onboarding_failures.replay.mark_skipped", err)
			return
		}
		metrics.RecordHookOutcome(row.Event, row.HookName, "replay_succeeded")
		slog.InfoContext(ctx, "onboarding_failures.replay.skipped_account_gone",
			"id", id, "account_id", row.AccountID,
			"request_id", c.GetString("request_id"))
		emitAudit(c, h.auditRepo, "hook.replay", auditEmitResultSuccess,
			actorIDFromContext(c), int64Ptr(id), "hook_failure",
			map[string]any{"event": row.Event, "hook": row.HookName, "skipped": "account_purged_since_failure"}, "")
		c.JSON(http.StatusOK, gin.H{
			"replayed": true,
			"skipped":  true,
			"reason":   "account_purged_since_failure",
		})

	default:
		// Hook still fails. Re-save the row so attempts++, last_failed_at
		// refreshes, error captures the latest reason. The row stays
		// pending and the operator sees the new error.
		row.Error = replayErr.Error()
		if err := h.repo.Save(ctx, row); err != nil {
			slog.ErrorContext(ctx, "onboarding_failures.replay.resave_failed",
				"id", id, "save_err", err, "replay_err", replayErr,
				"request_id", c.GetString("request_id"))
		}
		metrics.RecordHookOutcome(row.Event, row.HookName, "replay_failed")
		// Replay-supported-but-failed special case: distinguish hook
		// not registered (501) from genuine still-failing hook (502).
		// The "not registered" sentinel is detectable via substring
		// match without leaking the original error to the client.
		errMsg := replayErr.Error()
		if matchAny(errMsg, "not registered", "not supported by replay", "missing account_id", "fetcher not configured") {
			respondError(c, http.StatusNotImplemented, "replay_unsupported",
				"Replay is not supported for this row: "+errMsg)
			return
		}
		respondError(c, http.StatusBadGateway, "hook_replay_failed",
			"Hook replay failed: "+errMsg)
	}
}

// matchAny checks whether s contains any of the supplied needles.
func matchAny(s string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}
