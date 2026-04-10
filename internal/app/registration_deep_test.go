package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	lurusemail "github.com/hanmahong5-arch/lurus-platform/internal/pkg/email"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/sms"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/zitadel"
)

// makeRegSvcDeepRedis builds a RegistrationService with miniredis for code storage.
func makeRegSvcDeepRedis(t *testing.T, zitadelURL string) (*RegistrationService, *mockAccountStore, *redis.Client) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })

	accounts := newMockAccountStore()
	wallets := newMockWalletStore()
	vip := newMockVIPStore(nil)
	referral := NewReferralService(accounts, wallets)
	zc := zitadel.NewClient(zitadelURL, "test-pat")
	svc := NewRegistrationService(accounts, wallets, vip, referral, zc, testSessionSecret, lurusemail.NoopSender{}, sms.NoopSender{}, rdb, sms.SMSConfig{})
	return svc, accounts, rdb
}

// seedAccount inserts an account directly into the mock store.
func seedAccount(t *testing.T, store *mockAccountStore, username, email, phone, zitadelSub string) *entity.Account {
	t.Helper()
	a := &entity.Account{
		Username:   username,
		Email:      email,
		Phone:      phone,
		ZitadelSub: zitadelSub,
	}
	if err := store.Create(context.Background(), a); err != nil {
		t.Fatalf("seedAccount: %v", err)
	}
	return a
}

// ── CheckUsernameAvailable ────────────────────────────────────────────────

func TestRegDeep_CheckUsernameAvailable_Available(t *testing.T) {
	srv := newZitadelMockServer(t, http.StatusOK, "")
	svc, _, _ := makeRegSvc(t, srv.URL)

	available, err := svc.CheckUsernameAvailable(context.Background(), "newuser123")
	if err != nil {
		t.Fatalf("CheckUsernameAvailable: %v", err)
	}
	if !available {
		t.Error("expected available=true")
	}
}

func TestRegDeep_CheckUsernameAvailable_Taken(t *testing.T) {
	srv := newZitadelMockServer(t, http.StatusOK, "")
	svc, accounts, _ := makeRegSvc(t, srv.URL)
	seedAccount(t, accounts, "existing", "e@test.com", "", "")

	available, err := svc.CheckUsernameAvailable(context.Background(), "existing")
	if err != nil {
		t.Fatalf("CheckUsernameAvailable: %v", err)
	}
	if available {
		t.Error("expected available=false for existing username")
	}
}

// ── CheckEmailAvailable ───────────────────────────────────────────────────

func TestRegDeep_CheckEmailAvailable_Available(t *testing.T) {
	srv := newZitadelMockServer(t, http.StatusOK, "")
	svc, _, _ := makeRegSvc(t, srv.URL)

	available, err := svc.CheckEmailAvailable(context.Background(), "new@example.com")
	if err != nil {
		t.Fatalf("CheckEmailAvailable: %v", err)
	}
	if !available {
		t.Error("expected available=true")
	}
}

func TestRegDeep_CheckEmailAvailable_InvalidFormat(t *testing.T) {
	srv := newZitadelMockServer(t, http.StatusOK, "")
	svc, _, _ := makeRegSvc(t, srv.URL)

	_, err := svc.CheckEmailAvailable(context.Background(), "not-an-email")
	if err == nil {
		t.Fatal("expected error for invalid email format")
	}
}

func TestRegDeep_CheckEmailAvailable_Normalization(t *testing.T) {
	srv := newZitadelMockServer(t, http.StatusOK, "")
	svc, _, _ := makeRegSvc(t, srv.URL)

	available, err := svc.CheckEmailAvailable(context.Background(), "  Test@Example.COM  ")
	if err != nil {
		t.Fatalf("CheckEmailAvailable: %v", err)
	}
	if !available {
		t.Error("expected available=true after normalization")
	}
}

// ── ForgotPassword ────────────────────────────────────────────────────────

