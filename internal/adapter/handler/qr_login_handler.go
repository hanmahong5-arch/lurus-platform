package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/auth"
)

const (
	qrLoginKeyPrefix   = "qr_login:"
	qrLoginSessionTTL  = 5 * time.Minute
	qrLoginSessionBytes = 32 // 256-bit random session ID
	qrLoginPollMaxWait = 30 * time.Second
	qrLoginPollInterval = time.Second
	qrLoginTokenTTL    = 7 * 24 * time.Hour // session token validity
)

// qrLoginState represents the current state of a QR login session.
type qrLoginState struct {
	Status    string `json:"status"`               // pending | confirmed | consumed
	AccountID int64  `json:"account_id,omitempty"`
}

// QRLoginHandler handles QR-code-based login flow.
// Web client creates a session, APP confirms it, web client polls and receives a token.
type QRLoginHandler struct {
	rdb           *redis.Client
	sessionSecret string
}

// NewQRLoginHandler creates a new QR login handler.
func NewQRLoginHandler(rdb *redis.Client, sessionSecret string) *QRLoginHandler {
	return &QRLoginHandler{
		rdb:           rdb,
		sessionSecret: sessionSecret,
	}
}

// CreateSession generates a new QR login session.
// POST /api/v1/public/qr-login/session
func (h *QRLoginHandler) CreateSession(c *gin.Context) {
	buf := make([]byte, qrLoginSessionBytes)
	if _, err := rand.Read(buf); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "session_creation_failed",
			"message": "Failed to generate session ID, please retry",
		})
		return
	}
	sessionID := hex.EncodeToString(buf)

	state := qrLoginState{Status: "pending"}
	data, _ := json.Marshal(state)

	key := qrLoginKeyPrefix + sessionID
	if err := h.rdb.Set(c.Request.Context(), key, data, qrLoginSessionTTL).Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "session_creation_failed",
			"message": "Failed to store session, please retry",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"session_id": sessionID,
		"qr_url":     fmt.Sprintf("lurus://qr-login/%s", sessionID),
		"expires_in": int(qrLoginSessionTTL.Seconds()),
	})
}

// PollStatus polls the QR login session status (long-polling).
// GET /api/v1/public/qr-login/:id/status
func (h *QRLoginHandler) PollStatus(c *gin.Context) {
	sessionID := c.Param("id")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_request",
			"message": "Session ID is required",
		})
		return
	}

	timeout := qrLoginPollMaxWait
	if t := c.Query("timeout"); t != "" {
		if secs, err := strconv.Atoi(t); err == nil && secs > 0 && secs <= int(qrLoginPollMaxWait.Seconds()) {
			timeout = time.Duration(secs) * time.Second
		}
	}

	key := qrLoginKeyPrefix + sessionID
	ctx := c.Request.Context()
	deadline := time.Now().Add(timeout)

	for {
		state, err := h.getState(ctx, key)
		if err == redis.Nil {
			c.JSON(http.StatusNotFound, gin.H{
				"error":   "session_not_found",
				"message": "QR login session expired or does not exist",
			})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "poll_failed",
				"message": "Failed to check session status",
			})
			return
		}

		switch state.Status {
		case "confirmed":
			// Atomically transition confirmed → consumed to prevent double token issuance.
			consumed, err := h.tryConsume(ctx, key, state.AccountID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error":   "token_issue_failed",
					"message": "Failed to issue session token",
				})
				return
			}
			if !consumed {
				// Another poller consumed it first — treat as expired.
				c.JSON(http.StatusGone, gin.H{
					"error":   "session_consumed",
					"message": "Session has already been consumed",
				})
				return
			}

			token, err := auth.IssueSessionToken(state.AccountID, qrLoginTokenTTL, h.sessionSecret)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error":   "token_issue_failed",
					"message": "Failed to issue session token",
				})
				return
			}
			c.JSON(http.StatusOK, gin.H{
				"status": "confirmed",
				"token":  token,
			})
			return

		case "consumed":
			c.JSON(http.StatusGone, gin.H{
				"error":   "session_consumed",
				"message": "Session has already been consumed",
			})
			return
		}

		// Still pending — wait or return.
		if time.Now().After(deadline) {
			c.JSON(http.StatusOK, gin.H{"status": "pending"})
			return
		}

		select {
		case <-ctx.Done():
			c.JSON(http.StatusOK, gin.H{"status": "pending"})
			return
		case <-time.After(qrLoginPollInterval):
			// Continue polling.
		}
	}
}

