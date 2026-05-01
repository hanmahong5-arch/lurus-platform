package handler

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	// GetCallerRole returns the caller's specific role ("owner" | "admin" |
	// "member" | "") in the organization. Empty string means "no membership".
	// Used by confirmJoinOrg to enforce privilege non-escalation: admins may
	// only grant role=member, not role=admin.
	GetCallerRole(ctx context.Context, orgID, callerID int64) (string, error)
	AddMember(ctx context.Context, orgID, callerID, targetAccountID int64, role string) error
}

// QREventPublisher is the minimal NATS publish surface used for member-join events.
// Accepts nil at wiring time — publish failures never block the user-visible path.
type QREventPublisher interface {
	Publish(ctx context.Context, ev *event.IdentityEvent) error
}

// QRDelegateExecutor performs the side effect of a confirmed delegate
// action. Declared as an interface so the QR handler stays free of any
// app_registry / Zitadel / account-service imports — those concerns
// live behind concrete executors wired at startup. Multiple executors
// can be registered: dispatch is by op name via SupportedOps().
//
// Pre-validated by the handler: op was checked against the union of all
// executors' SupportedOps at session creation, so executors only handle
// the ops they claim to support. An unknown op surfaces as
// ErrUnsupportedDelegateOp so the handler can return 400 instead of 500.
type QRDelegateExecutor interface {
	ExecuteDelegate(ctx context.Context, params QRDelegateParams, callerID int64) error
	// SupportedOps returns the op identifiers this executor will accept.
	// CreateSessionAuthed validates against this set before persisting
	// the session so a typo fails fast at create-time rather than
	// surface only on confirm.
	SupportedOps() []string
}

// ErrUnsupportedDelegateOp signals an op that the executor does not
// implement. Returned by ExecuteDelegate when the dispatch table has
// drifted from SupportedOps; the handler maps this to 400 invalid_op.
var ErrUnsupportedDelegateOp = errors.New("qr: delegate op not supported by wired executor")

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

// qrDefaultMaxInflightPolls caps concurrent /status long-poll goroutines
// when no per-handler value was wired. Must stay in sync with
// config.Config.QRMaxInflightPolls default so tests and prod agree.
const qrDefaultMaxInflightPolls = 50000

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
	// delegates execute confirmed delegate-action side effects (delete
	// OIDC app, purge account, …). Empty slice = action gated with 501
	// at create-time. Multiple executors register independently;
	// dispatch is by op-name overlap with each executor's SupportedOps.
	delegates []QRDelegateExecutor
	// pollSem bounds the number of concurrent long-poll goroutines held by
	// PollStatus. A full buffered channel means "no slot available"; the
	// handler returns 503 immediately so upstream load balancers can shed.
	// Buffered struct{} channel is used instead of a semaphore package to
	// keep zero external deps and a cheap non-blocking select.
	pollSem chan struct{}
	// auditRepo is the optional persistent audit-events sink. Used by
	// confirmDelegate to record every QR-confirmed destructive op so
	// the trail survives pod restarts. nil = audit emit is a no-op.
	auditRepo repoAuditSink
}

// repoAuditSink is the minimal Save surface QRHandler needs from
// repo.AuditEventRepo. Declared as a local interface so the handler
// package keeps its existing import surface and tests can supply an
// in-memory fake without standing up the full repo.
type repoAuditSink interface {
	Save(ctx context.Context, e *entity.AuditEvent) error
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
		pollSem:       make(chan struct{}, qrDefaultMaxInflightPolls),
	}
}

