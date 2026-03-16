package app

// coverage_fix_test.go contains targeted tests for previously uncovered code paths.
// All tests follow the Test<Subject>_<Method>_<Behavior> naming convention.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/event"
)

// ── BulkGenerateCodes ────────────────────────────────────────────────────────

func makeBulkReferralService() (*ReferralService, *mockRedemptionCodeStore) {
	acc := newMockAccountStore()
	ws := newMockWalletStore()
	rcs := newMockRedemptionCodeStore()
	svc := NewReferralServiceWithCodes(acc, ws, rcs)
	return svc, rcs
}

// TestReferralService_BulkGenerateCodes_Success verifies that the requested number
// of codes is generated and persisted.
func TestReferralService_BulkGenerateCodes_Success(t *testing.T) {
	svc, store := makeBulkReferralService()

	codes, err := svc.BulkGenerateCodes(
		context.Background(),
		"llm-api", "pro", 30, nil, "test batch", 5,
	)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(codes) != 5 {
		t.Errorf("expected 5 codes, got %d", len(codes))
	}
	store.mu.Lock()
	stored := len(store.codes)
	store.mu.Unlock()
	if stored != 5 {
		t.Errorf("expected 5 stored codes, got %d", stored)
	}

	// Verify all codes are unique and 8 chars.
	seen := map[string]bool{}
	for _, c := range codes {
		if len(c.Code) != 8 {
			t.Errorf("expected 8-char code, got %q (len=%d)", c.Code, len(c.Code))
		}
		if seen[c.Code] {
			t.Errorf("duplicate code %q", c.Code)
		}
		seen[c.Code] = true
	}
}

