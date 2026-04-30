package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// ── In-memory delete-request store ───────────────────────────────────────────
//
// Mirrors the shape of repo.AccountDeleteRequestRepo for tests, without
// pulling in gorm. Behaviour matches the production unique-pending
// invariant: Create returns a typed sentinel when a pending row already
// exists.

type fakeDeleteReqStore struct {
	mu         sync.Mutex
	rows       map[int64]*entity.AccountDeleteRequest
	nextID     int64
	failOnce   bool        // when true, Create returns failErr once then resets
	failErr    error
}

func newFakeDeleteReqStore() *fakeDeleteReqStore {
	return &fakeDeleteReqStore{
		rows:   make(map[int64]*entity.AccountDeleteRequest),
		nextID: 1,
	}
}

// Create matches the AccountDeleteRequestRepo contract: returns the
// app-layer pending sentinel when a pending row already exists for the
// account. Test stores expose this string match (not the typed error)
// because the app/repo cycle would otherwise force a build dep.
var errFakePending = errors.New("repo: account delete request already pending")

func (f *fakeDeleteReqStore) Create(_ context.Context, req *entity.AccountDeleteRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failOnce {
		f.failOnce = false
		return f.failErr
	}
	for _, r := range f.rows {
		if r.AccountID == req.AccountID && r.Status == entity.AccountDeleteRequestStatusPending {
			return errFakePending
		}
	}
	req.ID = f.nextID
	f.nextID++
	cp := *req
	f.rows[req.ID] = &cp
	return nil
}

func (f *fakeDeleteReqStore) GetPending(_ context.Context, accountID int64) (*entity.AccountDeleteRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows {
		if r.AccountID == accountID && r.Status == entity.AccountDeleteRequestStatusPending {
			cp := *r
			return &cp, nil
		}
	}
	return nil, nil
}

// ── Harness ──────────────────────────────────────────────────────────────────

func setupSelfDeleteHandler(t *testing.T) (*AccountSelfDeleteHandler, *mockAccountStore, *fakeDeleteReqStore) {
	t.Helper()
	as := newMockAccountStore()
	store := newFakeDeleteReqStore()
	accountSvc := makeAccountServiceWith(as)
	reqSvc := app.NewAccountDeleteRequestService(store, accountSvc)
	return NewAccountSelfDeleteHandler(reqSvc), as, store
}

// postSelfDelete issues a POST to /api/v1/account/me/delete-request
// against an isolated router with the given account_id wired in.
func postSelfDelete(h *AccountSelfDeleteHandler, accountID int64, body any) *httptest.ResponseRecorder {
	r := testRouter()
	r.POST("/api/v1/account/me/delete-request", withAccountID(accountID), h.Submit)
	w := httptest.NewRecorder()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		buf, _ := json.Marshal(body)
		reader = bytes.NewReader(buf)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/account/me/delete-request", reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	r.ServeHTTP(w, req)
	return w
}

func decodeSelfDelete(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v — body=%s", err, w.Body.String())
	}
	return out
}

// ── Tests ────────────────────────────────────────────────────────────────────

func TestAccountSelfDelete_HappyPath_CreatesPending(t *testing.T) {
	h, as, store := setupSelfDeleteHandler(t)
	acct := as.seed(entity.Account{ZitadelSub: "sub-self-1", Email: "self1@x.com", Status: entity.AccountStatusActive})

	w := postSelfDelete(h, acct.ID, map[string]any{
		"reason":      "no_longer_using",
		"reason_text": "I don't need this account anymore.",
	})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d (body=%s); want 200", w.Code, w.Body.String())
	}
	resp := decodeSelfDelete(t, w)
	if resp["request_id"] == nil || resp["request_id"] == "" {
		t.Errorf("request_id missing/empty; resp=%+v", resp)
	}
	if resp["status"] != entity.AccountDeleteRequestStatusPending {
		t.Errorf("status = %v; want pending", resp["status"])
	}
	if _, ok := resp["cooling_off_until"].(string); !ok {
		t.Error("cooling_off_until missing/wrong type")
	}
	if len(store.rows) != 1 {
		t.Errorf("store has %d rows; want 1", len(store.rows))
	}
}

