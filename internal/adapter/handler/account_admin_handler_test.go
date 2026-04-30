package handler_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/hanmahong5-arch/lurus-platform/internal/adapter/handler"
	"github.com/hanmahong5-arch/lurus-platform/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// adminHandlerHarness wires AccountAdminHandler over a real
// AccountService + in-memory account store. The DeleteRequest path
// only needs accountSvc.GetByID (read) — no Purge primitives are
// invoked at mint time, so we avoid the broader executor wiring here.

// minimal accountStore stub satisfying just the methods AccountService
// calls during GetByID. Other methods return nil/zero so structural
// typing is satisfied without standing up a full mock.
type adminHandlerAccountStub struct {
	rows map[int64]*entity.Account
}

func (s *adminHandlerAccountStub) Create(_ context.Context, a *entity.Account) error {
	if s.rows == nil {
		s.rows = map[int64]*entity.Account{}
	}
	s.rows[a.ID] = a
	return nil
}
func (s *adminHandlerAccountStub) Update(_ context.Context, a *entity.Account) error {
	s.rows[a.ID] = a
	return nil
}
func (s *adminHandlerAccountStub) GetByID(_ context.Context, id int64) (*entity.Account, error) {
	if a, ok := s.rows[id]; ok {
		cp := *a
		return &cp, nil
	}
	return nil, nil
}
func (s *adminHandlerAccountStub) GetByEmail(context.Context, string) (*entity.Account, error)    { return nil, nil }
func (s *adminHandlerAccountStub) GetByZitadelSub(context.Context, string) (*entity.Account, error) { return nil, nil }
func (s *adminHandlerAccountStub) GetByLurusID(context.Context, string) (*entity.Account, error)  { return nil, nil }
func (s *adminHandlerAccountStub) GetByAffCode(context.Context, string) (*entity.Account, error)  { return nil, nil }
func (s *adminHandlerAccountStub) GetByPhone(context.Context, string) (*entity.Account, error)    { return nil, nil }
func (s *adminHandlerAccountStub) List(context.Context, string, int, int) ([]*entity.Account, int64, error) {
	return nil, 0, nil
}
func (s *adminHandlerAccountStub) UpsertOAuthBinding(context.Context, *entity.OAuthBinding) error { return nil }
func (s *adminHandlerAccountStub) GetByUsername(context.Context, string) (*entity.Account, error) { return nil, nil }
func (s *adminHandlerAccountStub) GetByOAuthBinding(context.Context, string, string) (*entity.Account, error) {
	return nil, nil
}
func (s *adminHandlerAccountStub) SetNewAPIUserID(context.Context, int64, int) error { return nil }
func (s *adminHandlerAccountStub) ListWithoutNewAPIUser(context.Context, int) ([]*entity.Account, error) {
	return nil, nil
}

// minimal walletStore + vipStore stubs that AccountService can swallow.
// Neither is invoked along the DeleteRequest path (which only reads
// accounts), but NewAccountService demands all three at construction.

type adminHandlerWalletStub struct{}

