package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/hanmahong5-arch/lurus-platform/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// RegistrationHandler handles user registration and password reset endpoints.
type RegistrationHandler struct {
	registration *app.RegistrationService
	cookieDomain string // parent domain for session cookie set after register; "" = host-only
}

// NewRegistrationHandler creates the handler. Returns nil if service is nil.
func NewRegistrationHandler(registration *app.RegistrationService) *RegistrationHandler {
	if registration == nil {
		return nil
	}
	return &RegistrationHandler{registration: registration}
}

// ── Pre-submit validation ───────────────────────────────────────────────────
// These endpoints let the frontend validate individual fields BEFORE the user
// submits the form, providing instant inline feedback.

// CheckUsername reports whether a username is available and valid.
// POST /api/v1/auth/check-username
// Response: { "available": true, "suggestion": "" }
// or       { "available": false, "reason": "taken" | "invalid" }
func (h *RegistrationHandler) CheckUsername(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		handleBindError(c, err)
		return
	}

	username := strings.TrimSpace(req.Username)

	// Validate format.
	if err := entity.ValidateUsername(username); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"available": false,
			"reason":    "invalid",
			"message":   "Username must be 3-32 alphanumeric/underscore characters, or a phone number",
		})
		return
	}

	// Check availability.
	existing, err := h.registration.CheckUsernameAvailable(c.Request.Context(), username)
	if err != nil {
		respondInternalError(c, "registration.check_username", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"available": existing,
	})
}

// CheckEmail reports whether an email address is available and valid.
// POST /api/v1/auth/check-email
func (h *RegistrationHandler) CheckEmail(c *gin.Context) {
	var req struct {
		Email string `json:"email" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		handleBindError(c, err)
		return
	}

	available, err := h.registration.CheckEmailAvailable(c.Request.Context(), strings.TrimSpace(req.Email))
	if err != nil {
		if strings.Contains(err.Error(), "invalid") {
			c.JSON(http.StatusOK, gin.H{
				"available": false,
				"reason":    "invalid",
				"message":   "Please enter a valid email address",
			})
			return
		}
		respondInternalError(c, "registration.check_email", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"available": available})
}

// ── Registration ────────────────────────────────────────────────────────────

// Register creates a new user account.
// POST /api/v1/auth/register
//
// Success response:
//
//	{
//	  "token": "...",
//	  "account_id": 1,
//	  "lurus_id": "LU0000001",
//	  "redirect_url": "/dashboard"
//	}
//
// Field-level validation error:
//
//	{
//	  "error": {
//	    "code": "validation_error",
//	    "message": "Please fix the issues below",
//	    "fields": { "username": "...", "password": "..." }
//	  }
//	}
//
// Conflict error (account exists):
//
//	{
//	  "error": {
//	    "code": "conflict",
//	    "message": "This email is already registered",
//	    "actions": [{ "type": "link", "label": "Sign in instead", "url": "/login" }]
//	  }
//	}
func (h *RegistrationHandler) Register(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
		Email    string `json:"email"`
		Phone    string `json:"phone"`
		AffCode  string `json:"aff_code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		handleBindError(c, err)
		return
	}

	result, err := h.registration.Register(c.Request.Context(), app.RegisterRequest{
		Username: req.Username,
		Password: req.Password,
		Email:    req.Email,
		Phone:    req.Phone,
		AffCode:  req.AffCode,
	})
	if err != nil {
		h.classifyRegistrationError(c, err)
		return
	}

	// Mirror DirectLogin: set parent-domain cookie so the new account is
	// immediately authenticated on every *.lurus.cn subdomain.
	SetSessionCookie(c, result.Token, h.cookieDomain)

	c.JSON(http.StatusCreated, gin.H{
		"token":        result.Token,
		"account_id":   result.AccountID,
		"lurus_id":     result.LurusID,
		"redirect_url": "/dashboard",
	})
}

// WithCookieDomain wires the cookie parent-domain (mirrors ZLoginHandler).
func (h *RegistrationHandler) WithCookieDomain(d string) *RegistrationHandler {
	if h == nil {
		return nil
	}
	h.cookieDomain = d
	return h
}

// classifyRegistrationError maps app-layer errors to rich UX-friendly responses.
// Each error is mapped to the specific form field that caused it, so the frontend
// can highlight the exact input and show an inline error message.
func (h *RegistrationHandler) classifyRegistrationError(c *gin.Context, err error) {
	msg := err.Error()

	// ── Field-level validation errors → 400 with field mapping ──
	fields := make(map[string]string)

	if containsAny(msg, "username is required", "username must be") {
		fields["username"] = "Username must be 3-32 alphanumeric characters, or a phone number"
	}
	if containsAny(msg, "invalid email") {
		fields["email"] = "Please enter a valid email address"
	}
	if containsAny(msg, "invalid phone") {
		fields["phone"] = "Please enter a valid 11-digit phone number"
	}
	if containsAny(msg, "password must be at least") {
		fields["password"] = "Password must be at least 8 characters"
	}

	if len(fields) > 0 {
		respondValidationError(c, "Please fix the issues below", fields)
		return
	}

	// ── Conflict errors → 409 with navigation action ──
	if containsAny(msg, "username already taken") {
		respondConflictWithAction(c,
			"This username is already taken. Try a different one, or sign in if you already have an account.",
			ActionGoToLogin(),
		)
		return
	}
	if containsAny(msg, "email already registered") {
		respondConflictWithAction(c,
			"This email is already registered.",
			ActionGoToLogin(),
			ActionGoToForgotPassword(),
		)
		return
	}
	if containsAny(msg, "phone number already registered") {
		respondConflictWithAction(c,
			"This phone number is already registered.",
			ActionGoToLogin(),
		)
		return
	}
	if containsAny(msg, "already exists in Zitadel") {
		respondConflictWithAction(c,
			"An account with these credentials already exists.",
			ActionGoToLogin(),
		)
		return
	}

	// ── Unknown errors → 500 with retry action ──
	respondRichError(c, http.StatusInternalServerError, ErrorBody{
		Code:    "internal_error",
		Message: "Registration failed. Please try again in a moment.",
		Actions: []ErrorAction{ActionRetry()},
	})
}

