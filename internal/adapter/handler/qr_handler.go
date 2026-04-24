package handler

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/auth"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/event"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/metrics"
)

// QROrgService is the subset of OrganizationService required by the QR primitive.
// Declared here (not in app/) to keep the handler free of transport-↔-usecase
// cycles and to make mocking trivial in tests.
type QROrgService interface {
	IsOwnerOrAdmin(ctx context.Context, orgID, callerID int64) (bool, error)
	AddMember(ctx context.Context, orgID, callerID, targetAccountID int64, role string) error
}

// QREventPublisher is the minimal NATS publish surface used for member-join events.
// Accepts nil at wiring time — publish failures never block the user-visible path.
type QREventPublisher interface {
	Publish(ctx context.Context, ev *event.IdentityEvent) error
}

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
	qrKeyPrefix      = "qr:"
	qrDefaultTTL     = 5 * time.Minute
	qrIDRandomBytes  = 32 // 256 bits — unguessable within session lifetime
	qrMaxPollWait    = 30 * time.Second
	qrPollInterval   = time.Second
	qrLoginTTL       = 7 * 24 * time.Hour // session token TTL after login consume
	qrPayloadHMACLen = 24                 // hex chars kept from HMAC-SHA256 (96 bits — well beyond rate-limit-bounded brute force)
	// qrLegacyHMACLen is the pre-B5 truncation length. Pinned to 16 hex chars
	// (64 bits) because APPs built before 2026-04-24 shipped QRs signed at
	// that length; widening qrPayloadHMACLen above did not change their sigs.
	// Retained only until the 2026-06-01 legacy removal window.
	qrLegacyHMACLen = 16
	// qrHMACDomainSeparator is prepended to the HMAC input so a master secret
	// that also signs JWTs cannot be confused across purposes. Zero bytes are
	// already used between fields (id\0action\0t); the leading 0x01 marks
	// "QR v2 payload" specifically.
	qrHMACDomainSeparator = 0x01
	qrMaxUALength         = 256 // truncate user-agent so Redis value stays small
	// qrTimestampSkew tolerates small client/server clock drift when validating
	// the issued-at (t) parameter. TTL + skew is the full replay window.
	qrTimestampSkew = 30 * time.Second
	// qrEventPublishTimeout bounds the side-channel NATS publish on confirm so
	// a slow/unavailable broker cannot wedge the user-visible confirm response.
	qrEventPublishTimeout = 2 * time.Second
)

// qrKeyring holds one or more HMAC secrets for QR payload signing. Signing
// always uses the highest-kid key; verification accepts ANY key in the ring,
// which lets operators rotate without dropping in-flight sessions: add a new
// key (it becomes the signer), wait ≥TTL (5 min by default) for any QRs
// signed with the old key to drain, then remove the old key from the ring.
type qrKeyring struct {
	keys []qrKeyEntry // ordered by kid ascending; last entry is current
}

type qrKeyEntry struct {
	kid    string
	secret []byte
}

// newQRKeyring builds a keyring. When `keysSpec` is non-empty it is parsed as
// `kid:hex32[,kid:hex32...]`. Otherwise the function falls back to a single
// key with kid "default" derived from `fallback`, preserving the single-secret
// deployment mode. Returns an error only on malformed spec; empty fallback
// with empty spec yields an empty ring (caller is responsible for rejecting).
func newQRKeyring(keysSpec, fallback string) (*qrKeyring, error) {
	kr := &qrKeyring{}
	if s := strings.TrimSpace(keysSpec); s != "" {
		for _, pair := range strings.Split(s, ",") {
			pair = strings.TrimSpace(pair)
			if pair == "" {
				continue
			}
			parts := strings.SplitN(pair, ":", 2)
			if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
				return nil, fmt.Errorf("qr keyring: malformed entry %q (want kid:secret)", pair)
			}
			kr.keys = append(kr.keys, qrKeyEntry{kid: parts[0], secret: []byte(parts[1])})
		}
		sort.Slice(kr.keys, func(i, j int) bool { return kr.keys[i].kid < kr.keys[j].kid })
		return kr, nil
	}
	if fallback != "" {
		kr.keys = append(kr.keys, qrKeyEntry{kid: "default", secret: []byte(fallback)})
	}
	return kr, nil
}

