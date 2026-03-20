package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hanmahong5-arch/lurus-platform/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/auth"
)

// ZLoginHandler proxies Zitadel Session API v2 requests for the custom OIDC login UI.
// Required config: ZITADEL_ISSUER + ZITADEL_SERVICE_ACCOUNT_PAT + SESSION_SECRET.
type ZLoginHandler struct {
	accounts          *app.AccountService
	accountStore      accountStoreForLogin
	zitadelIssuer     string // e.g. https://auth.lurus.cn
	serviceAccountPAT string // Zitadel PAT with session creation rights
	sessionSecret     string // for validating lurus-issued session tokens
}

// accountStoreForLogin is a minimal interface for resolving login identifiers.
type accountStoreForLogin interface {
	GetByEmail(ctx context.Context, email string) (*entity.Account, error)
	GetByPhone(ctx context.Context, phone string) (*entity.Account, error)
	GetByUsername(ctx context.Context, username string) (*entity.Account, error)
}

// NewZLoginHandler creates the handler.
// Returns nil when serviceAccountPAT is empty (custom login disabled).
func NewZLoginHandler(
	accounts *app.AccountService,
	accountStore accountStoreForLogin,
	zitadelIssuer, serviceAccountPAT, sessionSecret string,
) *ZLoginHandler {
	if serviceAccountPAT == "" {
		return nil
	}
	return &ZLoginHandler{
		accounts:          accounts,
		accountStore:      accountStore,
		zitadelIssuer:     zitadelIssuer,
		serviceAccountPAT: serviceAccountPAT,
		sessionSecret:     sessionSecret,
	}
}

// GetAuthInfo returns metadata about an OIDC auth request (e.g. requesting app name).
// GET /api/v1/auth/info?authRequestId=<id>
func (h *ZLoginHandler) GetAuthInfo(c *gin.Context) {
	authRequestID := c.Query("authRequestId")
	if authRequestID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "authRequestId is required"})
		return
	}

	reqCtx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	url := fmt.Sprintf("%s/v2/oidc/auth_requests/%s", h.zitadelIssuer, authRequestID)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to build request"})
		return
	}
	req.Header.Set("Authorization", "Bearer "+h.serviceAccountPAT)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// Non-fatal: return minimal info on upstream failure.
		slog.Warn("zlogin: fetch auth request info failed", "err", err)
		c.JSON(http.StatusOK, gin.H{"app_name": "Lurus"})
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var info map[string]any
	if err := json.Unmarshal(body, &info); err != nil {
		c.JSON(http.StatusOK, gin.H{"app_name": "Lurus"})
		return
	}
	c.JSON(http.StatusOK, info)
}

// SubmitPassword creates a Zitadel session using email + password and returns the OIDC callback URL.
// POST /api/v1/auth/zlogin/password
func (h *ZLoginHandler) SubmitPassword(c *gin.Context) {
	var req struct {
		AuthRequestID string `json:"auth_request_id" binding:"required"`
		Username      string `json:"username"         binding:"required"`
		Password      string `json:"password"         binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		handleBindError(c, err)
		return
	}

	sessionID, sessionToken, err := h.createSessionByCredentials(c.Request.Context(), req.Username, req.Password)
	if err != nil {
		slog.Warn("zlogin: password session creation failed", "err", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	callbackURL, err := h.createCallback(c.Request.Context(), req.AuthRequestID, sessionID, sessionToken)
	if err != nil {
		slog.Error("zlogin: OIDC callback creation failed", "auth_request_id", req.AuthRequestID, "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to complete OIDC flow"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"callback_url": callbackURL})
}

// LinkWechatAndComplete bridges a lurus WeChat session to an active Zitadel OIDC auth request.
// POST /api/v1/auth/wechat/link-oidc
func (h *ZLoginHandler) LinkWechatAndComplete(c *gin.Context) {
	var req struct {
		AuthRequestID string `json:"auth_request_id" binding:"required"`
		LurusToken    string `json:"lurus_token"      binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		handleBindError(c, err)
		return
	}

	// Validate lurus session token and extract account ID.
	if h.sessionSecret == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "session tokens not configured"})
		return
	}
	accountID, err := auth.ValidateSessionToken(req.LurusToken, h.sessionSecret)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid lurus token"})
		return
	}

	// Fetch account to obtain its Zitadel user ID (sub).
	account, err := h.accounts.GetByID(c.Request.Context(), accountID)
	if err != nil || account == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
		return
	}
	if account.ZitadelSub == "" {
		// WeChat-only account with no Zitadel binding cannot complete OIDC.
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error": "this account has no Zitadel binding — please login with email first",
		})
		return
	}

	// Create a Zitadel session for the account's Zitadel user ID (no password check needed).
	sessionID, sessionToken, err := h.createSessionByUserID(c.Request.Context(), account.ZitadelSub)
	if err != nil {
		slog.Error("zlogin: wechat session creation failed", "account_id", accountID, "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create Zitadel session"})
		return
	}

	callbackURL, err := h.createCallback(c.Request.Context(), req.AuthRequestID, sessionID, sessionToken)
	if err != nil {
		slog.Error("zlogin: OIDC callback creation failed", "auth_request_id", req.AuthRequestID, "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to complete OIDC flow"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"callback_url": callbackURL})
}

