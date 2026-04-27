package handler_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/hanmahong5-arch/lurus-platform/internal/adapter/handler"
)

// ── Fakes ───────────────────────────────────────────────────────────────────

// fakeDelegateExec captures every ExecuteDelegate call and lets tests
// inject a synthetic error path. Kept private to this file to mirror
// the join_org test's fakeOrgSvc style.
type fakeDelegateExec struct {
	mu          sync.Mutex
	supported   []string
	executeErr  error
	lastOp      string
	lastApp     string
	lastEnv     string
	lastCaller  int64
	executeHits int
}

func newFakeDelegateExec(ops ...string) *fakeDelegateExec {
	if len(ops) == 0 {
		ops = []string{"delete_oidc_app"}
	}
	cp := make([]string, len(ops))
	copy(cp, ops)
	return &fakeDelegateExec{supported: cp}
}

func (f *fakeDelegateExec) SupportedOps() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.supported))
	copy(out, f.supported)
	return out
}

func (f *fakeDelegateExec) ExecuteDelegate(_ context.Context, params handler.QRDelegateParams, callerID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.executeHits++
	f.lastOp = params.Op
	f.lastApp = params.AppName
	f.lastEnv = params.Env
	f.lastCaller = callerID
	return f.executeErr
}

// setupQRWithDelegate wires a QRHandler with a delegate executor in
// addition to the miniredis backend from setupQR.
func setupQRWithDelegate(t *testing.T, ops ...string) (*handler.QRHandler, *fakeDelegateExec) {
	t.Helper()
	h, _, _ := setupQR(t)
	exec := newFakeDelegateExec(ops...)
	h = h.WithDelegateExecutor(exec)
	return h, exec
}

// ── CreateSessionAuthed: delegate happy + error paths ───────────────────────

func TestQR_CreateSessionAuthed_Delegate_Happy(t *testing.T) {
	h, _ := setupQRWithDelegate(t)
	const adminID int64 = 99

	body := map[string]any{
		"action": "delegate",
		"params": map[string]any{
			"op":       "delete_oidc_app",
			"app_name": "tally",
			"env":      "stage",
		},
	}
	c, w := postJSON(http.MethodPost, "/api/v2/qr/session/authed", body)
	c.Set("account_id", adminID)
	h.CreateSessionAuthed(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d (body=%s)", w.Code, w.Body.String())
	}
	resp := decode(t, w)
	if resp["action"] != "delegate" {
		t.Errorf("action = %v, want delegate", resp["action"])
	}
	if id, _ := resp["id"].(string); len(id) != 64 {
		t.Errorf("id len = %d, want 64", len(id))
	}
	if _, ok := resp["qr_payload"].(string); !ok {
		t.Error("qr_payload missing")
	}
}

func TestQR_CreateSessionAuthed_Delegate_NotWired_501(t *testing.T) {
	// No WithDelegateExecutor — handler should still gate at 501.
	h, _, _ := setupQR(t)
	body := map[string]any{
		"action": "delegate",
		"params": map[string]any{"op": "delete_oidc_app", "app_name": "tally", "env": "stage"},
	}
	c, w := postJSON(http.MethodPost, "/api/v2/qr/session/authed", body)
	c.Set("account_id", int64(99))
	h.CreateSessionAuthed(c)

	if w.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d; want 501", w.Code)
	}
}

func TestQR_CreateSessionAuthed_Delegate_UnknownOp_400(t *testing.T) {
	h, _ := setupQRWithDelegate(t) // executor only supports delete_oidc_app
	body := map[string]any{
		"action": "delegate",
		"params": map[string]any{"op": "drop_database", "app_name": "tally", "env": "stage"},
	}
	c, w := postJSON(http.MethodPost, "/api/v2/qr/session/authed", body)
	c.Set("account_id", int64(99))
	h.CreateSessionAuthed(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d; want 400 (body=%s)", w.Code, w.Body.String())
	}
	if got := decode(t, w)["error"]; got != "invalid_op" {
		t.Errorf("error = %v; want invalid_op", got)
	}
}