// current returns the active signing key (highest kid). Never called when the
// ring is empty because handler construction validates non-empty.
func (k *qrKeyring) current() qrKeyEntry { return k.keys[len(k.keys)-1] }

// QRHandler owns the v2 QR primitive.
type QRHandler struct {
	rdb           *redis.Client
	keyring       *qrKeyring
	sessionSecret string           // retained for auth.IssueSessionToken (login action)
	now           func() time.Time // injectable for deterministic tests
	// orgService is required for action=join_org (both create and confirm).
	// nil when wiring has not yet set it — those code paths error out at 501
	// instead of panicking. login does not depend on it.
	orgService QROrgService
	// publisher is best-effort: join_org confirm emits identity.org.member_joined
	// to IDENTITY_EVENTS. nil = publish skipped (never blocks the confirm).
	publisher QREventPublisher
}

// NewQRHandler wires a handler. sessionSecret signs login-action JWTs and
// also feeds the QR signing keyring as a single-key fallback when
// QR_SIGNING_KEYS is unset.
func NewQRHandler(rdb *redis.Client, sessionSecret string) *QRHandler {
	return NewQRHandlerWithKeyring(rdb, sessionSecret, "")
}

// NewQRHandlerWithKeyring is the explicit form accepting a QR_SIGNING_KEYS
// spec (`kid:hex32[,kid:hex32...]`). Pass an empty spec to use sessionSecret
// as a single implicit key (kid "default").
func NewQRHandlerWithKeyring(rdb *redis.Client, sessionSecret, keysSpec string) *QRHandler {
	kr, err := newQRKeyring(keysSpec, sessionSecret)
	if err != nil {
		// Fail loud at construction so bad config surfaces at boot, not on
		// the first QR request. main.go already validates Config — this
		// branch catches spec drift after Config.Load succeeded.
		panic(fmt.Sprintf("qr: invalid QR_SIGNING_KEYS: %v", err))
	}
	return &QRHandler{
		rdb:           rdb,
		keyring:       kr,
		sessionSecret: sessionSecret,
		now:           time.Now,
	}
}

// WithOrgService wires the organization use case used by action=join_org.
// Chainable; safe to call with nil (leaves the action gated).
func (h *QRHandler) WithOrgService(svc QROrgService) *QRHandler {
	h.orgService = svc
	return h
}

// WithPublisher wires best-effort NATS publishing for confirm-time events.
// Chainable; safe to call with nil.
func (h *QRHandler) WithPublisher(p QREventPublisher) *QRHandler {
	h.publisher = p
	return h
}

// ── Create ──────────────────────────────────────────────────────────────────

type qrCreateRequest struct {
	Action string          `json:"action" binding:"required"`
	Params json.RawMessage `json:"params,omitempty"`
}

// qrJoinOrgParams is the expected shape of session.Params for action=join_org.
// The org admin names the org_id the scanner will be added to; role defaults
// to "member" when omitted — admins can elevate separately.
type qrJoinOrgParams struct {
	OrgID int64  `json:"org_id" binding:"required"`
	Role  string `json:"role,omitempty"`
}

