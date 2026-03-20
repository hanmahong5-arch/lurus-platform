package app

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"net/mail"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/auth"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/email"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/sms"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/zitadel"
)

const (
	minPasswordLen      = 8
	sessionTokenTTL     = 7 * 24 * time.Hour // 7-day session
	passwordResetCodeTTL = 10 * time.Minute
	phoneVerifyCodeTTL  = 10 * time.Minute
	resetCodeLen        = 6

	// Redis key prefixes for password reset and phone verification.
	redisPwdResetPrefix   = "pwd_reset:"
	redisPhoneVerifyPrefix = "phone_verify:"
)

// RegisterRequest holds input for user registration.
type RegisterRequest struct {
	Username string
	Password string
	Email    string
	Phone    string
	AffCode  string
}

// RegistrationResult holds the output of a successful registration.
type RegistrationResult struct {
	Token     string `json:"token"`
	AccountID int64  `json:"account_id"`
	LurusID   string `json:"lurus_id"`
}

// ForgotPasswordResult holds the output of a forgot-password request.
type ForgotPasswordResult struct {
	Message string `json:"message"`
	Channel string `json:"channel"` // "email" or "sms"
}

// ReferralSignupHookFunc is called after a referred user completes registration.
type ReferralSignupHookFunc func(ctx context.Context, referrerAccountID int64, referredName string)

// RegistrationService handles user registration and password reset flows.
type RegistrationService struct {
	accounts          accountStore
	wallets           walletStore
	vip               vipStore
	referral          *ReferralService
	zitadel           *zitadel.Client
	sessionSecret     string
	emailSender       email.Sender
	smsSender         sms.Sender
	redis             *redis.Client
	smsCfg            sms.SMSConfig
	onReferralSignup  ReferralSignupHookFunc
}

// NewRegistrationService creates the registration service.
// Returns nil if sessionSecret is not configured.
// Zitadel client is optional — if nil, users are created locally only.
func NewRegistrationService(
	accounts accountStore,
	wallets walletStore,
	vip vipStore,
	referral *ReferralService,
	zc *zitadel.Client,
	sessionSecret string,
	emailSender email.Sender,
	smsSender sms.Sender,
	rdb *redis.Client,
	smsCfg sms.SMSConfig,
) *RegistrationService {
	if sessionSecret == "" {
		return nil
	}
	return &RegistrationService{
		accounts:      accounts,
		wallets:       wallets,
		vip:           vip,
		referral:      referral,
		zitadel:       zc,
		sessionSecret: sessionSecret,
		emailSender:   emailSender,
		smsSender:     smsSender,
		redis:         rdb,
		smsCfg:        smsCfg,
	}
}

// SetOnReferralSignupHook sets the post-referral-signup hook (typically wired to module.Registry.FireReferralSignup).
func (s *RegistrationService) SetOnReferralSignupHook(fn ReferralSignupHookFunc) {
	s.onReferralSignup = fn
}

// CheckUsernameAvailable reports whether a username is not yet taken.
// Returns (true, nil) if available, (false, nil) if taken.
func (s *RegistrationService) CheckUsernameAvailable(ctx context.Context, username string) (bool, error) {
	existing, err := s.accounts.GetByUsername(ctx, username)
	if err != nil {
		return false, fmt.Errorf("check username: %w", err)
	}
	return existing == nil, nil
}

// CheckEmailAvailable reports whether an email is not yet registered.
// Returns an error if the email format is invalid.
func (s *RegistrationService) CheckEmailAvailable(ctx context.Context, email string) (bool, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if _, err := mail.ParseAddress(email); err != nil {
		return false, fmt.Errorf("invalid email format")
	}
	existing, err := s.accounts.GetByEmail(ctx, email)
	if err != nil {
		return false, fmt.Errorf("check email: %w", err)
	}
	return existing == nil, nil
}

