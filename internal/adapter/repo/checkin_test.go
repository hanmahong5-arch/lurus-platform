package repo

import (
	"context"
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// TestCheckinRepo_Create_Success verifies a checkin can be inserted and retrieved.
func TestCheckinRepo_Create_Success(t *testing.T) {
	db := setupCheckinDB(t)
	r := NewCheckinRepo(db)
	ctx := context.Background()

	c := &entity.Checkin{
		AccountID:   1,
		CheckinDate: "2026-01-01",
		RewardType:  "credits",
		RewardValue: 1.0,
	}
	if err := r.Create(ctx, c); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if c.ID == 0 {
		t.Error("expected non-zero ID after create")
	}
}

// TestCheckinRepo_Create_DuplicateDate verifies that creating a second checkin on the same date fails.
func TestCheckinRepo_Create_DuplicateDate(t *testing.T) {
	db := setupCheckinDB(t)
	r := NewCheckinRepo(db)
	ctx := context.Background()

	c1 := &entity.Checkin{AccountID: 1, CheckinDate: "2026-01-02", RewardValue: 1.0}
	if err := r.Create(ctx, c1); err != nil {
		t.Fatalf("first Create: %v", err)
	}

	c2 := &entity.Checkin{AccountID: 1, CheckinDate: "2026-01-02", RewardValue: 1.0}
	if err := r.Create(ctx, c2); err == nil {
		t.Error("expected unique constraint violation for duplicate date, got nil")
	}
}

// TestCheckinRepo_GetByAccountAndDate_Found verifies retrieval by account and date.
func TestCheckinRepo_GetByAccountAndDate_Found(t *testing.T) {
	db := setupCheckinDB(t)
	r := NewCheckinRepo(db)
	ctx := context.Background()

	_ = r.Create(ctx, &entity.Checkin{AccountID: 10, CheckinDate: "2026-02-01", RewardValue: 2.5})

	got, err := r.GetByAccountAndDate(ctx, 10, "2026-02-01")
	if err != nil {
		t.Fatalf("GetByAccountAndDate: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil checkin")
	}
	if got.AccountID != 10 {
		t.Errorf("AccountID = %d, want 10", got.AccountID)
	}
	if got.RewardValue != 2.5 {
		t.Errorf("RewardValue = %.2f, want 2.5", got.RewardValue)
	}
}

// TestCheckinRepo_GetByAccountAndDate_NotFound verifies nil is returned when no checkin exists.
func TestCheckinRepo_GetByAccountAndDate_NotFound(t *testing.T) {
	db := setupCheckinDB(t)
	r := NewCheckinRepo(db)

	got, err := r.GetByAccountAndDate(context.Background(), 99, "2099-01-01")
	if err != nil {
		t.Fatalf("GetByAccountAndDate: %v", err)
	}
	if got != nil {
		t.Error("expected nil for non-existent checkin")
	}
}

// TestCheckinRepo_ListByAccountAndMonth_Success verifies month-based listing.
func TestCheckinRepo_ListByAccountAndMonth_Success(t *testing.T) {
	db := setupCheckinDB(t)
	r := NewCheckinRepo(db)
	ctx := context.Background()

	_ = r.Create(ctx, &entity.Checkin{AccountID: 5, CheckinDate: "2026-03-01", RewardValue: 1.0})
	_ = r.Create(ctx, &entity.Checkin{AccountID: 5, CheckinDate: "2026-03-03", RewardValue: 1.0})
	_ = r.Create(ctx, &entity.Checkin{AccountID: 5, CheckinDate: "2026-04-01", RewardValue: 1.0}) // different month
	_ = r.Create(ctx, &entity.Checkin{AccountID: 6, CheckinDate: "2026-03-02", RewardValue: 1.0}) // different account

	list, err := r.ListByAccountAndMonth(ctx, 5, "2026-03")
	if err != nil {
		t.Fatalf("ListByAccountAndMonth: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("got %d checkins, want 2", len(list))
	}
}

// TestCheckinRepo_ListByAccountAndMonth_Empty verifies empty list when no checkins for the month.
func TestCheckinRepo_ListByAccountAndMonth_Empty(t *testing.T) {
	db := setupCheckinDB(t)
	r := NewCheckinRepo(db)

	list, err := r.ListByAccountAndMonth(context.Background(), 99, "2099-12")
	if err != nil {
		t.Fatalf("ListByAccountAndMonth: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected empty list, got %d", len(list))
	}
}

// TestCheckinRepo_CountConsecutive_Sequence verifies consecutive day counting.
func TestCheckinRepo_CountConsecutive_Sequence(t *testing.T) {
	db := setupCheckinDB(t)
	r := NewCheckinRepo(db)
	ctx := context.Background()

	// Create 3 consecutive days.
	_ = r.Create(ctx, &entity.Checkin{AccountID: 7, CheckinDate: "2026-03-05", RewardValue: 1.0})
	_ = r.Create(ctx, &entity.Checkin{AccountID: 7, CheckinDate: "2026-03-06", RewardValue: 1.0})
	_ = r.Create(ctx, &entity.Checkin{AccountID: 7, CheckinDate: "2026-03-07", RewardValue: 1.0})

	count, err := r.CountConsecutive(ctx, 7, "2026-03-07")
	if err != nil {
		t.Fatalf("CountConsecutive: %v", err)
	}
	if count != 3 {
		t.Errorf("count = %d, want 3", count)
	}
}

// TestCheckinRepo_CountConsecutive_Gap verifies consecutive count stops at a gap.
func TestCheckinRepo_CountConsecutive_Gap(t *testing.T) {
	db := setupCheckinDB(t)
	r := NewCheckinRepo(db)
	ctx := context.Background()

	// Day 10 and 12 — gap on day 11.
	_ = r.Create(ctx, &entity.Checkin{AccountID: 8, CheckinDate: "2026-03-10", RewardValue: 1.0})
	_ = r.Create(ctx, &entity.Checkin{AccountID: 8, CheckinDate: "2026-03-12", RewardValue: 1.0})

	count, err := r.CountConsecutive(ctx, 8, "2026-03-12")
	if err != nil {
		t.Fatalf("CountConsecutive: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1 (gap breaks streak)", count)
	}
}

// TestCheckinRepo_CountConsecutive_NoCheckin verifies 0 when no checkin exists for the date.
func TestCheckinRepo_CountConsecutive_NoCheckin(t *testing.T) {
	db := setupCheckinDB(t)
	r := NewCheckinRepo(db)

	count, err := r.CountConsecutive(context.Background(), 99, "2099-01-01")
	if err != nil {
		t.Fatalf("CountConsecutive: %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
}

// TestCheckinRepo_CountConsecutive_SingleDay verifies count of 1 for a single checkin.
func TestCheckinRepo_CountConsecutive_SingleDay(t *testing.T) {
	db := setupCheckinDB(t)
	r := NewCheckinRepo(db)
	ctx := context.Background()

	_ = r.Create(ctx, &entity.Checkin{AccountID: 11, CheckinDate: "2026-05-01", RewardValue: 1.0})

	count, err := r.CountConsecutive(ctx, 11, "2026-05-01")
	if err != nil {
		t.Fatalf("CountConsecutive: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
}

// TestSubtractDay_InvalidDate covers the error return path of subtractDay.
func TestSubtractDay_InvalidDate(t *testing.T) {
	got := subtractDay("not-a-valid-date")
	if got != "" {
		t.Errorf("subtractDay(invalid) = %q, want empty string", got)
	}
}