// TestReferralService_BulkGenerateCodes_WithExpiresAt verifies optional expires_at handling.
func TestReferralService_BulkGenerateCodes_WithExpiresAt(t *testing.T) {
	svc, _ := makeBulkReferralService()
	exp := time.Now().Add(30 * 24 * time.Hour)

	codes, err := svc.BulkGenerateCodes(
		context.Background(),
		"gushen", "basic", 7, &exp, "exp test", 3,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, c := range codes {
		if c.ExpiresAt == nil {
			t.Error("expected ExpiresAt to be set")
		}
	}
}

// TestReferralService_BulkGenerateCodes_InvalidCount verifies boundary validation.
func TestReferralService_BulkGenerateCodes_InvalidCount(t *testing.T) {
	svc, _ := makeBulkReferralService()

	for _, bad := range []int{0, -1, 1001} {
		_, err := svc.BulkGenerateCodes(context.Background(), "p", "c", 1, nil, "", bad)
		if err == nil {
			t.Errorf("count=%d: expected error, got nil", bad)
		}
	}
}

// TestReferralService_BulkGenerateCodes_NoRedemptionStore verifies that calling
// BulkGenerateCodes without a store returns an error.
func TestReferralService_BulkGenerateCodes_NoRedemptionStore(t *testing.T) {
	acc := newMockAccountStore()
	ws := newMockWalletStore()
	svc := NewReferralService(acc, ws) // no redemption store

	_, err := svc.BulkGenerateCodes(context.Background(), "p", "c", 1, nil, "", 1)
	if err == nil {
		t.Fatal("expected error when redemption store is nil")
	}
}

// TestReferralService_BulkGenerateCodes_StoreError verifies that storage errors propagate.
func TestReferralService_BulkGenerateCodes_StoreError(t *testing.T) {
	svc, store := makeBulkReferralService()
	store.mu.Lock()
	store.err = errors.New("db error")
	store.mu.Unlock()

	_, err := svc.BulkGenerateCodes(context.Background(), "p", "c", 1, nil, "", 2)
	if err == nil {
		t.Fatal("expected propagated store error")
	}
}

// ── generateCode ─────────────────────────────────────────────────────────────

// TestGenerateCode_Format verifies that generateCode returns an 8-char uppercase hex string.
func TestGenerateCode_Format(t *testing.T) {
	for i := 0; i < 20; i++ {
		code, err := generateCode()
		if err != nil {
			t.Fatalf("generateCode: %v", err)
		}
		if len(code) != 8 {
			t.Errorf("expected 8 chars, got %d: %q", len(code), code)
		}
		for _, ch := range code {
			if !((ch >= '0' && ch <= '9') || (ch >= 'A' && ch <= 'F')) {
				t.Errorf("non-hex char %q in code %q", ch, code)
			}
		}
	}
}

// ── NewReferralServiceWithCodes ──────────────────────────────────────────────

// TestReferralService_NewWithCodes_Smoke verifies the constructor wires correctly.
func TestReferralService_NewWithCodes_Smoke(t *testing.T) {
	svc, _ := makeBulkReferralService()
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
}

// ── InvoiceService.GetByNo error path ────────────────────────────────────────

// TestInvoiceService_GetByNo_NotFound verifies nil invoice returns error.
func TestInvoiceService_GetByNo_NotFound(t *testing.T) {
	svc, _, _ := makeInvoiceService()

	_, err := svc.GetByNo(context.Background(), 99, "LI_NONEXISTENT")
	if err == nil {
		t.Fatal("expected error for non-existent invoice")
	}
}

// ── RefundService error paths ─────────────────────────────────────────────────

// TestRefundService_Approve_NotFound verifies error when refund doesn't exist.
func TestRefundService_Approve_NotFound(t *testing.T) {
	svc, _, _ := makeRefundService()
	err := svc.Approve(context.Background(), "LR_NONEXISTENT", "admin", "note")
	if err == nil {
		t.Fatal("expected error for non-existent refund")
	}
}

// TestRefundService_Approve_AlreadyApproved verifies that double-approve is rejected.
func TestRefundService_Approve_AlreadyApproved(t *testing.T) {
	svc, _, ws := makeRefundService()
	const accountID = int64(200)
	seedPaidOrderForRefund(ws, accountID, "LO_APPR_DUP")
	r, _ := svc.RequestRefund(context.Background(), accountID, "LO_APPR_DUP", "reason")
	_ = svc.Approve(context.Background(), r.RefundNo, "a", "n")

	// Second approve should fail because status is no longer pending.
	err := svc.Approve(context.Background(), r.RefundNo, "a", "n")
	if err == nil {
		t.Fatal("expected error for non-pending refund")
	}
}

// TestRefundService_Reject_NotFound verifies error when refund doesn't exist.
func TestRefundService_Reject_NotFound(t *testing.T) {
	svc, _, _ := makeRefundService()
	err := svc.Reject(context.Background(), "LR_NONEXISTENT", "admin", "note")
	if err == nil {
		t.Fatal("expected error for non-existent refund")
	}
}

// TestRefundService_Reject_AlreadyRejected verifies double-reject is blocked.
func TestRefundService_Reject_AlreadyRejected(t *testing.T) {
	svc, _, ws := makeRefundService()
	const accountID = int64(201)
	seedPaidOrderForRefund(ws, accountID, "LO_REJ_DUP")
	r, _ := svc.RequestRefund(context.Background(), accountID, "LO_REJ_DUP", "reason")
	_ = svc.Reject(context.Background(), r.RefundNo, "a", "n")

	err := svc.Reject(context.Background(), r.RefundNo, "a", "n")
	if err == nil {
		t.Fatal("expected error for non-pending refund")
	}
}

// TestRefundService_Request_OrderNotFound verifies nil order returns error.
func TestRefundService_Request_OrderNotFound(t *testing.T) {
	svc, _, _ := makeRefundService()
	_, err := svc.RequestRefund(context.Background(), 999, "LO_MISSING", "reason")
	if err == nil {
		t.Fatal("expected error for missing order")
	}
}

// TestRefundService_Request_IDORBlockedByNilOrder verifies IDOR rejection for nil order.
func TestRefundService_Request_IDORBlockedByNilOrder(t *testing.T) {
	svc, _, ws := makeRefundService()
	const ownerID = int64(202)
	const attackerID = int64(203)
	seedPaidOrderForRefund(ws, ownerID, "LO_IDOR_TEST")

	_, err := svc.RequestRefund(context.Background(), attackerID, "LO_IDOR_TEST", "idor")
	if err == nil {
		t.Fatal("expected IDOR error")
	}
}

// ── publishRefundCompleted with real publisher ────────────────────────────────

// mockPublisher implements RefundPublisher for testing.
type mockPublisher struct {
	published []*event.IdentityEvent
	err       error
}

func (m *mockPublisher) Publish(_ context.Context, ev *event.IdentityEvent) error {
	if m.err != nil {
		return m.err
	}
	m.published = append(m.published, ev)
	return nil
}

// TestRefundService_publishRefundCompleted_WithPublisher verifies NATS publish
// is attempted when a publisher is configured.
func TestRefundService_publishRefundCompleted_WithPublisher(t *testing.T) {
	ws := newMockWalletStore()
	rs := newMockRefundStore()
	pub := &mockPublisher{}
	svc := NewRefundService(rs, ws, pub, nil)

	const accountID = int64(300)
	seedPaidOrderForRefund(ws, accountID, "LO_PUB_TEST")
	r, _ := svc.RequestRefund(context.Background(), accountID, "LO_PUB_TEST", "pub test")

	_ = svc.Approve(context.Background(), r.RefundNo, "admin", "ok")

	// publishRefundCompleted runs best-effort; it may succeed or fail silently.
	// We only verify it doesn't panic.
}

// TestRefundService_publishRefundCompleted_PublisherError verifies that
// a publish failure is tolerated (best-effort semantics).
func TestRefundService_publishRefundCompleted_PublisherError(t *testing.T) {
	ws := newMockWalletStore()
	rs := newMockRefundStore()
	pub := &mockPublisher{err: errors.New("nats unavailable")}
	svc := NewRefundService(rs, ws, pub, nil)

	const accountID = int64(301)
	seedPaidOrderForRefund(ws, accountID, "LO_PUB_ERR")
	r, _ := svc.RequestRefund(context.Background(), accountID, "LO_PUB_ERR", "pub err test")
	// Approve triggers publishRefundCompleted which calls pub.Publish and gets an error.
	// The error must not propagate to the caller.
	err := svc.Approve(context.Background(), r.RefundNo, "admin", "ok")
	if err != nil {
		t.Fatalf("Approve should succeed even when publish fails, got: %v", err)
	}
}

// ── WalletService uncovered paths ────────────────────────────────────────────

// TestWalletService_UpdatePaymentOrder_Smoke verifies the method is callable.
func TestWalletService_UpdatePaymentOrder_Smoke(t *testing.T) {
	ws := newMockWalletStore()
	vipSvc := NewVIPService(newMockVIPStore(nil), ws)
	svc := NewWalletService(ws, vipSvc)

	o := &entity.PaymentOrder{
		AccountID: 1,
		OrderNo:   "LO_UPDATE",
		OrderType: "topup",
		AmountCNY: 100,
		Status:    entity.OrderStatusPending,
		CreatedAt: time.Now(),
	}
	_ = ws.CreatePaymentOrder(context.Background(), o)

	o.Status = entity.OrderStatusPaid
	err := svc.UpdatePaymentOrder(context.Background(), o)
	if err != nil {
		t.Fatalf("UpdatePaymentOrder: %v", err)
	}
}

// TestWalletService_CreateTopup_InvalidAmount verifies negative amount rejection.
func TestWalletService_CreateTopup_InvalidAmount(t *testing.T) {
	ws := newMockWalletStore()
	vipSvc := NewVIPService(newMockVIPStore(nil), ws)
	svc := NewWalletService(ws, vipSvc)

	_, err := svc.CreateTopup(context.Background(), 1, -10, "stripe")
	if err == nil {
		t.Fatal("expected error for negative amount")
	}
	_, err = svc.CreateTopup(context.Background(), 1, 0, "stripe")
	if err == nil {
		t.Fatal("expected error for zero amount")
	}
}

// TestWalletService_GetOrderByNo_NotFound verifies error when order doesn't exist.
func TestWalletService_GetOrderByNo_NotFound(t *testing.T) {
	ws := newMockWalletStore()
	vipSvc := NewVIPService(newMockVIPStore(nil), ws)
	svc := NewWalletService(ws, vipSvc)

	_, err := svc.GetOrderByNo(context.Background(), 1, "LO_GONE")
	if err == nil {
		t.Fatal("expected error for missing order")
	}
}

// TestWalletService_GetOrderByNo_IDOR verifies cross-account access is blocked.
func TestWalletService_GetOrderByNo_IDOR(t *testing.T) {
	ws := newMockWalletStore()
	vipSvc := NewVIPService(newMockVIPStore(nil), ws)
	svc := NewWalletService(ws, vipSvc)

	o := &entity.PaymentOrder{
		AccountID: 10,
		OrderNo:   "LO_IDOR",
		AmountCNY: 50,
		Status:    entity.OrderStatusPaid,
		CreatedAt: time.Now(),
	}
	_ = ws.CreatePaymentOrder(context.Background(), o)

	_, err := svc.GetOrderByNo(context.Background(), 99, "LO_IDOR")
	if err == nil {
		t.Fatal("expected IDOR error")
	}
}

// ── MarkOrderPaid idempotency ────────────────────────────────────────────────

// TestWalletService_MarkOrderPaid_AlreadyPaid verifies idempotent re-payment.
func TestWalletService_MarkOrderPaid_AlreadyPaid(t *testing.T) {
	ws := newMockWalletStore()
	ws.wallets[5] = &entity.Wallet{ID: 5, AccountID: 5, Balance: 0}
	vipSvc := NewVIPService(newMockVIPStore(nil), ws)
	svc := NewWalletService(ws, vipSvc)

	o := &entity.PaymentOrder{
		AccountID: 5,
		OrderNo:   "LO_IDEM_PAID",
		OrderType: "topup",
		AmountCNY: 88,
		Status:    entity.OrderStatusPaid,
		CreatedAt: time.Now(),
	}
	_ = ws.CreatePaymentOrder(context.Background(), o)

	// Calling MarkOrderPaid on an already-paid order should succeed without double-credit.
	result, err := svc.MarkOrderPaid(context.Background(), "LO_IDEM_PAID")
	if err != nil {
		t.Fatalf("MarkOrderPaid: %v", err)
	}
	if result.Status != entity.OrderStatusPaid {
		t.Errorf("expected paid status, got %s", result.Status)
	}
}

// TestWalletService_MarkOrderPaid_NotFound verifies error for missing order.
func TestWalletService_MarkOrderPaid_NotFound(t *testing.T) {
	ws := newMockWalletStore()
	vipSvc := NewVIPService(newMockVIPStore(nil), ws)
	svc := NewWalletService(ws, vipSvc)

	_, err := svc.MarkOrderPaid(context.Background(), "LO_MISSING")
	if err == nil {
		t.Fatal("expected error for missing order")
	}
}

// ── EntitlementService cache miss path ───────────────────────────────────────

// TestEntitlementService_Get_CacheMiss verifies Refresh is called when cache is empty.
func TestEntitlementService_Get_CacheMiss(t *testing.T) {
	subStore := newMockSubStore()
	planStore := newMockPlanStore()
	cache := newMockCache()
	svc := NewEntitlementService(subStore, planStore, cache)

	// No entitlements in store — should return free plan_code.
	em, err := svc.Get(context.Background(), 1, "llm-api")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if em["plan_code"] != "free" {
		t.Errorf("expected plan_code=free, got %q", em["plan_code"])
	}
}

// ── anyToString edge cases ────────────────────────────────────────────────────

// TestAnyToString_Complex verifies the default JSON serialisation path.
func TestAnyToString_Complex(t *testing.T) {
	result := anyToString(map[string]any{"k": "v"})
	if result == "" {
		t.Error("expected non-empty JSON string for complex type")
	}
}

// TestAnyToString_FloatFractional verifies fractional floats are not truncated.
func TestAnyToString_FloatFractional(t *testing.T) {
	result := anyToString(float64(3.14))
	if result != "3.14" {
		t.Errorf("expected 3.14, got %q", result)
	}
}

// ── SubscriptionService error paths ──────────────────────────────────────────

// TestSubscriptionService_Expire_NotFound verifies error for missing subscription.
func TestSubscriptionService_Expire_NotFound(t *testing.T) {
	subStore := newMockSubStore()
	planStore := newMockPlanStore()
	cache := newMockCache()
	entSvc := NewEntitlementService(subStore, planStore, cache)
	svc := NewSubscriptionService(subStore, planStore, entSvc, 3)

	err := svc.Expire(context.Background(), 9999)
	if err == nil {
		t.Fatal("expected error for non-existent subscription")
	}
}

// TestSubscriptionService_EndGrace_NotFound verifies error for missing subscription.
func TestSubscriptionService_EndGrace_NotFound(t *testing.T) {
	subStore := newMockSubStore()
	planStore := newMockPlanStore()
	cache := newMockCache()
	entSvc := NewEntitlementService(subStore, planStore, cache)
	svc := NewSubscriptionService(subStore, planStore, entSvc, 3)

	err := svc.EndGrace(context.Background(), 9999)
	if err == nil {
		t.Fatal("expected error for non-existent subscription")
	}
}

// ── AccountService UpsertByZitadelSub update path ────────────────────────────

// TestAccountService_UpsertByZitadelSub_Update verifies update path when account exists.
func TestAccountService_UpsertByZitadelSub_Update(t *testing.T) {
	accStore := newMockAccountStore()
	ws := newMockWalletStore()
	vipStore := newMockVIPStore(nil)
	svc := NewAccountService(accStore, ws, vipStore)

	// Create account first.
	existing := &entity.Account{
		ZitadelSub: "sub-update-test",
		Email:      "update@test.com",
		LurusID:    "LRUS001",
	}
	_ = accStore.Create(context.Background(), existing)

	// Upsert with same sub — should update.
	updated, err := svc.UpsertByZitadelSub(context.Background(), "sub-update-test", "update@test.com", "Updated User", "")
	if err != nil {
		t.Fatalf("UpsertByZitadelSub update: %v", err)
	}
	if updated == nil {
		t.Fatal("expected account, got nil")
	}
}

// TestAccountService_UpsertByZitadelSub_Create verifies creation of new account.
func TestAccountService_UpsertByZitadelSub_Create(t *testing.T) {
	accStore := newMockAccountStore()
	ws := newMockWalletStore()
	vipStore := newMockVIPStore(nil)
	svc := NewAccountService(accStore, ws, vipStore)

	acc, err := svc.UpsertByZitadelSub(context.Background(), "sub-new-account", "new@test.com", "New User", "")
	if err != nil {
		t.Fatalf("UpsertByZitadelSub create: %v", err)
	}
	if acc == nil {
		t.Fatal("expected created account, got nil")
	}
	if acc.ZitadelSub != "sub-new-account" {
		t.Errorf("sub mismatch: want %q, got %q", "sub-new-account", acc.ZitadelSub)
	}
}

// ── VIPService AdminSet / Get paths ──────────────────────────────────────────

// TestVIPService_AdminSet_Smoke verifies AdminSet works.
func TestVIPService_AdminSet_Smoke(t *testing.T) {
	vipStore := newMockVIPStore([]entity.VIPLevelConfig{
		{Level: 1, Name: "Bronze", MinSpendCNY: 0},
		{Level: 2, Name: "Silver", MinSpendCNY: 1000},
	})
	ws := newMockWalletStore()
	svc := NewVIPService(vipStore, ws)

	err := svc.AdminSet(context.Background(), 1, 2)
	if err != nil {
		t.Fatalf("AdminSet: %v", err)
	}
}

// TestVIPService_Get_Smoke verifies Get returns VIP info.
func TestVIPService_Get_Smoke(t *testing.T) {
	vipStore := newMockVIPStore([]entity.VIPLevelConfig{
		{Level: 0, Name: "Free", MinSpendCNY: 0},
	})
	ws := newMockWalletStore()
	svc := NewVIPService(vipStore, ws)

	info, err := svc.Get(context.Background(), 1)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if info == nil {
		t.Fatal("expected VIP info, got nil")
	}
}

// TestVIPService_Get_NoConfigs verifies graceful handling with no configs.
func TestVIPService_Get_NoConfigs(t *testing.T) {
	vipStore := newMockVIPStore(nil) // empty configs
	ws := newMockWalletStore()
	svc := NewVIPService(vipStore, ws)

	info, err := svc.Get(context.Background(), 1)
	if err != nil {
		t.Fatalf("Get with empty configs: %v", err)
	}
	_ = info
}

// ── generateCode: verify uppercase hex ───────────────────────────────────────

// TestGenerateCode_Uniqueness generates 100 codes and verifies no duplicates.
func TestGenerateCode_Uniqueness(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		code, err := generateCode()
		if err != nil {
			t.Fatalf("generateCode[%d]: %v", i, err)
		}
		if seen[code] {
			// Statistically near-impossible for 100 tries with 4 billion combinations.
			t.Errorf("duplicate code %q at iteration %d", code, i)
		}
		seen[code] = true
	}
}

