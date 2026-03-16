package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// ---------- checkin store mock ----------

type mockCheckinStoreH struct {
	checkins map[string]*entity.Checkin // key = "accountID:date"
	monthly  map[string][]entity.Checkin
}

func newMockCheckinStoreH() *mockCheckinStoreH {
	return &mockCheckinStoreH{
		checkins: make(map[string]*entity.Checkin),
		monthly:  make(map[string][]entity.Checkin),
	}
}

func checkinKey(accountID int64, date string) string {
	return context.Background().Value("ignored").(string) + date // won't compile — use fmt
}

func (m *mockCheckinStoreH) Create(_ context.Context, c *entity.Checkin) error {
	key := formatCheckinKey(c.AccountID, c.CheckinDate)
	m.checkins[key] = c
	ym := c.CheckinDate[:7]
	monthKey := formatMonthKey(c.AccountID, ym)
	m.monthly[monthKey] = append(m.monthly[monthKey], *c)
	return nil
}

func (m *mockCheckinStoreH) GetByAccountAndDate(_ context.Context, accountID int64, date string) (*entity.Checkin, error) {
	c := m.checkins[formatCheckinKey(accountID, date)]
	return c, nil
}

func (m *mockCheckinStoreH) ListByAccountAndMonth(_ context.Context, accountID int64, yearMonth string) ([]entity.Checkin, error) {
	return m.monthly[formatMonthKey(accountID, yearMonth)], nil
}

func (m *mockCheckinStoreH) CountConsecutive(_ context.Context, accountID int64, date string) (int, error) {
	count := 0
	d, _ := time.Parse("2006-01-02", date)
	for {
		key := formatCheckinKey(accountID, d.Format("2006-01-02"))
		if m.checkins[key] == nil {
			break
		}
		count++
		d = d.AddDate(0, 0, -1)
	}
	return count, nil
}

func formatCheckinKey(accountID int64, date string) string {
	return date + ":" + itoa(accountID)
}

func formatMonthKey(accountID int64, ym string) string {
	return ym + ":" + itoa(accountID)
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

// ---------- helpers ----------

func makeCheckinService() *app.CheckinService {
	return app.NewCheckinService(newMockCheckinStoreH(), newMockWalletStore())
}

func makeCheckinHandler() (*CheckinHandler, *mockCheckinStoreH) {
	cs := newMockCheckinStoreH()
	ws := newMockWalletStore()
	svc := app.NewCheckinService(cs, ws)
	return NewCheckinHandler(svc), cs
}

// ---------- tests ----------

// TestCheckinHandler_GetStatus_Success verifies GetStatus returns 200 for a fresh account.
func TestCheckinHandler_GetStatus_Success(t *testing.T) {
	h, _ := makeCheckinHandler()
	r := testRouter()
	r.GET("/api/v1/checkin/status", withAccountID(1), h.GetStatus)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/checkin/status", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if _, ok := resp["checked_in_today"]; !ok {
		t.Error("response missing 'checked_in_today'")
	}
}

// TestCheckinHandler_DoCheckin_Success verifies DoCheckin returns 200 and reward data.
func TestCheckinHandler_DoCheckin_Success(t *testing.T) {
	cs := newMockCheckinStoreH()
	ws := newMockWalletStore()
	ws.GetOrCreate(context.Background(), 10)
	svc := app.NewCheckinService(cs, ws)
	h := NewCheckinHandler(svc)

	r := testRouter()
	r.POST("/api/v1/checkin", withAccountID(10), h.DoCheckin)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/checkin", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if _, ok := resp["reward_value"]; !ok {
		t.Error("response missing 'reward_value'")
	}
}

