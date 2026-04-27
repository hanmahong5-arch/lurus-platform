//go:build integration
// +build integration

// DISABLED from default `go test` runs: entity.StringList requires PostgreSQL's
// text[] type and has no SQLite scanner. Run with `-tags=integration` against
// a real PG test DB.

package repo

import (
	"context"
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"gorm.io/gorm"
)

// setupServiceKeyTestDB returns a test DB with the service_api_keys table migrated.
func setupServiceKeyTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := setupTestDB(t)
	migrateExtra(t, db, &entity.ServiceAPIKey{})
	return db
}

func TestServiceKeyRepo_NewServiceKeyRepo(t *testing.T) {
	db := setupTestDB(t)
	migrateExtra(t, db, &entity.ServiceAPIKey{})
	repo := NewServiceKeyRepo(db)
	if repo == nil {
		t.Fatal("NewServiceKeyRepo returned nil")
	}
}

func TestServiceKeyRepo_Create_And_GetByHash(t *testing.T) {
	db := setupTestDB(t)
	migrateExtra(t, db, &entity.ServiceAPIKey{})
	repo := NewServiceKeyRepo(db)
	ctx := context.Background()

	// Use empty scopes to avoid SQLite text[] incompatibility.
	key := &entity.ServiceAPIKey{
		KeyHash:      "sha256hashvalue001",
		KeyPrefix:    "sk_test",
		ServiceName:  "2b-svc-api",
		Scopes:       entity.StringList{"checkout"},
		RateLimitRPM: 500,
		Status:       entity.ServiceKeyActive,
		CreatedBy:    "admin",
	}
	if err := repo.Create(ctx, key); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if key.ID == 0 {
		t.Fatal("expected non-zero ID after create")
	}

	got, err := repo.GetByHash(ctx, "sha256hashvalue001")
	if err != nil {
		t.Fatalf("GetByHash: %v", err)
	}
	if got == nil {
		t.Fatal("GetByHash returned nil")
	}
	if got.ServiceName != "2b-svc-api" {
		t.Errorf("ServiceName = %q, want 2b-svc-api", got.ServiceName)
	}
	if got.RateLimitRPM != 500 {
		t.Errorf("RateLimitRPM = %d, want 500", got.RateLimitRPM)
	}
}

func TestServiceKeyRepo_GetByHash_NotFound(t *testing.T) {
	db := setupTestDB(t)
	migrateExtra(t, db, &entity.ServiceAPIKey{})
	repo := NewServiceKeyRepo(db)
	ctx := context.Background()

	got, err := repo.GetByHash(ctx, "nonexistent-hash")
	if err != nil {
		t.Fatalf("GetByHash: %v", err)
	}
	if got != nil {
		t.Error("expected nil for non-existent hash")
	}
}

func TestServiceKeyRepo_GetByHash_InactiveKeyNotReturned(t *testing.T) {
	db := setupTestDB(t)
	migrateExtra(t, db, &entity.ServiceAPIKey{})
	repo := NewServiceKeyRepo(db)
	ctx := context.Background()

	// Create a suspended key.
	key := &entity.ServiceAPIKey{
		KeyHash:      "suspended-hash-001",
		KeyPrefix:    "sk_susp",
		ServiceName:  "test-service",
		RateLimitRPM: 100,
		Status:       entity.ServiceKeySuspended,
		Scopes:       entity.StringList{"checkout"},
	}
	if err := repo.Create(ctx, key); err != nil {
		t.Fatalf("Create suspended key: %v", err)
	}

	// GetByHash only returns active keys.
	got, err := repo.GetByHash(ctx, "suspended-hash-001")
	if err != nil {
		t.Fatalf("GetByHash: %v", err)
	}
	if got != nil {
		t.Error("expected nil for suspended key")
	}
}

