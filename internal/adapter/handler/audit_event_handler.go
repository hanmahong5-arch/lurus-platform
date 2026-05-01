package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/hanmahong5-arch/lurus-platform/internal/adapter/repo"
)

// AuditEventHandler exposes the persistent audit log to the admin
// dashboard. Mounted under /admin/v1/audit-events with the admin-JWT
// middleware. Read-only — audit rows are append-only and there is no
// HTTP path for editing or deleting.
type AuditEventHandler struct {
	repo *repo.AuditEventRepo
}

// NewAuditEventHandler wires the handler. repo is required; main.go
// gates the route mount on (repo != nil) so a missing wiring keeps
// the endpoint at 404 rather than 500.
func NewAuditEventHandler(r *repo.AuditEventRepo) *AuditEventHandler {
	return &AuditEventHandler{repo: r}
}

// List — GET /admin/v1/audit-events
//
// Query params:
//
//	op            — exact match on the op column (optional)
//	target_kind   — exact match on the target_kind column (optional)
//	since         — RFC3339 lower bound on occurred_at (optional)
//	until         — RFC3339 upper bound on occurred_at (optional)
//	page          — 1-indexed (default 1)
//	page_size     — capped at 200 (default 50)
//
// Response:
//
//	{ "data": AuditEvent[], "total": int }
//
// Invalid since/until values return 400 with code "invalid_parameter".
func (h *AuditEventHandler) List(c *gin.Context) {
	filter := repo.AuditFilter{
		Op:         strings.TrimSpace(c.Query("op")),
		TargetKind: strings.TrimSpace(c.Query("target_kind")),
	}
	if raw := strings.TrimSpace(c.Query("since")); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			respondError(c, http.StatusBadRequest, ErrCodeInvalidParameter,
				"since must be an RFC3339 timestamp")
			return
		}
		filter.Since = t
	}
	if raw := strings.TrimSpace(c.Query("until")); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			respondError(c, http.StatusBadRequest, ErrCodeInvalidParameter,
				"until must be an RFC3339 timestamp")
			return
		}
		filter.Until = t
	}

	// Audit events allow a higher page_size cap than the platform-wide
	// parsePagination (50 default, 200 max) because operators routinely
	// scrub the last few hundred rows looking for a specific event.
	page, pageSize := parseAuditPagination(c)
	offset := (page - 1) * pageSize

	rows, total, err := h.repo.List(c.Request.Context(), filter, pageSize, offset)
	if err != nil {
		respondInternalError(c, "audit_events.list", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  rows,
		"total": total,
	})
}

// parseAuditPagination mirrors parsePagination but lifts the page_size
// cap from 100 to 200 — the audit dashboard needs the wider window for
// post-incident scrub. Default stays at 50.
func parseAuditPagination(c *gin.Context) (page, pageSize int) {
	page = 1
	pageSize = 50
	if p := c.Query("page"); p != "" {
		if v, err := strconv.Atoi(p); err == nil && v > 0 {
			page = v
		}
	}
	if ps := c.Query("page_size"); ps != "" {
		if v, err := strconv.Atoi(ps); err == nil && v > 0 {
			pageSize = v
		}
	}
	if pageSize > 200 {
		pageSize = 200
	}
	return page, pageSize
}
