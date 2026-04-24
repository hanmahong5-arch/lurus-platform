package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/hanmahong5-arch/lurus-platform/internal/adapter/handler"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/event"
)

// ── Fakes ───────────────────────────────────────────────────────────────────
//
// fakeOrgSvc is a hand-rolled stand-in for app.OrganizationService that
// satisfies handler.QROrgService. Kept local to this file (rather than
// reused across test packages) because the real orgStore lives in a
// different test package and has unexported mocks we can't import here.

type fakeOrgSvc struct {
	mu sync.Mutex
	// map[orgID]map[accountID]role
	members map[int64]map[int64]string
	// Override AddMember behaviour for error-path tests.
	addMemberErr error
	// Captured AddMember call for assertions.
	lastAdd *addMemberCall
}

type addMemberCall struct {
	OrgID, CallerID, TargetID int64
	Role                      string
}

func newFakeOrgSvc() *fakeOrgSvc {
	return &fakeOrgSvc{members: map[int64]map[int64]string{}}
}

// seedMember registers an existing membership so IsOwnerOrAdmin can resolve it.
func (f *fakeOrgSvc) seedMember(orgID, accountID int64, role string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.members[orgID] == nil {
		f.members[orgID] = map[int64]string{}
	}
	f.members[orgID][accountID] = role
}

func (f *fakeOrgSvc) IsOwnerOrAdmin(_ context.Context, orgID, callerID int64) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.members[orgID] == nil {
		return false, nil
	}
	role, ok := f.members[orgID][callerID]
	if !ok {
		return false, nil
	}
	return role == "owner" || role == "admin", nil
}

func (f *fakeOrgSvc) AddMember(_ context.Context, orgID, callerID, targetID int64, role string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastAdd = &addMemberCall{OrgID: orgID, CallerID: callerID, TargetID: targetID, Role: role}
	if f.addMemberErr != nil {
		return f.addMemberErr
	}
	// Mirror real-service permission check so tests stay honest.
	role2 := ""
	if f.members[orgID] != nil {
		role2 = f.members[orgID][callerID]
	}
	if role2 != "owner" && role2 != "admin" {
		return errors.New("permission denied: must be owner or admin to add members")
	}
	if f.members[orgID] == nil {
		f.members[orgID] = map[int64]string{}
	}
	f.members[orgID][targetID] = role
	return nil
}

// capturingPublisher stores every event it sees so tests can assert on them.
type capturingPublisher struct {
	mu     sync.Mutex
	events []*event.IdentityEvent
}

func (p *capturingPublisher) Publish(_ context.Context, ev *event.IdentityEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, ev)
	return nil
}

func (p *capturingPublisher) last() *event.IdentityEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.events) == 0 {
		return nil
	}
	return p.events[len(p.events)-1]
}

// ── Helpers ─────────────────────────────────────────────────────────────────

// setupQRWithOrg wires a QRHandler backed by miniredis, a fake org service,
// and a capturing publisher. Returns all three so individual tests can
// assert on emitted events / mutated membership.
func setupQRWithOrg(t *testing.T) (*handler.QRHandler, *fakeOrgSvc, *capturingPublisher) {
	t.Helper()
	h, _, _ := setupQR(t) // shared helper from qr_handler_test.go
	org := newFakeOrgSvc()
	pub := &capturingPublisher{}
	h = h.WithOrgService(org).WithPublisher(pub)
	return h, org, pub
}

// ── CreateSessionAuthed ─────────────────────────────────────────────────────

func TestQR_CreateSessionAuthed_JoinOrg_Happy(t *testing.T) {
	h, org, _ := setupQRWithOrg(t)
	const adminID, orgID int64 = 99, 42
	org.seedMember(orgID, adminID, "admin")

	body := map[string]any{
		"action": "join_org",
		"params": map[string]any{"org_id": orgID, "role": "member"},
	}
	c, w := postJSON(http.MethodPost, "/api/v2/qr/session/authed", body)
	c.Set("account_id", adminID)

	h.CreateSessionAuthed(c)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
	}
	resp := decode(t, w)
	if resp["action"] != "join_org" {
		t.Errorf("action = %v, want join_org", resp["action"])
	}
	if _, ok := resp["qr_payload"].(string); !ok {
		t.Error("qr_payload missing in response")
	}
	// The session's CreatedBy must be the admin — verifyable only through the
	// end-to-end confirm test below, but we can already assert the id shape.
	if id, _ := resp["id"].(string); len(id) != 64 {
		t.Errorf("id should be 64 hex chars; got %d: %q", len(id), id)
	}
}