func (adminHandlerWalletStub) GetOrCreate(context.Context, int64) (*entity.Wallet, error) {
	return &entity.Wallet{}, nil
}
func (adminHandlerWalletStub) GetByAccountID(context.Context, int64) (*entity.Wallet, error) {
	return nil, nil
}
func (adminHandlerWalletStub) Credit(context.Context, int64, float64, string, string, string, string, string) (*entity.WalletTransaction, error) {
	return nil, nil
}
func (adminHandlerWalletStub) Debit(context.Context, int64, float64, string, string, string, string, string) (*entity.WalletTransaction, error) {
	return nil, nil
}
func (adminHandlerWalletStub) ListTransactions(context.Context, int64, int, int) ([]entity.WalletTransaction, int64, error) {
	return nil, 0, nil
}
func (adminHandlerWalletStub) CreatePaymentOrder(context.Context, *entity.PaymentOrder) error {
	return nil
}
func (adminHandlerWalletStub) UpdatePaymentOrder(context.Context, *entity.PaymentOrder) error {
	return nil
}
func (adminHandlerWalletStub) GetPaymentOrderByNo(context.Context, string) (*entity.PaymentOrder, error) {
	return nil, nil
}
func (adminHandlerWalletStub) GetRedemptionCode(context.Context, string) (*entity.RedemptionCode, error) {
	return nil, nil
}
func (adminHandlerWalletStub) UpdateRedemptionCode(context.Context, *entity.RedemptionCode) error {
	return nil
}
func (adminHandlerWalletStub) ListOrders(context.Context, int64, int, int) ([]entity.PaymentOrder, int64, error) {
	return nil, 0, nil
}
func (adminHandlerWalletStub) MarkPaymentOrderPaid(context.Context, string) (*entity.PaymentOrder, bool, error) {
	return nil, false, nil
}
func (adminHandlerWalletStub) RedeemCode(context.Context, int64, string) (*entity.WalletTransaction, error) {
	return nil, nil
}
func (adminHandlerWalletStub) ExpireStalePendingOrders(context.Context, time.Duration) (int64, error) {
	return 0, nil
}
func (adminHandlerWalletStub) GetPendingOrderByIdempotencyKey(context.Context, string) (*entity.PaymentOrder, error) {
	return nil, nil
}
func (adminHandlerWalletStub) CreatePreAuth(context.Context, *entity.WalletPreAuthorization) error {
	return nil
}
func (adminHandlerWalletStub) GetPreAuthByID(context.Context, int64) (*entity.WalletPreAuthorization, error) {
	return nil, nil
}
func (adminHandlerWalletStub) GetPreAuthByReference(context.Context, string, string) (*entity.WalletPreAuthorization, error) {
	return nil, nil
}
func (adminHandlerWalletStub) SettlePreAuth(context.Context, int64, float64) (*entity.WalletPreAuthorization, error) {
	return nil, nil
}
func (adminHandlerWalletStub) ReleasePreAuth(context.Context, int64) (*entity.WalletPreAuthorization, error) {
	return nil, nil
}
func (adminHandlerWalletStub) ExpireStalePreAuths(context.Context) (int64, error) { return 0, nil }
func (adminHandlerWalletStub) CountActivePreAuths(context.Context, int64) (int64, error) {
	return 0, nil
}
func (adminHandlerWalletStub) CountPendingOrders(context.Context, int64) (int64, error) {
	return 0, nil
}
func (adminHandlerWalletStub) FindStalePendingOrders(context.Context, time.Duration) ([]entity.PaymentOrder, error) {
	return nil, nil
}
func (adminHandlerWalletStub) FindPaidTopupOrdersWithoutCredit(context.Context) ([]entity.PaidOrderWithoutCredit, error) {
	return nil, nil
}
func (adminHandlerWalletStub) CreateReconciliationIssue(context.Context, *entity.ReconciliationIssue) error {
	return nil
}
func (adminHandlerWalletStub) ListReconciliationIssues(context.Context, string, int, int) ([]entity.ReconciliationIssue, int64, error) {
	return nil, 0, nil
}
func (adminHandlerWalletStub) ResolveReconciliationIssue(context.Context, int64, string, string) error {
	return nil
}

type adminHandlerVIPStub struct{}

func (adminHandlerVIPStub) GetOrCreate(context.Context, int64) (*entity.AccountVIP, error) {
	return &entity.AccountVIP{}, nil
}
func (adminHandlerVIPStub) Update(context.Context, *entity.AccountVIP) error { return nil }
func (adminHandlerVIPStub) ListConfigs(context.Context) ([]entity.VIPLevelConfig, error) {
	return nil, nil
}

func setupAdminHandler(t *testing.T) (*handler.AccountAdminHandler, *handler.QRHandler, *adminHandlerAccountStub) {
	t.Helper()
	stub := &adminHandlerAccountStub{rows: map[int64]*entity.Account{}}
	// No purge store: DeleteRequest only reads via GetByID. The
	// executor wired below would error on confirm, but these tests
	// stop at the QR-mint step.
	svc := app.NewAccountService(stub, adminHandlerWalletStub{}, adminHandlerVIPStub{})
	qr, _, _ := setupQR(t)
	exec := handler.NewAccountDeleteExecutor(svc, nil, nil, nil)
	qr = qr.WithDelegateExecutor(exec)
	h := handler.NewAccountAdminHandler(svc).WithDeleteFlow(qr)
	return h, qr, stub
}

