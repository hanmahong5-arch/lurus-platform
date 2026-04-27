// Package sms provides the SMS OTP relay use-case for platform services.
package sms

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Sentinel errors — callers must use errors.Is() for matching.
var (
	// ErrInvalidPhone is returned when the phone number is not valid E.164 format.
	ErrInvalidPhone = errors.New("sms: invalid phone number (must be E.164, e.g. +8613800138000)")

	// ErrRateLimit is returned when the SMS provider reports a rate-limit response.
	// The caller should propagate this as HTTP 429.
	ErrRateLimit = errors.New("sms: rate limit exceeded")
)

// e164Pattern validates that phone numbers are E.164: + followed by 7–15 digits.
// For China (+86) the full number is 13 chars; we allow the range 8–16 to cover
// other country codes while still rejecting obviously wrong values.
var e164Pattern = regexp.MustCompile(`^\+\d{7,15}$`)

// SMSSender is the interface satisfied by pkg/sms.Sender (and test doubles).
type SMSSender interface {
	Send(ctx context.Context, phone, templateID string, params map[string]string) error
}

// SMSRelayUsecase orchestrates OTP SMS sending with retry on transient errors.
type SMSRelayUsecase struct {
	sender          SMSSender
	signName        string // Aliyun sign name (unused by Send, kept for logging)
	templateVerify  string // template code for OTP verification
	templateReset   string // template code for password reset (reserved)
	maxRetries      int
}

// NewSMSRelayUsecase creates a new usecase.
// maxRetries is the number of attempts (1 = no retry).
func NewSMSRelayUsecase(
	sender SMSSender,
	signName, templateVerify, templateReset string,
	maxRetries int,
) *SMSRelayUsecase {
	if maxRetries < 1 {
		maxRetries = 1
	}
	return &SMSRelayUsecase{
		sender:         sender,
		signName:       signName,
		templateVerify: templateVerify,
		templateReset:  templateReset,
		maxRetries:     maxRetries,
	}
}

// SendOTP sends a one-time password SMS to the given E.164 phone number.
// It retries up to maxRetries times on transient errors; rate-limit errors
// are returned immediately without retrying.
func (u *SMSRelayUsecase) SendOTP(ctx context.Context, phone, code string) error {
	if err := validateE164(phone); err != nil {
		return err
	}

	params := map[string]string{"code": code}

	var lastErr error
	for attempt := 0; attempt < u.maxRetries; attempt++ {
		err := u.sender.Send(ctx, phone, u.templateVerify, params)
		if err == nil {
			return nil
		}
		// Rate-limit errors must not be retried — surface immediately.
		if isRateLimit(err) {
			return ErrRateLimit
		}
		lastErr = err
	}
	return fmt.Errorf("sms: all %d attempts failed: %w", u.maxRetries, lastErr)
}

// validateE164 checks that phone matches E.164 format.
func validateE164(phone string) error {
	if phone == "" || !e164Pattern.MatchString(phone) {
		return fmt.Errorf("%w: %q", ErrInvalidPhone, phone)
	}
	return nil
}

// isRateLimit heuristically detects rate-limit responses from various SMS providers.
// Aliyun returns code "isv.BUSINESS_LIMIT_CONTROL" in error messages; we also detect
// callers that set ErrRateLimit directly (test doubles).
func isRateLimit(err error) bool {
	if errors.Is(err, ErrRateLimit) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "business_limit_control") ||
		strings.Contains(msg, "rate limit") ||
		strings.Contains(msg, "ratelimit") ||
		strings.Contains(msg, "isv.limit")
}