func TestQR_CreateSessionAuthed_JoinOrg_NotOwner_403(t *testing.T) {
	h, org, _ := setupQRWithOrg(t)
	const randomUser, orgID int64 = 77, 42
	// Only register a plain member role — neither owner nor admin.
	org.seedMember(orgID, randomUser, "member")

	body := map[string]any{
		"action": "join_org",
		"params": map[string]any{"org_id": orgID},
	}
	c, w := postJSON(http.MethodPost, "/api/v2/qr/session/authed", body)
	c.Set("account_id", randomUser)
	h.CreateSessionAuthed(c)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d; want 403 (body=%s)", w.Code, w.Body.String())
	}
	if decode(t, w)["error"] != "forbidden" {
		t.Errorf("error = %v", decode(t, w)["error"])
	}
}

func TestQR_CreateSessionAuthed_JoinOrg_NotMember_403(t *testing.T) {
	h, _, _ := setupQRWithOrg(t)
	// No seeded membership at all — IsOwnerOrAdmin returns false, nil.
	body := map[string]any{
		"action": "join_org",
		"params": map[string]any{"org_id": int64(42)},
	}
	c, w := postJSON(http.MethodPost, "/api/v2/qr/session/authed", body)
	c.Set("account_id", int64(1234))
	h.CreateSessionAuthed(c)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d; want 403", w.Code)
	}
}

func TestQR_CreateSessionAuthed_Login_400(t *testing.T) {
	h, _, _ := setupQRWithOrg(t)
	c, w := postJSON(http.MethodPost, "/api/v2/qr/session/authed", map[string]any{"action": "login"})
	c.Set("account_id", int64(42))
	h.CreateSessionAuthed(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d; want 400", w.Code)
	}
	if decode(t, w)["error"] != "action_not_allowed_on_authed_endpoint" {
		t.Errorf("error = %v", decode(t, w)["error"])
	}
}

func TestQR_CreateSessionAuthed_MissingAuth_401(t *testing.T) {
	h, _, _ := setupQRWithOrg(t)
	c, w := postJSON(http.MethodPost, "/api/v2/qr/session/authed", map[string]any{"action": "join_org"})
	// Intentionally no account_id — requireAccountID must 401.
	h.CreateSessionAuthed(c)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d; want 401", w.Code)
	}
}

func TestQR_CreateSessionAuthed_JoinOrg_MissingOrgID_400(t *testing.T) {
	h, org, _ := setupQRWithOrg(t)
	org.seedMember(42, 99, "admin")
	body := map[string]any{
		"action": "join_org",
		"params": map[string]any{"role": "member"}, // no org_id
	}
	c, w := postJSON(http.MethodPost, "/api/v2/qr/session/authed", body)
	c.Set("account_id", int64(99))
	h.CreateSessionAuthed(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d; want 400", w.Code)
	}
	if decode(t, w)["error"] != "invalid_params" {
		t.Errorf("error = %v", decode(t, w)["error"])
	}
}

func TestQR_CreateSessionAuthed_Delegate_501(t *testing.T) {
	h, _, _ := setupQRWithOrg(t)
	body := map[string]any{
		"action": "delegate",
		"params": map[string]any{"scopes": []string{"read"}, "ttl_seconds": 600},
	}
	c, w := postJSON(http.MethodPost, "/api/v2/qr/session/authed", body)
	c.Set("account_id", int64(99))
	h.CreateSessionAuthed(c)
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d; want 501", w.Code)
	}
}