func TestRegDeep_ForgotPassword_NoZitadelSub(t *testing.T) {
	srv := newZitadelMockServer(t, http.StatusOK, "")
	svc, accounts, _ := makeRegSvc(t, srv.URL)
	seedAccount(t, accounts, "nozsub", "nozsub@test.com", "", "")

	result, err := svc.ForgotPassword(context.Background(), "nozsub@test.com")
	if err != nil {
		t.Fatalf("ForgotPassword: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	// Account without ZitadelSub → generic message (anti-enumeration).
}

func TestRegDeep_ForgotPassword_ByUsername(t *testing.T) {
	srv := newZitadelMockServer(t, http.StatusOK, "")
	svc, accounts, _ := makeRegSvc(t, srv.URL)
	seedAccount(t, accounts, "userlookup", "", "", "")

	result, err := svc.ForgotPassword(context.Background(), "userlookup")
	if err != nil {
		t.Fatalf("ForgotPassword: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestRegDeep_ForgotPassword_EmailChannel(t *testing.T) {
	// Zitadel mock returns a password reset code.
	zSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"verificationCode":"ABC123"}`))
	}))
	t.Cleanup(zSrv.Close)

	svc, accounts, rdb := makeRegSvcDeepRedis(t, zSrv.URL)
	seedAccount(t, accounts, "emailreset", "emailreset@test.com", "", "zsub-reset")

	result, err := svc.ForgotPassword(context.Background(), "emailreset@test.com")
	if err != nil {
		t.Fatalf("ForgotPassword: %v", err)
	}
	if result.Channel != "email" {
		t.Errorf("channel = %s, want email", result.Channel)
	}

	// Verify code stored in Redis.
	val, err := rdb.Get(context.Background(), "pwd_reset:emailreset@test.com").Result()
	if err != nil {
		t.Fatalf("Redis get: %v", err)
	}
	if val == "" {
		t.Error("expected code in Redis")
	}
}

func TestRegDeep_ForgotPassword_NoEmail_NoPhone(t *testing.T) {
	zSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(zSrv.Close)

	svc, accounts, _ := makeRegSvcDeepRedis(t, zSrv.URL)
	seedAccount(t, accounts, "norecover", "", "", "zsub-norec")

	result, err := svc.ForgotPassword(context.Background(), "norecover")
	if err != nil {
		t.Fatalf("ForgotPassword: %v", err)
	}
	if result.Channel != "" {
		t.Errorf("channel = %s, want empty (no recovery method)", result.Channel)
	}
}

// ── ResetPassword ─────────────────────────────────────────────────────────

func TestRegDeep_ResetPassword_ShortPassword(t *testing.T) {
	srv := newZitadelMockServer(t, http.StatusOK, "")
	svc, _, _ := makeRegSvc(t, srv.URL)

	err := svc.ResetPassword(context.Background(), "test@x.com", "123456", "short")
	if err == nil {
		t.Fatal("expected error for short password")
	}
}

func TestRegDeep_ResetPassword_NoAccount(t *testing.T) {
	srv := newZitadelMockServer(t, http.StatusOK, "")
	svc, _, _ := makeRegSvc(t, srv.URL)

	err := svc.ResetPassword(context.Background(), "nobody@x.com", "123456", "LongEnoughPassword!")
	if err == nil {
		t.Fatal("expected error for nonexistent account")
	}
}

func TestRegDeep_ResetPassword_CodeMatch(t *testing.T) {
	zSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(zSrv.Close)

	svc, accounts, rdb := makeRegSvcDeepRedis(t, zSrv.URL)
	seedAccount(t, accounts, "resetme", "resetme@test.com", "", "zsub-resetme")

	// Store reset code in Redis (email channel).
	rdb.Set(context.Background(), "pwd_reset:resetme@test.com", "MYCODE:zsub-resetme", 0)

	err := svc.ResetPassword(context.Background(), "resetme@test.com", "MYCODE", "NewSecurePassword123!")
	if err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}
}

func TestRegDeep_ResetPassword_WrongCode(t *testing.T) {
	zSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(zSrv.Close)

	svc, accounts, rdb := makeRegSvcDeepRedis(t, zSrv.URL)
	seedAccount(t, accounts, "wrongreset", "wrongreset@test.com", "", "zsub-wr")

	rdb.Set(context.Background(), "pwd_reset:wrongreset@test.com", "CORRECT:zsub-wr", 0)

	err := svc.ResetPassword(context.Background(), "wrongreset@test.com", "WRONG", "NewSecurePassword123!")
	if err == nil {
		t.Fatal("expected error for wrong code")
	}
}

// ── SendPhoneVerificationCode with Redis ──────────────────────────────────

func TestRegDeep_SendPhoneCode_Success(t *testing.T) {
	srv := newZitadelMockServer(t, http.StatusOK, "")
	svc, _, rdb := makeRegSvcDeepRedis(t, srv.URL)

	err := svc.SendPhoneVerificationCode(context.Background(), 1, "13800138000")
	if err != nil {
		t.Fatalf("SendPhoneVerificationCode: %v", err)
	}

	val, err := rdb.Get(context.Background(), "phone_verify:1:13800138000").Result()
	if err != nil {
		t.Fatalf("Redis get: %v", err)
	}
	if len(val) != 6 {
		t.Errorf("code length = %d, want 6", len(val))
	}
}

func TestRegDeep_SendPhoneCode_AlreadyTaken(t *testing.T) {
	srv := newZitadelMockServer(t, http.StatusOK, "")
	svc, accounts, _ := makeRegSvcDeepRedis(t, srv.URL)

	// Seed account with phone.
	seedAccount(t, accounts, "phoneholder", "", "13800138000", "")

	// Account 2 tries to bind the same phone.
	err := svc.SendPhoneVerificationCode(context.Background(), 999, "13800138000")
	if err == nil {
		t.Fatal("expected error for already registered phone")
	}
}

// ── VerifyAndBindPhone with Redis ─────────────────────────────────────────

func TestRegDeep_VerifyPhone_Success(t *testing.T) {
	srv := newZitadelMockServer(t, http.StatusOK, "")
	svc, accounts, rdb := makeRegSvcDeepRedis(t, srv.URL)

	acct := seedAccount(t, accounts, "verifyme", "verifyme@test.com", "", "zsub-ver")

	key := fmt.Sprintf("phone_verify:%d:13900139000", acct.ID)
	rdb.Set(context.Background(), key, "654321", 0)

	err := svc.VerifyAndBindPhone(context.Background(), acct.ID, "13900139000", "654321")
	if err != nil {
		t.Fatalf("VerifyAndBindPhone: %v", err)
	}

	updated, _ := accounts.GetByID(context.Background(), acct.ID)
	if updated.Phone != "13900139000" {
		t.Errorf("phone = %s, want 13900139000", updated.Phone)
	}
	if !updated.PhoneVerified {
		t.Error("PhoneVerified should be true")
	}
}

func TestRegDeep_VerifyPhone_WrongCode(t *testing.T) {
	srv := newZitadelMockServer(t, http.StatusOK, "")
	svc, accounts, rdb := makeRegSvcDeepRedis(t, srv.URL)

	acct := seedAccount(t, accounts, "wrongver", "wrong@test.com", "", "")
	key := fmt.Sprintf("phone_verify:%d:13900139001", acct.ID)
	rdb.Set(context.Background(), key, "111111", 0)

	err := svc.VerifyAndBindPhone(context.Background(), acct.ID, "13900139001", "999999")
	if err == nil {
		t.Fatal("expected error for wrong code")
	}
}

func TestRegDeep_VerifyPhone_Expired(t *testing.T) {
	srv := newZitadelMockServer(t, http.StatusOK, "")
	svc, _, _ := makeRegSvcDeepRedis(t, srv.URL)

	err := svc.VerifyAndBindPhone(context.Background(), 999, "13900139002", "123456")
	if err == nil {
		t.Fatal("expected error for expired code")
	}
}

// ── SetOnAccountCreatedHook ───────────────────────────────────────────────

func TestRegDeep_SetOnAccountCreatedHook(t *testing.T) {
	srv := newZitadelMockServer(t, http.StatusCreated, "zid-hook")
	svc, _, _ := makeRegSvc(t, srv.URL)

	hookDone := make(chan struct{}, 1)
	svc.SetOnAccountCreatedHook(func(ctx context.Context, account *entity.Account) {
		hookDone <- struct{}{}
	})

	_, _ = svc.Register(context.Background(), RegisterRequest{
		Username: "hookeduser",
		Password: "SecurePassword123!",
	})

	select {
	case <-hookDone:
		// ok
	case <-time.After(2 * time.Second):
		t.Error("OnAccountCreatedHook should have been called")
	}
}

// ── Register with duplicate email ─────────────────────────────────────────

func TestRegDeep_Register_DuplicateEmail(t *testing.T) {
	srv := newZitadelMockServer(t, http.StatusCreated, "zid-dup")
	svc, accounts, _ := makeRegSvc(t, srv.URL)
	seedAccount(t, accounts, "existingmail", "dup@test.com", "", "")

	_, err := svc.Register(context.Background(), RegisterRequest{
		Username: "newuser",
		Password: "SecurePassword123!",
		Email:    "dup@test.com",
	})
	if err == nil {
		t.Fatal("expected error for duplicate email")
	}
	if !stringContains(err.Error(), "email already registered") {
		t.Errorf("error = %v, want 'email already registered'", err)
	}
}

// ── Register with duplicate phone ─────────────────────────────────────────

func TestRegDeep_Register_DuplicatePhone(t *testing.T) {
	srv := newZitadelMockServer(t, http.StatusCreated, "zid-dup-ph")
	svc, accounts, _ := makeRegSvc(t, srv.URL)
	seedAccount(t, accounts, "existingphone", "", "13800138000", "")

	_, err := svc.Register(context.Background(), RegisterRequest{
		Username: "newuser2",
		Password: "SecurePassword123!",
		Phone:    "13800138000",
	})
	if err == nil {
		t.Fatal("expected error for duplicate phone")
	}
	if !stringContains(err.Error(), "phone number already registered") {
		t.Errorf("error = %v, want 'phone number already registered'", err)
	}
}

// ── Register with invalid email/phone ─────────────────────────────────────

func TestRegDeep_Register_InvalidEmail(t *testing.T) {
	srv := newZitadelMockServer(t, http.StatusCreated, "")
	svc, _, _ := makeRegSvc(t, srv.URL)

	_, err := svc.Register(context.Background(), RegisterRequest{
		Username: "validuser",
		Password: "SecurePassword123!",
		Email:    "not-an-email",
	})
	if err == nil {
		t.Fatal("expected error for invalid email")
	}
}

func TestRegDeep_Register_InvalidPhone(t *testing.T) {
	srv := newZitadelMockServer(t, http.StatusCreated, "")
	svc, _, _ := makeRegSvc(t, srv.URL)

	_, err := svc.Register(context.Background(), RegisterRequest{
		Username: "validuser2",
		Password: "SecurePassword123!",
		Phone:    "123",
	})
	if err == nil {
		t.Fatal("expected error for invalid phone")
	}
}

// ── SetOnCheckinHook (one-liner coverage) ─────────────────────────────────

func TestSetOnCheckinHook_Coverage(t *testing.T) {
	svc := NewCheckinService(newMockCheckinStore(), newMockWalletStore())
	svc.SetOnCheckinHook(func(ctx context.Context, accountID int64, streak int) {})
	if svc.onCheckin == nil {
		t.Error("onCheckin hook should be set")
	}
}
