package repo

// testutil_extra_test.go provides setupDB helpers for entity types
// that are not included in the base setupTestDB (which covers the core billing/identity tables).

import (
	"strings"
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// migrateExtra registers additional entity schemas in GORM's cache (stripping schema
// prefix the same way setupTestDB does) and migrates their tables to the DB.
// Call this after setupTestDB to add support for Checkin, OutboxEvent, AdminSetting,
// and Organization-family tables.
func migrateExtra(t *testing.T, db *gorm.DB, models ...interface{}) {
	t.Helper()
	for _, m := range models {
		stmt := &gorm.Statement{DB: db}
		if err := stmt.Parse(m); err != nil {
			t.Fatalf("parse schema for %T: %v", m, err)
		}
		if idx := strings.Index(stmt.Schema.Table, "."); idx >= 0 {
			stmt.Schema.Table = stmt.Schema.Table[idx+1:]
		}
		patchJSONRawMessageFields(stmt.Schema.Fields)
		patchNowDefaultFields(stmt.Schema.Fields)
	}
	if err := db.AutoMigrate(models...); err != nil {
		t.Fatalf("auto-migrate extra: %v", err)
	}
}

// patchNowDefaultFields clears "now()" column defaults that SQLite does not support.
// PostgreSQL accepts now() as a default expression; SQLite does not.
func patchNowDefaultFields(fields []*schema.Field) {
	for _, f := range fields {
		if f.DefaultValue == "now()" {
			f.DefaultValue = ""
			f.HasDefaultValue = false
		}
	}
}

// setupCheckinDB returns a test DB with the checkins table migrated.
func setupCheckinDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := setupTestDB(t)
	migrateExtra(t, db, &entity.Checkin{})
	return db
}

// setupAdminSettingsDB returns a test DB with the admin settings table migrated.
func setupAdminSettingsDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := setupTestDB(t)
	migrateExtra(t, db, &entity.AdminSetting{})
	return db
}

// setupOrgDB returns a test DB with all organization-family tables migrated.
func setupOrgDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := setupTestDB(t)
	migrateExtra(t, db,
		&entity.Organization{},
		&entity.OrgMember{},
		&entity.OrgAPIKey{},
		&entity.OrgWallet{},
	)
	return db
}