// qrDelegateParams is a placeholder for Phase 2 — the delegate action is not
// wired yet (CreateSessionAuthed returns 501). Kept here as the canonical
// shape so clients / tests can evolve against a stable type once enabled.
type qrDelegateParams struct {
	Scopes     []string `json:"scopes" binding:"required"`
	TTLSeconds int      `json:"ttl_seconds,omitempty"`
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

	issuedAt := h.now().UTC()
	session := entity.QRSession{
		ID:        id,
		Action:    action,
		Params:    req.Params,
		Status:    entity.QRStatusPending,
		CreatedAt: issuedAt,
		IP:        c.ClientIP(),
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

	metrics.RecordQRSessionCreated(string(action))
	expiresAt := issuedAt.Add(qrDefaultTTL)
	c.JSON(http.StatusOK, gin.H{
		"id":         id,
		"action":     string(action),
		"qr_payload": h.buildPayload(id, action, issuedAt),
		"expires_in": int(qrDefaultTTL.Seconds()),
		"expires_at": expiresAt.Format(time.RFC3339),
	})
}

// CreateSessionAuthed — POST /api/v2/qr/session/authed (JWT-authenticated)
//
// Companion to CreateSession for actions that require the initiator's identity
// up-front. Accepts only {join_org, delegate}; login stays on the public door
// where no caller identity is needed. Caller authorization is pre-flighted
// here so we return 403 immediately instead of deferring to confirm-time.
func (h *QRHandler) CreateSessionAuthed(c *gin.Context) {
	callerID, ok := requireAccountID(c)
	if !ok {
		return
	}

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
			"message": fmt.Sprintf("Unknown QR action %q (expected: join_org|delegate)", req.Action),
		})
		return
	}

	// login belongs on the unauthenticated door — rejecting it here prevents
	// an authed client from accidentally minting a login code "as themselves".
	if action == entity.QRActionLogin {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "action_not_allowed_on_authed_endpoint",
			"message": "action=login must use POST /api/v2/qr/session (unauthenticated)",
		})
		return
	}

	ctx := c.Request.Context()

	switch action {
	case entity.QRActionJoinOrg:
		if h.orgService == nil {
			c.JSON(http.StatusNotImplemented, gin.H{
				"error":   "action_not_supported_yet",
				"message": "join_org is not wired on this deployment",
			})
			return
		}
		var p qrJoinOrgParams
		if len(req.Params) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "invalid_params",
				"message": "join_org requires params={org_id, role?}",
			})
			return
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.OrgID == 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "invalid_params",
				"message": "join_org params must include a non-zero org_id",
			})
			return
		}
		if p.Role == "" {
			p.Role = "member"
		}
		// Authorize the initiator: only owners/admins may mint a join code.
		allowed, err := h.orgService.IsOwnerOrAdmin(ctx, p.OrgID, callerID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "permission_check_failed",
				"message": "Failed to verify organization membership",
			})
			return
		}
		if !allowed {
			c.JSON(http.StatusForbidden, gin.H{
				"error":   "forbidden",
				"message": "Only organization owners or admins can create a join code",
			})
			return
		}
		// Re-serialize params with defaulted role so the confirm side sees the
		// authoritative role even if the caller omitted it.
		canonical, err := json.Marshal(p)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "marshal_failed",
				"message": "Failed to serialize params",
			})
			return
		}
		req.Params = canonical

	case entity.QRActionDelegate:
		// Phase 2 — shape reserved, handler not wired.
		c.JSON(http.StatusNotImplemented, gin.H{
			"error":   "action_not_supported_yet",
			"message": "delegate will ship in a later phase",
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

	issuedAt := h.now().UTC()
	session := entity.QRSession{
		ID:        id,
		Action:    action,
		Params:    req.Params,
		Status:    entity.QRStatusPending,
		CreatedBy: callerID,
		CreatedAt: issuedAt,
		IP:        c.ClientIP(),
		UA:        truncate(c.GetHeader("User-Agent"), qrMaxUALength),
	}

	data, _ := json.Marshal(session)
	if err := h.rdb.Set(ctx, qrKey(id), data, qrDefaultTTL).Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "store_failed",
			"message": "Failed to persist QR session",
		})
		return
	}

	metrics.RecordQRSessionCreated(string(action))
	expiresAt := issuedAt.Add(qrDefaultTTL)
	c.JSON(http.StatusOK, gin.H{
		"id":         id,
		"action":     string(action),
		"qr_payload": h.buildPayload(id, action, issuedAt),
		"expires_in": int(qrDefaultTTL.Seconds()),
		"expires_at": expiresAt.Format(time.RFC3339),
	})
}

