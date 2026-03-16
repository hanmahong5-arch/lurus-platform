package repo

import (
	"context"
	"testing"
)

// TestAdminSettingsRepo_GetAll_Empty verifies an empty table returns an empty slice.
func TestAdminSettingsRepo_GetAll_Empty(t *testing.T) {
	db := setupAdminSettingsDB(t)
	r := NewAdminSettingsRepo(db)

	settings, err := r.GetAll(context.Background())
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	if len(settings) != 0 {
		t.Errorf("expected empty slice, got %d rows", len(settings))
	}
}

// TestAdminSettingsRepo_GetAll_WithRows verifies all inserted rows are returned.
func TestAdminSettingsRepo_GetAll_WithRows(t *testing.T) {
	db := setupAdminSettingsDB(t)
	r := NewAdminSettingsRepo(db)
	ctx := context.Background()

	if err := r.Set(ctx, "key.one", "val1", "system"); err != nil {
		t.Fatalf("Set key.one: %v", err)
	}
	if err := r.Set(ctx, "key.two", "val2", "admin"); err != nil {
		t.Fatalf("Set key.two: %v", err)
	}

	settings, err := r.GetAll(ctx)
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	if len(settings) != 2 {
		t.Errorf("expected 2 rows, got %d", len(settings))
	}
}

// TestAdminSettingsRepo_Set_Insert verifies a new key is inserted correctly.
func TestAdminSettingsRepo_Set_Insert(t *testing.T) {
	db := setupAdminSettingsDB(t)
	r := NewAdminSettingsRepo(db)
	ctx := context.Background()

	if err := r.Set(ctx, "grace_period_days", "5", "admin"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	all, err := r.GetAll(ctx)
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 setting, got %d", len(all))
	}
	if all[0].Key != "grace_period_days" {
		t.Errorf("Key = %q, want %q", all[0].Key, "grace_period_days")
	}
	if all[0].Value != "5" {
		t.Errorf("Value = %q, want %q", all[0].Value, "5")
	}
	if all[0].UpdatedBy != "admin" {
		t.Errorf("UpdatedBy = %q, want %q", all[0].UpdatedBy, "admin")
	}
}

// TestAdminSettingsRepo_Set_Update verifies setting the same key updates the value (upsert).
func TestAdminSettingsRepo_Set_Update(t *testing.T) {
	db := setupAdminSettingsDB(t)
	r := NewAdminSettingsRepo(db)
	ctx := context.Background()

	if err := r.Set(ctx, "feature_flag", "false", "system"); err != nil {
		t.Fatalf("initial Set: %v", err)
	}

	// Update same key with a new value.
	if err := r.Set(ctx, "feature_flag", "true", "operator"); err != nil {
		t.Fatalf("upsert Set: %v", err)
	}

	all, err := r.GetAll(ctx)
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	// Should still have exactly one row.
	if len(all) != 1 {
		t.Errorf("expected 1 setting after upsert, got %d", len(all))
	}
	if all[0].Value != "true" {
		t.Errorf("Value = %q, want %q", all[0].Value, "true")
	}
	if all[0].UpdatedBy != "operator" {
		t.Errorf("UpdatedBy = %q, want %q", all[0].UpdatedBy, "operator")
	}
}

// TestAdminSettingsRepo_Set_MultipleKeys verifies distinct keys coexist without conflict.
func TestAdminSettingsRepo_Set_MultipleKeys(t *testing.T) {
	db := setupAdminSettingsDB(t)
	r := NewAdminSettingsRepo(db)
	ctx := context.Background()

	keys := []string{"alpha", "beta", "gamma"}
	for _, k := range keys {
		if err := r.Set(ctx, k, "v", "system"); err != nil {
			t.Fatalf("Set %s: %v", k, err)
		}
	}

	all, err := r.GetAll(ctx)
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("expected 3 settings, got %d", len(all))
	}
}
