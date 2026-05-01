package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hanmahong5-arch/lurus-platform/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/auth"
)

const wechatSessionTTL = 7 * 24 * time.Hour

// WechatAuthHandler handles the WeChat OAuth login flow via a WeChat proxy service.
type WechatAuthHandler struct {
	accountSvc        *app.AccountService
	wechatServerAddr  string // base URL of the WeChat proxy, e.g. http://wechat-proxy.svc:8080
	wechatServerToken string // bearer token for the WeChat proxy
	sessionSecret     string // HS256 key for lurus session tokens
}

// NewWechatAuthHandler creates the handler.
// Returns nil when wechatServerAddr or sessionSecret is empty (WeChat login disabled).
func NewWechatAuthHandler(
	accountSvc *app.AccountService,
	wechatServerAddr, wechatServerToken, sessionSecret string,
) *WechatAuthHandler {
	if wechatServerAddr == "" || sessionSecret == "" {
		return nil
	}
	return &WechatAuthHandler{
		accountSvc:        accountSvc,
		wechatServerAddr:  wechatServerAddr,
		wechatServerToken: wechatServerToken,
		sessionSecret:     sessionSecret,
	}
}

// Initiate redirects the browser to the WeChat QR scan page on the proxy.
// GET /api/v1/auth/wechat
func (h *WechatAuthHandler) Initiate(c *gin.Context) {
	state, err := generateWxState()
	if err != nil {
		respondInternalError(c, "wechat.initiate.state", err)
		return
	}

	// Store state in an HttpOnly cookie for CSRF protection (TTL: 10 min).
	c.SetCookie("wx_state", state, 600, "/", "", false, true)

	// Build the callback URL from the inbound request host + forwarded proto.
	scheme := "https"
	if proto := c.GetHeader("X-Forwarded-Proto"); proto == "http" {
		scheme = "http"
	} else if proto == "" && c.Request.TLS == nil {
		scheme = "http"
	}
	callbackURL := scheme + "://" + c.Request.Host + "/api/v1/auth/wechat/callback"

	qrURL := fmt.Sprintf("%s/api/wechat/qrcode?redirect_uri=%s&state=%s",
		h.wechatServerAddr, callbackURL, state)
	c.Redirect(http.StatusFound, qrURL)
}

// Callback handles the OAuth callback from the WeChat proxy.
// GET /api/v1/auth/wechat/callback?code=<code>&state=<state>
func (h *WechatAuthHandler) Callback(c *gin.Context) {
	code := c.Query("code")
	state := c.Query("state")

	// CSRF: validate state matches the cookie set in Initiate.
	cookieState, err := c.Cookie("wx_state")
	if err != nil || cookieState == "" || cookieState != state {
		respondError(c, http.StatusBadRequest, ErrCodeInvalidRequest,
			"Invalid state (CSRF check failed)")
		return
	}
	// Clear the one-time CSRF cookie.
	c.SetCookie("wx_state", "", -1, "/", "", false, true)

	if code == "" {
		respondError(c, http.StatusBadRequest, ErrCodeInvalidParameter,
			"missing code parameter")
		return
	}

	// Exchange code for WeChat OpenID via the proxy service.
	wechatID, err := h.fetchWechatUser(c.Request.Context(), code)
	if err != nil {
		slog.Error("wechat user fetch failed", "err", err)
		respondError(c, http.StatusBadGateway, ErrCodeUpstreamFailed,
			"WeChat authentication failed")
		return
	}

	// Find or create the lurus account for this WeChat user.
	account, err := h.accountSvc.UpsertByWechat(c.Request.Context(), wechatID)
	if err != nil {
		respondInternalError(c, "wechat.callback.upsert", err)
		return
	}

	// Issue a lurus session token (HS256 JWT) for subsequent API calls.
	token, err := auth.IssueSessionToken(account.ID, wechatSessionTTL, h.sessionSecret)
	if err != nil {
		respondInternalError(c, "wechat.callback.token_issue", err)
		return
	}

	// Redirect the frontend to /callback with the lurus token in the query string.
	// The frontend CallbackPage will detect lurus_token and store it in localStorage.
	scheme := "https"
	if proto := c.GetHeader("X-Forwarded-Proto"); proto == "http" {
		scheme = "http"
	} else if proto == "" && c.Request.TLS == nil {
		scheme = "http"
	}
	frontendURL := scheme + "://" + c.Request.Host + "/callback?lurus_token=" + token
	c.Redirect(http.StatusFound, frontendURL)
}

// fetchWechatUser calls the WeChat proxy to exchange the OAuth code for a user OpenID.
func (h *WechatAuthHandler) fetchWechatUser(ctx context.Context, code string) (string, error) {
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

	// The proxy may return either openid or wechat_id field.
	var result struct {
		OpenID   string `json:"openid"`
		WechatID string `json:"wechat_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode wechat response: %w", err)
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

func generateWxState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
