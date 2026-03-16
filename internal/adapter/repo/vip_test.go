package repo

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

func TestVIPRepo_GetOrCreate(t *testing.T) {
	db := setupTestDB(t)
	repo := NewVIPRepo(db)
	ctx := context.Background()

	v, err := repo.GetOrCreate(ctx, 1)
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	if v == nil || v.AccountID != 1 {
		t.Fatalf("got %+v", v)
	}
	if v.Level != 0 {
		t.Errorf("Level = %d, want 0", v.Level)
	}

	// Second call returns the same record
	v2, _ := repo.GetOrCreate(ctx, 1)
	if v2.AccountID != v.AccountID {
		t.Error("expected same record on second call")
	}
}

func TestVIPRepo_GetByAccountID(t *testing.T) {
	db := setupTestDB(t)
	repo := NewVIPRepo(db)
	ctx := context.Background()

	// Not found
	got, err := repo.GetByAccountID(ctx, 999)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != nil {
		t.Error("expected nil")
	}

	// After create
	repo.GetOrCreate(ctx, 1)
	got, err = repo.GetByAccountID(ctx, 1)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil")
	}
}

func TestVIPRepo_Update(t *testing.T) {
	db := setupTestDB(t)
	repo := NewVIPRepo(db)
	ctx := context.Background()

	repo.GetOrCreate(ctx, 1)

	updated := &entity.AccountVIP{
		AccountID: 1, Level: 3, LevelName: "Gold", Points: 500,
	}
	if err := repo.Update(ctx, updated); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, _ := repo.GetByAccountID(ctx, 1)
	if got.Level != 3 {
		t.Errorf("Level = %d, want 3", got.Level)
	}
	if got.LevelName != "Gold" {
		t.Errorf("LevelName = %q, want Gold", got.LevelName)
	}
	if got.Points != 500 {
		t.Errorf("Points = %d, want 500", got.Points)
	}
}

func TestVIPRepo_ListConfigs(t *testing.T) {
	db := setupTestDB(t)
	repo := NewVIPRepo(db)
	ctx := context.Background()

	// Seed configs (start from Level 1 because GORM omits zero-value PKs)
	db.Create(&entity.VIPLevelConfig{Level: 1, Name: "Silver", PerksJSON: json.RawMessage(`{}`)})
	db.Create(&entity.VIPLevelConfig{Level: 2, Name: "Gold", PerksJSON: json.RawMessage(`{}`)})
	db.Create(&entity.VIPLevelConfig{Level: 3, Name: "Platinum", PerksJSON: json.RawMessage(`{}`)})

	list, err := repo.ListConfigs(ctx)
	if err != nil {
		t.Fatalf("ListConfigs: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("len = %d, want 3", len(list))
	}
	if list[0].Name != "Silver" {
		t.Errorf("first config = %q, want Silver", list[0].Name)
	}
}