// WithMaxInflightPolls sets the ceiling on concurrent /status long-poll
// goroutines. A value ≤ 0 falls back to qrDefaultMaxInflightPolls so a
// misconfigured env (e.g. "0") still produces a usable handler. Chainable.
// Reallocating the semaphore is safe only at construction time; callers
// must wire this before the router starts serving.
func (h *QRHandler) WithMaxInflightPolls(n int) *QRHandler {
	if n <= 0 {
		n = qrDefaultMaxInflightPolls
	}
	h.pollSem = make(chan struct{}, n)
	return h
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

// WithDelegateExecutor registers an executor for action=delegate. The
// call is append-style: each executor handles its own SupportedOps and
// dispatch picks the first executor whose op set contains the request.
// Passing nil is a no-op so call sites can wire conditionally without a
// guard. Chainable.
func (h *QRHandler) WithDelegateExecutor(d QRDelegateExecutor) *QRHandler {
	if d == nil {
		return h
	}
	h.delegates = append(h.delegates, d)
	return h
}

// WithAuditRepo wires a persistent audit-events sink for delegate
// confirms. Chainable; nil-safe so existing tests don't need to
// update wiring.
func (h *QRHandler) WithAuditRepo(r repoAuditSink) *QRHandler {
	h.auditRepo = r
	return h
}

// delegateFor returns the registered executor that supports op, or nil
// if none does. Walks executors in registration order; the first match
// wins, so collisions between executors are decided by who registered
// first (operationally we expect SupportedOps sets to be disjoint).
func (h *QRHandler) delegateFor(op string) QRDelegateExecutor {
	for _, exec := range h.delegates {
		for _, supported := range exec.SupportedOps() {
			if supported == op {
				return exec
			}
		}
	}
	return nil
}

// supportsDelegateOp reports whether any registered executor accepts
// op. Centralised so CreateSessionAuthed and Confirm cannot disagree on
// the whitelist.
func (h *QRHandler) supportsDelegateOp(op string) bool {
	return h.delegateFor(op) != nil
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

// QRDelegateParams describes the operation a confirmed delegate session
// will perform. Field requirements depend on op:
//
//   - delete_oidc_app: AppName + Env required, others ignored.
//   - delete_account:  AccountID required, others ignored.
//   - approve_refund:  RefundNo required, others ignored.
//
// Per-op validation is centralised in Validate() so the create-side
// (CreateSessionAuthed / CreateDelegateSession) and the confirm-side
// (confirmDelegate) cannot disagree on the schema. New ops add their
// own clause; unknown ops return nil here so the executor — which
// knows what it accepts — owns the final say.
//
// The struct is intentionally a flat bag rather than a discriminated
// union: it keeps Redis-stored sessions JSON-stable across versions,
// and at three ops the field count is still trivially manageable.
// If we ever exceed (say) six op-specific fields, switching to
// `Params json.RawMessage` per op will be the right move.
//
// Exported because handler-package test fakes implement
// QRDelegateExecutor from a sibling _test package, which cannot
// reference unexported types in method signatures.
type QRDelegateParams struct {
	// Op identifies the action. The set of valid values is defined by
	// the union of all registered executors' SupportedOps. Kept as a
	// string (rather than a typed enum) so new ops can ship behind
	// configuration without adding entity types.
	Op string `json:"op" binding:"required"`
	// AppName is the apps.yaml slug of the target app (e.g. "tally")
	// for delete_oidc_app. Empty for ops that don't target an app.
	AppName string `json:"app_name,omitempty"`
	// Env is the environment tag in apps.yaml (e.g. "stage", "prod")
	// for delete_oidc_app. Empty for ops that don't target an env.
	Env string `json:"env,omitempty"`
	// AccountID is the target account for delete_account. Zero for ops
	// that don't target a user.
	AccountID int64 `json:"account_id,omitempty"`
	// RefundNo is the target refund number for approve_refund. Empty
	// for ops that don't target a refund.
	RefundNo string `json:"refund_no,omitempty"`
}

// Validate enforces per-op required-field rules. Returns nil when the
// params shape matches the op; returns a descriptive error otherwise.
//
// Op-name validity (i.e. is this op supported by some registered
// executor?) is handled separately by QRHandler.supportsDelegateOp;
// Validate only checks shape. Unknown ops return nil here so a
// deployment-time op known to an executor but not yet known to this
// schema is passed through to the executor — which may accept it or
// reject with ErrUnsupportedDelegateOp.
func (p QRDelegateParams) Validate() error {
	switch p.Op {
	case qrDelegateOpDeleteOIDCApp:
		if strings.TrimSpace(p.AppName) == "" || strings.TrimSpace(p.Env) == "" {
			return errors.New("delete_oidc_app requires app_name and env")
		}
		return nil
	case qrDelegateOpDeleteAccount:
		if p.AccountID <= 0 {
			return errors.New("delete_account requires a positive account_id")
		}
		return nil
	case qrDelegateOpApproveRefund:
		if strings.TrimSpace(p.RefundNo) == "" {
			return errors.New("approve_refund requires refund_no")
		}
		return nil
	default:
		return nil
	}
}

const (
	// qrDelegateOpDeleteOIDCApp is the canonical op string for the
	// delete-OIDC-app flow. Exported via the executor's SupportedOps so
	// the handler and apps_admin executor cannot disagree on spelling.
	qrDelegateOpDeleteOIDCApp = "delete_oidc_app"
	// qrDelegateOpDeleteAccount is the canonical op string for GDPR-
	// grade account purges (Phase 4 / Sprint 1A).
	qrDelegateOpDeleteAccount = "delete_account"
	// qrDelegateOpApproveRefund is the canonical op string for
	// QR-confirmed refund approval (Phase 4 / Sprint 3A) — used for
	// large refunds where the customer-service rep wants the boss to
	// sign off rather than approve directly.
	qrDelegateOpApproveRefund = "approve_refund"
)

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
		if len(h.delegates) == 0 {
			c.JSON(http.StatusNotImplemented, gin.H{
				"error":   "action_not_supported_yet",
				"message": "delegate is not wired on this deployment",
			})
			return
		}
		var p QRDelegateParams
		if len(req.Params) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "invalid_params",
				"message": "delegate requires params={op, ...}",
			})
			return
		}
		if err := json.Unmarshal(req.Params, &p); err != nil ||
			strings.TrimSpace(p.Op) == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "invalid_params",
				"message": "delegate params must include op",
			})
			return
		}
		// Op name comes first: an unsupported op is a different failure
		// mode than valid op + bad params, and clients (the admin Web
		// UI) need to tell them apart.
		if !h.supportsDelegateOp(p.Op) {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "invalid_op",
				"message": fmt.Sprintf("Op %q is not a supported delegate operation", p.Op),
			})
			return
		}
		if err := p.Validate(); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "invalid_params",
				"message": err.Error(),
			})
			return
		}
		// Re-serialise with trimmed fields so the confirm side never
		// sees stray whitespace / mismatched casing leaking through.
		canonical, err := json.Marshal(QRDelegateParams{
			Op:        p.Op,
			AppName:   strings.TrimSpace(p.AppName),
			Env:       strings.TrimSpace(p.Env),
			AccountID: p.AccountID,
			RefundNo:  strings.TrimSpace(p.RefundNo),
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "marshal_failed",
				"message": "Failed to serialize delegate params",
			})
			return
		}
		req.Params = canonical
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

