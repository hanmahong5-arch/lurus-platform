package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/auth"
	lurusemail "github.com/hanmahong5-arch/lurus-platform/internal/pkg/email"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/sms"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/zitadel"
)

const testSessionSecret = "test-session-secret-at-least-32-bytes!!"

// newZitadelMockServer creates an httptest server that simulates Zitadel user creation.
func newZitadelMockServer(t *testing.T, statusCode int, userID string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(statusCode)
		if userID != "" {
			json.NewEncoder(w).Encode(map[string]any{"userId": userID})
		} else {
			w.Write([]byte(`{"message":"error"}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// makeRegSvc builds a RegistrationService with in-memory mocks.
func makeRegSvc(t *testing.T, zitadelURL string) (*RegistrationService, *mockAccountStore, *mockWalletStore) {
	t.Helper()
	accounts := newMockAccountStore()
	wallets := newMockWalletStore()
	vip := newMockVIPStore(nil)
	referral := NewReferralService(accounts, wallets)
	zc := zitadel.NewClient(zitadelURL, "test-pat")
	svc := NewRegistrationService(accounts, wallets, vip, referral, zc, testSessionSecret, lurusemail.NoopSender{}, sms.NoopSender{}, nil, sms.SMSConfig{})
	return svc, accounts, wallets
}

// TestRegistrationService_Register_Success verifies the happy-path registration flow.
func TestRegistrationService_Register_Success(t *testing.T) {
	srv := newZitadelMockServer(t, http.StatusCreated, "zid-001")
	svc, accounts, _ := makeRegSvc(t, srv.URL)

	result, err := svc.Register(context.Background(), RegisterRequest{
		Username: "alice123",
		Password: "Password123!",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if result.Token == "" {
		t.Error("Token must not be empty")
	}
	if result.AccountID == 0 {
		t.Error("AccountID must not be zero")
	}
	if result.LurusID == "" {
		t.Error("LurusID must not be empty")
	}

	// Validate token is a proper session token.
	id, err := auth.ValidateSessionToken(result.Token, testSessionSecret)
	if err != nil {
		t.Fatalf("issued token is invalid: %v", err)
	}
	if id != result.AccountID {
		t.Errorf("token account id = %d, want %d", id, result.AccountID)
	}

	// Account must exist in DB.
	acc, _ := accounts.GetByUsername(context.Background(), "alice123")
	if acc == nil {
		t.Error("account not found in store after registration")
	}
}

// TestRegistrationService_Register_DuplicateUsername verifies duplicate username rejection.
func TestRegistrationService_Register_DuplicateUsername(t *testing.T) {
	srv := newZitadelMockServer(t, http.StatusCreated, "zid-002")
	svc, _, _ := makeRegSvc(t, srv.URL)

	_, err := svc.Register(context.Background(), RegisterRequest{
		Username: "bob123",
		Password: "Password123!",
	})
	if err != nil {
		t.Fatalf("first registration: %v", err)
	}

	_, err = svc.Register(context.Background(), RegisterRequest{
		Username: "bob123",
		Password: "Password123!",
	})
	if err == nil {
		t.Fatal("duplicate username should be rejected")
	}
	if !strings.Contains(err.Error(), "already taken") {
		t.Errorf("want 'already taken' in error, got: %v", err)
	}
}

// TestRegistrationService_Register_WeakPassword verifies passwords < 8 chars are rejected.
func TestRegistrationService_Register_WeakPassword(t *testing.T) {
	srv := newZitadelMockServer(t, http.StatusCreated, "zid-003")
	svc, _, _ := makeRegSvc(t, srv.URL)

	tests := []string{"", "1234567", "abc", "a"}
	for _, pw := range tests {
		t.Run("pw="+pw, func(t *testing.T) {
			_, err := svc.Register(context.Background(), RegisterRequest{
				Username: "carol123",
				Password: pw,
			})
			if err == nil {
				t.Errorf("password %q should be rejected", pw)
			}
		})
	}
}

// TestRegistrationService_Register_ExactMinPassword verifies exactly 8-char password is accepted.
func TestRegistrationService_Register_ExactMinPassword(t *testing.T) {
	srv := newZitadelMockServer(t, http.StatusCreated, "zid-003b")
	svc, _, _ := makeRegSvc(t, srv.URL)

	_, err := svc.Register(context.Background(), RegisterRequest{
		Username: "min_user",
		Password: "12345678",
	})
	if err != nil {
		t.Errorf("exactly 8-char password should be accepted: %v", err)
	}
}

// TestRegistrationService_Register_InvalidUsername verifies malformed usernames are rejected.
func TestRegistrationService_Register_InvalidUsername(t *testing.T) {
	srv := newZitadelMockServer(t, http.StatusCreated, "zid-004")
	svc, _, _ := makeRegSvc(t, srv.URL)

	badUsernames := []string{
		"",    // empty
		"ab",  // too short
		"a b", // contains space
		"a@b", // contains @
	}

	for _, u := range badUsernames {
		t.Run(u, func(t *testing.T) {
			_, err := svc.Register(context.Background(), RegisterRequest{
				Username: u,
				Password: "Password123!",
			})
			if err == nil {
				t.Errorf("invalid username %q should be rejected", u)
			}
		})
	}
}

// TestRegistrationService_Register_ZitadelDown verifies Zitadel failure propagates.
func TestRegistrationService_Register_ZitadelDown(t *testing.T) {
	srv := newZitadelMockServer(t, http.StatusInternalServerError, "")
	svc, _, _ := makeRegSvc(t, srv.URL)

	_, err := svc.Register(context.Background(), RegisterRequest{
		Username: "dave123",
		Password: "Password123!",
	})
	if err == nil {
		t.Fatal("Zitadel 500 should fail registration")
	}
}

// TestRegistrationService_Register_ZitadelConflict verifies Zitadel 409 returns proper error.
func TestRegistrationService_Register_ZitadelConflict(t *testing.T) {
	srv := newZitadelMockServer(t, http.StatusConflict, "")
	svc, _, _ := makeRegSvc(t, srv.URL)

	_, err := svc.Register(context.Background(), RegisterRequest{
		Username: "existing123",
		Password: "Password123!",
	})
	if err == nil {
		t.Fatal("Zitadel 409 should fail registration")
	}
}

// TestRegistrationService_Register_WithOptionalEmail verifies email is stored when provided.
func TestRegistrationService_Register_WithOptionalEmail(t *testing.T) {
	srv := newZitadelMockServer(t, http.StatusCreated, "zid-005")
	svc, accounts, _ := makeRegSvc(t, srv.URL)

	_, err := svc.Register(context.Background(), RegisterRequest{
		Username: "emailuser",
		Password: "Password123!",
		Email:    "  Alice@EXAMPLE.COM  ",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Must be stored in lowercase.
	acc, _ := accounts.GetByEmail(context.Background(), "alice@example.com")
	if acc == nil {
		t.Error("email should be normalized to lowercase: alice@example.com")
	}
}

// TestRegistrationService_Register_WithValidAffCode verifies referral tracking on signup.
func TestRegistrationService_Register_WithValidAffCode(t *testing.T) {
	srv := newZitadelMockServer(t, http.StatusCreated, "zid-006")
	svc, accounts, _ := makeRegSvc(t, srv.URL)

	// First: register the referrer.
	_, err := svc.Register(context.Background(), RegisterRequest{
		Username: "referrer1",
		Password: "Password123!",
	})
	if err != nil {
		t.Fatalf("referrer registration: %v", err)
	}
	referrerAcc, _ := accounts.GetByUsername(context.Background(), "referrer1")
	if referrerAcc == nil {
		t.Fatal("referrer not found")
	}

	// Second: register referee with referrer's affCode.
	_, err = svc.Register(context.Background(), RegisterRequest{
		Username: "referee1",
		Password: "Password123!",
		AffCode:  referrerAcc.AffCode,
	})
	if err != nil {
		t.Fatalf("referee registration: %v", err)
	}

	refereeAcc, _ := accounts.GetByUsername(context.Background(), "referee1")
	if refereeAcc == nil {
		t.Fatal("referee not found")
	}
	if refereeAcc.ReferrerID == nil || *refereeAcc.ReferrerID != referrerAcc.ID {
		t.Errorf("ReferrerID = %v, want %d", refereeAcc.ReferrerID, referrerAcc.ID)
	}
}

// TestRegistrationService_Register_InvalidAffCode verifies that bad affCode does not abort registration.
func TestRegistrationService_Register_InvalidAffCode(t *testing.T) {
	srv := newZitadelMockServer(t, http.StatusCreated, "zid-007")
	svc, _, _ := makeRegSvc(t, srv.URL)

	result, err := svc.Register(context.Background(), RegisterRequest{
		Username: "frank123",
		Password: "Password123!",
		AffCode:  "BADCODE",
	})
	if err != nil {
		t.Fatalf("invalid affCode should not fail registration: %v", err)
	}
	if result.Token == "" {
		t.Error("token must not be empty even with invalid affCode")
	}
}

// TestRegistrationService_NewRegistrationService_NilForEmptySecret verifies nil return for empty secret.
func TestRegistrationService_NewRegistrationService_NilForEmptySecret(t *testing.T) {
	srv := newZitadelMockServer(t, http.StatusCreated, "zid")
	svc := NewRegistrationService(
		newMockAccountStore(), newMockWalletStore(), newMockVIPStore(nil), nil,
		zitadel.NewClient(srv.URL, "pat"),
		"", // empty secret -> should return nil
		lurusemail.NoopSender{}, sms.NoopSender{}, nil, sms.SMSConfig{},
	)
	if svc != nil {
		t.Error("empty sessionSecret should produce nil service")
	}
}

// TestRegistrationService_NewRegistrationService_NilZitadelStillWorks verifies service works without Zitadel.
func TestRegistrationService_NewRegistrationService_NilZitadelStillWorks(t *testing.T) {
	svc := NewRegistrationService(
		newMockAccountStore(), newMockWalletStore(), newMockVIPStore(nil), nil,
		nil, // nil Zitadel client -> service still works, just skips Zitadel user creation
		testSessionSecret,
		lurusemail.NoopSender{}, sms.NoopSender{}, nil, sms.SMSConfig{},
	)
	if svc == nil {
		t.Error("nil Zitadel client should still produce a working service")
	}
}

// makeRegSvcWithRedis builds a RegistrationService with miniredis for ForgotPassword/ResetPassword/Phone tests.
func makeRegSvcWithRedis(t *testing.T) (*RegistrationService, *mockAccountStore) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	accounts := newMockAccountStore()
	wallets := newMockWalletStore()
	vip := newMockVIPStore(nil)
	svc := NewRegistrationService(
		accounts, wallets, vip, nil, nil,
		testSessionSecret,
		lurusemail.NoopSender{}, sms.NoopSender{}, rdb,
		sms.SMSConfig{Provider: "tencent", TencentSecretID: "x", TencentSecretKey: "x", TencentAppID: "x"},
	)
	return svc, accounts
}

// TestRegistrationService_Register_PhoneAsUsername verifies phone username auto-fills phone field.
func TestRegistrationService_Register_PhoneAsUsername(t *testing.T) {
	srv := newZitadelMockServer(t, http.StatusCreated, "zid-phone")
	svc, accounts, _ := makeRegSvc(t, srv.URL)

	_, err := svc.Register(context.Background(), RegisterRequest{
		Username: "13800138000",
		Password: "Password123!",
	})
	if err != nil {
		t.Fatalf("Register with phone username: %v", err)
	}

	acc, _ := accounts.GetByPhone(context.Background(), "13800138000")
	if acc == nil {
		t.Error("phone should be auto-filled when username is a phone number")
	}
}

// ── ForgotPassword ──────────────────────────────────────────────────────────

func TestRegistrationService_ForgotPassword_EmptyIdentifier(t *testing.T) {
	svc, _ := makeRegSvcWithRedis(t)

	_, err := svc.ForgotPassword(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty identifier")
	}
}

func TestRegistrationService_ForgotPassword_NonExistentAccount(t *testing.T) {
	svc, _ := makeRegSvcWithRedis(t)

	// Non-existent account should return a result (not reveal existence).
	result, err := svc.ForgotPassword(context.Background(), "nobody@lurus.cn")
	if err != nil {
		t.Fatalf("ForgotPassword: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result even for non-existent account")
	}
}

func TestRegistrationService_ForgotPassword_AccountWithoutZitadel(t *testing.T) {
	svc, _ := makeRegSvcWithRedis(t)
	ctx := context.Background()

	// Register creates a local-only account (no Zitadel since zc=nil).
	svc.Register(ctx, RegisterRequest{Username: "localonly", Password: "password123", Email: "local@lurus.cn"})

	// ForgotPassword for account without ZitadelSub should return generic message.
	result, err := svc.ForgotPassword(ctx, "local@lurus.cn")
	if err != nil {
		t.Fatalf("ForgotPassword: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

// ── ResetPassword ───────────────────────────────────────────────────────────

func TestRegistrationService_ResetPassword_ShortPassword(t *testing.T) {
	svc, _ := makeRegSvcWithRedis(t)

	err := svc.ResetPassword(context.Background(), "someone@lurus.cn", "123456", "short")
	if err == nil {
		t.Fatal("expected error for short password")
	}
}

func TestRegistrationService_ResetPassword_NoAccount(t *testing.T) {
	svc, _ := makeRegSvcWithRedis(t)

	err := svc.ResetPassword(context.Background(), "nobody@lurus.cn", "123456", "newpassword123")
	if err == nil {
		t.Fatal("expected error for non-existent account")
	}
}

// ── SendPhoneVerificationCode ───────────────────────────────────────────────

func TestRegistrationService_SendPhoneVerificationCode_InvalidPhone(t *testing.T) {
	svc, _ := makeRegSvcWithRedis(t)

	err := svc.SendPhoneVerificationCode(context.Background(), 1, "12345")
	if err == nil {
		t.Fatal("expected error for invalid phone number")
	}
}

func TestRegistrationService_SendPhoneVerificationCode_Success(t *testing.T) {
	svc, _ := makeRegSvcWithRedis(t)

	err := svc.SendPhoneVerificationCode(context.Background(), 1, "13800138000")
	if err != nil {
		t.Fatalf("SendPhoneVerificationCode: %v", err)
	}
}

func TestRegistrationService_SendPhoneVerificationCode_AlreadyTaken(t *testing.T) {
	svc, _ := makeRegSvcWithRedis(t)
	ctx := context.Background()

	// Register user with this phone.
	svc.Register(ctx, RegisterRequest{Username: "owner", Password: "password123", Phone: "13800138001"})

	// Different user tries to bind same phone.
	err := svc.SendPhoneVerificationCode(ctx, 9999, "13800138001")
	if err == nil {
		t.Fatal("expected error for phone already taken")
	}
}

// ── VerifyAndBindPhone ──────────────────────────────────────────────────────

func TestRegistrationService_VerifyAndBindPhone_InvalidPhone(t *testing.T) {
	svc, _ := makeRegSvcWithRedis(t)

	err := svc.VerifyAndBindPhone(context.Background(), 1, "12345", "123456")
	if err == nil {
		t.Fatal("expected error for invalid phone")
	}
}

func TestRegistrationService_VerifyAndBindPhone_NoPendingCode(t *testing.T) {
	svc, _ := makeRegSvcWithRedis(t)

	err := svc.VerifyAndBindPhone(context.Background(), 1, "13800138000", "123456")
	if err == nil {
		t.Fatal("expected error for no pending verification")
	}
}

// ── resolveAccount ──────────────────────────────────────────────────────────

func TestResolveAccount_ByEmail(t *testing.T) {
	svc, _ := makeRegSvcWithRedis(t)
	ctx := context.Background()

	svc.Register(ctx, RegisterRequest{Username: "resolver", Password: "password123", Email: "resolve@lurus.cn"})

	acc, err := svc.resolveAccount(ctx, "resolve@lurus.cn")
	if err != nil {
		t.Fatalf("resolveAccount by email: %v", err)
	}
	if acc == nil {
		t.Fatal("expected account found by email")
	}
}

func TestResolveAccount_ByPhone(t *testing.T) {
	svc, _ := makeRegSvcWithRedis(t)
	ctx := context.Background()

	svc.Register(ctx, RegisterRequest{Username: "phonelookup2", Password: "password123", Phone: "13700137000"})

	acc, err := svc.resolveAccount(ctx, "13700137000")
	if err != nil {
		t.Fatalf("resolveAccount by phone: %v", err)
	}
	if acc == nil {
		t.Fatal("expected account found by phone")
	}
}

func TestResolveAccount_ByUsername(t *testing.T) {
	svc, _ := makeRegSvcWithRedis(t)
	ctx := context.Background()

	svc.Register(ctx, RegisterRequest{Username: "namelookup2", Password: "password123"})

	acc, err := svc.resolveAccount(ctx, "namelookup2")
	if err != nil {
		t.Fatalf("resolveAccount by username: %v", err)
	}
	if acc == nil {
		t.Fatal("expected account found by username")
	}
}

func TestResolveAccount_NotFound(t *testing.T) {
	svc, _ := makeRegSvcWithRedis(t)

	acc, err := svc.resolveAccount(context.Background(), "nobody")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if acc != nil {
		t.Error("expected nil for unknown identifier")
	}
}

// ── generateNumericCode ─────────────────────────────────────────────────────

func TestGenerateNumericCode_Length(t *testing.T) {
	code, err := generateNumericCode(6)
	if err != nil {
		t.Fatalf("generateNumericCode: %v", err)
	}
	if len(code) != 6 {
		t.Errorf("len = %d, want 6", len(code))
	}
	for _, c := range code {
		if c < '0' || c > '9' {
			t.Errorf("non-digit character: %c", c)
		}
	}
}

func TestGenerateNumericCode_Uniqueness(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		code, _ := generateNumericCode(6)
		seen[code] = true
	}
	if len(seen) < 90 {
		t.Errorf("only %d unique codes in 100 samples", len(seen))
	}
}

// ── SetOnReferralSignupHook ─────────────────────────────────────────────────

func TestRegistrationService_SetOnReferralSignupHook(t *testing.T) {
	svc, _ := makeRegSvcWithRedis(t)
	svc.SetOnReferralSignupHook(func(_ context.Context, _ int64, _ string) {})
	if svc.onReferralSignup == nil {
		t.Error("expected hook to be set")
	}
}
