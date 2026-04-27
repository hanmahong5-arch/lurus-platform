package sms_test

import (
	"context"
	"errors"
	"testing"

	appsms "github.com/hanmahong5-arch/lurus-platform/internal/app/sms"
)

// stubSender records calls and returns configurable errors.
type stubSender struct {
	calls  []stubCall
	errors []error // errors[i] returned on the i-th call; nil = success
}

type stubCall struct {
	phone      string
	templateID string
	params     map[string]string
}

func (s *stubSender) Send(_ context.Context, phone, templateID string, params map[string]string) error {
	idx := len(s.calls)
	s.calls = append(s.calls, stubCall{phone: phone, templateID: templateID, params: params})
	if idx < len(s.errors) {
		return s.errors[idx]
	}
	return nil
}

// TestSMSRelayUsecase_SendOTP_Success verifies a valid phone+code sends and returns nil.
func TestSMSRelayUsecase_SendOTP_Success(t *testing.T) {
	stub := &stubSender{}
	uc := appsms.NewSMSRelayUsecase(stub, "SIGN", "TPL_VERIFY", "TPL_RESET", 1)

	err := uc.SendOTP(context.Background(), "+8613800138000", "382910")
	if err != nil {
		t.Fatalf("SendOTP returned unexpected error: %v", err)
	}
	if len(stub.calls) != 1 {
		t.Fatalf("expected 1 SMS call, got %d", len(stub.calls))
	}
	if stub.calls[0].phone != "+8613800138000" {
		t.Errorf("phone = %q, want +8613800138000", stub.calls[0].phone)
	}
	if stub.calls[0].params["code"] != "382910" {
		t.Errorf("params[code] = %q, want 382910", stub.calls[0].params["code"])
	}
}

// TestSMSRelayUsecase_SendOTP_InvalidPhone verifies E.164 validation.
func TestSMSRelayUsecase_SendOTP_InvalidPhone(t *testing.T) {
	stub := &stubSender{}
	uc := appsms.NewSMSRelayUsecase(stub, "SIGN", "TPL_VERIFY", "TPL_RESET", 1)

	cases := []string{
		"13800138000",         // missing + prefix
		"+86138",              // too short (only 5 digits after +)
		"+86138001380011223",  // too long (17 digits after +, exceeds E.164 max 15)
		"",
		"+123456",             // 6 digits (below 7-digit minimum)
	}
	for _, phone := range cases {
		err := uc.SendOTP(context.Background(), phone, "123456")
		if err == nil {
			t.Errorf("phone %q: expected validation error, got nil", phone)
		}
		if !errors.Is(err, appsms.ErrInvalidPhone) {
			t.Errorf("phone %q: error = %v, want ErrInvalidPhone", phone, err)
		}
	}
	if len(stub.calls) != 0 {
		t.Errorf("expected 0 SMS calls for invalid phones, got %d", len(stub.calls))
	}
}

// TestSMSRelayUsecase_SendOTP_RetryOnTransient verifies the usecase retries up to maxRetries
// on transient (non-rate-limit) errors, then succeeds on a subsequent attempt.
func TestSMSRelayUsecase_SendOTP_RetryOnTransient(t *testing.T) {
	transient := errors.New("dysmsapi: service temporarily unavailable")
	stub := &stubSender{
		errors: []error{transient, transient, nil}, // fail twice, succeed third
	}
	uc := appsms.NewSMSRelayUsecase(stub, "SIGN", "TPL_VERIFY", "TPL_RESET", 3)

	err := uc.SendOTP(context.Background(), "+8613800138000", "654321")
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if len(stub.calls) != 3 {
		t.Errorf("expected 3 calls (2 fail + 1 success), got %d", len(stub.calls))
	}
}

// TestSMSRelayUsecase_SendOTP_FailAfterAllRetries verifies that exhausting all retries returns an error.
func TestSMSRelayUsecase_SendOTP_FailAfterAllRetries(t *testing.T) {
	transient := errors.New("dysmsapi: 500 internal server error")
	stub := &stubSender{
		errors: []error{transient, transient, transient, transient}, // always fail
	}
	uc := appsms.NewSMSRelayUsecase(stub, "SIGN", "TPL_VERIFY", "TPL_RESET", 3)

	err := uc.SendOTP(context.Background(), "+8613800138000", "654321")
	if err == nil {
		t.Fatal("expected error after all retries exhausted")
	}
	if len(stub.calls) != 3 {
		t.Errorf("expected 3 calls (all retries), got %d", len(stub.calls))
	}
}

// TestSMSRelayUsecase_SendOTP_RateLimitNoRetry verifies rate-limit errors are NOT retried.
func TestSMSRelayUsecase_SendOTP_RateLimitNoRetry(t *testing.T) {
	rateLimitErr := appsms.ErrRateLimit
	stub := &stubSender{
		errors: []error{rateLimitErr},
	}
	uc := appsms.NewSMSRelayUsecase(stub, "SIGN", "TPL_VERIFY", "TPL_RESET", 3)

	err := uc.SendOTP(context.Background(), "+8613800138000", "111111")
	if err == nil {
		t.Fatal("expected ErrRateLimit, got nil")
	}
	if !errors.Is(err, appsms.ErrRateLimit) {
		t.Errorf("error = %v, want ErrRateLimit", err)
	}
	if len(stub.calls) != 1 {
		t.Errorf("expected 1 call (no retry on rate limit), got %d", len(stub.calls))
	}
}
