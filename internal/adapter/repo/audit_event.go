package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"gorm.io/gorm"
)

// auditErrMaxLen caps the persisted error string. A runaway stack
// trace from a misbehaving downstream shouldn't bloat the audit row.
// Mirrors the 1 KB ceiling already used by AccountPurgeRepo.MarkFailed.
const auditErrMaxLen = 1024

// AuditFilter narrows a List call by op, target, or time window. Zero
// values mean "no filter on this dimension".
type AuditFilter struct {
	Op         string
	TargetKind string
	Since      time.Time
	Until      time.Time
}

// AuditEventRepo is the Postgres-backed audit log for destructive
// admin operations. Append-only — there is no Update path.
type AuditEventRepo struct {
	db *gorm.DB
}

// NewAuditEventRepo wires the repo. db must already have the
// module.audit_events table migrated (migrations/031).
func NewAuditEventRepo(db *gorm.DB) *AuditEventRepo {
	return &AuditEventRepo{db: db}
}

// Save inserts a single audit row. Best-effort: the caller is expected
// to log+swallow any error returned here so that an audit-write
// failure does NOT cascade and fail the underlying op. Trims overly-
// long Error strings before insert.
func (r *AuditEventRepo) Save(ctx context.Context, e *entity.AuditEvent) error {
	if e == nil {
		return errors.New("audit_event: nil row")
	}
	if e.Op == "" {
		return errors.New("audit_event: op required")
	}
	if e.Result == "" {
		return errors.New("audit_event: result required")
	}
	if len(e.Error) > auditErrMaxLen {
		e.Error = e.Error[:auditErrMaxLen]
	}
	if len(e.Params) == 0 {
		e.Params = json.RawMessage("{}")
	}
	if e.OccurredAt.IsZero() {
		e.OccurredAt = time.Now().UTC()
	}
	return r.db.WithContext(ctx).Create(e).Error
}

// List returns audit rows matching the filter, newest-first, plus the
// total count. limit is capped at 200, offset is clamped to >= 0.
func (r *AuditEventRepo) List(ctx context.Context, filter AuditFilter, limit, offset int) ([]entity.AuditEvent, int64, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}

	q := r.db.WithContext(ctx).Model(&entity.AuditEvent{})
	if filter.Op != "" {
		q = q.Where("op = ?", filter.Op)
	}
	if filter.TargetKind != "" {
		q = q.Where("target_kind = ?", filter.TargetKind)
	}
	if !filter.Since.IsZero() {
		q = q.Where("occurred_at >= ?", filter.Since)
	}
	if !filter.Until.IsZero() {
		q = q.Where("occurred_at <= ?", filter.Until)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("audit_event list: count: %w", err)
	}

	var rows []entity.AuditEvent
	err := q.Order("occurred_at DESC").
		Limit(limit).Offset(offset).Find(&rows).Error
	if err != nil {
		return nil, 0, fmt.Errorf("audit_event list: scan: %w", err)
	}
	return rows, total, nil
}
