package app

import (
	"context"
	"sync"
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// makeWalletServiceFin creates a WalletService with in-memory mocks (includes vipStore reference).
func makeWalletServiceFin() (*WalletService, *mockWalletStore, *mockVIPStore) {
	wallets := newMockWalletStore()
	vipStore := newMockVIPStore(nil)
	vipSvc := NewVIPService(vipStore, wallets)
	svc := NewWalletService(wallets, vipSvc)
	return svc, wallets, vipStore
}

// TestWalletService_Debit_ExactBalance verifies that debit of exact balance leaves zero.
func TestWalletService_Debit_ExactBalance(t *testing.T) {
	svc, wallets, _ := makeWalletServiceFin()
	const accountID int64 = 100
	const amount = 10.5

	wallets.Credit(context.Background(), accountID, amount, entity.TxTypeTopup, "", "", "", "")

	_, err := svc.Debit(context.Background(), accountID, amount, entity.TxTypeProductPurchase, "test", "", "", "")
	if err != nil {
		t.Fatalf("Debit exact balance: %v", err)
	}

	wallet, _ := wallets.GetByAccountID(context.Background(), accountID)
	if absFloat64(wallet.Balance) > 0.00001 {
		t.Errorf("balance after exact debit = %.4f, want 0", wallet.Balance)
	}
}

// TestWalletService_Debit_InsufficientBalance verifies that insufficient balance is rejected.
func TestWalletService_Debit_InsufficientBalance(t *testing.T) {
	svc, wallets, _ := makeWalletServiceFin()
	const accountID int64 = 101

	wallets.Credit(context.Background(), accountID, 5.0, entity.TxTypeTopup, "", "", "", "")

	_, err := svc.Debit(context.Background(), accountID, 10.0, entity.TxTypeProductPurchase, "test", "", "", "")
	if err == nil {
		t.Fatal("debit exceeding balance should fail")
	}
	if !stringContains(err.Error(), "insufficient") {
		t.Errorf("want 'insufficient' in error, got: %v", err)
	}
}

// TestWalletService_Debit_ZeroAmount verifies zero debit does not change balance.
func TestWalletService_Debit_ZeroAmount(t *testing.T) {
	svc, wallets, _ := makeWalletServiceFin()
	const accountID int64 = 102

	wallets.Credit(context.Background(), accountID, 10.0, entity.TxTypeTopup, "", "", "", "")

	_, err := svc.Debit(context.Background(), accountID, 0, entity.TxTypeProductPurchase, "zero debit", "", "", "")
	if err != nil {
		t.Fatalf("zero debit: %v", err)
	}

	wallet, _ := wallets.GetByAccountID(context.Background(), accountID)
	if absFloat64(wallet.Balance-10.0) > 0.00001 {
		t.Errorf("balance after zero debit = %.4f, want 10.0", wallet.Balance)
	}
}

// TestWalletService_Debit_SmallDecimal verifies micro-amount precision (0.0001).
func TestWalletService_Debit_SmallDecimal(t *testing.T) {
	svc, wallets, _ := makeWalletServiceFin()
	const accountID int64 = 103

	wallets.Credit(context.Background(), accountID, 1.0, entity.TxTypeTopup, "", "", "", "")

	// Debit 0.0001
	_, err := svc.Debit(context.Background(), accountID, 0.0001, entity.TxTypeProductPurchase, "small debit", "", "", "")
	if err != nil {
		t.Fatalf("small decimal debit: %v", err)
	}

	wallet, _ := wallets.GetByAccountID(context.Background(), accountID)
	expected := 1.0 - 0.0001
	if absFloat64(wallet.Balance-expected) > 0.000001 {
		t.Errorf("balance = %.6f, want %.6f", wallet.Balance, expected)
	}
}

// TestWalletService_Credit_MultipleTimes verifies cumulative credits.
func TestWalletService_Credit_MultipleTimes(t *testing.T) {
	svc, wallets, _ := makeWalletServiceFin()
	const accountID int64 = 104

	for i := 0; i < 10; i++ {
		_, err := svc.Credit(context.Background(), accountID, 0.0001, entity.TxTypeReferralReward, "bonus", "ref", "1", "")
		if err != nil {
			t.Fatalf("credit %d: %v", i, err)
		}
	}

	wallet, _ := wallets.GetByAccountID(context.Background(), accountID)
	expected := 10 * 0.0001
	if absFloat64(wallet.Balance-expected) > 0.000001 {
		t.Errorf("balance = %.6f, want %.6f", wallet.Balance, expected)
	}
}

// TestWalletService_Topup_LifetimeTopupAccumulates verifies lifetime_topup tracking.
func TestWalletService_Topup_LifetimeTopupAccumulates(t *testing.T) {
	svc, wallets, _ := makeWalletServiceFin()
	const accountID int64 = 105

	_, _ = svc.Topup(context.Background(), accountID, 50.0, "order-001")
	_, _ = svc.Topup(context.Background(), accountID, 30.0, "order-002")

	wallet, _ := wallets.GetByAccountID(context.Background(), accountID)
	expectedLifetime := 80.0
	if absFloat64(wallet.LifetimeTopup-expectedLifetime) > 0.00001 {
		t.Errorf("LifetimeTopup = %.2f, want %.2f", wallet.LifetimeTopup, expectedLifetime)
	}
}

// TestWalletService_ConcurrentDebit_NoOverdraft verifies no overdraft under concurrency.
func TestWalletService_ConcurrentDebit_NoOverdraft(t *testing.T) {
	_, wallets, _ := makeWalletServiceFin()
	const accountID int64 = 106
	const initialBalance = 10.0
	const debitAmount = 1.0
	const goroutines = 20 // 20 goroutines each trying to debit 1.0 from balance of 10.0

	wallets.Credit(context.Background(), accountID, initialBalance, entity.TxTypeTopup, "", "", "", "")

	var wg sync.WaitGroup
	wg.Add(goroutines)
	var mu sync.Mutex
	successCount := 0

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			_, err := wallets.Debit(context.Background(), accountID, debitAmount, entity.TxTypeProductPurchase, "concurrent debit", "", "", "")
			if err == nil {
				mu.Lock()
				successCount++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	// At most initialBalance/debitAmount debits should succeed.
	maxSuccessful := int(initialBalance / debitAmount)
	if successCount > maxSuccessful {
		t.Errorf("concurrent debits: %d succeeded, want <= %d (no overdraft)", successCount, maxSuccessful)
	}

	// Final balance must never be negative.
	wallet, _ := wallets.GetByAccountID(context.Background(), accountID)
	if wallet.Balance < -0.00001 {
		t.Errorf("balance went negative: %.4f", wallet.Balance)
	}
}

// TestWalletService_ConcurrentCredit_FinalBalance verifies concurrent credits sum correctly.
func TestWalletService_ConcurrentCredit_FinalBalance(t *testing.T) {
	_, wallets, _ := makeWalletServiceFin()
	const accountID int64 = 107
	const goroutines = 50
	const creditAmount = 1.0

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			wallets.Credit(context.Background(), accountID, creditAmount, entity.TxTypeReferralReward, "concurrent credit", "", "", "")
		}()
	}
	wg.Wait()

	wallet, _ := wallets.GetByAccountID(context.Background(), accountID)
	expectedBalance := float64(goroutines) * creditAmount
	if absFloat64(wallet.Balance-expectedBalance) > 0.00001 {
		t.Errorf("final balance = %.2f, want %.2f (concurrent credits)", wallet.Balance, expectedBalance)
	}
}