func TestQR_CreateSessionAuthed_Delegate_MissingFields_400(t *testing.T) {
	cases := []struct {
		name   string
		params map[string]any
	}{
		{"missing op", map[string]any{"app_name": "tally", "env": "stage"}},
		{"missing app_name", map[string]any{"op": "delete_oidc_app", "env": "stage"}},
		{"missing env", map[string]any{"op": "delete_oidc_app", "app_name": "tally"}},
		{"empty op", map[string]any{"op": "  ", "app_name": "tally", "env": "stage"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := setupQRWithDelegate(t)
			body := map[string]any{"action": "delegate", "params": tc.params}
			c, w := postJSON(http.MethodPost, "/api/v2/qr/session/authed", body)
			c.Set("account_id", int64(99))
			h.CreateSessionAuthed(c)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d; want 400 (body=%s)", w.Code, w.Body.String())
			}
			if got := decode(t, w)["error"]; got != "invalid_params" {
				t.Errorf("error = %v; want invalid_params", got)
			}
		})
	}
}

// ── Confirm: delegate end-to-end ────────────────────────────────────────────

func TestQR_Confirm_Delegate_DeleteApp_Success(t *testing.T) {
	h, exec := setupQRWithDelegate(t)
	const adminID, scannerID int64 = 99, 777

	createBody := map[string]any{
		"action": "delegate",
		"params": map[string]any{
			"op":       "delete_oidc_app",
			"app_name": "tally",
			"env":      "stage",
		},
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
		t.Fatalf("confirm = %d (body=%s)", wConfirm.Code, wConfirm.Body.String())
	}
	resp := decode(t, wConfirm)
	if resp["action"] != "delegate" {
		t.Errorf("action = %v, want delegate", resp["action"])
	}
	if resp["op"] != "delete_oidc_app" {
		t.Errorf("op = %v, want delete_oidc_app", resp["op"])
	}
	if resp["app"] != "tally" || resp["env"] != "stage" {
		t.Errorf("app/env = %v/%v, want tally/stage", resp["app"], resp["env"])
	}

	// Executor must have been invoked with the right inputs and the
	// initiator id (not the scanner) — the boss authorises, the
	// scanner just confirms presence.
	if exec.executeHits != 1 {
		t.Fatalf("ExecuteDelegate hits = %d, want 1", exec.executeHits)
	}
	if exec.lastOp != "delete_oidc_app" || exec.lastApp != "tally" || exec.lastEnv != "stage" {
		t.Errorf("exec last = (op=%q app=%q env=%q); want (delete_oidc_app, tally, stage)",
			exec.lastOp, exec.lastApp, exec.lastEnv)
	}
	if exec.lastCaller != adminID {
		t.Errorf("exec caller = %d, want %d", exec.lastCaller, adminID)
	}
}

func TestQR_Confirm_Delegate_ExecutorFailure_500(t *testing.T) {
	h, exec := setupQRWithDelegate(t)
	exec.executeErr = errors.New("zitadel: 500 internal")
	const adminID, scannerID int64 = 99, 777

	createBody := map[string]any{
		"action": "delegate",
		"params": map[string]any{"op": "delete_oidc_app", "app_name": "tally", "env": "stage"},
	}
	cCreate, wCreate := postJSON(http.MethodPost, "/api/v2/qr/session/authed", createBody)
	cCreate.Set("account_id", adminID)
	h.CreateSessionAuthed(cCreate)
	id := decode(t, wCreate)["id"].(string)
	_, _, tStr, sig := parsePayload(t, decode(t, wCreate)["qr_payload"].(string))
	issuedAt, _ := strconv.ParseInt(tStr, 10, 64)

	cConfirm, wConfirm := postJSON(http.MethodPost, "/api/v2/qr/"+id+"/confirm",
		map[string]any{"sig": sig, "t": issuedAt},
		gin.Param{Key: "id", Value: id},
	)
	cConfirm.Set("account_id", scannerID)
	h.Confirm(cConfirm)

	if wConfirm.Code != http.StatusInternalServerError {
		t.Fatalf("confirm = %d; want 500 (body=%s)", wConfirm.Code, wConfirm.Body.String())
	}
	if got := decode(t, wConfirm)["error"]; got != "delegate_failed" {
		t.Errorf("error = %v; want delegate_failed", got)
	}
}

