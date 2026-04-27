package handler_test

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/hanmahong5-arch/lurus-platform/internal/adapter/handler"
	"github.com/hanmahong5-arch/lurus-platform/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// fakeAccountPurger implements handler.AccountPurgeOrchestrator. The
// executor depends on this interface (not the concrete
// *app.AccountService), so unit tests stay DB-free.
type fakeAccountPurger struct {
	mu sync.Mutex

	accounts        map[int64]*entity.Account
	purges          map[int64]*entity.AccountPurge
	nextPurgeID     int64
	failBegin       bool // simulates store error path
	beginErr        error
	failFinish      bool
	finishErr       error
	beginCalls      int
	finishCalls     int
	lastFinishReq   app.FinishPurgeRequest
}

func newFakeAccountPurger() *fakeAccountPurger {
	return &fakeAccountPurger{
		accounts:    map[int64]*entity.Account{},
		purges:      map[int64]*entity.AccountPurge{},
		nextPurgeID: 1,
	}
}

func (f *fakeAccountPurger) addAccount(a *entity.Account) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *a
	f.accounts[cp.ID] = &cp
}

func (f *fakeAccountPurger) BeginPurge(_ context.Context, req app.PurgeBeginRequest) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.beginCalls++
	if f.failBegin {
		return 0, f.beginErr
	}
	a, ok := f.accounts[req.AccountID]
	if !ok {
		return 0, errors.New("account not found")
	}
	if a.Status == entity.AccountStatusDeleted {
		return 0, app.ErrAccountAlreadyPurged
	}
	for _, row := range f.purges {
		if row.AccountID == req.AccountID && row.Status == entity.AccountPurgeStatusInflight {
			return 0, app.ErrPurgeInFlight
		}
	}
	id := f.nextPurgeID
	f.nextPurgeID++
	f.purges[id] = &entity.AccountPurge{
		ID:          id,
		AccountID:   req.AccountID,
		InitiatedBy: req.InitiatedBy,
		Status:      entity.AccountPurgeStatusInflight,
	}
	return id, nil
}

func (f *fakeAccountPurger) FinishPurge(_ context.Context, req app.FinishPurgeRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.finishCalls++
	f.lastFinishReq = req
	if f.failFinish {
		return f.finishErr
	}
	row, ok := f.purges[req.PurgeID]
	if !ok {
		return errors.New("audit row not found")
	}
	if !req.Success {
		row.Status = entity.AccountPurgeStatusFailed
		row.Error = req.ErrMsg
		return nil
	}
	a := f.accounts[req.AccountID]
	if a != nil {
		a.Status = entity.AccountStatusDeleted
	}
	row.Status = entity.AccountPurgeStatusCompleted
	approver := req.ApprovedBy
	row.ApprovedBy = &approver
	return nil
}

func (f *fakeAccountPurger) GetByID(_ context.Context, id int64) (*entity.Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.accounts[id]
	if !ok {
		return nil, nil
	}
	cp := *a
	return &cp, nil
}

func (f *fakeAccountPurger) snapshot() (purges []*entity.AccountPurge, beginCalls, finishCalls int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*entity.AccountPurge, 0, len(f.purges))
	for _, p := range f.purges {
		cp := *p
		out = append(out, &cp)
	}
	return out, f.beginCalls, f.finishCalls
}

// ── Tests ───────────────────────────────────────────────────────────────────

func TestAccountDeleteExecutor_SupportedOps_Delete(t *testing.T) {
	exec := handler.NewAccountDeleteExecutor(nil, nil, nil, nil)
	got := exec.SupportedOps()
	if len(got) != 1 || got[0] != "delete_account" {
		t.Errorf("SupportedOps = %v; want [delete_account]", got)
	}
}

func TestAccountDeleteExecutor_WrongOp_ReturnsUnsupported(t *testing.T) {
	purger := newFakeAccountPurger()
	exec := handler.NewAccountDeleteExecutor(purger, nil, nil, nil)
	err := exec.ExecuteDelegate(context.Background(), handler.QRDelegateParams{
		Op: "delete_oidc_app", AppName: "x", Env: "y",
	}, 1)
	if !errors.Is(err, handler.ErrUnsupportedDelegateOp) {
		t.Fatalf("err = %v; want ErrUnsupportedDelegateOp", err)
	}
}

func TestAccountDeleteExecutor_MissingAccountID_Errors(t *testing.T) {
	purger := newFakeAccountPurger()
	exec := handler.NewAccountDeleteExecutor(purger, nil, nil, nil)
	err := exec.ExecuteDelegate(context.Background(), handler.QRDelegateParams{
		Op: "delete_account", AccountID: 0,
	}, 1)
	if err == nil {
		t.Fatal("expected error for missing account_id")
	}
}

func TestAccountDeleteExecutor_AccountsNotWired_Errors(t *testing.T) {
	exec := handler.NewAccountDeleteExecutor(nil, nil, nil, nil)
	err := exec.ExecuteDelegate(context.Background(), handler.QRDelegateParams{
		Op: "delete_account", AccountID: 7,
	}, 1)
	if err == nil {
		t.Fatal("expected error when accounts service unwired")
	}
}