// Register creates a new user account with username + password.
func (s *RegistrationService) Register(ctx context.Context, req RegisterRequest) (*RegistrationResult, error) {
	// Validate username.
	req.Username = strings.TrimSpace(req.Username)
	if err := entity.ValidateUsername(req.Username); err != nil {
		return nil, fmt.Errorf("register: %w", err)
	}

	// Validate password length.
	if len(req.Password) < minPasswordLen {
		return nil, fmt.Errorf("register: password must be at least %d characters", minPasswordLen)
	}

	// Check username uniqueness.
	existing, err := s.accounts.GetByUsername(ctx, req.Username)
	if err != nil {
		return nil, fmt.Errorf("register: check existing username: %w", err)
	}
	if existing != nil {
		return nil, fmt.Errorf("register: username already taken")
	}

	// Normalize and validate optional email.
	var emailAddr string
	if req.Email != "" {
		emailAddr = strings.TrimSpace(strings.ToLower(req.Email))
		if _, err := mail.ParseAddress(emailAddr); err != nil {
			return nil, fmt.Errorf("register: invalid email format")
		}
		existingEmail, err := s.accounts.GetByEmail(ctx, emailAddr)
		if err != nil {
			return nil, fmt.Errorf("register: check existing email: %w", err)
		}
		if existingEmail != nil {
			return nil, fmt.Errorf("register: email already registered")
		}
	}

	// Normalize and validate optional phone.
	var phone string
	if req.Phone != "" {
		phone = strings.TrimSpace(req.Phone)
		if !entity.IsPhoneNumber(phone) {
			return nil, fmt.Errorf("register: invalid phone number format")
		}
		existingPhone, err := s.accounts.GetByPhone(ctx, phone)
		if err != nil {
			return nil, fmt.Errorf("register: check existing phone: %w", err)
		}
		if existingPhone != nil {
			return nil, fmt.Errorf("register: phone number already registered")
		}
	}

	// If username is a phone number, auto-fill phone field.
	if entity.IsPhoneNumber(req.Username) && phone == "" {
		phone = req.Username
	}

	// Create user in Zitadel with username (optional — skip if Zitadel not configured).
	var zitadelSub string
	if s.zitadel != nil {
		zUser, err := s.zitadel.CreateHumanUserWithUsername(ctx, req.Username, req.Password, emailAddr)
		if err != nil {
			return nil, fmt.Errorf("register: create zitadel user: %w", err)
		}
		zitadelSub = zUser.UserID
	}

	// Create local account.
	affCodeVal, err := generateAffCode()
	if err != nil {
		return nil, fmt.Errorf("register: generate aff code: %w", err)
	}
	displayName := req.Username
	if emailAddr != "" {
		displayName = emailAddr
	}
	account := &entity.Account{
		ZitadelSub:  zitadelSub,
		Username:    req.Username,
		Email:       emailAddr,
		Phone:       phone,
		DisplayName: displayName,
		AffCode:     affCodeVal,
		Status:      entity.AccountStatusActive,
	}
	if err := s.accounts.Create(ctx, account); err != nil {
		return nil, fmt.Errorf("register: create account: %w", err)
	}

	// Assign LurusID after insert (needs auto-increment ID).
	account.LurusID = entity.GenerateLurusID(account.ID)
	if err := s.accounts.Update(ctx, account); err != nil {
		return nil, fmt.Errorf("register: set lurus_id: %w", err)
	}

	// Bootstrap wallet and VIP.
	if _, err := s.wallets.GetOrCreate(ctx, account.ID); err != nil {
		return nil, fmt.Errorf("register: create wallet: %w", err)
	}
	if _, err := s.vip.GetOrCreate(ctx, account.ID); err != nil {
		return nil, fmt.Errorf("register: create vip: %w", err)
	}

	// Process referral (non-critical).
	if req.AffCode != "" && s.referral != nil {
		referrer, rerr := s.accounts.GetByAffCode(ctx, req.AffCode)
		if rerr == nil && referrer != nil && referrer.ID != account.ID {
			referrerID := referrer.ID
			account.ReferrerID = &referrerID
			if uerr := s.accounts.Update(ctx, account); uerr == nil {
				_ = s.referral.OnSignup(ctx, account.ID, referrer.ID)
				// Fire referral signup hook (notification to referrer) — non-blocking.
				if s.onReferralSignup != nil {
					referredName := account.DisplayName
					if referredName == "" {
						referredName = account.Username
					}
					go s.onReferralSignup(ctx, referrer.ID, referredName)
				}
			}
		}
	}

	// Issue session token.
	token, err := auth.IssueSessionToken(account.ID, sessionTokenTTL, s.sessionSecret)
	if err != nil {
		return nil, fmt.Errorf("register: issue token: %w", err)
	}

	return &RegistrationResult{
		Token:     token,
		AccountID: account.ID,
		LurusID:   account.LurusID,
	}, nil
}

