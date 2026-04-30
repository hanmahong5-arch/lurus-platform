package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// makeCheckinService builds a CheckinService with in-memory mocks.
func makeCheckinService() (*CheckinService, *mockCheckinStore, *mockWalletStore) {
	checkins := newMockCheckinStore()
	wallets := newMockWalletStore()
	return NewCheckinService(checkins, wallets), checkins, wallets
}

// seedCheckin inserts a checkin record for the given account and date.
func seedCheckin(store *mockCheckinStore, accountID int64, date string) {
	_ = store.Create(context.Background(), &entity.Checkin{
		AccountID:   accountID,
		CheckinDate: date,
		RewardType:  "credits",
		RewardValue: 0.10,
	})
}

// absFloat64 returns the absolute value.
func absFloat64(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// TestCheckinService_DoCheckin_Success verifies a first-time daily checkin succeeds.
func TestCheckinService_DoCheckin_Success(t *testing.T) {
	svc, checkins, wallets := makeCheckinService()
	const accountID int64 = 1

	wallets.GetOrCreate(context.Background(), accountID)

	result, err := svc.DoCheckin(context.Background(), accountID)
	if err != nil {
		t.Fatalf("DoCheckin: %v", err)
	}
	if result.RewardValue <= 0 {
		t.Errorf("RewardValue = %f, want > 0", result.RewardValue)
	}
	if result.RewardType != "credits" {
		t.Errorf("RewardType = %q, want 'credits'", result.RewardType)
	}
	if result.ConsecutiveDays < 1 {
		t.Errorf("ConsecutiveDays = %d, want >= 1", result.ConsecutiveDays)
	}

	// Verify checkin record created.
	today := time.Now().Format("2006-01-02")
	c, _ := checkins.GetByAccountAndDate(context.Background(), accountID, today)
	if c == nil {
		t.Error("checkin record should be created for today")
	}
}

// TestCheckinService_DoCheckin_AlreadyCheckedIn verifies duplicate daily checkin is rejected.
func TestCheckinService_DoCheckin_AlreadyCheckedIn(t *testing.T) {
	svc, checkins, _ := makeCheckinService()
	const accountID int64 = 2

	today := time.Now().Format("2006-01-02")
	seedCheckin(checkins, accountID, today)

	_, err := svc.DoCheckin(context.Background(), accountID)
	if err == nil {
		t.Fatal("second checkin today should fail")
	}
	if !stringContains(err.Error(), "already checked in") {
		t.Errorf("want 'already checked in' in error, got: %v", err)
	}
}

// TestCheckinService_DoCheckin_WalletCredit verifies that wallet balance increases after checkin.
func TestCheckinService_DoCheckin_WalletCredit(t *testing.T) {
	svc, _, wallets := makeCheckinService()
	const accountID int64 = 3

	wallets.GetOrCreate(context.Background(), accountID)
	walletBefore, _ := wallets.GetByAccountID(context.Background(), accountID)
	balanceBefore := walletBefore.Balance

	result, err := svc.DoCheckin(context.Background(), accountID)
	if err != nil {
		t.Fatalf("DoCheckin: %v", err)
	}

	walletAfter, _ := wallets.GetByAccountID(context.Background(), accountID)
	if walletAfter == nil {
		t.Fatal("wallet not found after checkin")
	}
	expectedBalance := balanceBefore + result.RewardValue
	if absFloat64(walletAfter.Balance-expectedBalance) > 0.00001 {
		t.Errorf("wallet balance = %.4f, want %.4f", walletAfter.Balance, expectedBalance)
	}
}

// TestCheckinService_GetStatus_CheckedInToday verifies status reflects today's checkin.
func TestCheckinService_GetStatus_CheckedInToday(t *testing.T) {
	svc, checkins, _ := makeCheckinService()
	const accountID int64 = 4

	today := time.Now().Format("2006-01-02")
	seedCheckin(checkins, accountID, today)

	status, err := svc.GetStatus(context.Background(), accountID)
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if !status.CheckedInToday {
		t.Error("CheckedInToday should be true")
	}
}

// TestCheckinService_GetStatus_NotCheckedIn verifies status for a fresh account.
func TestCheckinService_GetStatus_NotCheckedIn(t *testing.T) {
	svc, _, _ := makeCheckinService()
	const accountID int64 = 5

	status, err := svc.GetStatus(context.Background(), accountID)
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if status.CheckedInToday {
		t.Error("CheckedInToday should be false for new account")
	}
	if status.ConsecutiveDays != 0 {
		t.Errorf("ConsecutiveDays = %d, want 0", status.ConsecutiveDays)
	}
}

// TestCheckinService_GetStatus_WithHistory verifies month history is returned.
func TestCheckinService_GetStatus_WithHistory(t *testing.T) {
	svc, checkins, _ := makeCheckinService()
	const accountID int64 = 6

	now := time.Now()
	for i := 1; i <= 3; i++ {
		date := now.AddDate(0, 0, -i).Format("2006-01-02")
		seedCheckin(checkins, accountID, date)
	}

	status, err := svc.GetStatus(context.Background(), accountID)
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if len(status.MonthCheckins) < 3 {
		t.Errorf("MonthCheckins = %d, want >= 3", len(status.MonthCheckins))
	}
}

// TestCheckinService_GetStatus_EmptyMonth verifies empty month returns empty list.
func TestCheckinService_GetStatus_EmptyMonth(t *testing.T) {
	svc, _, _ := makeCheckinService()
	const accountID int64 = 60

	status, err := svc.GetStatus(context.Background(), accountID)
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if len(status.MonthCheckins) != 0 {
		t.Errorf("MonthCheckins = %d, want 0 for new account", len(status.MonthCheckins))
	}
}

// TestCheckinService_CalculateReward_BaseReward verifies day-1 reward equals base reward.
func TestCheckinService_CalculateReward_BaseReward(t *testing.T) {
	reward := calculateCheckinReward(1)
	if absFloat64(reward-checkinBaseReward) > 0.000001 {
		t.Errorf("day 1 reward = %.4f, want %.4f", reward, checkinBaseReward)
	}
}

// TestCheckinService_CalculateReward_StreakBonus verifies 7-day streak applies multiplier.
func TestCheckinService_CalculateReward_StreakBonus(t *testing.T) {
	reward7 := calculateCheckinReward(7)
	expected7 := checkinBaseReward * checkinStreakBonusFactor
	if absFloat64(reward7-expected7) > 0.000001 {
		t.Errorf("day 7 reward = %.4f, want %.4f", reward7, expected7)
	}
}

// TestCheckinService_CalculateReward_MaxCap verifies reward is capped at checkinMaxReward.
func TestCheckinService_CalculateReward_MaxCap(t *testing.T) {
	reward := calculateCheckinReward(100)
	if reward > checkinMaxReward+0.000001 {
		t.Errorf("reward = %.4f exceeds max cap %.4f", reward, checkinMaxReward)
	}
}

// TestCheckinService_CalculateReward_Range verifies all rewards are within valid bounds.
func TestCheckinService_CalculateReward_Range(t *testing.T) {
	for day := 1; day <= 365; day++ {
		reward := calculateCheckinReward(day)
		if reward < checkinBaseReward-0.000001 {
			t.Errorf("day %d reward %.4f < base %.4f", day, reward, checkinBaseReward)
		}
		if reward > checkinMaxReward+0.000001 {
			t.Errorf("day %d reward %.4f > max %.4f", day, reward, checkinMaxReward)
		}
	}
}

// TestCheckinService_ConsecutiveDays_Streak verifies 2-day streak.
func TestCheckinService_ConsecutiveDays_Streak(t *testing.T) {
	svc, checkins, wallets := makeCheckinService()
	const accountID int64 = 7

	wallets.GetOrCreate(context.Background(), accountID)
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	seedCheckin(checkins, accountID, yesterday)

	result, err := svc.DoCheckin(context.Background(), accountID)
	if err != nil {
		t.Fatalf("DoCheckin: %v", err)
	}
	if result.ConsecutiveDays != 2 {
		t.Errorf("ConsecutiveDays = %d, want 2 (yesterday + today)", result.ConsecutiveDays)
	}
}

// TestCheckinService_DoCheckin_Concurrent verifies only one concurrent checkin succeeds.
func TestCheckinService_DoCheckin_Concurrent(t *testing.T) {
	svc, _, wallets := makeCheckinService()
	const accountID int64 = 8

	wallets.GetOrCreate(context.Background(), accountID)

	const goroutines = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)
	var mu sync.Mutex
	successCount := 0

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			_, err := svc.DoCheckin(context.Background(), accountID)
			if err == nil {
				mu.Lock()
				successCount++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if successCount != 1 {
		t.Errorf("concurrent checkins: %d succeeded, want exactly 1", successCount)
	}
}

// TestCheckinService_DoCheckin_WalletBalanceAfter verifies WalletBalance field in result.
func TestCheckinService_DoCheckin_WalletBalanceAfter(t *testing.T) {
	svc, _, wallets := makeCheckinService()
	const accountID int64 = 9

	wallets.Credit(context.Background(), accountID, 5.0, "init", "", "", "", "")

	result, err := svc.DoCheckin(context.Background(), accountID)
	if err != nil {
		t.Fatalf("DoCheckin: %v", err)
	}

	expectedBalance := 5.0 + result.RewardValue
	if absFloat64(result.WalletBalance-expectedBalance) > 0.00001 {
		t.Errorf("WalletBalance = %.4f, want %.4f", result.WalletBalance, expectedBalance)
	}
}

// stringContains checks if s contains substr.
func stringContains(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	if len(s) < len(substr) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// errCheckinLookupStore wraps mockCheckinStore with configurable per-method errors.
type errCheckinLookupStore struct {
	*mockCheckinStore
	lookupErr error
	listErr   error
	countErr  error
}

func (s *errCheckinLookupStore) GetByAccountAndDate(ctx context.Context, accountID int64, date string) (*entity.Checkin, error) {
	if s.lookupErr != nil {
		return nil, s.lookupErr
	}
	return s.mockCheckinStore.GetByAccountAndDate(ctx, accountID, date)
}

func (s *errCheckinLookupStore) ListByAccountAndMonth(ctx context.Context, accountID int64, yearMonth string) ([]entity.Checkin, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.mockCheckinStore.ListByAccountAndMonth(ctx, accountID, yearMonth)
}

func (s *errCheckinLookupStore) CountConsecutive(ctx context.Context, accountID int64, date string) (int, error) {
	if s.countErr != nil {
		return 0, s.countErr
	}
	return s.mockCheckinStore.CountConsecutive(ctx, accountID, date)
}

// TestCheckinService_GetStatus_LookupError verifies GetStatus propagates GetByAccountAndDate error.
func TestCheckinService_GetStatus_LookupError(t *testing.T) {
	errStore := &errCheckinLookupStore{
		mockCheckinStore: newMockCheckinStore(),
		lookupErr:        fmt.Errorf("db unavailable"),
	}
	svc := NewCheckinService(errStore, newMockWalletStore())

	_, err := svc.GetStatus(context.Background(), 1)
	if err == nil {
		t.Error("expected error from GetByAccountAndDate, got nil")
	}
}

// TestCheckinService_GetStatus_CountError verifies GetStatus propagates CountConsecutive error.
func TestCheckinService_GetStatus_CountError(t *testing.T) {
	errStore := &errCheckinLookupStore{
		mockCheckinStore: newMockCheckinStore(),
		countErr:         fmt.Errorf("count db error"),
	}
	svc := NewCheckinService(errStore, newMockWalletStore())

	_, err := svc.GetStatus(context.Background(), 1)
	if err == nil {
		t.Error("expected error from CountConsecutive, got nil")
	}
}

// TestCheckinService_GetStatus_ListError verifies GetStatus propagates ListByAccountAndMonth error.
func TestCheckinService_GetStatus_ListError(t *testing.T) {
	errStore := &errCheckinLookupStore{
		mockCheckinStore: newMockCheckinStore(),
		listErr:          fmt.Errorf("list db error"),
	}
	svc := NewCheckinService(errStore, newMockWalletStore())

	_, err := svc.GetStatus(context.Background(), 1)
	if err == nil {
		t.Error("expected error from ListByAccountAndMonth, got nil")
	}
}

// TestCheckinService_DoCheckin_AlreadyToday returns ErrCheckinAlreadyToday
// (not a wrapped DB error) when the account already checked in today.
// This is the post-TOCTOU-fix flow: the service no longer reads-then-writes;
// the repo's ON CONFLICT path returns the typed sentinel directly.
func TestCheckinService_DoCheckin_AlreadyToday(t *testing.T) {
	store := newMockCheckinStore()
	// Pre-seed today's checkin so the next DoCheckin's Create hits the
	// uniqueness conflict.
	today := time.Now().Format("2006-01-02")
	_ = store.Create(context.Background(), &entity.Checkin{
		AccountID: 1, CheckinDate: today, RewardType: "credits", RewardValue: 0.10,
	})

	svc := NewCheckinService(store, newMockWalletStore())
	_, err := svc.DoCheckin(context.Background(), 1)
	if !errors.Is(err, ErrCheckinAlreadyToday) {
		t.Errorf("expected ErrCheckinAlreadyToday, got %v", err)
	}
}

// TestCheckinService_DoCheckin_CountError verifies DoCheckin propagates CountConsecutive error.
func TestCheckinService_DoCheckin_CountError(t *testing.T) {
	errStore := &errCheckinLookupStore{
		mockCheckinStore: newMockCheckinStore(),
		countErr:         fmt.Errorf("count failed"),
	}
	svc := NewCheckinService(errStore, newMockWalletStore())

	_, err := svc.DoCheckin(context.Background(), 1)
	if err == nil {
		t.Error("expected error from CountConsecutive, got nil")
	}
}

// TestCheckinService_DoCheckin_CreateError verifies DoCheckin propagates Create error.
func TestCheckinService_DoCheckin_CreateError(t *testing.T) {
	store := newMockCheckinStore()
	store.createErr = fmt.Errorf("insert failed")
	svc := NewCheckinService(store, newMockWalletStore())

	_, err := svc.DoCheckin(context.Background(), 1)
	if err == nil {
		t.Error("expected error from Create, got nil")
	}
}