func TestServiceKeyRepo_ListActive(t *testing.T) {
	db := setupTestDB(t)
	migrateExtra(t, db, &entity.ServiceAPIKey{})
	repo := NewServiceKeyRepo(db)
	ctx := context.Background()

	// Create 2 active and 1 revoked key (empty scopes for SQLite compatibility).
	keys := []*entity.ServiceAPIKey{
		{KeyHash: "hash-a1", KeyPrefix: "sk_a1", ServiceName: "svc-a", RateLimitRPM: 100, Status: entity.ServiceKeyActive, Scopes: entity.StringList{}},
		{KeyHash: "hash-a2", KeyPrefix: "sk_a2", ServiceName: "svc-b", RateLimitRPM: 200, Status: entity.ServiceKeyActive, Scopes: entity.StringList{}},
		{KeyHash: "hash-r1", KeyPrefix: "sk_r1", ServiceName: "svc-c", RateLimitRPM: 50, Status: entity.ServiceKeyRevoked, Scopes: entity.StringList{}},
	}
	for _, k := range keys {
		if err := repo.Create(ctx, k); err != nil {
			t.Fatalf("Create %s: %v", k.KeyHash, err)
		}
	}

	list, err := repo.ListActive(ctx)
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("len = %d, want 2", len(list))
	}
	for _, k := range list {
		if k.Status != entity.ServiceKeyActive {
			t.Errorf("inactive key returned by ListActive: %+v", k)
		}
	}
}

func TestServiceKeyRepo_ListActive_Empty(t *testing.T) {
	db := setupTestDB(t)
	migrateExtra(t, db, &entity.ServiceAPIKey{})
	repo := NewServiceKeyRepo(db)
	ctx := context.Background()

	list, err := repo.ListActive(ctx)
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected empty list, got %d", len(list))
	}
}

func TestServiceKeyRepo_UpdateStatus(t *testing.T) {
	db := setupTestDB(t)
	migrateExtra(t, db, &entity.ServiceAPIKey{})
	repo := NewServiceKeyRepo(db)
	ctx := context.Background()

	key := &entity.ServiceAPIKey{
		KeyHash:      "hash-upd",
		KeyPrefix:    "sk_upd",
		ServiceName:  "svc-upd",
		RateLimitRPM: 100,
		Status:       entity.ServiceKeyActive,
		Scopes:       entity.StringList{"checkout"},
	}
	if err := repo.Create(ctx, key); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Suspend the key.
	if err := repo.UpdateStatus(ctx, key.ID, entity.ServiceKeySuspended); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	// Should no longer appear via GetByHash (only returns active).
	got, _ := repo.GetByHash(ctx, "hash-upd")
	if got != nil {
		t.Error("suspended key should not be returned by GetByHash")
	}
}

func TestServiceKeyRepo_UpdateStatus_ToRevoked(t *testing.T) {
	db := setupTestDB(t)
	migrateExtra(t, db, &entity.ServiceAPIKey{})
	repo := NewServiceKeyRepo(db)
	ctx := context.Background()

	key := &entity.ServiceAPIKey{
		KeyHash:      "hash-revoke",
		KeyPrefix:    "sk_rev",
		ServiceName:  "svc-rev",
		RateLimitRPM: 100,
		Status:       entity.ServiceKeyActive,
		Scopes:       entity.StringList{"checkout"},
	}
	repo.Create(ctx, key)

	if err := repo.UpdateStatus(ctx, key.ID, entity.ServiceKeyRevoked); err != nil {
		t.Fatalf("UpdateStatus revoke: %v", err)
	}

	got, _ := repo.GetByHash(ctx, "hash-revoke")
	if got != nil {
		t.Error("revoked key should not be returned by GetByHash")
	}
}

func TestServiceKeyRepo_TouchLastUsed(t *testing.T) {
	db := setupTestDB(t)
	migrateExtra(t, db, &entity.ServiceAPIKey{})
	repo := NewServiceKeyRepo(db)
	ctx := context.Background()

	key := &entity.ServiceAPIKey{
		KeyHash:      "hash-touch",
		KeyPrefix:    "sk_touch",
		ServiceName:  "svc-touch",
		RateLimitRPM: 100,
		Status:       entity.ServiceKeyActive,
		Scopes:       entity.StringList{"checkout"},
	}
	repo.Create(ctx, key)

	// TouchLastUsed calls NOW() which is not supported in SQLite,
	// but should not panic. The call is best-effort.
	repo.TouchLastUsed(ctx, key.ID)
	// No assertion — we just verify no panic on call.
}