// QRDelegateSession is the result of CreateDelegateSession — exactly the
// fields a Web initiator needs to render a QR for the APP to scan.
type QRDelegateSession struct {
	ID        string    `json:"id"`
	QRPayload string    `json:"qr_payload"`
	ExpiresAt time.Time `json:"expires_at"`
	ExpiresIn int       `json:"expires_in"`
}

// CreateDelegateSession mints a delete_oidc_app delegate session.
// Retained as a convenience wrapper because callers (the AppsAdmin
// DeleteRequest endpoint, multiple tests) hand the (op, appName, env)
// triple directly. Internally builds a QRDelegateParams and delegates
// to CreateDelegateSessionWithParams.
//
// Unlike CreateSessionAuthed this never reaches into the Gin context —
// IP / UA capture is intentionally omitted because the initiator is
// another server-side handler, not the end user. The user's device
// metadata is captured at confirm time on the APP side.
func (h *QRHandler) CreateDelegateSession(ctx context.Context, callerID int64, op, appName, env string) (*QRDelegateSession, error) {
	return h.CreateDelegateSessionWithParams(ctx, callerID, QRDelegateParams{
		Op:      strings.TrimSpace(op),
		AppName: strings.TrimSpace(appName),
		Env:     strings.TrimSpace(env),
	})
}