// ── Status (long-poll) ──────────────────────────────────────────────────────

// PollStatus — GET /api/v2/qr/:id/status?timeout=<seconds> (unauthenticated)
//
// Pending → returns {"status":"pending"} when the poll window elapses.
// Confirmed → atomically transitions to consumed and returns action-specific
// payload (login → {"status":"confirmed", "token": "..."}).
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
			metrics.RecordQRExpired()
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

// writeConfirmResult dispatches per-action confirm-time payload on the
// status-poll path. Actions whose side effects belong to the APP (Confirm)
// path instead return a descriptive 400 here.
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

	case entity.QRActionJoinOrg:
		// join_org's side effect runs on the Confirm (APP) path, not here —
		// there is no Web long-poller waiting for the result. This branch is
		// only reached if some legacy code path still polls status for a
		// join_org session; return a descriptive 400 rather than silently
		// running AddMember twice.
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "action_uses_confirm_path",
			"message": "join_org is completed on the APP confirm path; status polling is not applicable",
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

// publishMemberJoined emits identity.org.member_joined best-effort. Failures
// are logged but never fail the confirm response — the DB write is the source
// of truth; NATS is a downstream notification channel.
func (h *QRHandler) publishMemberJoined(orgID, accountID int64, role string, joinedAt time.Time) {
	if h.publisher == nil {
		return
	}
	payload := event.OrgMemberJoinedPayload{
		OrgID:          orgID,
		AccountID:      accountID,
		Role:           role,
		JoinedAt:       joinedAt.Format(time.RFC3339),
		ConfirmedViaQR: true,
	}
	ev, err := event.NewEvent(event.SubjectOrgMemberJoined, accountID, "", "", payload)
	if err != nil {
		slog.Warn("qr.join_org.event_build_failed", "org_id", orgID, "account_id", accountID, "err", err)
		return
	}
	pubCtx, cancel := context.WithTimeout(context.Background(), qrEventPublishTimeout)
	defer cancel()
	if err := h.publisher.Publish(pubCtx, ev); err != nil {
		slog.Warn("qr.join_org.event_publish_failed", "org_id", orgID, "account_id", accountID, "err", err)
	}
}

// ── Confirm ─────────────────────────────────────────────────────────────────

type qrConfirmRequest struct {
	// Sig is the payload HMAC returned to the APP in the QR payload.
	// Requiring it on confirm ensures scanned payloads cannot be substituted
	// for a fabricated id by a malicious app.
	Sig string `json:"sig" binding:"required"`
	// T is the unix-seconds issued-at timestamp parsed from the scanned payload
	// (the "t" query param in the lurus://qr URI). Clients from 2026-04-24 onward
	// MUST include it; absence triggers the legacy (id|action) verification path
	// for backward compatibility with pre-B5 APP builds.
	//
	// TODO(2026-06-01): drop legacy path once all APP clients ship B5-aware scan
	// code — then make `t` required and remove the fallback in verifyPayloadSig.
	T int64 `json:"t,omitempty"`
}