func TestQR_Confirm_Delegate_UnsupportedOpFromExecutor_400(t *testing.T) {
	h, exec := setupQRWithDelegate(t, "delete_oidc_app", "rotate_secret")
	// Mint via the rotate_secret op (whitelisted), then have the
	// executor reject it as if a deployment swap removed support
	// between create and confirm.
	exec.executeErr = fmt.Errorf("%w: rotate_secret", handler.ErrUnsupportedDelegateOp)
	const adminID, scannerID int64 = 99, 777

	createBody := map[string]any{
		"action": "delegate",
		"params": map[string]any{"op": "rotate_secret", "app_name": "tally", "env": "stage"},
	}
	cCreate, wCreate := postJSON(http.MethodPost, "/api/v2/qr/session/authed", createBody)
	cCreate.Set("account_id", adminID)
	h.CreateSessionAuthed(cCreate)
	id := decode(t, wCreate)["id"].(string)
	_, _, tStr, sig := parsePayload(t, decode(t, wCreate)["qr_payload"].(string))
	issuedAt, _ := strconv.ParseInt(tStr, 10, 64)

	cConfirm, wConfirm := postJSON(http.MethodPost, "/api/v2/qr/"+id+"/confirm",
		map[string]any{"sig": sig, "t": issuedAt},
		gin.Param{Key: "id", Value: id},
	)
	cConfirm.Set("account_id", scannerID)
	h.Confirm(cConfirm)

	if wConfirm.Code != http.StatusBadRequest {
		t.Fatalf("confirm = %d; want 400", wConfirm.Code)
	}
	if got := decode(t, wConfirm)["error"]; got != "invalid_op" {
		t.Errorf("error = %v; want invalid_op", got)
	}
}

func TestQR_Confirm_Delegate_MissingAuth_401(t *testing.T) {
	h, _ := setupQRWithDelegate(t)
	const adminID int64 = 99

	createBody := map[string]any{
		"action": "delegate",
		"params": map[string]any{"op": "delete_oidc_app", "app_name": "tally", "env": "stage"},
	}
	cCreate, wCreate := postJSON(http.MethodPost, "/api/v2/qr/session/authed", createBody)
	cCreate.Set("account_id", adminID)
	h.CreateSessionAuthed(cCreate)
	id := decode(t, wCreate)["id"].(string)
	_, _, tStr, sig := parsePayload(t, decode(t, wCreate)["qr_payload"].(string))
	issuedAt, _ := strconv.ParseInt(tStr, 10, 64)

	cConfirm, wConfirm := postJSON(http.MethodPost, "/api/v2/qr/"+id+"/confirm",
		map[string]any{"sig": sig, "t": issuedAt},
		gin.Param{Key: "id", Value: id},
	)
	// Intentionally no account_id — require 401.
	h.Confirm(cConfirm)

	if wConfirm.Code != http.StatusUnauthorized {
		t.Fatalf("confirm = %d; want 401", wConfirm.Code)
	}
}

