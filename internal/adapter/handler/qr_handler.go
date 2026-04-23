package handler

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/auth"
)

// qr_handler implements the v2 multi-action QR primitive.
//
// Contract (all routes mounted under /api/v2/qr):
//   POST /session           unauthenticated — creates a session. Only action=login
//                           is accepted at this door; authorized actions (join_org,
//                           delegate) will live behind authenticated routes in a
//                           later phase.
//   GET  /:id/status        unauthenticated long-poll (timeout query param ≤30s).
//                           On confirm→consumed transition returns the action-specific
//                           result (for login: a session token).
//   POST /:id/confirm       JWT-authenticated — the APP user confirming the request.
//                           Action-specific side effects happen here.
//
// Redis key layout: qr:<id> → JSON(entity.QRSession), TTL defaultTTL.
// State transitions (pending → confirmed → consumed) are performed via Lua
// scripts so each mutation is atomic.

const (
	qrKeyPrefix    = "qr:"
	qrDefaultTTL   = 5 * time.Minute
	qrIDRandomBytes = 32 // 256 bits — unguessable within session lifetime
	qrMaxPollWait  = 30 * time.Second
	qrPollInterval = time.Second
	qrLoginTTL     = 7 * 24 * time.Hour // session token TTL after login consume
	qrPayloadHMACLen = 16 // hex chars kept from HMAC-SHA256 (64 bits — enough for anti-tamper)
	qrMaxUALength  = 256 // truncate user-agent so Redis value stays small
)

// QRHandler owns the v2 QR primitive.
type QRHandler struct {
	rdb           *redis.Client
	sessionSecret string      // signs payload HMAC and issued session tokens
	now           func() time.Time // injectable for deterministic tests
}

// NewQRHandler wires a handler. sessionSecret must be the same secret used by
// auth.IssueSessionToken (already validated ≥32 bytes at boot by Config.Validate).
func NewQRHandler(rdb *redis.Client, sessionSecret string) *QRHandler {
	return &QRHandler{
		rdb:           rdb,
		sessionSecret: sessionSecret,
		now:           time.Now,
	}
}

// ── Create ──────────────────────────────────────────────────────────────────

type qrCreateRequest struct {
	Action string          `json:"action" binding:"required"`
	Params json.RawMessage `json:"params,omitempty"`
}

// CreateSession — POST /api/v2/qr/session (unauthenticated)
//
// Only action=login is accepted here. Returning a distinct error for
// known-but-gated actions lets clients discover capability without guessing.
func (h *QRHandler) CreateSession(c *gin.Context) {
	var req qrCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_request",
			"message": "Body must be JSON with an 'action' field",
		})
		return
	}

	action := entity.QRAction(req.Action)
	if !entity.IsValidQRAction(action) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_action",
			"message": fmt.Sprintf("Unknown QR action %q (expected: login|join_org|delegate)", req.Action),
		})
		return
	}

	// Phase 1 gates non-login actions behind authenticated routes that don't
	// exist yet. Surface this explicitly rather than silently accepting.
	if action != entity.QRActionLogin {
		c.JSON(http.StatusNotImplemented, gin.H{
			"error":   "action_not_supported_yet",
			"message": fmt.Sprintf("Action %q is not yet wired on this endpoint", req.Action),
		})
		return
	}

	id, err := newQRID()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "id_generation_failed",
			"message": "Failed to generate QR session id",
		})
		return
	}

	session := entity.QRSession{
		ID:        id,
		Action:    action,
		Params:    req.Params,
		Status:    entity.QRStatusPending,
		CreatedAt: h.now().UTC(),
		IP:        clientIP(c),
		UA:        truncate(c.GetHeader("User-Agent"), qrMaxUALength),
	}

	data, _ := json.Marshal(session)
	if err := h.rdb.Set(c.Request.Context(), qrKey(id), data, qrDefaultTTL).Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "store_failed",
			"message": "Failed to persist QR session",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":          id,
		"action":      string(action),
		"qr_payload":  h.buildPayload(id, action),
		"expires_in":  int(qrDefaultTTL.Seconds()),
	})
}

// ── Status (long-poll) ──────────────────────────────────────────────────────