// DirectLogin authenticates with identifier (username/email/phone) + password and returns a lurus session token.
// POST /api/v1/auth/login  (no OIDC — stays entirely within identity.lurus.cn)
func (h *ZLoginHandler) DirectLogin(c *gin.Context) {
	var req struct {
		Identifier string `json:"identifier"`
		Username   string `json:"username"` // backward compatibility
		Password   string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		handleBindError(c, err)
		return
	}

	// Support both "identifier" and legacy "username" field.
	identifier := req.Identifier
	if identifier == "" {
		identifier = req.Username
	}
	if identifier == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "identifier is required"})
		return
	}

	if h.sessionSecret == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "session tokens not configured"})
		return
	}

	// Resolve identifier to Zitadel loginName.
	loginName, err := h.resolveLoginName(c.Request.Context(), identifier)
	if err != nil {
		slog.Warn("direct-login: resolve login name failed", "identifier", identifier, "err", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	// Step 1: Verify credentials via Zitadel Session API.
	sessionID, _, err := h.createSessionByCredentials(c.Request.Context(), loginName, req.Password)
	if err != nil {
		slog.Warn("direct-login: credential check failed", "err", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	// Step 2: Fetch user info from the Zitadel session.
	userID, loginName, displayName, err := h.getSessionUser(c.Request.Context(), sessionID)
	if err != nil {
		slog.Error("direct-login: fetch session user failed", "session_id", sessionID, "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to retrieve user info"})
		return
	}

	// Step 3: Upsert lurus account (create if first login).
	account, err := h.accounts.UpsertByZitadelSub(c.Request.Context(), userID, loginName, displayName, "")
	if err != nil {
		slog.Error("direct-login: upsert account failed", "user_id", userID, "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to provision account"})
		return
	}

	// Step 4: Issue a lurus session token (HS256 JWT, 7-day TTL).
	const tokenTTL = 7 * 24 * time.Hour
	token, err := auth.IssueSessionToken(account.ID, tokenTTL, h.sessionSecret)
	if err != nil {
		slog.Error("direct-login: token issuance failed", "account_id", account.ID, "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to issue session token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token":      token,
		"account_id": account.ID,
	})
}

// getSessionUser fetches user factors from an existing Zitadel session.
// Returns (userId, loginName, displayName, error).
func (h *ZLoginHandler) getSessionUser(ctx context.Context, sessionID string) (string, string, string, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	url := fmt.Sprintf("%s/v2/sessions/%s", h.zitadelIssuer, sessionID)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return "", "", "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+h.serviceAccountPAT)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", "", fmt.Errorf("call Zitadel Sessions API: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if resp.StatusCode != http.StatusOK {
		return "", "", "", fmt.Errorf("Zitadel Sessions API returned %d: %s", resp.StatusCode, respBody)
	}

	var result struct {
		Session struct {
			Factors struct {
				User struct {
					ID          string `json:"id"`
					LoginName   string `json:"loginName"`
					DisplayName string `json:"displayName"`
				} `json:"user"`
			} `json:"factors"`
		} `json:"session"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", "", "", fmt.Errorf("decode response: %w", err)
	}

	user := result.Session.Factors.User
	if user.ID == "" {
		return "", "", "", fmt.Errorf("Zitadel session has no user factor (body: %s)", respBody)
	}
	return user.ID, user.LoginName, user.DisplayName, nil
}

// resolveLoginName resolves an identifier (email/phone/username) to the Zitadel loginName.
// For old users, Zitadel loginName is their email. For new users, it's their username.
func (h *ZLoginHandler) resolveLoginName(ctx context.Context, identifier string) (string, error) {
	identifier = strings.TrimSpace(identifier)

	// If it looks like an email, try email lookup first.
	if strings.Contains(identifier, "@") {
		acc, err := h.accountStore.GetByEmail(ctx, identifier)
		if err != nil {
			return "", fmt.Errorf("lookup by email: %w", err)
		}
		if acc != nil {
			// Old users have Zitadel loginName = email; new users have loginName = username.
			// Use username if set, otherwise fall back to email (for pre-migration accounts).
			if acc.Username != "" {
				return acc.Username, nil
			}
			return acc.Email, nil
		}
		// Not found locally — try using the identifier directly as Zitadel loginName.
		return identifier, nil
	}

	// If it looks like a phone number, try phone lookup.
	if entity.IsPhoneNumber(identifier) {
		acc, err := h.accountStore.GetByPhone(ctx, identifier)
		if err != nil {
			return "", fmt.Errorf("lookup by phone: %w", err)
		}
		if acc != nil && acc.Username != "" {
			return acc.Username, nil
		}
	}

	// Try username lookup.
	acc, err := h.accountStore.GetByUsername(ctx, identifier)
	if err != nil {
		return "", fmt.Errorf("lookup by username: %w", err)
	}
	if acc != nil {
		return acc.Username, nil
	}

	// Fall through: use identifier directly (let Zitadel reject if invalid).
	return identifier, nil
}

// --- Internal helpers ---

// createSessionByCredentials calls POST /v2/sessions with user loginName + password.
func (h *ZLoginHandler) createSessionByCredentials(ctx context.Context, loginName, password string) (string, string, error) {
	body, _ := json.Marshal(map[string]any{
		"checks": map[string]any{
			"user":     map[string]any{"loginName": loginName},
			"password": map[string]any{"password": password},
		},
	})
	return h.postSession(ctx, body)
}

// createSessionByUserID calls POST /v2/sessions with a Zitadel user ID (no password check).
// Used for WeChat users whose Zitadel identity is already established.
func (h *ZLoginHandler) createSessionByUserID(ctx context.Context, userID string) (string, string, error) {
	body, _ := json.Marshal(map[string]any{
		"checks": map[string]any{
			"user": map[string]any{"userId": userID},
		},
	})
	return h.postSession(ctx, body)
}

// postSession performs POST /v2/sessions and returns (sessionId, sessionToken, error).
func (h *ZLoginHandler) postSession(ctx context.Context, body []byte) (string, string, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost,
		h.zitadelIssuer+"/v2/sessions",
		bytes.NewReader(body),
	)
	if err != nil {
		return "", "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+h.serviceAccountPAT)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("call Zitadel Sessions API: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("Zitadel Sessions API returned %d: %s", resp.StatusCode, respBody)
	}

	// Zitadel v2 may return session data nested or at top level.
	var result struct {
		Session struct {
			SessionID    string `json:"sessionId"`
			SessionToken string `json:"sessionToken"`
		} `json:"session"`
		SessionID    string `json:"sessionId"`
		SessionToken string `json:"sessionToken"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", "", fmt.Errorf("decode response: %w", err)
	}

	sessionID := result.Session.SessionID
	if sessionID == "" {
		sessionID = result.SessionID
	}
	sessionToken := result.Session.SessionToken
	if sessionToken == "" {
		sessionToken = result.SessionToken
	}
	if sessionID == "" {
		return "", "", fmt.Errorf("Zitadel returned empty sessionId (body: %s)", respBody)
	}
	return sessionID, sessionToken, nil
}

// createCallback calls POST /v2/oidc/auth_requests/{id}/create_callback and returns the redirect URL.
func (h *ZLoginHandler) createCallback(ctx context.Context, authRequestID, sessionID, sessionToken string) (string, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	body, _ := json.Marshal(map[string]any{
		"session": map[string]any{
			"sessionId":    sessionID,
			"sessionToken": sessionToken,
		},
	})

	url := fmt.Sprintf("%s/v2/oidc/auth_requests/%s/create_callback", h.zitadelIssuer, authRequestID)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+h.serviceAccountPAT)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("call Zitadel OIDC callback API: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Zitadel OIDC callback API returned %d: %s", resp.StatusCode, respBody)
	}

	var result struct {
		CallbackURL string `json:"callbackUrl"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if result.CallbackURL == "" {
		return "", fmt.Errorf("Zitadel returned empty callbackUrl (body: %s)", respBody)
	}
	return result.CallbackURL, nil
}
