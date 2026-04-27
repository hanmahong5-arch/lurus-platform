package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/hanmahong5-arch/lurus-platform/internal/adapter/handler"
	"github.com/hanmahong5-arch/lurus-platform/internal/module/ops"
)

// fakeRefundApprover captures the Approve call so tests can verify
// the executor wired the right reviewer / refund_no without spinning
// up a real RefundService.
type fakeRefundApprover struct {
	mu          sync.Mutex
	calls       int
	lastRefund  string
	lastReview  string
	lastNote    string
	returnErr   error
}

func (f *fakeRefundApprover) Approve(_ context.Context, refundNo, reviewerID, reviewNote string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.lastRefund = refundNo
	f.lastReview = reviewerID
	f.lastNote = reviewNote
	return f.returnErr
}

// snapshot reads the last captured Approve call under lock.
func (f *fakeRefundApprover) snapshot() (calls int, refund, reviewer, note string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls, f.lastRefund, f.lastReview, f.lastNote
}

// ── ExecuteDelegate / SupportedOps / metadata ──────────────────────────────

func TestRefundApprove_SupportedOps(t *testing.T) {
	exec := handler.NewRefundApproveExecutor(nil)
	got := exec.SupportedOps()
	if len(got) != 1 || got[0] != "approve_refund" {
		t.Errorf("SupportedOps = %v; want [approve_refund]", got)
	}
}

func TestRefundApprove_Metadata(t *testing.T) {
	exec := handler.NewRefundApproveExecutor(nil)
	if exec.Type() != "approve_refund" {
		t.Errorf("Type = %q", exec.Type())
	}
	if exec.RiskLevel() != ops.RiskWarn {
		t.Errorf("RiskLevel = %q; want warn", exec.RiskLevel())
	}
	if exec.IsDestructive() {
		t.Error("IsDestructive = true; want false (refund approval is reversible)")
	}
	if exec.Description() == "" {
		t.Error("Description should be non-empty")
	}
}

func TestRefundApprove_WrongOp_ReturnsUnsupported(t *testing.T) {
	exec := handler.NewRefundApproveExecutor(&fakeRefundApprover{})
	err := exec.ExecuteDelegate(context.Background(), handler.QRDelegateParams{
		Op: "delete_oidc_app", AppName: "x", Env: "y",
	}, 1)
	if !errors.Is(err, handler.ErrUnsupportedDelegateOp) {
		t.Fatalf("err = %v; want ErrUnsupportedDelegateOp", err)
	}
}

func TestRefundApprove_MissingRefundNo_Errors(t *testing.T) {
	exec := handler.NewRefundApproveExecutor(&fakeRefundApprover{})
	err := exec.ExecuteDelegate(context.Background(), handler.QRDelegateParams{
		Op: "approve_refund", RefundNo: "",
	}, 1)
	if err == nil {
		t.Fatal("expected error for missing refund_no")
	}
}

func TestRefundApprove_NotWired_Errors(t *testing.T) {
	exec := handler.NewRefundApproveExecutor(nil)
	err := exec.ExecuteDelegate(context.Background(), handler.QRDelegateParams{
		Op: "approve_refund", RefundNo: "RF-1",
	}, 1)
	if err == nil {
		t.Fatal("expected error when refunds service unwired")
	}
}

func TestRefundApprove_HappyPath_RecordsBossAsReviewer(t *testing.T) {
	approver := &fakeRefundApprover{}
	exec := handler.NewRefundApproveExecutor(approver)
	const callerID int64 = 99

	err := exec.ExecuteDelegate(context.Background(), handler.QRDelegateParams{
		Op: "approve_refund", RefundNo: "RF-2026-0001",
	}, callerID)
	if err != nil {
		t.Fatalf("ExecuteDelegate: %v", err)
	}
	calls, refundNo, reviewer, note := approver.snapshot()
	if calls != 1 {
		t.Errorf("Approve calls = %d; want 1", calls)
	}
	if refundNo != "RF-2026-0001" {
		t.Errorf("refundNo = %q; want RF-2026-0001", refundNo)
	}
	// callerID gets stringified — boss's account id is recorded as
	// the reviewer so the audit trail captures who really signed off.
	if reviewer != strconv.FormatInt(callerID, 10) {
		t.Errorf("reviewer = %q; want %q", reviewer, strconv.FormatInt(callerID, 10))
	}
	if note == "" {
		t.Error("review note should be non-empty (audit trail)")
	}
}

