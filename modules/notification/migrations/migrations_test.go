package migrations_test

// Package migrations_test verifies that migration files added in the
// unified-notifications work (005_source_and_payload, 006_seed_templates_v2)
// are idempotent — i.e. every CREATE / ALTER / INSERT either uses
// IF NOT EXISTS / ADD COLUMN IF NOT EXISTS / ON CONFLICT DO NOTHING — so
// re-running the migration on the same database is a no-op.
//
// When NOTIFICATION_TEST_POSTGRES_DSN is set, the test additionally:
//   1. Creates a fresh notification.notifications table (pre-005 shape).
//   2. Seeds rows with mixed event_type prefixes.
//   3. Applies migration 005 twice.
//   4. Asserts the source backfill matches the expected per-source counts.

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// migrationsDir resolves the repository directory holding the .sql files.
func migrationsDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Dir(file)
}

func readMigration(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(migrationsDir(t), name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

func TestMigration005_IsIdempotent_Static(t *testing.T) {
	sqlText := readMigration(t, "005_source_and_payload.sql")

	mustContain := []string{
		"ADD COLUMN IF NOT EXISTS source",
		"ADD COLUMN IF NOT EXISTS payload",
		"CREATE INDEX IF NOT EXISTS idx_notifications_account_source_unread",
	}
	for _, want := range mustContain {
		if !strings.Contains(sqlText, want) {
			t.Errorf("migration 005 missing idempotent guard: %q", want)
		}
	}

	// Defensive: forbid bare CREATE/ALTER/ADD COLUMN forms (without IF NOT EXISTS)
	// because they would crash on re-run. Our migration uses guarded forms only.
	for _, line := range strings.Split(sqlText, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		upper := strings.ToUpper(trimmed)
		switch {
		case strings.HasPrefix(upper, "CREATE INDEX ") && !strings.Contains(upper, "IF NOT EXISTS"):
			t.Errorf("migration 005: unguarded CREATE INDEX: %s", trimmed)
		case strings.HasPrefix(upper, "ADD COLUMN ") && !strings.Contains(upper, "IF NOT EXISTS"):
			t.Errorf("migration 005: unguarded ADD COLUMN: %s", trimmed)
		}
	}
}

func TestMigration006_IsIdempotent_Static(t *testing.T) {
	sqlText := readMigration(t, "006_seed_templates_v2.sql")

	if !strings.Contains(sqlText, "ON CONFLICT (event_type, channel) DO NOTHING") {
		t.Error("migration 006 must end with ON CONFLICT (event_type, channel) DO NOTHING for idempotency")
	}

	// Sanity: 8 new event_types are seeded (some have multi-channel rows).
	wantEventTypes := []string{
		"identity.vip.level_changed",
		"lucrum.advisor.output",
		"lucrum.market.event",
		"llm.image.generated",
		"llm.usage.milestone",
		"psi.order.approval_needed",
		"psi.inventory.redline",
		"psi.payment.received",
	}
	for _, et := range wantEventTypes {
		if !strings.Contains(sqlText, "'"+et+"'") {
			t.Errorf("migration 006 missing template for event_type %q", et)
		}
	}
}

// TestMigration005_SourceBackfill exercises the actual backfill on a real
// Postgres if a DSN is configured, otherwise t.Skip.
//
// To run locally:
//   export NOTIFICATION_TEST_POSTGRES_DSN="postgres://lurus:***@127.0.0.1:5432/identity?sslmode=disable"
//   go test -run TestMigration005_SourceBackfill ./migrations/...
func TestMigration005_SourceBackfill(t *testing.T) {
	dsn := os.Getenv("NOTIFICATION_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("NOTIFICATION_TEST_POSTGRES_DSN not set; skipping live-Postgres migration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping db: %v", err)
	}

	// Use a transient schema so we never mutate production tables.
	const schema = "notification_test_005"
	stmts := []string{
		"DROP SCHEMA IF EXISTS " + schema + " CASCADE",
		"CREATE SCHEMA " + schema,
		`CREATE TABLE ` + schema + `.notifications (
			id          BIGSERIAL PRIMARY KEY,
			account_id  BIGINT NOT NULL,
			channel     VARCHAR(20) NOT NULL,
			category    VARCHAR(50) NOT NULL,
			title       VARCHAR(200) NOT NULL,
			body        TEXT NOT NULL,
			priority    VARCHAR(20) NOT NULL DEFAULT 'normal',
			status      VARCHAR(20) NOT NULL DEFAULT 'pending',
			event_type  VARCHAR(100),
			event_id    VARCHAR(50),
			metadata    JSONB DEFAULT '{}',
			read_at     TIMESTAMPTZ,
			sent_at     TIMESTAMPTZ,
			created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
	}
	for _, s := range stmts {
		if _, err := db.ExecContext(ctx, s); err != nil {
			t.Fatalf("setup %q: %v", s, err)
		}
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
	})

	// Seed mixed event types so backfill must produce 4 distinct sources.
	seed := []struct {
		eventType string
	}{
		{"identity.account.created"},
		{"identity.subscription.activated"},
		{"lucrum.strategy.triggered"},
		{"lucrum.risk.alert"},
		{"llm.quota.threshold"},
		{"psi.order.approval_needed"},
		{"psi.inventory.redline"},
	}
	for _, s := range seed {
		_, err := db.ExecContext(ctx,
			"INSERT INTO "+schema+".notifications (account_id,channel,category,title,body,event_type) VALUES ($1,$2,$3,$4,$5,$6)",
			1, "in_app", "test", "t", "b", s.eventType,
		)
		if err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	// The migration file targets `notification.notifications`. We rewrite it
	// to point at our test schema so we don't touch the production table.
	sqlText := readMigration(t, "005_source_and_payload.sql")
	sqlText = strings.ReplaceAll(sqlText, "notification.notifications", schema+".notifications")

	// Run twice: must succeed both times (idempotency).
	for i := 1; i <= 2; i++ {
		if _, err := db.ExecContext(ctx, sqlText); err != nil {
			t.Fatalf("apply migration 005 (run %d): %v", i, err)
		}
	}

	// Verify backfill: each event_type prefix maps to its source.
	rows, err := db.QueryContext(ctx,
		"SELECT source, COUNT(*) FROM "+schema+".notifications GROUP BY source ORDER BY source")
	if err != nil {
		t.Fatalf("group by source: %v", err)
	}
	defer rows.Close()

	got := map[string]int{}
	for rows.Next() {
		var src string
		var n int
		if err := rows.Scan(&src, &n); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got[src] = n
	}

	want := map[string]int{
		"identity": 2,
		"lucrum":   2,
		"llm":      1,
		"psi":      2,
	}
	for src, n := range want {
		if got[src] != n {
			t.Errorf("source=%q count = %d, want %d (full: %v)", src, got[src], n, got)
		}
	}
}