// Confirm allows an authenticated APP user to confirm a QR login session.
// POST /api/v1/qr-login/:id/confirm
func (h *QRLoginHandler) Confirm(c *gin.Context) {
	accountID, ok := requireAccountID(c)
	if !ok {
		return
	}

	sessionID := c.Param("id")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_request",
			"message": "Session ID is required",
		})
		return
	}

	key := qrLoginKeyPrefix + sessionID
	ctx := c.Request.Context()

	state, err := h.getState(ctx, key)
	if err == redis.Nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "session_not_found",
			"message": "QR login session expired or does not exist",
		})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "confirm_failed",
			"message": "Failed to read session state",
		})
		return
	}

	if state.Status != "pending" {
		c.JSON(http.StatusConflict, gin.H{
			"error":   "invalid_state",
			"message": fmt.Sprintf("Session is already %s, cannot confirm", state.Status),
		})
		return
	}

	// Atomically set confirmed only if still pending (CAS via Lua script).
	confirmed, err := h.tryConfirm(ctx, key, accountID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "confirm_failed",
			"message": "Failed to update session state",
		})
		return
	}
	if !confirmed {
		c.JSON(http.StatusConflict, gin.H{
			"error":   "invalid_state",
			"message": "Session state changed, cannot confirm",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"confirmed": true})
}

// getState reads the QR login session state from Redis.
func (h *QRLoginHandler) getState(ctx context.Context, key string) (*qrLoginState, error) {
	data, err := h.rdb.Get(ctx, key).Bytes()
	if err != nil {
		return nil, err
	}
	var state qrLoginState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("qr_login: unmarshal state: %w", err)
	}
	return &state, nil
}

// tryConfirm atomically transitions pending → confirmed using a Lua script (CAS).
var confirmScript = redis.NewScript(`
local data = redis.call('GET', KEYS[1])
if not data then return 0 end
local state = cjson.decode(data)
if state.status ~= 'pending' then return 0 end
state.status = 'confirmed'
state.account_id = tonumber(ARGV[1])
local ttl = redis.call('TTL', KEYS[1])
if ttl > 0 then
	redis.call('SET', KEYS[1], cjson.encode(state), 'EX', ttl)
else
	redis.call('SET', KEYS[1], cjson.encode(state), 'EX', 300)
end
return 1
`)

func (h *QRLoginHandler) tryConfirm(ctx context.Context, key string, accountID int64) (bool, error) {
	result, err := confirmScript.Run(ctx, h.rdb, []string{key}, accountID).Int64()
	if err != nil {
		return false, fmt.Errorf("qr_login: confirm script: %w", err)
	}
	return result == 1, nil
}

// tryConsume atomically transitions confirmed → consumed using a Lua script (CAS).
var consumeScript = redis.NewScript(`
local data = redis.call('GET', KEYS[1])
if not data then return 0 end
local state = cjson.decode(data)
if state.status ~= 'confirmed' then return 0 end
state.status = 'consumed'
local ttl = redis.call('TTL', KEYS[1])
if ttl > 0 then
	redis.call('SET', KEYS[1], cjson.encode(state), 'EX', ttl)
else
	redis.call('SET', KEYS[1], cjson.encode(state), 'EX', 60)
end
return 1
`)

func (h *QRLoginHandler) tryConsume(ctx context.Context, key string, accountID int64) (bool, error) {
	result, err := consumeScript.Run(ctx, h.rdb, []string{key}).Int64()
	if err != nil {
		return false, fmt.Errorf("qr_login: consume script: %w", err)
	}
	return result == 1, nil
}