// TestWalletService_Topup_OrderNoUnique verifies each topup gets a unique order number.
func TestWalletService_Topup_OrderNoUnique(t *testing.T) {
	svc, _, _ := makeWalletServiceFin()
	const accountID int64 = 108

	order1, err1 := svc.CreateTopup(context.Background(), accountID, 10.0, "stripe")
	if err1 != nil {
		t.Fatalf("CreateTopup 1: %v", err1)
	}
	order2, err2 := svc.CreateTopup(context.Background(), accountID, 20.0, "stripe")
	if err2 != nil {
		t.Fatalf("CreateTopup 2: %v", err2)
	}
	if order1.OrderNo == order2.OrderNo {
		t.Errorf("duplicate order number %q — order numbers must be unique", order1.OrderNo)
	}
}

// TestWalletService_CreateTopup_LargeAmount verifies that large amounts are accepted.
func TestWalletService_CreateTopup_LargeAmount(t *testing.T) {
	svc, _, _ := makeWalletServiceFin()
	const accountID int64 = 109

	_, err := svc.CreateTopup(context.Background(), accountID, 99999.99, "stripe")
	if err != nil {
		t.Errorf("large topup amount should be accepted: %v", err)
	}
}

// TestWalletService_Redeem_ValidCode verifies redemption code credit.
func TestWalletService_Redeem_ValidCode(t *testing.T) {
	svc, wallets, _ := makeWalletServiceFin()
	const accountID int64 = 110
	const rewardValue = 25.0

	// Seed a valid redemption code in the wallet store.
	now := context.Background()
	wallets.codes["TESTCODE01"] = &entity.RedemptionCode{
		Code:        "TESTCODE01",
		RewardType:  "credits",
		RewardValue: rewardValue,
		MaxUses:     1,
		UsedCount:   0,
	}

	err := svc.Redeem(now, accountID, "testcode01") // lowercase should work
	if err != nil {
		t.Fatalf("Redeem: %v", err)
	}

	wallet, _ := wallets.GetByAccountID(now, accountID)
	if absFloat64(wallet.Balance-rewardValue) > 0.00001 {
		t.Errorf("balance = %.2f, want %.2f after redemption", wallet.Balance, rewardValue)
	}
}

