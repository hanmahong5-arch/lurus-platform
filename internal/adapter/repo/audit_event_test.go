package repo

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"gorm.io/gorm"
)

// setupAuditTestDB returns an in-memory SQLite DB with module.audit_events
// migrated. Mirrors the setupCheckinDB / setupOrgDB pattern in
// testutil_extra_test.go.
func setupAuditTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := setupTestDB(t)
	migrateExtra(t, db, &entity.AuditEvent{})
	return db
}

// TestAuditEventRepo_SaveAndList exercises the round-trip: a row goes
// in, comes out via List, with all the persisted fields surviving the
// trip. Matches the testutil pattern (in-memory SQLite, schema prefix
// stripped) used by the rest of the repo package.
func TestAuditEventRepo_SaveAndList(t *testing.T) {
	db := setupAuditTestDB(t)
	r := NewAuditEventRepo(db)
	ctx := context.Background()

	actor := int64(42)
	target := int64(100)
	row := &entity.AuditEvent{
		Op:         "apps.delete_request",
		ActorID:    &actor,
		TargetID:   &target,
		TargetKind: "oidc_app",
		Params:     json.RawMessage(`{"app":"tally","env":"prod"}`),
		Result:     "success",
		IP:         "10.0.0.5",
		UserAgent:  "test-agent/1.0",
		RequestID:  "req-abc",
	}
	if err := r.Save(ctx, row); err != nil {
		t.Fatalf("Save: %v", err)
	}

	rows, total, err := r.List(ctx, AuditFilter{}, 50, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 1 {
		t.Errorf("total = %d, want 1", total)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	got := rows[0]
	if got.Op != "apps.delete_request" {
		t.Errorf("Op = %q, want apps.delete_request", got.Op)
	}
	if got.ActorID == nil || *got.ActorID != actor {
		t.Errorf("ActorID = %v, want %d", got.ActorID, actor)
	}
	if got.TargetID == nil || *got.TargetID != target {
		t.Errorf("TargetID = %v, want %d", got.TargetID, target)
	}
	if got.Result != "success" {
		t.Errorf("Result = %q, want success", got.Result)
	}
	if got.OccurredAt.IsZero() {
		t.Error("OccurredAt should be auto-stamped")
	}
}

// TestAuditEventRepo_FilterByOp verifies that the Op filter narrows the
// result set correctly while leaving total in step with the filter.
func TestAuditEventRepo_FilterByOp(t *testing.T) {
	db := setupAuditTestDB(t)
	r := NewAuditEventRepo(db)
	ctx := context.Background()

	for _, op := range []string{"apps.delete_request", "apps.delete_request", "refund.qr_approve_request"} {
		if err := r.Save(ctx, &entity.AuditEvent{
			Op:     op,
			Result: "success",
		}); err != nil {
			t.Fatalf("Save %s: %v", op, err)
		}
	}

	rows, total, err := r.List(ctx, AuditFilter{Op: "apps.delete_request"}, 50, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
	if len(rows) != 2 {
		t.Errorf("rows = %d, want 2", len(rows))
	}
	for _, row := range rows {
		if row.Op != "apps.delete_request" {
			t.Errorf("filter leaked %q", row.Op)
		}
	}

	// Empty filter returns all rows
	_, total, err = r.List(ctx, AuditFilter{}, 50, 0)
	if err != nil {
		t.Fatalf("List unfiltered: %v", err)
	}
	if total != 3 {
		t.Errorf("unfiltered total = %d, want 3", total)
	}
}

// TestAuditEventRepo_TruncatesError verifies the 1024-char ceiling on
// Error so a runaway stack trace cannot bloat the row.
func TestAuditEventRepo_TruncatesError(t *testing.T) {
	db := setupAuditTestDB(t)
	r := NewAuditEventRepo(db)
	ctx := context.Background()

	huge := strings.Repeat("x", 5000) // > 1024
	row := &entity.AuditEvent{
		Op:     "delete_oidc_app",
		Result: "failed",
		Error:  huge,
	}
	if err := r.Save(ctx, row); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got := len(row.Error); got != auditErrMaxLen {
		t.Errorf("after Save, len(Error) = %d, want %d", got, auditErrMaxLen)
	}

	rows, _, err := r.List(ctx, AuditFilter{}, 10, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if got := len(rows[0].Error); got != auditErrMaxLen {
		t.Errorf("persisted len(Error) = %d, want %d", got, auditErrMaxLen)
	}
}

// TestAuditEventRepo_FilterByTime verifies the since/until window narrows
// the result set without re-counting outside-window rows.
func TestAuditEventRepo_FilterByTime(t *testing.T) {
	db := setupAuditTestDB(t)
	r := NewAuditEventRepo(db)
	ctx := context.Background()

	now := time.Now().UTC()
	older := &entity.AuditEvent{
		Op: "old.op", Result: "success",
		OccurredAt: now.Add(-24 * time.Hour),
	}
	newer := &entity.AuditEvent{
		Op: "new.op", Result: "success",
		OccurredAt: now,
	}
	if err := r.Save(ctx, older); err != nil {
		t.Fatalf("Save older: %v", err)
	}
	if err := r.Save(ctx, newer); err != nil {
		t.Fatalf("Save newer: %v", err)
	}

	rows, total, err := r.List(ctx, AuditFilter{
		Since: now.Add(-1 * time.Hour),
	}, 50, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 1 {
		t.Errorf("total = %d, want 1 (since filter should drop older)", total)
	}
	if len(rows) != 1 || rows[0].Op != "new.op" {
		t.Errorf("rows = %+v, want only new.op", rows)
	}
}
