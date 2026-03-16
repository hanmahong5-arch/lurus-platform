package app

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

func TestGenerateAffCode(t *testing.T) {
	code, err := generateAffCode()
	if err != nil {
		t.Fatalf("generateAffCode returned error: %v", err)
	}
	if len(code) != 8 {
		t.Errorf("len(code)=%d, want 8; got %q", len(code), code)
	}
	if _, err := hex.DecodeString(code); err != nil {
		t.Errorf("aff code %q is not valid hex: %v", code, err)
	}
	if strings.ToLower(code) != code {
		t.Errorf("aff code %q is not lowercase", code)
	}
}

func TestGenerateAffCode_Uniqueness(t *testing.T) {
	seen := make(map[string]bool, 100)
	for i := 0; i < 100; i++ {
		code, err := generateAffCode()
		if err != nil {
			t.Fatalf("generateAffCode error at iteration %d: %v", i, err)
		}
		if seen[code] {
			t.Errorf("duplicate aff code %q at iteration %d", code, i)
		}
		seen[code] = true
	}
}

// ── AccountService integration tests (using in-memory mocks) ─────────────────

func makeAccountService() *AccountService {
	return NewAccountService(newMockAccountStore(), newMockWalletStore(), newMockVIPStore(nil))
}

func TestAccountService_UpsertByZitadelSub_NewAccount(t *testing.T) {
	svc := makeAccountService()
	ctx := context.Background()

	a, err := svc.UpsertByZitadelSub(ctx, "sub-001", "alice@example.com", "Alice", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a == nil {
		t.Fatal("expected non-nil account")
	}
	if a.Email != "alice@example.com" {
		t.Errorf("Email=%q, want alice@example.com", a.Email)
	}
	if a.ZitadelSub != "sub-001" {
		t.Errorf("ZitadelSub=%q, want sub-001", a.ZitadelSub)
	}
	if a.LurusID == "" {
		t.Error("LurusID should not be empty after creation")
	}
	if len(a.AffCode) != 8 {
		t.Errorf("AffCode len=%d, want 8", len(a.AffCode))
	}
}

func TestAccountService_UpsertByZitadelSub_ExistingSub(t *testing.T) {
	svc := makeAccountService()
	ctx := context.Background()

	// Create
	a1, _ := svc.UpsertByZitadelSub(ctx, "sub-002", "bob@example.com", "Bob", "")
	// Upsert again with same sub — should update display name, not create new
	a2, err := svc.UpsertByZitadelSub(ctx, "sub-002", "bob@example.com", "Bobby", "https://avatar.png")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a2.ID != a1.ID {
		t.Errorf("expected same account ID, got %d vs %d", a2.ID, a1.ID)
	}
	if a2.DisplayName != "Bobby" {
		t.Errorf("DisplayName=%q, want Bobby", a2.DisplayName)
	}
}

func TestAccountService_UpsertByZitadelSub_EmailMatchLinksSub(t *testing.T) {
	// Simulates: account exists with email but no ZitadelSub yet
	store := newMockAccountStore()
	svc := NewAccountService(store, newMockWalletStore(), newMockVIPStore(nil))
	ctx := context.Background()

	// Pre-create account with email but no sub
	a1, _ := svc.UpsertByZitadelSub(ctx, "", "carol@example.com", "Carol", "")
	if a1 == nil {
		t.Fatal("pre-create failed")
	}

	// Now upsert with same email + a zitadel sub
	a2, err := svc.UpsertByZitadelSub(ctx, "sub-003", "carol@example.com", "Carol Z", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a2.ZitadelSub != "sub-003" {
		t.Errorf("ZitadelSub=%q, want sub-003", a2.ZitadelSub)
	}
}

func TestAccountService_GetByID_NotFound(t *testing.T) {
	svc := makeAccountService()
	a, err := svc.GetByID(context.Background(), 999)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a != nil {
		t.Errorf("expected nil for unknown ID, got %+v", a)
	}
}

func TestAccountService_GetByZitadelSub(t *testing.T) {
	svc := makeAccountService()
	ctx := context.Background()
	created, _ := svc.UpsertByZitadelSub(ctx, "sub-zit", "dan@example.com", "Dan", "")

	got, err := svc.GetByZitadelSub(ctx, "sub-zit")
	if err != nil {
		t.Fatalf("GetByZitadelSub error: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil account")
	}
	if got.ID != created.ID {
		t.Errorf("ID=%d, want %d", got.ID, created.ID)
	}
}

func TestAccountService_Update(t *testing.T) {
	svc := makeAccountService()
	ctx := context.Background()
	a, _ := svc.UpsertByZitadelSub(ctx, "sub-upd", "eve@example.com", "Eve", "")

	a.DisplayName = "Eve Updated"
	if err := svc.Update(ctx, a); err != nil {
		t.Fatalf("Update error: %v", err)
	}
	// Verify through GetByID
	got, _ := svc.GetByID(ctx, a.ID)
	if got == nil || got.DisplayName != "Eve Updated" {
		t.Errorf("DisplayName after update=%q, want Eve Updated", got.DisplayName)
	}
}

func TestAccountService_List(t *testing.T) {
	svc := makeAccountService()
	ctx := context.Background()
	_, _ = svc.UpsertByZitadelSub(ctx, "sub-a", "a@example.com", "A", "")
	_, _ = svc.UpsertByZitadelSub(ctx, "sub-b", "b@example.com", "B", "")

	accounts, total, err := svc.List(ctx, "", 1, 10)
	if err != nil {
		t.Fatalf("List error: %v", err)
	}
	if total < 2 {
		t.Errorf("total=%d, want ≥2", total)
	}
	if len(accounts) < 2 {
		t.Errorf("len(accounts)=%d, want ≥2", len(accounts))
	}
}

func TestAccountService_BindOAuth(t *testing.T) {
	svc := makeAccountService()
	ctx := context.Background()
	a, _ := svc.UpsertByZitadelSub(ctx, "sub-oauth", "frank@example.com", "Frank", "")

	err := svc.BindOAuth(ctx, a.ID, "github", "gh-12345", "frank@github.com")
	if err != nil {
		t.Fatalf("BindOAuth error: %v", err)
	}
}

// ── UpsertByWechat tests ───────────────────────────────────────────────────────

func TestAccountService_UpsertByWechat_CreatesNew(t *testing.T) {
	svc := makeAccountService()
	ctx := context.Background()

	a, err := svc.UpsertByWechat(ctx, "wx123")
	if err != nil {
		t.Fatalf("UpsertByWechat error: %v", err)
	}
	if a == nil {
		t.Fatal("expected non-nil account")
	}
	if a.Email != "wechat.wx123@noreply.lurus.cn" {
		t.Errorf("Email = %q, want wechat.wx123@noreply.lurus.cn", a.Email)
	}
	if a.ZitadelSub != "wechat:wx123" {
		t.Errorf("ZitadelSub = %q, want wechat:wx123", a.ZitadelSub)
	}
	if a.ID <= 0 {
		t.Errorf("ID = %d, want > 0", a.ID)
	}
}

func TestAccountService_UpsertByWechat_ReturnsExisting(t *testing.T) {
	store := newMockAccountStore()
	svc := NewAccountService(store, newMockWalletStore(), newMockVIPStore(nil))
	ctx := context.Background()

	// First call creates the account and its OAuthBinding
	a1, err := svc.UpsertByWechat(ctx, "wx999")
	if err != nil {
		t.Fatalf("first UpsertByWechat error: %v", err)
	}

	// Second call should return the same account via the stored OAuthBinding
	a2, err := svc.UpsertByWechat(ctx, "wx999")
	if err != nil {
		t.Fatalf("second UpsertByWechat error: %v", err)
	}
	if a2 == nil {
		t.Fatal("expected non-nil account on second call")
	}
	if a2.ID != a1.ID {
		t.Errorf("second call returned different account ID %d, want %d", a2.ID, a1.ID)
	}
}

func TestAccountService_UpsertByWechat_EmptyID_Error(t *testing.T) {
	svc := makeAccountService()
	_, err := svc.UpsertByWechat(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty wechatID, got nil")
	}
}

func TestAccountService_GetByOAuthBinding_Delegates(t *testing.T) {
	svc := makeAccountService()
	ctx := context.Background()

	// Create account via UpsertByWechat — this also creates the OAuthBinding
	a, err := svc.UpsertByWechat(ctx, "wx-lookup-test")
	if err != nil {
		t.Fatalf("UpsertByWechat error: %v", err)
	}

	got, err := svc.GetByOAuthBinding(ctx, "wechat", "wx-lookup-test")
	if err != nil {
		t.Fatalf("GetByOAuthBinding error: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil account from GetByOAuthBinding")
	}
	if got.ID != a.ID {
		t.Errorf("got account ID %d, want %d", got.ID, a.ID)
	}
}

// TestAccountService_GetByEmail_Delegates verifies GetByEmail delegates to the store.
func TestAccountService_GetByEmail_Delegates(t *testing.T) {
	svc := makeAccountService()
	ctx := context.Background()

	// Register an account so the email is in the store.
	a, err := svc.UpsertByZitadelSub(ctx, "sub-email-test", "getbyemail@example.com", "Tester", "")
	if err != nil {
		t.Fatalf("UpsertByZitadelSub: %v", err)
	}

	got, err := svc.GetByEmail(ctx, "getbyemail@example.com")
	if err != nil {
		t.Fatalf("GetByEmail: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil account")
	}
	if got.ID != a.ID {
		t.Errorf("ID = %d, want %d", got.ID, a.ID)
	}
}

// TestAccountService_GetByEmail_NotFound verifies nil is returned for unknown email.
func TestAccountService_GetByEmail_NotFound(t *testing.T) {
	svc := makeAccountService()

	got, err := svc.GetByEmail(context.Background(), "nobody@example.com")
	if err != nil {
		t.Fatalf("GetByEmail: %v", err)
	}
	if got != nil {
		t.Error("expected nil for unknown email")
	}
}

// TestAccountService_GetByPhone_Delegates verifies GetByPhone delegates to the store.
func TestAccountService_GetByPhone_Delegates(t *testing.T) {
	svc := makeAccountService()
	ctx := context.Background()

	// Seed account with a phone number.
	a, err := svc.UpsertByZitadelSub(ctx, "sub-phone-test", "phoneuser@example.com", "PhoneUser", "")
	if err != nil {
		t.Fatalf("UpsertByZitadelSub: %v", err)
	}
	a.Phone = "+8613800138000"
	_ = svc.Update(ctx, a)

	got, err := svc.GetByPhone(ctx, "+8613800138000")
	if err != nil {
		t.Fatalf("GetByPhone: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil account")
	}
	if got.ID != a.ID {
		t.Errorf("ID = %d, want %d", got.ID, a.ID)
	}
}

// TestAccountService_GetByPhone_NotFound verifies nil is returned for unknown phone.
func TestAccountService_GetByPhone_NotFound(t *testing.T) {
	svc := makeAccountService()

	got, err := svc.GetByPhone(context.Background(), "+0000000000")
	if err != nil {
		t.Fatalf("GetByPhone: %v", err)
	}
	if got != nil {
		t.Error("expected nil for unknown phone")
	}
}

// TestAccountService_GetByAffCode_Delegates verifies GetByAffCode delegates to the store.
func TestAccountService_GetByAffCode_Delegates(t *testing.T) {
	svc := makeAccountService()
	ctx := context.Background()

	a, err := svc.UpsertByZitadelSub(ctx, "sub-aff-test", "affuser@example.com", "AffUser", "")
	if err != nil {
		t.Fatalf("UpsertByZitadelSub: %v", err)
	}

	// AffCode is auto-generated on first upsert.
	if a.AffCode == "" {
		t.Skip("AffCode not populated, skipping")
	}

	got, err := svc.GetByAffCode(ctx, a.AffCode)
	if err != nil {
		t.Fatalf("GetByAffCode: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil account")
	}
	if got.ID != a.ID {
		t.Errorf("ID = %d, want %d", got.ID, a.ID)
	}
}

// TestAccountService_GetByAffCode_NotFound verifies nil is returned for unknown affcode.
func TestAccountService_GetByAffCode_NotFound(t *testing.T) {
	svc := makeAccountService()

	got, err := svc.GetByAffCode(context.Background(), "nonexistent-aff")
	if err != nil {
		t.Fatalf("GetByAffCode: %v", err)
	}
	if got != nil {
		t.Error("expected nil for unknown aff code")
	}
}

// errOAuthBindingStore returns an error from GetByOAuthBinding to cover the UpsertByWechat error branch.
type errOAuthBindingStore struct{ mockAccountStore }

func (s *errOAuthBindingStore) GetByOAuthBinding(_ context.Context, _, _ string) (*entity.Account, error) {
	return nil, fmt.Errorf("oauth db error")
}

// TestAccountService_UpsertByWechat_OAuthBindingError covers the GetByOAuthBinding error branch.
func TestAccountService_UpsertByWechat_OAuthBindingError(t *testing.T) {
	errStore := &errOAuthBindingStore{*newMockAccountStore()}
	svc := NewAccountService(errStore, newMockWalletStore(), newMockVIPStore(nil))

	_, err := svc.UpsertByWechat(context.Background(), "wx-err-test")
	if err == nil {
		t.Fatal("expected error from GetByOAuthBinding, got nil")
	}
}