// TestCheckinHandler_DoCheckin_AlreadyCheckedIn verifies 409 on duplicate checkin.
func TestCheckinHandler_DoCheckin_AlreadyCheckedIn(t *testing.T) {
	cs := newMockCheckinStoreH()
	ws := newMockWalletStore()
	ws.GetOrCreate(context.Background(), 11)
	svc := app.NewCheckinService(cs, ws)
	h := NewCheckinHandler(svc)

	r := testRouter()
	r.POST("/api/v1/checkin", withAccountID(11), h.DoCheckin)

	// First checkin succeeds.
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, httptest.NewRequest(http.MethodPost, "/api/v1/checkin", nil))
	if w1.Code != http.StatusOK {
		t.Fatalf("first checkin: status = %d, want 200", w1.Code)
	}

	// Second checkin today should return 409.
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, httptest.NewRequest(http.MethodPost, "/api/v1/checkin", nil))
	if w2.Code != http.StatusConflict {
		t.Errorf("second checkin: status = %d, want 409", w2.Code)
	}
}

// TestCheckinHandler_GetStatus_AfterCheckin verifies that GetStatus reflects a completed checkin.
func TestCheckinHandler_GetStatus_AfterCheckin(t *testing.T) {
	cs := newMockCheckinStoreH()
	ws := newMockWalletStore()
	ws.GetOrCreate(context.Background(), 12)
	svc := app.NewCheckinService(cs, ws)
	h := NewCheckinHandler(svc)

	r := testRouter()
	r.POST("/api/v1/checkin", withAccountID(12), h.DoCheckin)
	r.GET("/api/v1/checkin/status", withAccountID(12), h.GetStatus)

	// Do checkin.
	wPost := httptest.NewRecorder()
	r.ServeHTTP(wPost, httptest.NewRequest(http.MethodPost, "/api/v1/checkin", nil))
	if wPost.Code != http.StatusOK {
		t.Fatalf("DoCheckin: status = %d, want 200", wPost.Code)
	}

	// Get status — should show checked in today.
	wGet := httptest.NewRecorder()
	r.ServeHTTP(wGet, httptest.NewRequest(http.MethodGet, "/api/v1/checkin/status", nil))
	if wGet.Code != http.StatusOK {
		t.Fatalf("GetStatus: status = %d, want 200", wGet.Code)
	}

	var status map[string]interface{}
	if err := json.Unmarshal(wGet.Body.Bytes(), &status); err != nil {
		t.Fatalf("response not JSON: %v", err)
	}
	if checked, ok := status["checked_in_today"].(bool); !ok || !checked {
		t.Errorf("checked_in_today = %v, want true after checkin", status["checked_in_today"])
	}
}

// ---------- error-injecting store ----------

// errCheckinStoreH wraps mockCheckinStoreH with a configurable lookup error.
type errCheckinStoreH struct {
	*mockCheckinStoreH
	lookupErr error
}

func (s *errCheckinStoreH) GetByAccountAndDate(ctx context.Context, accountID int64, date string) (*entity.Checkin, error) {
	if s.lookupErr != nil {
		return nil, s.lookupErr
	}
	return s.mockCheckinStoreH.GetByAccountAndDate(ctx, accountID, date)
}

// ---------- error path tests ----------

// TestCheckinHandler_GetStatus_Error verifies that a store error returns 500.
func TestCheckinHandler_GetStatus_Error(t *testing.T) {
	errStore := &errCheckinStoreH{
		mockCheckinStoreH: newMockCheckinStoreH(),
		lookupErr:         fmt.Errorf("db unavailable"),
	}
	svc := app.NewCheckinService(errStore, newMockWalletStore())
	h := NewCheckinHandler(svc)

	r := testRouter()
	r.GET("/api/v1/checkin/status", withAccountID(20), h.GetStatus)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/checkin/status", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
}

// TestCheckinHandler_DoCheckin_GenericError verifies that a non-duplicate store error
// returns 500 (not 409).
func TestCheckinHandler_DoCheckin_GenericError(t *testing.T) {
	errStore := &errCheckinStoreH{
		mockCheckinStoreH: newMockCheckinStoreH(),
		lookupErr:         fmt.Errorf("connection reset"),
	}
	svc := app.NewCheckinService(errStore, newMockWalletStore())
	h := NewCheckinHandler(svc)

	r := testRouter()
	r.POST("/api/v1/checkin", withAccountID(21), h.DoCheckin)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/checkin", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
}