// ── Password reset ──────────────────────────────────────────────────────────

// ForgotPassword initiates a password reset flow.
// POST /api/v1/auth/forgot-password
//
// Always returns 200 to prevent account enumeration.
// The "channel" field tells the frontend which verification UI to show.
func (h *RegistrationHandler) ForgotPassword(c *gin.Context) {
	var req struct {
		Identifier string `json:"identifier" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		handleBindError(c, err)
		return
	}

	result, err := h.registration.ForgotPassword(c.Request.Context(), req.Identifier)
	if err != nil {
		// Always 200 — do not reveal whether account exists.
		c.JSON(http.StatusOK, gin.H{
			"message": "If the account exists, a reset code has been sent",
			"channel": "email",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":      result.Message,
		"channel":      result.Channel,
		"redirect_url": "/reset-password",
	})
}

// ResetPassword executes a password reset using a verification code.
// POST /api/v1/auth/reset-password
func (h *RegistrationHandler) ResetPassword(c *gin.Context) {
	var req struct {
		Identifier  string `json:"identifier"   binding:"required"`
		Code        string `json:"code"         binding:"required"`
		NewPassword string `json:"new_password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		handleBindError(c, err)
		return
	}

	if err := h.registration.ResetPassword(c.Request.Context(), req.Identifier, req.Code, req.NewPassword); err != nil {
		msg := err.Error()
		switch {
		case containsAny(msg, "password must be at least"):
			respondValidationError(c, "Password is too short", map[string]string{
				"new_password": "Password must be at least 8 characters",
			})
		case containsAny(msg, "no pending reset", "expired"):
			respondRichError(c, http.StatusBadRequest, ErrorBody{
				Code:    "code_expired",
				Message: "This reset code has expired. Please request a new one.",
				Actions: []ErrorAction{
					{Type: "link", Label: "Request new code", URL: "/forgot-password"},
				},
			})
		case containsAny(msg, "invalid verification"):
			respondValidationError(c, "Incorrect code", map[string]string{
				"code": "The verification code is incorrect. Please check and try again.",
			})
		default:
			respondRichError(c, http.StatusInternalServerError, ErrorBody{
				Code:    "internal_error",
				Message: "Password reset failed. Please try again.",
				Actions: []ErrorAction{ActionRetry()},
			})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":      "Password reset successful. You can now sign in.",
		"redirect_url": "/login",
		"actions":      []ErrorAction{{Type: "link", Label: "Sign in now", URL: "/login"}},
	})
}

// ── Phone verification ──────────────────────────────────────────────────────

// SendSMSCode sends an SMS code for password reset (unauthenticated).
// POST /api/v1/auth/send-sms
func (h *RegistrationHandler) SendSMSCode(c *gin.Context) {
	var req struct {
		Identifier string `json:"identifier" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		handleBindError(c, err)
		return
	}

	result, _ := h.registration.ForgotPassword(c.Request.Context(), req.Identifier)
	if result != nil {
		c.JSON(http.StatusOK, gin.H{"message": result.Message, "channel": result.Channel})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "If the account exists, a reset code has been sent"})
}

// SendPhoneCode sends a phone verification code (authenticated, for binding phone).
// POST /api/v1/account/me/send-phone-code
func (h *RegistrationHandler) SendPhoneCode(c *gin.Context) {
	accountID, ok := requireAccountID(c)
	if !ok {
		return
	}

	var req struct {
		Phone string `json:"phone" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		handleBindError(c, err)
		return
	}

	if err := h.registration.SendPhoneVerificationCode(c.Request.Context(), accountID, req.Phone); err != nil {
		msg := err.Error()
		switch {
		case containsAny(msg, "invalid phone"):
			respondValidationError(c, "Invalid phone number", map[string]string{
				"phone": "Please enter a valid 11-digit phone number",
			})
		case containsAny(msg, "already registered"):
			respondConflictWithAction(c,
				"This phone number is already registered to another account.",
			)
		default:
			respondInternalError(c, "registration.send_phone_code", err)
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Verification code sent to your phone",
	})
}

// VerifyPhone verifies a phone code and binds the phone to the account.
// POST /api/v1/account/me/verify-phone
func (h *RegistrationHandler) VerifyPhone(c *gin.Context) {
	accountID, ok := requireAccountID(c)
	if !ok {
		return
	}

	var req struct {
		Phone string `json:"phone" binding:"required"`
		Code  string `json:"code"  binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		handleBindError(c, err)
		return
	}

	if err := h.registration.VerifyAndBindPhone(c.Request.Context(), accountID, req.Phone, req.Code); err != nil {
		msg := err.Error()
		switch {
		case containsAny(msg, "invalid phone"):
			respondValidationError(c, "Invalid phone number", map[string]string{
				"phone": "Please enter a valid 11-digit phone number",
			})
		case containsAny(msg, "invalid verification", "no pending"):
			respondValidationError(c, "Verification failed", map[string]string{
				"code": "The verification code is incorrect or has expired",
			})
		default:
			respondInternalError(c, "registration.verify_phone", err)
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Phone number verified and linked to your account",
	})
}

// ── Helpers ─────────────────────────────────────────────────────────────────

// containsAny reports whether s contains any of the substrings.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