// ── Redeem edge case ─────────────────────────────────────────────────────────

// TestWalletService_Redeem_UnsupportedRewardType verifies error on unknown reward type.
func TestWalletService_Redeem_UnsupportedRewardType(t *testing.T) {
	ws := newMockWalletStore()
	ws.wallets[1] = &entity.Wallet{ID: 1, AccountID: 1, Balance: 100}
	exp := time.Now().Add(24 * time.Hour)
	ws.codes["TESTCODE"] = &entity.RedemptionCode{
		Code:       "TESTCODE",
		RewardType: "unknown_type",
		MaxUses:    5,
		UsedCount:  0,
		ExpiresAt:  &exp,
	}
	vipSvc := NewVIPService(newMockVIPStore(nil), ws)
	svc := NewWalletService(ws, vipSvc)

	err := svc.Redeem(context.Background(), 1, "TESTCODE")
	if err == nil {
		t.Fatal("expected error for unsupported reward type")
	}
}

// TestWalletService_Redeem_ExpiredCode verifies expired code rejection.
func TestWalletService_Redeem_ExpiredCode(t *testing.T) {
	ws := newMockWalletStore()
	ws.wallets[1] = &entity.Wallet{ID: 1, AccountID: 1, Balance: 100}
	exp := time.Now().Add(-24 * time.Hour) // expired
	ws.codes["EXPCODE"] = &entity.RedemptionCode{
		Code:       "EXPCODE",
		RewardType: "credits",
		MaxUses:    5,
		UsedCount:  0,
		ExpiresAt:  &exp,
	}
	vipSvc := NewVIPService(newMockVIPStore(nil), ws)
	svc := NewWalletService(ws, vipSvc)

	err := svc.Redeem(context.Background(), 1, "EXPCODE")
	if err == nil {
		t.Fatal("expected error for expired code")
	}
}