func TestRefundApprove_ApproveError_Propagates(t *testing.T) {
	approveErr := errors.New("refund is no longer in pending state")
	approver := &fakeRefundApprover{returnErr: approveErr}
	exec := handler.NewRefundApproveExecutor(approver)

	err := exec.ExecuteDelegate(context.Background(), handler.QRDelegateParams{
		Op: "approve_refund", RefundNo: "RF-X",
	}, 1)
	if err == nil {
		t.Fatal("expected error to propagate")
	}
	if !errors.Is(err, approveErr) {
		t.Errorf("err = %v; want chain to %v", err, approveErr)
	}
}

// E2E confirms the executor runs through the QR confirm path and
// the response includes refund_no — proves the end-to-end shape
// matches what the admin Web UI's poll-result handler will see.
func TestRefundApprove_E2EViaConfirm(t *testing.T) {
	approver := &fakeRefundApprover{}
	exec := handler.NewRefundApproveExecutor(approver)

	h, _, _ := setupQR(t)
	h = h.WithDelegateExecutor(exec)
	const adminID, scannerID int64 = 88, 777

	got, err := h.CreateDelegateSessionWithParams(context.Background(), adminID, handler.QRDelegateParams{
		Op:       "approve_refund",
		RefundNo: "RF-E2E-001",
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
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v body=%s", err, w.Body.String())
	}
	if resp["op"] != "approve_refund" {
		t.Errorf("op = %v; want approve_refund", resp["op"])
	}
	if resp["refund_no"] != "RF-E2E-001" {
		t.Errorf("refund_no = %v; want RF-E2E-001", resp["refund_no"])
	}
	calls, _, _, _ := approver.snapshot()
	if calls != 1 {
		t.Errorf("Approve calls = %d; want 1", calls)
	}
}

// ── AdminQRApprove handler ─────────────────────────────────────────────────

func TestAdminQRApprove_NotWired_Returns501(t *testing.T) {
	// No WithQRApprove → qr is nil.
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/v1/refunds/RF-X/qr-approve", nil)
	c.Set("account_id", int64(1))
	c.Params = gin.Params{{Key: "refund_no", Value: "RF-X"}}

	h := handler.NewRefundHandler(nil) // safe — endpoint short-circuits before touching refunds
	h.AdminQRApprove(c)
	if w.Code != http.StatusNotImplemented {
		t.Errorf("status = %d; want 501", w.Code)
	}
}

func TestAdminQRApprove_HappyPath_ReturnsQR(t *testing.T) {
	qrH, _, _ := setupQR(t)
	qrH = qrH.WithDelegateExecutor(handler.NewRefundApproveExecutor(&fakeRefundApprover{}))
	h := handler.NewRefundHandler(nil).WithQRApprove(qrH)

	c, w := postJSON(http.MethodPost, "/admin/v1/refunds/RF-001/qr-approve", nil,
		gin.Param{Key: "refund_no", Value: "RF-001"},
	)
	c.Set("account_id", int64(42))
	h.AdminQRApprove(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["refund_no"] != "RF-001" {
		t.Errorf("refund_no = %v; want RF-001", resp["refund_no"])
	}
	if resp["qr_payload"] == nil || resp["qr_payload"] == "" {
		t.Error("qr_payload should be present and non-empty")
	}
}

func TestAdminQRApprove_MissingRefundNo_Returns400(t *testing.T) {
	qrH, _, _ := setupQR(t)
	qrH = qrH.WithDelegateExecutor(handler.NewRefundApproveExecutor(&fakeRefundApprover{}))
	h := handler.NewRefundHandler(nil).WithQRApprove(qrH)

	// No refund_no param.
	c, w := postJSON(http.MethodPost, "/admin/v1/refunds//qr-approve", nil)
	c.Set("account_id", int64(42))
	h.AdminQRApprove(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d; want 400", w.Code)
	}
}