// ForgotPassword initiates a password reset flow.
// Supports lookup by email, phone, or username.
// Returns the channel used (email/sms) so the frontend can show the right prompt.
func (s *RegistrationService) ForgotPassword(ctx context.Context, identifier string) (*ForgotPasswordResult, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return nil, fmt.Errorf("forgot-password: identifier is required")
	}

	// Resolve account by email → phone → username.
	account, err := s.resolveAccount(ctx, identifier)
	if err != nil {
		return nil, fmt.Errorf("forgot-password: lookup: %w", err)
	}
	if account == nil || account.ZitadelSub == "" {
		// Do not reveal whether the account exists.
		return &ForgotPasswordResult{
			Message: "if the account exists, a reset code has been sent",
			Channel: "email",
		}, nil
	}

	// Determine channel: prefer SMS if phone is available and SMS is configured.
	if account.Phone != "" && s.smsCfg.Provider != "" {
		// SMS channel: generate our own 6-digit code and store in Redis.
		code, err := generateNumericCode(resetCodeLen)
		if err != nil {
			return nil, fmt.Errorf("forgot-password: generate code: %w", err)
		}

		key := redisPwdResetPrefix + account.Phone
		// Store code + zitadel sub as composite value.
		val := code + ":" + account.ZitadelSub
		if err := s.redis.Set(ctx, key, val, passwordResetCodeTTL).Err(); err != nil {
			return nil, fmt.Errorf("forgot-password: store code: %w", err)
		}

		templateID := s.smsCfg.TencentTemplateIDReset
		if s.smsCfg.Provider == "aliyun" {
			templateID = s.smsCfg.AliyunTemplateReset
		}
		if err := s.smsSender.Send(ctx, account.Phone, templateID, map[string]string{
			"code": code,
			"ttl":  "10",
		}); err != nil {
			return nil, fmt.Errorf("forgot-password: send sms: %w", err)
		}

		return &ForgotPasswordResult{
			Message: "a reset code has been sent to your phone",
			Channel: "sms",
		}, nil
	}

	// Email channel: use Zitadel password reset + send email.
	if account.Email == "" {
		return &ForgotPasswordResult{
			Message: "no recovery method available for this account",
			Channel: "",
		}, nil
	}

	code, err := s.zitadel.RequestPasswordReset(ctx, account.ZitadelSub, true)
	if err != nil {
		return nil, fmt.Errorf("forgot-password: zitadel reset: %w", err)
	}

	// Store in Redis (replaces old in-memory map).
	key := redisPwdResetPrefix + account.Email
	val := code + ":" + account.ZitadelSub
	if err := s.redis.Set(ctx, key, val, passwordResetCodeTTL).Err(); err != nil {
		return nil, fmt.Errorf("forgot-password: store code: %w", err)
	}

	subject := "Password Reset - Lurus"
	body := fmt.Sprintf("Your password reset code is: %s\n\nThis code expires in 10 minutes.", code)
	if err := s.emailSender.Send(ctx, account.Email, subject, body); err != nil {
		return nil, fmt.Errorf("forgot-password: send email: %w", err)
	}

	return &ForgotPasswordResult{
		Message: "a reset code has been sent to your email",
		Channel: "email",
	}, nil
}

// ResetPassword executes a password reset using the verification code.
// Identifier can be email, phone, or username.
func (s *RegistrationService) ResetPassword(ctx context.Context, identifier, code, newPassword string) error {
	identifier = strings.TrimSpace(identifier)
	if len(newPassword) < minPasswordLen {
		return fmt.Errorf("reset-password: password must be at least %d characters", minPasswordLen)
	}

	// Try to find the reset code in Redis by email or phone.
	account, err := s.resolveAccount(ctx, identifier)
	if err != nil {
		return fmt.Errorf("reset-password: lookup: %w", err)
	}
	if account == nil {
		return fmt.Errorf("reset-password: no pending reset for this identifier")
	}

	// Try phone key first, then email key.
	var storedVal string
	var redisKey string
	if account.Phone != "" {
		redisKey = redisPwdResetPrefix + account.Phone
		storedVal, err = s.redis.Get(ctx, redisKey).Result()
		if err != nil {
			storedVal = ""
		}
	}
	if storedVal == "" && account.Email != "" {
		redisKey = redisPwdResetPrefix + account.Email
		storedVal, err = s.redis.Get(ctx, redisKey).Result()
		if err != nil {
			storedVal = ""
		}
	}
	if storedVal == "" {
		return fmt.Errorf("reset-password: no pending reset for this identifier")
	}

	// Parse stored value: "code:zitadelSub"
	parts := strings.SplitN(storedVal, ":", 2)
	if len(parts) != 2 {
		return fmt.Errorf("reset-password: invalid stored reset data")
	}
	storedCode, zitadelSub := parts[0], parts[1]

	if storedCode != code {
		return fmt.Errorf("reset-password: invalid verification code")
	}

	// Set new password in Zitadel.
	if err := s.zitadel.SetNewPassword(ctx, zitadelSub, storedCode, newPassword); err != nil {
		return fmt.Errorf("reset-password: zitadel set password: %w", err)
	}

	// Clean up.
	_ = s.redis.Del(ctx, redisKey).Err()
	return nil
}