// CreateDelegateSessionWithParams is the generic, op-agnostic minter
// used by handlers that need to mint sessions for ops with non-(app,
// env) param shapes (delete_account, future ops). All failure modes
// return wrapped errors so the calling HTTP handler can map to the
// right status — e.g. ErrUnsupportedDelegateOp → 501 / 400 depending on
// whether wiring is missing or the op was simply unknown.
func (h *QRHandler) CreateDelegateSessionWithParams(ctx context.Context, callerID int64, params QRDelegateParams) (*QRDelegateSession, error) {
	if callerID == 0 {
		return nil, errors.New("qr: CreateDelegateSession requires non-zero callerID")
	}
	if len(h.delegates) == 0 {
		return nil, errors.New("qr: delegate executor not wired")
	}
	params.Op = strings.TrimSpace(params.Op)
	if params.Op == "" {
		return nil, errors.New("qr: delegate session needs op")
	}
	if !h.supportsDelegateOp(params.Op) {
		return nil, fmt.Errorf("qr: %w: %q", ErrUnsupportedDelegateOp, params.Op)
	}
	if err := params.Validate(); err != nil {
		return nil, fmt.Errorf("qr: invalid params: %w", err)
	}
	encoded, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("qr: marshal delegate params: %w", err)
	}
	id, err := newQRID()
	if err != nil {
		return nil, fmt.Errorf("qr: id generation: %w", err)
	}
	issuedAt := h.now().UTC()
	session := entity.QRSession{
		ID:        id,
		Action:    entity.QRActionDelegate,
		Params:    encoded,
		Status:    entity.QRStatusPending,
		CreatedBy: callerID,
		CreatedAt: issuedAt,
	}
	data, _ := json.Marshal(session)
	if err := h.rdb.Set(ctx, qrKey(id), data, qrDefaultTTL).Err(); err != nil {
		return nil, fmt.Errorf("qr: persist session: %w", err)
	}
	metrics.RecordQRSessionCreated(string(entity.QRActionDelegate))
	return &QRDelegateSession{
		ID:        id,
		QRPayload: h.buildPayload(id, entity.QRActionDelegate, issuedAt),
		ExpiresAt: issuedAt.Add(qrDefaultTTL),
		ExpiresIn: int(qrDefaultTTL.Seconds()),
	}, nil
}

// ── Status (long-poll) ──────────────────────────────────────────────────────

// PollStatus — GET /api/v2/qr/:id/status?timeout=<seconds> (unauthenticated)
//
// Pending → returns {"status":"pending"} when the poll window elapses.
// Confirmed → atomically transitions to consumed and returns action-specific
// payload (login → {"status":"confirmed", "token": "..."}).
// Consumed → 410 Gone.
// Missing / TTL-expired → 404.
//
// Wait mechanism: subscribes to Redis Pub/Sub channel `qr-events:<id>` and
// blocks on the message stream instead of polling every second. Confirm
// publishes to that channel after a successful state flip, waking any
// waiting poller immediately. Falls back to the legacy 1s polling loop if
// Subscribe fails (e.g. transient Redis hiccup).
//
// Concurrency: the first action is a non-blocking acquire on pollSem; if
// the semaphore is full we 503 immediately so the pod stays responsive
// instead of piling up 30s goroutines.
func (h *QRHandler) PollStatus(c *gin.Context) {
	// Bound the number of in-flight long-poll goroutines. A saturated
	// semaphore → 503 rather than a blocked acquire: the client should
	// back off and retry, not stack another 30s hold on this pod.
	select {
	case h.pollSem <- struct{}{}:
		metrics.IncQRPollsInflight()
		defer func() {
			<-h.pollSem
			metrics.DecQRPollsInflight()
		}()
	default:
		metrics.RecordQRPollRejectedOverload()
		c.Header("Retry-After", "1")
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":   "server_overloaded",
			"message": "Long-poll capacity temporarily exhausted; retry shortly",
		})
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

	timeout := qrMaxPollWait
	if raw := c.Query("timeout"); raw != "" {
		if secs, err := strconv.Atoi(raw); err == nil && secs > 0 && secs <= int(qrMaxPollWait.Seconds()) {
			timeout = time.Duration(secs) * time.Second
		}
	}

	ctx := c.Request.Context()
	deadline := h.now().Add(timeout)
	key := qrKey(id)

	// Fast path: first read. Most of the time the session is already pending
	// (client just opened the login page) but we must also cheaply answer
	// 404/410 without entering the subscribe dance.
	session, err := h.readSession(ctx, key)
	if handled := h.writePollResult(c, ctx, key, session, err); handled {
		return
	}

	// Pending → subscribe and wait for a Confirm to publish.
	//
	// Subscribe BEFORE the second read so the Subscribe/Publish rendezvous
	// closes the race: if Confirm publishes between our first read and
	// subscription, the second read catches it; if it publishes after the
	// subscription is active, the Channel() delivers it.
	sub := h.rdb.Subscribe(ctx, pubsubChannel(id))
	defer func() { _ = sub.Close() }()

	// Wait until the subscription is actually registered with the Redis
	// server; Subscribe() is asynchronous, so Receive() serves as the
	// "ready" signal. Any transport error here falls back to the legacy
	// 1s polling loop so a flaky Pub/Sub doesn't break login.
	if _, err := sub.Receive(ctx); err != nil {
		metrics.RecordQRPollFallback()
		slog.WarnContext(ctx, "qr.pubsub_subscribe_failed_fallback_to_poll",
			"id", id, "err", err)
		h.pollStatusPollingFallback(c, ctx, key, deadline)
		return
	}

	// Second read — catches Confirm events that fired between our first
	// read and the subscription going live.
	session, err = h.readSession(ctx, key)
	if handled := h.writePollResult(c, ctx, key, session, err); handled {
		return
	}

	// Still pending: block on Channel() until deadline / ctx cancel / event.
	msgCh := sub.Channel()
	remaining := time.Until(deadline)
	if remaining <= 0 {
		c.JSON(http.StatusOK, gin.H{"status": string(entity.QRStatusPending)})
		return
	}
	select {
	case <-msgCh:
		// Someone confirmed (or a stray PUBLISH landed) — re-read and dispatch.
		session, err = h.readSession(ctx, key)
		h.writePollResult(c, ctx, key, session, err)
		return
	case <-time.After(remaining):
		c.JSON(http.StatusOK, gin.H{"status": string(entity.QRStatusPending)})
		return
	case <-ctx.Done():
		c.JSON(http.StatusOK, gin.H{"status": string(entity.QRStatusPending)})
		return
	}
}