// ── Tests ───────────────────────────────────────────────────────────────────

func TestAccountAdmin_DeleteRequest_NotWired_501(t *testing.T) {
	stub := &adminHandlerAccountStub{rows: map[int64]*entity.Account{}}
	// No purge store: DeleteRequest only reads via GetByID. The
	// executor wired below would error on confirm, but these tests
	// stop at the QR-mint step.
	svc := app.NewAccountService(stub, adminHandlerWalletStub{}, adminHandlerVIPStub{})
	h := handler.NewAccountAdminHandler(svc) // no WithDeleteFlow → QR nil

	c, w := postJSON(http.MethodPost, "/admin/v1/accounts/1/delete-request",
		map[string]any{}, gin.Param{Key: "id", Value: "1"})
	c.Set("account_id", int64(99))
	h.DeleteRequest(c)

	if w.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d; want 501", w.Code)
	}
}

func TestAccountAdmin_DeleteRequest_InvalidID_400(t *testing.T) {
	h, _, _ := setupAdminHandler(t)
	c, w := postJSON(http.MethodPost, "/admin/v1/accounts/abc/delete-request",
		map[string]any{}, gin.Param{Key: "id", Value: "abc"})
	c.Set("account_id", int64(99))
	h.DeleteRequest(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d; want 400", w.Code)
	}
}

func TestAccountAdmin_DeleteRequest_AccountNotFound_404(t *testing.T) {
	h, _, _ := setupAdminHandler(t)
	c, w := postJSON(http.MethodPost, "/admin/v1/accounts/9999/delete-request",
		map[string]any{}, gin.Param{Key: "id", Value: "9999"})
	c.Set("account_id", int64(99))
	h.DeleteRequest(c)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d; want 404 (body=%s)", w.Code, w.Body.String())
	}
}

func TestAccountAdmin_DeleteRequest_AlreadyDeleted_IsIdempotent200(t *testing.T) {
	h, _, stub := setupAdminHandler(t)
	stub.rows[42] = &entity.Account{ID: 42, Status: entity.AccountStatusDeleted}

	c, w := postJSON(http.MethodPost, "/admin/v1/accounts/42/delete-request",
		map[string]any{}, gin.Param{Key: "id", Value: "42"})
	c.Set("account_id", int64(99))
	h.DeleteRequest(c)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 idempotent", w.Code)
	}
	resp := decode(t, w)
	if alreadyDeleted, _ := resp["already_deleted"].(bool); !alreadyDeleted {
		t.Errorf("already_deleted = %v; want true", resp["already_deleted"])
	}
	if _, hasQR := resp["qr_payload"]; hasQR {
		t.Errorf("response should NOT include qr_payload on idempotent path")
	}
}

func TestAccountAdmin_DeleteRequest_HappyPath_MintsQR(t *testing.T) {
	h, _, stub := setupAdminHandler(t)
	stub.rows[100] = &entity.Account{ID: 100, Status: entity.AccountStatusActive}

	c, w := postJSON(http.MethodPost, "/admin/v1/accounts/100/delete-request",
		map[string]any{}, gin.Param{Key: "id", Value: "100"})
	c.Set("account_id", int64(99))
	h.DeleteRequest(c)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (body=%s)", w.Code, w.Body.String())
	}
	resp := decode(t, w)
	if id, _ := resp["id"].(string); len(id) != 64 {
		t.Errorf("session id len = %d; want 64", len(id))
	}
	if _, ok := resp["qr_payload"].(string); !ok {
		t.Error("qr_payload missing")
	}
	gotID, ok := resp["account_id"].(float64)
	if !ok || int64(gotID) != 100 {
		t.Errorf("account_id = %v; want 100", resp["account_id"])
	}
}

func TestAccountAdmin_DeleteRequest_MissingAuth_401(t *testing.T) {
	h, _, stub := setupAdminHandler(t)
	stub.rows[7] = &entity.Account{ID: 7, Status: entity.AccountStatusActive}

	c, w := postJSON(http.MethodPost, "/admin/v1/accounts/7/delete-request",
		map[string]any{}, gin.Param{Key: "id", Value: "7"})
	// Intentionally no account_id in context.
	h.DeleteRequest(c)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d; want 401", w.Code)
	}
}