// TestWalletService_Redeem_DoubleRedeem verifies that a code cannot be used twice.
func TestWalletService_Redeem_DoubleRedeem(t *testing.T) {
	svc, wallets, _ := makeWalletServiceFin()
	const accountID int64 = 111

	wallets.codes["ONETIME01"] = &entity.RedemptionCode{
		Code:        "ONETIME01",
		RewardType:  "credits",
		RewardValue: 5.0,
		MaxUses:     1,
		UsedCount:   0,
	}

	// First use succeeds.
	if err := svc.Redeem(context.Background(), accountID, "ONETIME01"); err != nil {
		t.Fatalf("first Redeem: %v", err)
	}
	// Second use must fail (exhausted).
	if err := svc.Redeem(context.Background(), accountID, "ONETIME01"); err == nil {
		t.Fatal("second Redeem of exhausted code should fail")
	}
}

// TestWalletService_Redeem_ExhaustedCode verifies exhausted code is rejected.
func TestWalletService_Redeem_ExhaustedCode(t *testing.T) {
	svc, wallets, _ := makeWalletServiceFin()
	const accountID int64 = 112

	wallets.codes["EXHAUSTED1"] = &entity.RedemptionCode{
		Code:        "EXHAUSTED1",
		RewardType:  "credits",
		RewardValue: 10.0,
		MaxUses:     1,
		UsedCount:   1, // already used
	}

	err := svc.Redeem(context.Background(), accountID, "EXHAUSTED1")
	if err == nil {
		t.Fatal("exhausted code should be rejected")
	}
}

// TestWalletService_GetOrderByNo_WrongOwner verifies that order enumeration is prevented.
func TestWalletService_GetOrderByNo_WrongOwner(t *testing.T) {
	svc, wallets, _ := makeWalletServiceFin()
	const ownerID int64 = 113
	const otherID int64 = 114

	// Create order for ownerID.
	order, _ := svc.CreateTopup(context.Background(), ownerID, 50.0, "stripe")
	wallets.GetOrCreate(context.Background(), ownerID)

	// Other user tries to get the same order.
	_, err := svc.GetOrderByNo(context.Background(), otherID, order.OrderNo)
	if err == nil {
		t.Fatal("other user should not be able to access order")
	}
}

// TestWalletService_Debit_WalletNotFound verifies error when wallet does not exist.
func TestWalletService_Debit_WalletNotFound(t *testing.T) {
	svc, _, _ := makeWalletServiceFin()
	const accountID int64 = 115

	// No wallet exists for this account.
	_, err := svc.Debit(context.Background(), accountID, 10.0, entity.TxTypeProductPurchase, "test", "", "", "")
	if err == nil {
		t.Fatal("debit without existing wallet should fail")
	}
}