// writePollResult inspects a freshly-read session and writes the matching
// HTTP response. Returns true when a response was written (caller should
// stop processing), false when the session is still pending (caller
// should keep waiting).
func (h *QRHandler) writePollResult(c *gin.Context, ctx context.Context, key string, session *entity.QRSession, err error) bool {
	if err == redis.Nil {
		metrics.RecordQRExpired()
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "session_not_found",
			"message": "QR session expired or does not exist",
		})
		return true
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "read_failed",
			"message": "Failed to read QR session",
		})
		return true
	}

	switch session.Status {
	case entity.QRStatusConfirmed:
		consumed, cerr := h.tryConsume(ctx, key)
		if cerr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "consume_failed",
				"message": "Failed to consume QR session",
			})
			return true
		}
		if !consumed {
			// Another poller beat us to the transition — treat as consumed.
			c.JSON(http.StatusGone, gin.H{
				"error":   "session_consumed",
				"message": "Session has already been consumed",
			})
			return true
		}
		h.writeConfirmResult(c, session)
		return true

	case entity.QRStatusConsumed:
		c.JSON(http.StatusGone, gin.H{
			"error":   "session_consumed",
			"message": "Session has already been consumed",
		})
		return true
	}

	// Still pending.
	return false
}

// pollStatusPollingFallback runs the pre-Pub/Sub 1s polling loop. Invoked
// only when the Pub/Sub subscribe fails so long-poll keeps working even
// when the broker is degraded.
func (h *QRHandler) pollStatusPollingFallback(c *gin.Context, ctx context.Context, key string, deadline time.Time) {
	for {
		session, err := h.readSession(ctx, key)
		if handled := h.writePollResult(c, ctx, key, session, err); handled {
			return
		}
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

// pubsubChannel is the Redis Pub/Sub channel name for a session's state
// transition events. Confirm publishes here on successful pending→confirmed;
// PollStatus subscribes here to wake up instantly.
func pubsubChannel(id string) string { return "qr-events:" + id }

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
		// Structured success audit — paired with the failure-path warn logs so
		// Loki queries can compute confirm rate / geo distribution / unusual
		// location patterns without parsing Gin access logs.
		slog.InfoContext(c.Request.Context(), "qr.confirm.login",
			"account_id", s.AccountID,
			"session_id", s.ID,
			"ip", c.ClientIP(),
			"ua", s.UA,
			"created_ip", s.IP)
		c.JSON(http.StatusOK, gin.H{
			"status":     string(entity.QRStatusConfirmed),
			"action":     string(s.Action),
			"token":      token,
			"expires_in": int(qrLoginTTL.Seconds()),
		})

	case entity.QRActionJoinOrg:
		// The APP confirm path has already executed AddMember and
		// emitted the audit log. Web pollers (the org admin's UI
		// that minted the QR) need a 200 with the join details so
		// they can refresh their member list — returning 400 here
		// would hide a successful state transition. We deliberately
		// do NOT include any auth-bearing token; the Web initiator
		// is already authenticated for their own session.
		var p qrJoinOrgParams
		_ = json.Unmarshal(s.Params, &p)
		resp := gin.H{
			"status": string(entity.QRStatusConfirmed),
			"action": string(s.Action),
		}
		if p.OrgID != 0 {
			resp["org_id"] = p.OrgID
		}
		if p.Role != "" {
			resp["role"] = p.Role
		}
		if s.AccountID != 0 {
			resp["new_member_id"] = s.AccountID
		}
		c.JSON(http.StatusOK, resp)

	case entity.QRActionDelegate:
		// The APP confirm path has already executed the op (or
		// recorded its failure in the audit log). Web pollers (the
		// admin UI that minted the delete-app / refund-approve /
		// account-purge QR) get a 200 with the op metadata so they
		// can refresh their data view. No auth token is included —
		// delegate sessions never produce login credentials.
		var p QRDelegateParams
		_ = json.Unmarshal(s.Params, &p)
		resp := gin.H{
			"status": string(entity.QRStatusConfirmed),
			"action": string(s.Action),
			"op":     p.Op,
		}
		if p.AppName != "" {
			resp["app"] = p.AppName
		}
		if p.Env != "" {
			resp["env"] = p.Env
		}
		if p.AccountID != 0 {
			resp["account_id"] = p.AccountID
		}
		if p.RefundNo != "" {
			resp["refund_no"] = p.RefundNo
		}
		c.JSON(http.StatusOK, resp)

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
		metrics.RecordQRLegacySignature()
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

	// Defer the Pub/Sub publish until AFTER the action-specific
	// handler runs. The earlier in-flight order (publish *before*
	// confirmDelegate) caused a race where Web pollers could wake
	// up to a "confirmed" status while the executor was still
	// running — Web UI then refreshed the data view and saw stale
	// state. By deferring, pollers see "confirmed" only once the
	// world has converged. Publish failures stay best-effort: a
	// dropped publish only delays pollers until their own deadline,
	// at which point they re-read and discover the state.
	defer func() {
		if perr := h.rdb.Publish(ctx, pubsubChannel(id), "confirmed").Err(); perr != nil {
			slog.WarnContext(ctx, "qr.pubsub_publish_failed",
				"id", id, "action", string(session.Action), "err", perr)
		}
	}()

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
	if session.Action == entity.QRActionDelegate {
		h.confirmDelegate(c, session)
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
//
// Role authorization is enforced here (not at CreateSessionAuthed) so that
// an admin who was downgraded between session creation and APP confirm
// cannot still mint an admin-level membership. The rules are:
//
//  1. Whitelist — only {"member", "admin"} are grantable via QR. "owner"
//     MUST be granted out-of-band and is rejected with role_forbidden.
//  2. Non-escalation — only owners may grant role="admin"; admins are
//     capped at role="member". Scanning the QR cannot elevate beyond the
//     initiator's own authority.
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

	// 1. Whitelist check — "owner" is never grantable via QR. Unknown roles
	//    are rejected too, so a typo can't silently store a junk role string.
	if p.Role != "member" && p.Role != "admin" {
		slog.WarnContext(ctx, "qr.join_org.role_rejected",
			"org_id", p.OrgID, "initiator", s.CreatedBy,
			"target", s.AccountID, "requested_role", p.Role)
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "role_forbidden",
			"message": fmt.Sprintf("Role %q cannot be granted via QR (allowed: member|admin)", p.Role),
		})
		return
	}

	// 2. Privilege non-escalation — re-check the initiator's role right now
	//    (not at session creation) so a mid-flight demotion is honoured. An
	//    admin initiator may only grant "member"; "admin" requires an owner.
	callerRole, err := h.orgService.GetCallerRole(ctx, p.OrgID, s.CreatedBy)
	if err != nil {
		slog.WarnContext(ctx, "qr.join_org.caller_role_lookup_failed",
			"org_id", p.OrgID, "initiator", s.CreatedBy, "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "permission_check_failed",
			"message": "Failed to verify organization role",
		})
		return
	}
	if callerRole != "owner" && callerRole != "admin" {
		// Initiator was stripped of admin/owner between session mint and
		// scanner confirm — refuse rather than fall through to AddMember.
		c.JSON(http.StatusForbidden, gin.H{
			"error":   "forbidden",
			"message": "Session initiator is no longer owner or admin",
		})
		return
	}
	if p.Role == "admin" && callerRole != "owner" {
		slog.WarnContext(ctx, "qr.join_org.escalation_blocked",
			"org_id", p.OrgID, "initiator", s.CreatedBy,
			"initiator_role", callerRole, "target", s.AccountID, "requested_role", p.Role)
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "role_forbidden",
			"message": "Only organization owners can grant role=admin via QR",
		})
		return
	}

	// CreatedBy = initiator (rechecked above; also re-enforced by AddMember).
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
	// Structured success audit — org membership is a privileged, irreversible
	// state transition, so every successful confirm goes to the audit stream
	// regardless of downstream NATS reachability.
	slog.InfoContext(ctx, "qr.confirm.join_org",
		"org_id", p.OrgID,
		"initiator", s.CreatedBy,
		"new_member", s.AccountID,
		"role", p.Role,
		"ip", c.ClientIP(),
		"ua", s.UA,
		"created_ip", s.IP)
	c.JSON(http.StatusOK, gin.H{
		"confirmed": true,
		"action":    string(s.Action),
		"org_id":    p.OrgID,
		"role":      p.Role,
		"joined_at": joinedAt.Format(time.RFC3339),
	})
}