func TestAccountDeleteExecutor_HappyPath_AuditCompleted(t *testing.T) {
	purger := newFakeAccountPurger()
	const targetID, approver int64 = 11, 42
	purger.addAccount(&entity.Account{ID: targetID, Status: entity.AccountStatusActive})
	exec := handler.NewAccountDeleteExecutor(purger, nil, nil, nil)

	err := exec.ExecuteDelegate(context.Background(), handler.QRDelegateParams{
		Op: "delete_account", AccountID: targetID,
	}, approver)
	if err != nil {
		t.Fatalf("ExecuteDelegate: %v", err)
	}
	rows, beginCalls, finishCalls := purger.snapshot()
	if beginCalls != 1 || finishCalls != 1 {
		t.Errorf("calls = (begin=%d, finish=%d); want (1,1)", beginCalls, finishCalls)
	}
	if len(rows) != 1 {
		t.Fatalf("audit rows = %d; want 1", len(rows))
	}
	row := rows[0]
	if row.Status != entity.AccountPurgeStatusCompleted {
		t.Errorf("status = %q; want completed", row.Status)
	}
	if row.AccountID != targetID {
		t.Errorf("account_id = %d; want %d", row.AccountID, targetID)
	}
	if row.ApprovedBy == nil || *row.ApprovedBy != approver {
		t.Errorf("approved_by = %v; want %d", row.ApprovedBy, approver)
	}
}

func TestAccountDeleteExecutor_AlreadyPurged_NoOp(t *testing.T) {
	purger := newFakeAccountPurger()
	const targetID int64 = 22
	// Pre-deleted: BeginPurge will return ErrAccountAlreadyPurged.
	purger.addAccount(&entity.Account{ID: targetID, Status: entity.AccountStatusDeleted})
	exec := handler.NewAccountDeleteExecutor(purger, nil, nil, nil)

	err := exec.ExecuteDelegate(context.Background(), handler.QRDelegateParams{
		Op: "delete_account", AccountID: targetID,
	}, 1)
	if err != nil {
		t.Fatalf("expected idempotent success, got: %v", err)
	}
	rows, beginCalls, finishCalls := purger.snapshot()
	if beginCalls != 1 {
		t.Errorf("beginCalls = %d; want 1", beginCalls)
	}
	if finishCalls != 0 {
		t.Errorf("finishCalls = %d; want 0 (no row to finish on idempotent)", finishCalls)
	}
	if len(rows) != 0 {
		t.Errorf("audit rows = %d; want 0 (no work was done)", len(rows))
	}
}

func TestAccountDeleteExecutor_InFlightConcurrent_Returns409Like(t *testing.T) {
	purger := newFakeAccountPurger()
	const targetID int64 = 33
	purger.addAccount(&entity.Account{ID: targetID, Status: entity.AccountStatusActive})
	// Pre-seed an in-flight row to simulate "another admin started this".
	purger.purges[purger.nextPurgeID] = &entity.AccountPurge{
		ID:        purger.nextPurgeID,
		AccountID: targetID,
		Status:    entity.AccountPurgeStatusInflight,
	}
	purger.nextPurgeID++

	exec := handler.NewAccountDeleteExecutor(purger, nil, nil, nil)
	err := exec.ExecuteDelegate(context.Background(), handler.QRDelegateParams{
		Op: "delete_account", AccountID: targetID,
	}, 1)
	if err == nil {
		t.Fatal("expected error for concurrent in-flight purge")
	}
	if !errors.Is(err, app.ErrPurgeInFlight) {
		t.Errorf("err = %v; want chain to ErrPurgeInFlight", err)
	}
}

// E2E: confirm the executor is reachable through QRHandler's
// multi-executor dispatch — exercises the full CreateDelegateSession
// → Confirm → cascade chain via the existing test harness.
func TestAccountDeleteExecutor_E2EViaConfirm(t *testing.T) {
	purger := newFakeAccountPurger()
	const targetID int64 = 4521
	purger.addAccount(&entity.Account{ID: targetID, Status: entity.AccountStatusActive})
	exec := handler.NewAccountDeleteExecutor(purger, nil, nil, nil)

	h, _, _ := setupQR(t)
	// Stack two executors: existing apps_admin fake + new account exec.
	// Multi-executor dispatch must route delete_account to the right one.
	h = h.WithDelegateExecutor(newFakeDelegateExec()).WithDelegateExecutor(exec)
	const adminID, scannerID int64 = 99, 777

	got, err := h.CreateDelegateSessionWithParams(context.Background(), adminID, handler.QRDelegateParams{
		Op:        "delete_account",
		AccountID: targetID,
	})
	if err != nil {
		t.Fatalf("CreateDelegateSession: %v", err)
	}
	_, _, tStr, sig := parsePayload(t, got.QRPayload)
	issuedAt, _ := strconv.ParseInt(tStr, 10, 64)

	c, w := postJSON(http.MethodPost, "/api/v2/qr/"+got.ID+"/confirm",
		map[string]any{"sig": sig, "t": issuedAt},
		gin.Param{Key: "id", Value: got.ID},
	)
	c.Set("account_id", scannerID)
	h.Confirm(c)

	if w.Code != http.StatusOK {
		t.Fatalf("confirm = %d (body=%s)", w.Code, w.Body.String())
	}
	resp := decode(t, w)
	if resp["op"] != "delete_account" {
		t.Errorf("op = %v; want delete_account", resp["op"])
	}
	gotID, ok := resp["account_id"].(float64)
	if !ok || int64(gotID) != targetID {
		t.Errorf("account_id = %v; want %d", resp["account_id"], targetID)
	}

	rows, _, finishCalls := purger.snapshot()
	if finishCalls != 1 {
		t.Errorf("finishCalls = %d; want 1", finishCalls)
	}
	if len(rows) != 1 || rows[0].Status != entity.AccountPurgeStatusCompleted {
		t.Errorf("audit rows = %v; want 1 completed", rows)
	}
}