// PollStatus — GET /api/v2/qr/:id/status?timeout=<seconds> (unauthenticated)
//
// Pending → returns {"status":"pending"} when the poll window elapses.
// Confirmed → atomically transitions to consumed and returns action-specific
//             payload (login → {"status":"confirmed", "token": "..."}).
// Consumed → 410 Gone.
// Missing / TTL-expired → 404.
func (h *QRHandler) PollStatus(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_request",
			"message": "QR session id is required",
		})
		return
	}

	timeout := qrMaxPollWait
	if raw := c.Query("timeout"); raw != "" {
		if secs, err := strconv.Atoi(raw); err == nil && secs > 0 && secs <= int(qrMaxPollWait.Seconds()) {
			timeout = time.Duration(secs) * time.Second
		}
	}

	ctx := c.Request.Context()
	deadline := h.now().Add(timeout)
	key := qrKey(id)

	for {
		session, err := h.readSession(ctx, key)
		if err == redis.Nil {
			c.JSON(http.StatusNotFound, gin.H{
				"error":   "session_not_found",
				"message": "QR session expired or does not exist",
			})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "read_failed",
				"message": "Failed to read QR session",
			})
			return
		}

		switch session.Status {
		case entity.QRStatusConfirmed:
			consumed, err := h.tryConsume(ctx, key)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error":   "consume_failed",
					"message": "Failed to consume QR session",
				})
				return
			}
			if !consumed {
				// Another poller beat us to the transition — treat as consumed.
				c.JSON(http.StatusGone, gin.H{
					"error":   "session_consumed",
					"message": "Session has already been consumed",
				})
				return
			}
			h.writeConfirmResult(c, session)
			return

		case entity.QRStatusConsumed:
			c.JSON(http.StatusGone, gin.H{
				"error":   "session_consumed",
				"message": "Session has already been consumed",
			})
			return
		}

		// Still pending — honour the poll deadline or ctx cancel.
		if !h.now().Before(deadline) {
			c.JSON(http.StatusOK, gin.H{"status": string(entity.QRStatusPending)})
			return
		}
		select {
		case <-ctx.Done():
			c.JSON(http.StatusOK, gin.H{"status": string(entity.QRStatusPending)})
			return
		case <-time.After(qrPollInterval):
		}
	}
}

// writeConfirmResult dispatches per-action confirm-time payload.
func (h *QRHandler) writeConfirmResult(c *gin.Context, s *entity.QRSession) {
	switch s.Action {
	case entity.QRActionLogin:
		token, err := auth.IssueSessionToken(s.AccountID, qrLoginTTL, h.sessionSecret)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "token_issue_failed",
				"message": "Failed to issue session token",
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"status":     string(entity.QRStatusConfirmed),
			"action":     string(s.Action),
			"token":      token,
			"expires_in": int(qrLoginTTL.Seconds()),
		})

	default:
		// Reached only if an action is added to entity.IsValidQRAction but not
		// plumbed here. Fail loudly rather than returning an empty success body.
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "action_unsupported",
			"message": fmt.Sprintf("Action %q has no confirm handler", s.Action),
		})
	}
}

// ── Confirm ─────────────────────────────────────────────────────────────────

type qrConfirmRequest struct {
	// Sig is the payload HMAC returned to the APP in the QR payload.
	// Requiring it on confirm ensures scanned payloads cannot be substituted
	// for a fabricated id by a malicious app.
	Sig string `json:"sig" binding:"required"`
}