// confirmDelegate runs the executor for a confirmed delegate session
// inline on the APP confirm path. Mirrors the join_org pattern — the
// destructive side effect must complete before the APP sees 200, so a
// failure surfaces as a structured error the user can act on
// immediately rather than as a stale "still pending" UI state.
//
// Authorisation note: delegate sessions are minted via the admin-gated
// /admin/v1/apps/.../delete-request endpoint (see AppsAdminHandler), so
// CreatedBy is necessarily an admin at create-time. We do NOT re-check
// admin role here — the AppsAdminHandler is the only authorised
// initiator and any direct call to /api/v2/qr/session/authed with
// action=delegate bypassing it is already gated by SupportedOps + the
// op whitelist.
func (h *QRHandler) confirmDelegate(c *gin.Context, s *entity.QRSession) {
	ctx := c.Request.Context()
	if len(h.delegates) == 0 {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "action_unsupported",
			"message": "delegate requires executor, not wired",
		})
		return
	}
	if s.CreatedBy == 0 {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "invalid_session",
			"message": "delegate session missing initiator",
		})
		return
	}
	var p QRDelegateParams
	if err := json.Unmarshal(s.Params, &p); err != nil || strings.TrimSpace(p.Op) == "" {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "invalid_session",
			"message": "delegate session has malformed params",
		})
		return
	}
	if err := p.Validate(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "invalid_session",
			"message": "delegate session has malformed params",
		})
		return
	}
	exec := h.delegateFor(p.Op)
	if exec == nil {
		// Defensive: SupportedOps could narrow between create and
		// confirm if the executor set was swapped (it isn't today, but
		// the check is cheap and makes rejection legible in logs).
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_op",
			"message": fmt.Sprintf("Op %q is not supported", p.Op),
		})
		return
	}

	if err := exec.ExecuteDelegate(ctx, p, s.CreatedBy); err != nil {
		metrics.RecordQRDelegateConfirm(p.Op, "failed")
		if errors.Is(err, ErrUnsupportedDelegateOp) {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "invalid_op",
				"message": err.Error(),
			})
			return
		}
		slog.WarnContext(ctx, "qr.confirm.delegate_failed",
			"op", p.Op, "app", p.AppName, "env", p.Env,
			"account_id", p.AccountID, "refund_no", p.RefundNo,
			"initiator", s.CreatedBy, "scanner", s.AccountID, "err", err)
		h.emitDelegateAudit(ctx, c, p, s, auditEmitResultFailed, err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "delegate_failed",
			"message": "Failed to execute delegate operation",
		})
		return
	}
	metrics.RecordQRDelegateConfirm(p.Op, "success")
	// Audit log — destructive admin operations always emit a structured
	// success line so Loki queries can compute "who deleted what when"
	// without parsing access logs. AccountID is included only when the
	// op carried one (delete_account); zero-valued for app-targeted ops.
	auditAttrs := []any{
		"op", p.Op,
		"app", p.AppName,
		"env", p.Env,
		"initiator", s.CreatedBy,
		"scanner", s.AccountID,
		"ip", c.ClientIP(),
		"ua", s.UA,
		"created_ip", s.IP,
	}
	if p.AccountID != 0 {
		auditAttrs = append(auditAttrs, "account_id", p.AccountID)
	}
	if p.RefundNo != "" {
		auditAttrs = append(auditAttrs, "refund_no", p.RefundNo)
	}
	slog.InfoContext(ctx, "qr.confirm.delegate", auditAttrs...)
	h.emitDelegateAudit(ctx, c, p, s, auditEmitResultSuccess, "")

	resp := gin.H{
		"confirmed": true,
		"action":    string(s.Action),
		"op":        p.Op,
	}
	if p.AppName != "" {
		resp["app"] = p.AppName
	}
	if p.Env != "" {
		resp["env"] = p.Env
	}
	if p.AccountID != 0 {
		resp["account_id"] = p.AccountID
	}
	if p.RefundNo != "" {
		resp["refund_no"] = p.RefundNo
	}
	c.JSON(http.StatusOK, resp)
}

