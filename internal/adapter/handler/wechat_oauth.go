package handler

// WechatOAuthHandler exposes a standard OAuth2 authorization server interface
// that wraps WeChat's non-standard login flow via the WeChat proxy service.
//
// This enables Zitadel to treat WeChat as a Generic OAuth IDP.
// Configure in Zitadel Console → Identity Providers → Generic OAuth:
//
//   Authorization URL: https://identity.lurus.cn/oauth/wechat/authorize
//   Token URL:         https://identity.lurus.cn/oauth/wechat/token
//   User Info URL:     https://identity.lurus.cn/oauth/wechat/userinfo
//   Client ID:         lurus-wechat
//   Client Secret:     <WECHAT_OAUTH_CLIENT_SECRET>
//   Scopes:            snsapi_login
//   ID Attribute:      sub

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

const (
	wechatOAuthStateTTL = 10 * time.Minute
	wechatOAuthCodeTTL  = 5 * time.Minute
)

// WechatOAuthHandler bridges WeChat's proprietary OAuth flow into a standard OAuth2 interface.
type WechatOAuthHandler struct {
	wechatServerAddr  string
	wechatServerToken string
	oauthClientSecret string // shared secret between Zitadel IDP config and this server
	rdb               *redis.Client
}

// NewWechatOAuthHandler creates the handler.
// Returns nil when wechatServerAddr or oauthClientSecret is empty.
func NewWechatOAuthHandler(
	wechatServerAddr, wechatServerToken, oauthClientSecret string,
	rdb *redis.Client,
) *WechatOAuthHandler {
	if wechatServerAddr == "" || oauthClientSecret == "" {
		return nil
	}
	return &WechatOAuthHandler{
		wechatServerAddr:  wechatServerAddr,
		wechatServerToken: wechatServerToken,
		oauthClientSecret: oauthClientSecret,
		rdb:               rdb,
	}
}