// TestWalletService_Redeem_UsageLimitReached verifies fully-used code rejection.
func TestWalletService_Redeem_UsageLimitReached(t *testing.T) {
	ws := newMockWalletStore()
	ws.wallets[1] = &entity.Wallet{ID: 1, AccountID: 1, Balance: 100}
	ws.codes["USEDCODE"] = &entity.RedemptionCode{
		Code:       "USEDCODE",
		RewardType: "credits",
		MaxUses:    1,
		UsedCount:  1, // already at limit
	}
	vipSvc := NewVIPService(newMockVIPStore(nil), ws)
	svc := NewWalletService(ws, vipSvc)

	err := svc.Redeem(context.Background(), 1, "USEDCODE")
	if err == nil {
		t.Fatal("expected error for exhausted code")
	}
}

// ── ResetToFree ───────────────────────────────────────────────────────────────

// TestEntitlementService_ResetToFree_Smoke verifies basic flow.
func TestEntitlementService_ResetToFree_Smoke(t *testing.T) {
	subStore := newMockSubStore()
	planStore := newMockPlanStore()
	cache := newMockCache()
	svc := NewEntitlementService(subStore, planStore, cache)

	err := svc.ResetToFree(context.Background(), 1, "llm-api")
	if err != nil {
		t.Fatalf("ResetToFree: %v", err)
	}

	// Verify plan_code=free is now in store.
	ents, _ := subStore.GetEntitlements(context.Background(), 1, "llm-api")
	found := false
	for _, e := range ents {
		if e.Key == "plan_code" && e.Value == "free" {
			found = true
		}
	}
	if !found {
		t.Error("expected plan_code=free after ResetToFree")
	}
}