// Confirm — POST /api/v2/qr/:id/confirm (auth required)
//
// The APP user confirms the pending session. Requires the HMAC signature
// from the scanned payload; if the HMAC doesn't match id+action, the call
// is rejected to prevent blind-confirm attacks where a malicious app tricks
// the user into confirming an attacker-chosen id.
func (h *QRHandler) Confirm(c *gin.Context) {
	// Observe latency for every request that makes it past auth; "unknown"
	// action label until we learn the session's action from Redis.
	start := h.now()
	labelAction := "unknown"
	defer func() {
		metrics.RecordQRConfirmLatency(labelAction, h.now().Sub(start))
	}()

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
		metrics.RecordQRExpired()
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
	labelAction = string(session.Action)

	if req.T == 0 {
		// Legacy pre-B5 client: fall back to the timestamp-less HMAC. Kept for
		// backward compatibility with APP builds from before 2026-04-24.
		// TODO(2026-06-01): remove once all clients upgrade.
		slog.WarnContext(c.Request.Context(), "qr.legacy_payload_signature",
			"id", id, "action", string(session.Action), "account_id", accountID)
		if !h.verifyPayloadSigLegacy(id, session.Action, req.Sig) {
			metrics.RecordQRSignatureRejected()
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "invalid_signature",
				"message": "QR payload signature did not match",
			})
			return
		}
	} else {
		if !h.verifyPayloadSig(id, session.Action, req.T, req.Sig) {
			metrics.RecordQRSignatureRejected()
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "invalid_signature",
				"message": "QR payload signature did not match or has expired",
			})
			return
		}
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

	metrics.RecordQRConfirmed(string(session.Action))

	// tryConfirm stored accountID in Redis; mirror it onto our local copy so
	// downstream side effects (e.g. AddMember target) see the right user.
	session.AccountID = accountID

	// Per-action side effects on the APP confirm path. login is a no-op here
	// because the token is issued to the Web client via writeConfirmResult
	// when it polls /status. join_org needs to run AddMember synchronously
	// before the APP sees success, since the scanner IS the new member.
	if session.Action == entity.QRActionJoinOrg {
		h.confirmJoinOrg(c, session)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"confirmed": true,
		"action":    string(session.Action),
	})
}