// Authorize starts the WeChat OAuth flow.
// Zitadel redirects the browser here when the user clicks the WeChat IDP button.
//
// GET /oauth/wechat/authorize?client_id=...&redirect_uri=<zitadel_callback>&state=...&response_type=code
func (h *WechatOAuthHandler) Authorize(c *gin.Context) {
	redirectURI := c.Query("redirect_uri")
	state := c.Query("state")

	if redirectURI == "" || state == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "error_description": "redirect_uri and state are required"})
		return
	}

	// Persist state → redirectURI so the callback can reconstruct the response.
	stateKey := "wechat_oauth_state:" + state
	if err := h.rdb.Set(c.Request.Context(), stateKey, redirectURI, wechatOAuthStateTTL).Err(); err != nil {
		slog.Error("wechat oauth: failed to store state", "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}

	// Build our own callback URL for the WeChat proxy.
	scheme := "https"
	if proto := c.GetHeader("X-Forwarded-Proto"); proto == "http" {
		scheme = "http"
	} else if proto == "" && c.Request.TLS == nil {
		scheme = "http"
	}
	ourCallback := scheme + "://" + c.Request.Host + "/oauth/wechat/callback"

	// Redirect to WeChat QR page via the proxy.
	qrURL := fmt.Sprintf("%s/api/wechat/qrcode?redirect_uri=%s&state=%s",
		h.wechatServerAddr, ourCallback, state)
	c.Redirect(http.StatusFound, qrURL)
}

// Callback is called by the WeChat proxy after the user scans the QR code.
// Exchanges the WeChat code for an openid, then issues a short-lived authorization code
// and redirects back to Zitadel.
//
// GET /oauth/wechat/callback?code=<wx_code>&state=<original_state>
func (h *WechatOAuthHandler) Callback(c *gin.Context) {
	code := c.Query("code")
	state := c.Query("state")

	if code == "" || state == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}

	// Recover the original Zitadel redirect_uri from Redis.
	stateKey := "wechat_oauth_state:" + state
	redirectURI, err := h.rdb.Get(c.Request.Context(), stateKey).Result()
	if err != nil {
		slog.Warn("wechat oauth: unknown state", "state", state, "err", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "error_description": "unknown or expired state"})
		return
	}
	h.rdb.Del(c.Request.Context(), stateKey)

	// Exchange the WeChat code for an openid via the proxy.
	openid, err := h.fetchWechatOpenID(c.Request.Context(), code)
	if err != nil {
		slog.Error("wechat oauth: failed to fetch openid", "err", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "server_error", "error_description": "WeChat authentication failed"})
		return
	}

	// Issue a one-time authorization code that maps to the openid.
	authCode, err := generateOAuthCode()
	if err != nil {
		slog.Error("wechat oauth: failed to generate code", "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}
	codeKey := "wechat_oauth_code:" + authCode
	if err := h.rdb.Set(c.Request.Context(), codeKey, openid, wechatOAuthCodeTTL).Err(); err != nil {
		slog.Error("wechat oauth: failed to store code", "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}

	// Redirect back to Zitadel's IDP callback with our authorization code.
	separator := "?"
	if strings.Contains(redirectURI, "?") {
		separator = "&"
	}
	callbackURL := fmt.Sprintf("%s%scode=%s&state=%s", redirectURI, separator, authCode, state)
	c.Redirect(http.StatusFound, callbackURL)
}

// Token exchanges our short-lived authorization code for an "access token" (which encodes the openid).
// Called by Zitadel's backend when completing the IDP authorization code flow.
//
// POST /oauth/wechat/token
// Body (form-encoded): grant_type=authorization_code&code=<auth_code>&client_id=...&client_secret=...
func (h *WechatOAuthHandler) Token(c *gin.Context) {
	// Verify client credentials (client_secret in form or Basic Auth).
	clientSecret := c.PostForm("client_secret")
	if clientSecret == "" {
		if username, password, ok := c.Request.BasicAuth(); ok && username != "" {
			_ = username
			clientSecret = password
		}
	}
	if clientSecret != h.oauthClientSecret {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_client"})
		return
	}

	grantType := c.PostForm("grant_type")
	if grantType != "authorization_code" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported_grant_type"})
		return
	}

	authCode := c.PostForm("code")
	if authCode == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "error_description": "code is required"})
		return
	}

	// Exchange our auth code for the stored openid.
	codeKey := "wechat_oauth_code:" + authCode
	openid, err := h.rdb.Get(c.Request.Context(), codeKey).Result()
	if err != nil {
		slog.Warn("wechat oauth: unknown or expired code", "err", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_grant", "error_description": "unknown or expired code"})
		return
	}
	h.rdb.Del(c.Request.Context(), codeKey)

	// Return the openid as the access token.
	// Zitadel will use it to call /userinfo.
	c.JSON(http.StatusOK, gin.H{
		"access_token": openid,
		"token_type":   "bearer",
		"expires_in":   3600,
	})
}

// UserInfo returns the WeChat user's profile given the access token (openid).
// Called by Zitadel to map the external identity to a Zitadel user.
//
// GET /oauth/wechat/userinfo
// Header: Authorization: Bearer <openid>
func (h *WechatOAuthHandler) UserInfo(c *gin.Context) {
	authHeader := c.GetHeader("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_token"})
		return
	}
	openid := strings.TrimPrefix(authHeader, "Bearer ")
	if openid == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_token"})
		return
	}

	// Return a minimal OIDC-compatible user info payload.
	// Zitadel uses "sub" to uniquely identify the external account.
	suffix := openid
	if len(openid) > 8 {
		suffix = openid[len(openid)-8:]
	}
	c.JSON(http.StatusOK, gin.H{
		"sub":  openid,
		"name": "WeChat_" + suffix,
	})
}

// fetchWechatOpenID calls the WeChat proxy to exchange an OAuth code for an openid.
func (h *WechatOAuthHandler) fetchWechatOpenID(ctx context.Context, code string) (string, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	url := fmt.Sprintf("%s/api/wechat/user?code=%s", h.wechatServerAddr, code)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	if h.wechatServerToken != "" {
		req.Header.Set("Authorization", "Bearer "+h.wechatServerToken)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("call wechat proxy: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("wechat proxy returned %d: %s", resp.StatusCode, body)
	}

	var result struct {
		OpenID   string `json:"openid"`
		WechatID string `json:"wechat_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	id := result.OpenID
	if id == "" {
		id = result.WechatID
	}
	if id == "" {
		return "", fmt.Errorf("wechat proxy returned empty openid/wechat_id")
	}
	return id, nil
}

func generateOAuthCode() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("rand read: %w", err)
	}
	return hex.EncodeToString(b), nil
}