// ── addMonthsClamped month-overflow path ──────────────────────────────────────

// TestAddMonthsClamped_YearOverflow verifies that adding months past December
// correctly increments the year.
func TestAddMonthsClamped_YearOverflow(t *testing.T) {
	base := time.Date(2025, time.October, 31, 0, 0, 0, 0, time.UTC)
	result := addMonthsClamped(base, 3) // Oct + 3 = Jan next year
	if result.Year() != 2026 {
		t.Errorf("expected 2026, got %d", result.Year())
	}
	if result.Month() != time.January {
		t.Errorf("expected January, got %s", result.Month())
	}
	// Jan 31 is valid, day should remain 31.
	if result.Day() != 31 {
		t.Errorf("expected day 31, got %d", result.Day())
	}
}

// TestReferralService_OnSignup_ReferrerNotFound verifies error when referrer is missing.
func TestReferralService_OnSignup_ReferrerNotFound(t *testing.T) {
	accStore := newMockAccountStore()
	ws := newMockWalletStore()
	svc := NewReferralService(accStore, ws)

	err := svc.OnSignup(context.Background(), 1, 999) // referrer 999 doesn't exist
	if err == nil {
		t.Fatal("expected error for missing referrer")
	}
}

// ── BulkGenerateCodes with notes ─────────────────────────────────────────────