// TestQR_Confirm_Delegate_PollingReturnsOpInfo verifies that the Web
// initiator (the admin UI that minted the QR) gets a 200 with op
// metadata when polling /status after the boss confirmed on his
// APP. Two invariants this test guards:
//
//   - The response MUST NOT include a session token. Delegate
//     sessions are not login flows; smuggling a token here would
//     let any party that scrapes the admin UI's network log
//     impersonate the scanner.
//
//   - The response MUST include the op type so the Web UI knows
//     what data view to refresh ("the boss approved the refund
//     RF-001"). Returning an opaque "confirmed" without op
//     metadata would force the Web UI into per-page bookkeeping.
func TestQR_Confirm_Delegate_PollingReturnsOpInfo(t *testing.T) {
	h, _ := setupQRWithDelegate(t)
	const adminID, scannerID int64 = 99, 777

	createBody := map[string]any{
		"action": "delegate",
		"params": map[string]any{"op": "delete_oidc_app", "app_name": "tally", "env": "stage"},
	}
	cCreate, wCreate := postJSON(http.MethodPost, "/api/v2/qr/session/authed", createBody)
	cCreate.Set("account_id", adminID)
	h.CreateSessionAuthed(cCreate)
	id := decode(t, wCreate)["id"].(string)
	_, _, tStr, sig := parsePayload(t, decode(t, wCreate)["qr_payload"].(string))
	issuedAt, _ := strconv.ParseInt(tStr, 10, 64)

	// Confirm runs — flips to confirmed.
	cConfirm, wConfirm := postJSON(http.MethodPost, "/api/v2/qr/"+id+"/confirm",
		map[string]any{"sig": sig, "t": issuedAt},
		gin.Param{Key: "id", Value: id},
	)
	cConfirm.Set("account_id", scannerID)
	h.Confirm(cConfirm)
	if wConfirm.Code != http.StatusOK {
		t.Fatalf("confirm = %d (body=%s)", wConfirm.Code, wConfirm.Body.String())
	}

	// Web poller arrives after confirm — should receive 200 + op
	// metadata so it can refresh the apps list.
	cPoll, wPoll := postJSON(http.MethodGet, "/", nil, gin.Param{Key: "id", Value: id})
	cPoll.Params = gin.Params{{Key: "id", Value: id}}
	h.PollStatus(cPoll)

	// Acceptable: 200 (we beat any concurrent poller to consume) OR
	// 410 (some other poller consumed first — also a successful
	// terminal state).
	if wPoll.Code != http.StatusOK && wPoll.Code != http.StatusGone {
		t.Fatalf("poll = %d (body=%s); want 200 or 410", wPoll.Code, wPoll.Body.String())
	}

	if wPoll.Code == http.StatusOK {
		body := decode(t, wPoll)
		// HARD invariant: never a token on a delegate session.
		if _, hasToken := body["token"]; hasToken {
			t.Errorf("poll body must NOT include a token; got %v", body)
		}
		if body["status"] != "confirmed" {
			t.Errorf("status = %v; want confirmed", body["status"])
		}
		if body["op"] != "delete_oidc_app" {
			t.Errorf("op = %v; want delete_oidc_app", body["op"])
		}
		if body["app"] != "tally" {
			t.Errorf("app = %v; want tally", body["app"])
		}
	}
}

// ── CreateDelegateSession (server-side helper) ──────────────────────────────

func TestQR_CreateDelegateSession_Happy(t *testing.T) {
	h, _ := setupQRWithDelegate(t)
	got, err := h.CreateDelegateSession(context.Background(), 42, "delete_oidc_app", "tally", "stage")
	if err != nil {
		t.Fatalf("CreateDelegateSession: %v", err)
	}
	if len(got.ID) != 64 {
		t.Errorf("ID len = %d, want 64", len(got.ID))
	}
	if got.QRPayload == "" {
		t.Error("QRPayload empty")
	}
	if got.ExpiresIn != 300 {
		t.Errorf("ExpiresIn = %d, want 300", got.ExpiresIn)
	}
}

func TestQR_CreateDelegateSession_NotWired_Errors(t *testing.T) {
	h, _, _ := setupQR(t)
	_, err := h.CreateDelegateSession(context.Background(), 42, "delete_oidc_app", "tally", "stage")
	if err == nil {
		t.Fatal("expected error when executor not wired")
	}
}

func TestQR_CreateDelegateSession_UnsupportedOp_Errors(t *testing.T) {
	h, _ := setupQRWithDelegate(t) // only delete_oidc_app
	_, err := h.CreateDelegateSession(context.Background(), 42, "drop_database", "tally", "stage")
	if err == nil || !errors.Is(err, handler.ErrUnsupportedDelegateOp) {
		t.Fatalf("err = %v; want ErrUnsupportedDelegateOp", err)
	}
}

func TestQR_CreateDelegateSession_ZeroCaller_Errors(t *testing.T) {
	h, _ := setupQRWithDelegate(t)
	_, err := h.CreateDelegateSession(context.Background(), 0, "delete_oidc_app", "tally", "stage")
	if err == nil {
		t.Fatal("expected error for zero callerID")
	}
}