// SendPhoneVerificationCode sends a verification code to bind a phone number.
func (s *RegistrationService) SendPhoneVerificationCode(ctx context.Context, accountID int64, phone string) error {
	phone = strings.TrimSpace(phone)
	if !entity.IsPhoneNumber(phone) {
		return fmt.Errorf("send-phone-code: invalid phone number format")
	}

	// Check phone not already taken.
	existing, err := s.accounts.GetByPhone(ctx, phone)
	if err != nil {
		return fmt.Errorf("send-phone-code: check existing: %w", err)
	}
	if existing != nil && existing.ID != accountID {
		return fmt.Errorf("send-phone-code: phone number already registered")
	}

	code, err := generateNumericCode(resetCodeLen)
	if err != nil {
		return fmt.Errorf("send-phone-code: generate code: %w", err)
	}

	key := fmt.Sprintf("%s%d:%s", redisPhoneVerifyPrefix, accountID, phone)
	if err := s.redis.Set(ctx, key, code, phoneVerifyCodeTTL).Err(); err != nil {
		return fmt.Errorf("send-phone-code: store code: %w", err)
	}

	templateID := s.smsCfg.TencentTemplateIDVerify
	if s.smsCfg.Provider == "aliyun" {
		templateID = s.smsCfg.AliyunTemplateVerify
	}
	if err := s.smsSender.Send(ctx, phone, templateID, map[string]string{
		"code": code,
		"ttl":  "10",
	}); err != nil {
		return fmt.Errorf("send-phone-code: send sms: %w", err)
	}

	return nil
}

// VerifyAndBindPhone verifies the code and binds the phone to the account.
func (s *RegistrationService) VerifyAndBindPhone(ctx context.Context, accountID int64, phone, code string) error {
	phone = strings.TrimSpace(phone)
	if !entity.IsPhoneNumber(phone) {
		return fmt.Errorf("verify-phone: invalid phone number format")
	}

	key := fmt.Sprintf("%s%d:%s", redisPhoneVerifyPrefix, accountID, phone)
	storedCode, err := s.redis.Get(ctx, key).Result()
	if err != nil {
		return fmt.Errorf("verify-phone: no pending verification or code expired")
	}
	if storedCode != code {
		return fmt.Errorf("verify-phone: invalid verification code")
	}

	// Bind phone to account.
	account, err := s.accounts.GetByID(ctx, accountID)
	if err != nil || account == nil {
		return fmt.Errorf("verify-phone: account not found")
	}
	account.Phone = phone
	account.PhoneVerified = true
	if err := s.accounts.Update(ctx, account); err != nil {
		return fmt.Errorf("verify-phone: update account: %w", err)
	}

	_ = s.redis.Del(ctx, key).Err()
	return nil
}

// resolveAccount looks up an account by email, phone, or username.
func (s *RegistrationService) resolveAccount(ctx context.Context, identifier string) (*entity.Account, error) {
	identifier = strings.ToLower(strings.TrimSpace(identifier))

	// Try email first.
	if strings.Contains(identifier, "@") {
		return s.accounts.GetByEmail(ctx, identifier)
	}

	// Try phone number.
	if entity.IsPhoneNumber(identifier) {
		acc, err := s.accounts.GetByPhone(ctx, identifier)
		if err != nil || acc != nil {
			return acc, err
		}
	}

	// Try username.
	return s.accounts.GetByUsername(ctx, identifier)
}

// generateNumericCode generates a random N-digit numeric string.
func generateNumericCode(length int) (string, error) {
	code := make([]byte, length)
	for i := range code {
		n, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", err
		}
		code[i] = byte('0' + n.Int64())
	}
	return string(code), nil
}