// Confirm — POST /api/v2/qr/:id/confirm (auth required)
//
// The APP user confirms the pending session. Requires the HMAC signature
// from the scanned payload; if the HMAC doesn't match id+action, the call
// is rejected to prevent blind-confirm attacks where a malicious app tricks
// the user into confirming an attacker-chosen id.
func (h *QRHandler) Confirm(c *gin.Context) {
	accountID, ok := requireAccountID(c)
	if !ok {
		return
	}

	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_request",
			"message": "QR session id is required",
		})
		return
	}

	var req qrConfirmRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_request",
			"message": "Body must include 'sig' from the scanned payload",
		})
		return
	}

	ctx := c.Request.Context()
	key := qrKey(id)

	session, err := h.readSession(ctx, key)
	if err == redis.Nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "session_not_found",
			"message": "QR session expired or does not exist",
		})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "read_failed",
			"message": "Failed to read QR session",
		})
		return
	}

	if !h.verifyPayloadSig(id, session.Action, req.Sig) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_signature",
			"message": "QR payload signature did not match",
		})
		return
	}

	if session.Status != entity.QRStatusPending {
		c.JSON(http.StatusConflict, gin.H{
			"error":   "invalid_state",
			"message": fmt.Sprintf("Session is %s, cannot confirm", session.Status),
		})
		return
	}

	confirmed, err := h.tryConfirm(ctx, key, accountID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "confirm_failed",
			"message": "Failed to update session state",
		})
		return
	}
	if !confirmed {
		// Lost CAS race — someone else confirmed first.
		c.JSON(http.StatusConflict, gin.H{
			"error":   "invalid_state",
			"message": "Session state changed, cannot confirm",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"confirmed": true,
		"action":    string(session.Action),
	})
}

// ── Helpers: id / payload / HMAC / IP ──────────────────────────────────────

func newQRID() (string, error) {
	buf := make([]byte, qrIDRandomBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func qrKey(id string) string { return qrKeyPrefix + id }

// buildPayload assembles the scannable payload handed back to the client.
//
// Format: lurus://qr?v=1&id=<hex>&a=<action>&h=<hmac16>
// Version prefix lets us evolve the payload shape without flag days.
func (h *QRHandler) buildPayload(id string, action entity.QRAction) string {
	sig := h.payloadSig(id, action)
	return fmt.Sprintf("lurus://qr?v=1&id=%s&a=%s&h=%s", id, action, sig)
}

func (h *QRHandler) payloadSig(id string, action entity.QRAction) string {
	mac := hmac.New(sha256.New, []byte(h.sessionSecret))
	mac.Write([]byte(id))
	mac.Write([]byte{0})
	mac.Write([]byte(action))
	return hex.EncodeToString(mac.Sum(nil))[:qrPayloadHMACLen]
}

func (h *QRHandler) verifyPayloadSig(id string, action entity.QRAction, got string) bool {
	want := h.payloadSig(id, action)
	// Constant-time compare — id/action are known to the attacker but we
	// shouldn't let timing leak whether an incorrect candidate was close.
	return hmac.Equal([]byte(want), []byte(got))
}

func clientIP(c *gin.Context) string {
	if ip := c.GetHeader("X-Forwarded-For"); ip != "" {
		return ip
	}
	return c.ClientIP()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// ── Redis I/O ──────────────────────────────────────────────────────────────

func (h *QRHandler) readSession(ctx context.Context, key string) (*entity.QRSession, error) {
	data, err := h.rdb.Get(ctx, key).Bytes()
	if err != nil {
		return nil, err
	}
	var s entity.QRSession
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("qr: unmarshal session: %w", err)
	}
	return &s, nil
}

// tryConfirm atomically flips pending → confirmed + stores the confirming
// account's id. Preserves whatever TTL the key currently has so the window
// for the eventual consume step isn't accidentally extended.
var qrConfirmScript = redis.NewScript(`
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

func (h *QRHandler) tryConfirm(ctx context.Context, key string, accountID int64) (bool, error) {
	res, err := qrConfirmScript.Run(ctx, h.rdb, []string{key}, accountID).Int64()
	if err != nil {
		return false, fmt.Errorf("qr: confirm script: %w", err)
	}
	return res == 1, nil
}

// tryConsume flips confirmed → consumed atomically and shortens TTL so the
// record is cleaned up quickly after token delivery.
var qrConsumeScript = redis.NewScript(`
local data = redis.call('GET', KEYS[1])
if not data then return 0 end
local state = cjson.decode(data)
if state.status ~= 'confirmed' then return 0 end
state.status = 'consumed'
redis.call('SET', KEYS[1], cjson.encode(state), 'EX', 60)
return 1
`)

func (h *QRHandler) tryConsume(ctx context.Context, key string) (bool, error) {
	res, err := qrConsumeScript.Run(ctx, h.rdb, []string{key}).Int64()
	if err != nil {
		return false, fmt.Errorf("qr: consume script: %w", err)
	}
	return res == 1, nil
}