// emitDelegateAudit best-effort writes one row to module.audit_events
// for a QR-confirmed delegate operation. Op is the canonical name
// (delete_oidc_app / delete_account / approve_refund); the actor is
// the boss who biometric-confirmed; the target identifies the
// affected account / refund / oidc_app. Save failures log WARN and
// do NOT block the user-visible response.
func (h *QRHandler) emitDelegateAudit(ctx context.Context, c *gin.Context, p QRDelegateParams, s *entity.QRSession, result, errMsg string) {
	if h.auditRepo == nil {
		return
	}

	// Resolve target_id / target_kind from op-specific params.
	var targetID *int64
	var targetKind string
	switch p.Op {
	case qrDelegateOpDeleteOIDCApp:
		targetKind = "oidc_app"
	case qrDelegateOpDeleteAccount:
		targetKind = "account"
		if p.AccountID != 0 {
			id := p.AccountID
			targetID = &id
		}
	case qrDelegateOpApproveRefund:
		targetKind = "refund"
	}

	params := map[string]any{}
	if p.AppName != "" {
		params["app"] = p.AppName
	}
	if p.Env != "" {
		params["env"] = p.Env
	}
	if p.RefundNo != "" {
		params["refund_no"] = p.RefundNo
	}
	if s != nil {
		params["session_id"] = s.ID
		if s.AccountID != 0 {
			params["scanner"] = s.AccountID
		}
	}

	rawParams, mErr := json.Marshal(params)
	if mErr != nil || len(rawParams) == 0 {
		rawParams = json.RawMessage("{}")
	}
	var actor *int64
	if s != nil && s.CreatedBy != 0 {
		id := s.CreatedBy
		actor = &id
	}

	row := &entity.AuditEvent{
		Op:         p.Op,
		ActorID:    actor,
		TargetID:   targetID,
		TargetKind: targetKind,
		Params:     rawParams,
		Result:     result,
		Error:      errMsg,
		OccurredAt: time.Now().UTC(),
	}
	if c != nil {
		row.IP = c.ClientIP()
		row.UserAgent = c.Request.UserAgent()
		row.RequestID = c.GetString("request_id")
	}

	saveCtx, cancel := context.WithTimeout(context.Background(), auditEmitTimeout)
	defer cancel()
	if err := h.auditRepo.Save(saveCtx, row); err != nil {
		slog.WarnContext(ctx, "qr.confirm.delegate.audit_save_failed",
			"op", p.Op, "result", result, "err", err, "request_id", row.RequestID)
	}
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
