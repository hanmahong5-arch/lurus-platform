package repo

import (
	"context"
	"fmt"
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

func TestAccountRepo_CreateAndGetByID(t *testing.T) {
	db := setupTestDB(t)
	repo := NewAccountRepo(db)
	ctx := context.Background()

	acct := &entity.Account{
		LurusID:     "LU0000001",
		ZitadelSub:  "sub-123",
		DisplayName: "Test User",
		Email:       "test@example.com",
		AffCode:     "AFF001",
	}
	if err := repo.Create(ctx, acct); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if acct.ID == 0 {
		t.Fatal("expected non-zero ID after create")
	}

	got, err := repo.GetByID(ctx, acct.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got == nil {
		t.Fatal("GetByID returned nil")
	}
	if got.Email != "test@example.com" {
		t.Errorf("Email = %q, want %q", got.Email, "test@example.com")
	}
	if got.DisplayName != "Test User" {
		t.Errorf("DisplayName = %q, want %q", got.DisplayName, "Test User")
	}
}

func TestAccountRepo_GetByID_NotFound(t *testing.T) {
	db := setupTestDB(t)
	repo := NewAccountRepo(db)

	got, err := repo.GetByID(context.Background(), 999)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got != nil {
		t.Error("expected nil for non-existent ID")
	}
}

func TestAccountRepo_GetByEmail(t *testing.T) {
	db := setupTestDB(t)
	repo := NewAccountRepo(db)
	ctx := context.Background()

	repo.Create(ctx, &entity.Account{
		LurusID: "LU0000002", ZitadelSub: "sub-e",
		DisplayName: "Email User", Email: "user@example.com", AffCode: "AFF002",
	})

	got, err := repo.GetByEmail(ctx, "user@example.com")
	if err != nil {
		t.Fatalf("GetByEmail: %v", err)
	}
	if got == nil || got.LurusID != "LU0000002" {
		t.Errorf("got %+v, want LurusID=LU0000002", got)
	}

	got, err = repo.GetByEmail(ctx, "nobody@example.com")
	if err != nil {
		t.Fatalf("GetByEmail not found: %v", err)
	}
	if got != nil {
		t.Error("expected nil for non-existent email")
	}
}

func TestAccountRepo_GetByZitadelSub(t *testing.T) {
	db := setupTestDB(t)
	repo := NewAccountRepo(db)
	ctx := context.Background()

	repo.Create(ctx, &entity.Account{
		LurusID: "LU0000003", ZitadelSub: "zit-abc",
		DisplayName: "Zit User", Email: "zit@example.com", AffCode: "AFF003",
	})

	got, err := repo.GetByZitadelSub(ctx, "zit-abc")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got == nil || got.Email != "zit@example.com" {
		t.Errorf("got %+v", got)
	}

	got, _ = repo.GetByZitadelSub(ctx, "nonexistent")
	if got != nil {
		t.Error("expected nil")
	}
}

func TestAccountRepo_GetByLurusID(t *testing.T) {
	db := setupTestDB(t)
	repo := NewAccountRepo(db)
	ctx := context.Background()

	repo.Create(ctx, &entity.Account{
		LurusID: "LU0000004", ZitadelSub: "sub-l",
		DisplayName: "Lurus User", Email: "lu@example.com", AffCode: "AFF004",
	})

	got, err := repo.GetByLurusID(ctx, "LU0000004")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got == nil || got.DisplayName != "Lurus User" {
		t.Errorf("got %+v", got)
	}
}

func TestAccountRepo_GetByAffCode(t *testing.T) {
	db := setupTestDB(t)
	repo := NewAccountRepo(db)
	ctx := context.Background()

	repo.Create(ctx, &entity.Account{
		LurusID: "LU0000005", ZitadelSub: "sub-a",
		DisplayName: "Aff User", Email: "aff@example.com", AffCode: "MYAFF",
	})

	got, err := repo.GetByAffCode(ctx, "MYAFF")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got == nil || got.Email != "aff@example.com" {
		t.Errorf("got %+v", got)
	}
}

func TestAccountRepo_Update(t *testing.T) {
	db := setupTestDB(t)
	repo := NewAccountRepo(db)
	ctx := context.Background()

	acct := &entity.Account{
		LurusID: "LU0000006", ZitadelSub: "sub-u",
		DisplayName: "Before", Email: "upd@example.com", AffCode: "AFF006",
	}
	repo.Create(ctx, acct)

	acct.DisplayName = "After"
	if err := repo.Update(ctx, acct); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, _ := repo.GetByID(ctx, acct.ID)
	if got.DisplayName != "After" {
		t.Errorf("DisplayName = %q, want After", got.DisplayName)
	}
}

func TestAccountRepo_List(t *testing.T) {
	db := setupTestDB(t)
	repo := NewAccountRepo(db)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		repo.Create(ctx, &entity.Account{
			LurusID:     fmt.Sprintf("LU%07d", i+10),
			ZitadelSub:  fmt.Sprintf("sub-list-%d", i),
			DisplayName: fmt.Sprintf("User%d", i),
			Email:       fmt.Sprintf("user%d@example.com", i),
			AffCode:     fmt.Sprintf("AFL%d", i),
		})
	}

	list, total, err := repo.List(ctx, "", 1, 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 5 {
		t.Errorf("total = %d, want 5", total)
	}
	if len(list) != 5 {
		t.Errorf("len = %d, want 5", len(list))
	}

	// Pagination
	list, total, err = repo.List(ctx, "", 1, 2)
	if err != nil {
		t.Fatalf("List page: %v", err)
	}
	if total != 5 {
		t.Errorf("total = %d, want 5", total)
	}
	if len(list) != 2 {
		t.Errorf("len = %d, want 2", len(list))
	}
}

func TestAccountRepo_OAuthBindings(t *testing.T) {
	db := setupTestDB(t)
	repo := NewAccountRepo(db)
	ctx := context.Background()

	acct := &entity.Account{
		LurusID: "LU0000007", ZitadelSub: "sub-oauth",
		DisplayName: "OAuth User", Email: "oauth@example.com", AffCode: "AFF007",
	}
	repo.Create(ctx, acct)

	binding := &entity.OAuthBinding{
		AccountID:     acct.ID,
		Provider:      "github",
		ProviderID:    "gh-123",
		ProviderEmail: "gh@example.com",
	}
	if err := repo.UpsertOAuthBinding(ctx, binding); err != nil {
		t.Fatalf("UpsertOAuthBinding: %v", err)
	}

	bindings, err := repo.GetOAuthBindings(ctx, acct.ID)
	if err != nil {
		t.Fatalf("GetOAuthBindings: %v", err)
	}
	if len(bindings) != 1 || bindings[0].Provider != "github" {
		t.Errorf("bindings = %+v, want 1 github binding", bindings)
	}

	// Upsert same provider (should update, not duplicate)
	binding2 := &entity.OAuthBinding{
		AccountID:     acct.ID,
		Provider:      "github",
		ProviderID:    "gh-123",
		ProviderEmail: "new@example.com",
	}
	if err := repo.UpsertOAuthBinding(ctx, binding2); err != nil {
		t.Fatalf("Upsert update: %v", err)
	}

	bindings, _ = repo.GetOAuthBindings(ctx, acct.ID)
	if len(bindings) != 1 {
		t.Errorf("after upsert: count = %d, want 1", len(bindings))
	}
}
