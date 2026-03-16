package handler

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hanmahong5-arch/lurus-platform/internal/app"
)

// RegistrationHandler handles user registration and password reset endpoints.
type RegistrationHandler struct {
	registration *app.RegistrationService
}

// NewRegistrationHandler creates the handler. Returns nil if service is nil.
func NewRegistrationHandler(registration *app.RegistrationService) *RegistrationHandler {
	if registration == nil {
		return nil
	}
	return &RegistrationHandler{registration: registration}
}

// Register creates a new user account.
// POST /api/v1/auth/register
func (h *RegistrationHandler) Register(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
		Email    string `json:"email"`
		Phone    string `json:"phone"`
		AffCode  string `json:"aff_code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
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
		switch {
		case contains(err.Error(), "username is required"),
			contains(err.Error(), "username must be"):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		case contains(err.Error(), "invalid email"):
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid email format"})
		case contains(err.Error(), "invalid phone"):
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid phone number format"})
		case contains(err.Error(), "password must be at least"):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		case contains(err.Error(), "already taken"),
			contains(err.Error(), "already registered"),
			contains(err.Error(), "already exists in Zitadel"):
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		default:
			slog.Error("registration failed", "err", err, "username", req.Username)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "registration failed"})
		}
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"token":      result.Token,
		"account_id": result.AccountID,
		"lurus_id":   result.LurusID,
	})
}

// ForgotPassword initiates a password reset flow.
// POST /api/v1/auth/forgot-password
func (h *RegistrationHandler) ForgotPassword(c *gin.Context) {
	var req struct {
		Identifier string `json:"identifier" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	result, err := h.registration.ForgotPassword(c.Request.Context(), req.Identifier)
	if err != nil {
		// Always return 200 to prevent enumeration.
		c.JSON(http.StatusOK, gin.H{
			"message": "if the account exists, a reset code has been sent",
			"channel": "email",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": result.Message,
		"channel": result.Channel,
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	if err := h.registration.ResetPassword(c.Request.Context(), req.Identifier, req.Code, req.NewPassword); err != nil {
		switch {
		case contains(err.Error(), "password must be at least"):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		case contains(err.Error(), "no pending reset"),
			contains(err.Error(), "expired"),
			contains(err.Error(), "invalid verification"):
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid or expired reset code"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "password reset failed"})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "password reset successful"})
}

// SendSMSCode sends an SMS code for password reset (unauthenticated).
// POST /api/v1/auth/send-sms
func (h *RegistrationHandler) SendSMSCode(c *gin.Context) {
	var req struct {
		Identifier string `json:"identifier" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	// Reuse ForgotPassword which already handles SMS channel.
	result, _ := h.registration.ForgotPassword(c.Request.Context(), req.Identifier)
	if result != nil {
		c.JSON(http.StatusOK, gin.H{"message": result.Message, "channel": result.Channel})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "if the account exists, a reset code has been sent"})
}

// SendPhoneCode sends a phone verification code (authenticated, for binding phone).
// POST /api/v1/account/me/send-phone-code
func (h *RegistrationHandler) SendPhoneCode(c *gin.Context) {
	accountID, exists := c.Get("account_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	var req struct {
		Phone string `json:"phone" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	if err := h.registration.SendPhoneVerificationCode(c.Request.Context(), accountID.(int64), req.Phone); err != nil {
		switch {
		case contains(err.Error(), "invalid phone"):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		case contains(err.Error(), "already registered"):
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to send verification code"})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "verification code sent"})
}

// VerifyPhone verifies a phone code and binds the phone to the account.
// POST /api/v1/account/me/verify-phone
func (h *RegistrationHandler) VerifyPhone(c *gin.Context) {
	accountID, exists := c.Get("account_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	var req struct {
		Phone string `json:"phone" binding:"required"`
		Code  string `json:"code"  binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	if err := h.registration.VerifyAndBindPhone(c.Request.Context(), accountID.(int64), req.Phone, req.Code); err != nil {
		switch {
		case contains(err.Error(), "invalid phone"),
			contains(err.Error(), "invalid verification"),
			contains(err.Error(), "no pending"):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "phone verification failed"})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "phone verified and bound successfully"})
}

// contains is a simple helper to avoid importing strings just for error matching.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