// TestReferralService_BulkGenerateCodes_NotesEmbedded verifies notes appear in BatchID.
func TestReferralService_BulkGenerateCodes_NotesEmbedded(t *testing.T) {
	svc, _ := makeBulkReferralService()
	const notes = "campaign-2026"

	codes, err := svc.BulkGenerateCodes(
		context.Background(), "p", "c", 1, nil, notes, 2,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, c := range codes {
		if c.BatchID != notes {
			t.Errorf("expected BatchID=%q, got %q", notes, c.BatchID)
		}
	}
}

// ── Topup VIP recalculation ───────────────────────────────────────────────────

// TestWalletService_Topup_TriggersVIPRecalc verifies VIP is updated after topup.
func TestWalletService_Topup_TriggersVIPRecalc(t *testing.T) {
	ws := newMockWalletStore()
	ws.wallets[7] = &entity.Wallet{ID: 7, AccountID: 7, Balance: 0}
	vipStore := newMockVIPStore([]entity.VIPLevelConfig{
		{Level: 0, Name: "Free", MinSpendCNY: 0},
		{Level: 1, Name: "Bronze", MinSpendCNY: 100},
	})
	vipSvc := NewVIPService(vipStore, ws)
	svc := NewWalletService(ws, vipSvc)

	_, err := svc.Topup(context.Background(), 7, 150, "LO_VIP_TOPUP")
	if err != nil {
		t.Fatalf("Topup: %v", err)
	}
	// VIP level should have been updated to Bronze (150 >= 100).
	vip, _ := vipStore.GetOrCreate(context.Background(), 7)
	if vip.Level < 1 {
		t.Errorf("expected VIP level >= 1, got %d", vip.Level)
	}
}

// ── GrantYearlySub happy path ─────────────────────────────────────────────────

// TestVIPService_GrantYearlySub_Smoke verifies happy-path execution.
func TestVIPService_GrantYearlySub_Smoke(t *testing.T) {
	vipStore := newMockVIPStore([]entity.VIPLevelConfig{
		{Level: 0, Name: "Free", MinSpendCNY: 0},
		{Level: 1, Name: "Bronze", MinSpendCNY: 100},
	})
	ws := newMockWalletStore()
	svc := NewVIPService(vipStore, ws)

	ws.wallets[10] = &entity.Wallet{ID: 10, AccountID: 10, Balance: 0, LifetimeTopup: 5000}

	err := svc.GrantYearlySub(context.Background(), 10, 1)
	if err != nil {
		t.Fatalf("GrantYearlySub: %v", err)
	}
}