func TestQR_CreateSessionAuthed_InvalidAction_400(t *testing.T) {
	h, _, _ := setupQRWithOrg(t)
	c, w := postJSON(http.MethodPost, "/api/v2/qr/session/authed", map[string]any{"action": "hack"})
	c.Set("account_id", int64(99))
	h.CreateSessionAuthed(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d; want 400", w.Code)
	}
	if decode(t, w)["error"] != "invalid_action" {
		t.Errorf("error = %v", decode(t, w)["error"])
	}
}

// ── Confirm: join_org end-to-end ────────────────────────────────────────────

func TestQR_Confirm_JoinOrg_Success(t *testing.T) {
	h, org, pub := setupQRWithOrg(t)
	const adminID, orgID, scannerID int64 = 99, 42, 777
	org.seedMember(orgID, adminID, "admin")

	// 1. Authed create by the admin.
	createBody := map[string]any{
		"action": "join_org",
		"params": map[string]any{"org_id": orgID, "role": "member"},
	}
	cCreate, wCreate := postJSON(http.MethodPost, "/api/v2/qr/session/authed", createBody)
	cCreate.Set("account_id", adminID)
	h.CreateSessionAuthed(cCreate)
	if wCreate.Code != http.StatusOK {
		t.Fatalf("create failed: %d %s", wCreate.Code, wCreate.Body.String())
	}
	create := decode(t, wCreate)
	id := create["id"].(string)
	_, _, tStr, sig := parsePayload(t, create["qr_payload"].(string))
	issuedAt, _ := strconv.ParseInt(tStr, 10, 64)

	// 2. Scanner confirms — join_org side effect runs inline on Confirm, so
	//    the enriched response (org_id/role/joined_at) comes back here, not
	//    from a subsequent poll.
	cConfirm, wConfirm := postJSON(http.MethodPost, "/api/v2/qr/"+id+"/confirm",
		map[string]any{"sig": sig, "t": issuedAt},
		gin.Param{Key: "id", Value: id},
	)
	cConfirm.Set("account_id", scannerID)
	h.Confirm(cConfirm)
	if wConfirm.Code != http.StatusOK {
		t.Fatalf("confirm failed: %d %s", wConfirm.Code, wConfirm.Body.String())
	}
	confirmResp := decode(t, wConfirm)
	if confirmResp["action"] != "join_org" {
		t.Errorf("action = %v, want join_org", confirmResp["action"])
	}
	if _, hasToken := confirmResp["token"]; hasToken {
		t.Error("join_org must NOT issue a session token")
	}
	// org_id comes back as a JSON number (float64).
	if got, want := int64(confirmResp["org_id"].(float64)), orgID; got != want {
		t.Errorf("org_id = %d, want %d", got, want)
	}
	if confirmResp["role"] != "member" {
		t.Errorf("role = %v, want member", confirmResp["role"])
	}

	// 3. AddMember was called with the right triple.
	if org.lastAdd == nil {
		t.Fatal("AddMember was never called")
	}
	if got := org.lastAdd; got.OrgID != orgID || got.CallerID != adminID || got.TargetID != scannerID || got.Role != "member" {
		t.Errorf("AddMember(org=%d caller=%d target=%d role=%q); want (%d, %d, %d, %q)",
			got.OrgID, got.CallerID, got.TargetID, got.Role,
			orgID, adminID, scannerID, "member")
	}

	// 5. Event was published with the expected shape.
	ev := pub.last()
	if ev == nil {
		t.Fatal("no identity.org.member_joined event emitted")
	}
	if ev.EventType != event.SubjectOrgMemberJoined {
		t.Errorf("event_type = %q, want %q", ev.EventType, event.SubjectOrgMemberJoined)
	}
	var payload event.OrgMemberJoinedPayload
	if err := json.Unmarshal(ev.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.OrgID != orgID || payload.AccountID != scannerID || payload.Role != "member" {
		t.Errorf("payload = %+v; want org=%d account=%d role=member", payload, orgID, scannerID)
	}
	if !payload.ConfirmedViaQR {
		t.Error("ConfirmedViaQR should be true for QR-driven joins")
	}
	if payload.JoinedAt == "" {
		t.Error("JoinedAt should be populated")
	}
}

// TestQR_Confirm_JoinOrg_DefaultsRoleMember asserts that an omitted role in
// create params is defaulted to "member" at the authoritative confirm step —
// not left empty and silently stored as such.
func TestQR_Confirm_JoinOrg_DefaultsRoleMember(t *testing.T) {
	h, org, _ := setupQRWithOrg(t)
	const adminID, orgID, scannerID int64 = 99, 42, 777
	org.seedMember(orgID, adminID, "owner")

	createBody := map[string]any{
		"action": "join_org",
		"params": map[string]any{"org_id": orgID}, // no role
	}
	cCreate, wCreate := postJSON(http.MethodPost, "/api/v2/qr/session/authed", createBody)
	cCreate.Set("account_id", adminID)
	h.CreateSessionAuthed(cCreate)
	if wCreate.Code != http.StatusOK {
		t.Fatalf("create failed: %d %s", wCreate.Code, wCreate.Body.String())
	}
	create := decode(t, wCreate)
	id := create["id"].(string)
	_, _, tStr, sig := parsePayload(t, create["qr_payload"].(string))
	issuedAt, _ := strconv.ParseInt(tStr, 10, 64)

	cConfirm, wConfirm := postJSON(http.MethodPost, "/api/v2/qr/"+id+"/confirm",
		map[string]any{"sig": sig, "t": issuedAt},
		gin.Param{Key: "id", Value: id},
	)
	cConfirm.Set("account_id", scannerID)
	h.Confirm(cConfirm)
	if wConfirm.Code != http.StatusOK {
		t.Fatalf("confirm failed: %d %s", wConfirm.Code, wConfirm.Body.String())
	}

	if org.lastAdd == nil || org.lastAdd.Role != "member" {
		t.Errorf("lastAdd.Role = %v, want member", org.lastAdd)
	}
}

// TestQR_Confirm_JoinOrg_AddMemberFails_Forbidden exercises the failure path:
// if OrganizationService.AddMember rejects the call (e.g. initiator is no
// longer owner/admin by the time the scanner confirms), the APP must see 403
// rather than a spurious 500.
func TestQR_Confirm_JoinOrg_AddMemberFails_Forbidden(t *testing.T) {
	h, org, _ := setupQRWithOrg(t)
	const adminID, orgID, scannerID int64 = 99, 42, 777
	org.seedMember(orgID, adminID, "admin")

	// Inject a failure so AddMember rejects after the session is minted.
	org.addMemberErr = fmt.Errorf("permission denied: stripped mid-flight")

	createBody := map[string]any{
		"action": "join_org",
		"params": map[string]any{"org_id": orgID, "role": "member"},
	}
	cCreate, wCreate := postJSON(http.MethodPost, "/api/v2/qr/session/authed", createBody)
	cCreate.Set("account_id", adminID)
	h.CreateSessionAuthed(cCreate)
	if wCreate.Code != http.StatusOK {
		t.Fatalf("create failed: %d %s", wCreate.Code, wCreate.Body.String())
	}
	create := decode(t, wCreate)
	id := create["id"].(string)
	_, _, tStr, sig := parsePayload(t, create["qr_payload"].(string))
	issuedAt, _ := strconv.ParseInt(tStr, 10, 64)

	// Confirm runs AddMember inline for join_org; when AddMember fails the
	// Confirm response itself must surface 403 (no separate poll needed).
	cConfirm, wConfirm := postJSON(http.MethodPost, "/api/v2/qr/"+id+"/confirm",
		map[string]any{"sig": sig, "t": issuedAt},
		gin.Param{Key: "id", Value: id},
	)
	cConfirm.Set("account_id", scannerID)
	h.Confirm(cConfirm)
	if wConfirm.Code != http.StatusForbidden {
		t.Fatalf("confirm (AddMember failed) = %d; want 403 (body=%s)", wConfirm.Code, wConfirm.Body.String())
	}
}