// confirmJoinOrg runs the join_org side effect (AddMember + event publish)
// inline on the Confirm path and writes the enriched response. Called only
// after tryConfirm succeeded, so state is pending→confirmed in Redis.
func (h *QRHandler) confirmJoinOrg(c *gin.Context, s *entity.QRSession) {
	ctx := c.Request.Context()
	if h.orgService == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "action_unsupported",
			"message": "join_org requires organization service, not wired",
		})
		return
	}
	if s.CreatedBy == 0 {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "invalid_session",
			"message": "join_org session missing initiator",
		})
		return
	}
	var p qrJoinOrgParams
	if err := json.Unmarshal(s.Params, &p); err != nil || p.OrgID == 0 {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "invalid_session",
			"message": "join_org session has malformed params",
		})
		return
	}
	if p.Role == "" {
		p.Role = "member"
	}
	// CreatedBy = initiator (must be owner/admin — rechecked by AddMember).
	// AccountID  = scanner/APP user confirming — this is the new member.
	if err := h.orgService.AddMember(ctx, p.OrgID, s.CreatedBy, s.AccountID, p.Role); err != nil {
		slog.WarnContext(ctx, "qr.join_org.add_member_failed",
			"org_id", p.OrgID, "initiator", s.CreatedBy,
			"target", s.AccountID, "err", err)
		c.JSON(http.StatusForbidden, gin.H{
			"error":   "forbidden",
			"message": "Failed to add member to organization",
		})
		return
	}
	joinedAt := h.now().UTC()
	h.publishMemberJoined(p.OrgID, s.AccountID, p.Role, joinedAt)
	c.JSON(http.StatusOK, gin.H{
		"confirmed": true,
		"action":    string(s.Action),
		"org_id":    p.OrgID,
		"role":      p.Role,
		"joined_at": joinedAt.Format(time.RFC3339),
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
// Format: lurus://qr?v=1&id=<hex>&a=<action>&t=<unix>&h=<hmac16>
// The HMAC is over `id|action|unix(t)` so a screenshot QR cannot be replayed
// past its issued-at window even if the server-side Redis key somehow lived
// longer. Version prefix lets us evolve the payload shape without flag days.
func (h *QRHandler) buildPayload(id string, action entity.QRAction, issuedAt time.Time) string {
	t := issuedAt.Unix()
	sig := h.payloadSig(id, action, t)
	return fmt.Sprintf("lurus://qr?v=1&id=%s&a=%s&t=%d&h=%s", id, action, t, sig)
}

// payloadSig signs id|action|issued_at with the keyring's current (highest-kid)
// key. The 0x01 domain-separator byte prevents cross-purpose confusion if the
// same master secret is ever reused for JWT or other HMAC primitives.
func (h *QRHandler) payloadSig(id string, action entity.QRAction, issuedAt int64) string {
	return hmacHex(h.keyring.current().secret, id, action, issuedAt)
}

// verifyPayloadSig validates a B5+ signature, trying each key in the keyring.
// Rejects when `t` is outside the allowed window (TTL + small clock-skew
// buffer) so screenshot replay cannot succeed even if the server Redis
// record somehow outlives its TTL.
func (h *QRHandler) verifyPayloadSig(id string, action entity.QRAction, issuedAt int64, got string) bool {
	// Bound the signing timestamp inside (issued_at - skew, issued_at + TTL + skew)
	// around now. The upper bound protects against replay; the lower bound
	// protects against a client sending a far-future t with a forged HMAC
	// (would be impossible without the secret, but bounding is cheap insurance).
	now := h.now().Unix()
	if now-issuedAt > int64((qrDefaultTTL + qrTimestampSkew).Seconds()) {
		return false
	}
	if issuedAt-now > int64(qrTimestampSkew.Seconds()) {
		return false
	}
	// Try every key in the ring so mid-rotation sessions still validate.
	// hmac.Equal is constant-time per comparison; the total runtime leaks
	// only the keyring size, which is a static operational parameter.
	gotBytes := []byte(got)
	matched := false
	for _, k := range h.keyring.keys {
		want := hmacHex(k.secret, id, action, issuedAt)
		if hmac.Equal([]byte(want), gotBytes) {
			matched = true
		}
	}
	return matched
}

// hmacHex is the shared signing primitive. Format:
//
//	0x01 || id || 0x00 || action || 0x00 || decimal(issuedAt)
//
// The 0x01 prefix is a domain separator (see qrHMACDomainSeparator); the
// 0x00 bytes between fields prevent concatenation ambiguity.
func hmacHex(secret []byte, id string, action entity.QRAction, issuedAt int64) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte{qrHMACDomainSeparator})
	mac.Write([]byte(id))
	mac.Write([]byte{0})
	mac.Write([]byte(action))
	mac.Write([]byte{0})
	mac.Write([]byte(strconv.FormatInt(issuedAt, 10)))
	return hex.EncodeToString(mac.Sum(nil))[:qrPayloadHMACLen]
}

// payloadSigLegacy is the pre-B5 HMAC over id|action only. Retained solely
// for backward compatibility with APP builds that do not yet echo `t` on
// confirm. Scheduled for removal on 2026-06-01 (see qrConfirmRequest.T TODO).
// Tries each key in the ring so a key rotation during the legacy window is
// still consistent with the B5 verification path.
func (h *QRHandler) verifyPayloadSigLegacy(id string, action entity.QRAction, got string) bool {
	gotBytes := []byte(got)
	matched := false
	for _, k := range h.keyring.keys {
		mac := hmac.New(sha256.New, k.secret)
		mac.Write([]byte(id))
		mac.Write([]byte{0})
		mac.Write([]byte(action))
		// Pin to qrLegacyHMACLen (16) — pre-B5 APPs signed at that truncation.
		want := hex.EncodeToString(mac.Sum(nil))[:qrLegacyHMACLen]
		if hmac.Equal([]byte(want), gotBytes) {
			matched = true
		}
	}
	return matched
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