func TestAccountSelfDelete_TableDriven(t *testing.T) {
	type seedAccount struct {
		status int16
	}
	tests := []struct {
		name        string
		seed        *seedAccount // nil = no seed (account_id 1 maps to nothing)
		accountID   int64        // overrides default seeded id when != 0
		body        any
		wantStatus  int
		wantField   string // optional: a JSON field key that must be present
		wantValue   any    // optional: expected value of wantField
	}{
		{
			name:       "missing_jwt_returns_401",
			seed:       &seedAccount{status: entity.AccountStatusActive},
			accountID:  0, // forces requireAccountID failure
			body:       map[string]any{},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "invalid_reason_enum_returns_400",
			seed:       &seedAccount{status: entity.AccountStatusActive},
			body:       map[string]any{"reason": "totally_made_up"},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "empty_body_is_accepted",
			seed:       &seedAccount{status: entity.AccountStatusActive},
			body:       nil,
			wantStatus: http.StatusOK,
			wantField:  "status",
			wantValue:  entity.AccountDeleteRequestStatusPending,
		},
		{
			name: "long_reason_text_is_truncated_not_rejected",
			seed: &seedAccount{status: entity.AccountStatusActive},
			body: map[string]any{
				"reason":      "other",
				"reason_text": strings.Repeat("我", 2000), // 2000 runes (CJK) — well over 500
			},
			wantStatus: http.StatusOK,
			wantField:  "status",
			wantValue:  entity.AccountDeleteRequestStatusPending,
		},
		{
			name:       "already_deleted_is_idempotent_200",
			seed:       &seedAccount{status: entity.AccountStatusDeleted},
			body:       map[string]any{},
			wantStatus: http.StatusOK,
			wantField:  "already_deleted",
			wantValue:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, as, _ := setupSelfDeleteHandler(t)
			var resolvedID int64
			if tt.seed != nil {
				acct := as.seed(entity.Account{
					ZitadelSub: "sub-" + tt.name,
					Email:      tt.name + "@x.com",
					Status:     tt.seed.status,
				})
				resolvedID = acct.ID
			}
			callerID := resolvedID
			if tt.accountID != 0 {
				callerID = tt.accountID
			}
			// accountID==0 specifically triggers the missing-auth path
			// in withAccountID middleware.
			if tt.name == "missing_jwt_returns_401" {
				callerID = 0
			}

			w := postSelfDelete(h, callerID, tt.body)
			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d (body=%s); want %d", w.Code, w.Body.String(), tt.wantStatus)
			}
			if tt.wantField != "" {
				resp := decodeSelfDelete(t, w)
				if got := resp[tt.wantField]; got != tt.wantValue {
					t.Errorf("%s = %v; want %v", tt.wantField, got, tt.wantValue)
				}
			}
		})
	}
}

func TestAccountSelfDelete_IdempotentRecall_ReusesRequestID(t *testing.T) {
	h, as, store := setupSelfDeleteHandler(t)
	acct := as.seed(entity.Account{ZitadelSub: "sub-idem", Email: "idem@x.com", Status: entity.AccountStatusActive})

	// First call — creates the pending row.
	w1 := postSelfDelete(h, acct.ID, map[string]any{"reason": "privacy_concern"})
	if w1.Code != http.StatusOK {
		t.Fatalf("first call status = %d (body=%s)", w1.Code, w1.Body.String())
	}
	resp1 := decodeSelfDelete(t, w1)
	id1 := resp1["request_id"]

	// Second call — should be idempotent on the same row.
	w2 := postSelfDelete(h, acct.ID, map[string]any{"reason": "experience_issue"})
	if w2.Code != http.StatusOK {
		t.Fatalf("second call status = %d (body=%s)", w2.Code, w2.Body.String())
	}
	resp2 := decodeSelfDelete(t, w2)
	id2 := resp2["request_id"]

	if id1 == nil || id1 != id2 {
		t.Errorf("expected idempotent re-call to reuse id; got %v then %v", id1, id2)
	}
	if len(store.rows) != 1 {
		t.Errorf("store has %d rows; want 1 (idempotent must not duplicate)", len(store.rows))
	}
}

func TestAccountSelfDelete_StoreFailure_Returns500(t *testing.T) {
	h, as, store := setupSelfDeleteHandler(t)
	acct := as.seed(entity.Account{ZitadelSub: "sub-fail", Email: "fail@x.com", Status: entity.AccountStatusActive})

	store.failOnce = true
	store.failErr = errors.New("simulated db outage")

	w := postSelfDelete(h, acct.ID, map[string]any{"reason": "other"})
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d (body=%s); want 500", w.Code, w.Body.String())
	}
	resp := decodeSelfDelete(t, w)
	if code, _ := resp["error"].(string); code != ErrCodeInternal {
		t.Errorf("error code = %v; want %s", resp["error"], ErrCodeInternal)
	}
}

func TestAccountSelfDelete_AccountNotFound_Returns500(t *testing.T) {
	// No seed; the JWT-resolved account id points at a row that doesn't
	// exist. AccountDeleteRequestService surfaces this as a generic
	// error, which the handler wraps into 500. (We don't 404 here on
	// purpose: a JWT-authenticated caller pointing at a nonexistent
	// account row is a system-integrity issue, not a user-facing one.)
	h, _, _ := setupSelfDeleteHandler(t)
	w := postSelfDelete(h, 9999, map[string]any{})
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d (body=%s); want 500", w.Code, w.Body.String())
	}
}
