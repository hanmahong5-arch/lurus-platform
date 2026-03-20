package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

func TestAccountHandler_GetMe(t *testing.T) {
	as := newMockAccountStore()
	acct := as.seed(entity.Account{ZitadelSub: "sub-1", Email: "me@x.com", DisplayName: "Me"})

	h := NewAccountHandler(makeAccountServiceWith(as), makeVIPService(), makeSubService(), nil, makeReferralService())
	r := testRouter()
	r.GET("/api/v1/account/me", withAccountID(acct.ID), h.GetMe)

	t.Run("authenticated_user", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/account/me", nil)
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
		}
		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["account"] == nil {
			t.Error("response missing 'account' field")
		}
		if resp["vip"] == nil {
			t.Error("response missing 'vip' field")
		}
	})

	t.Run("missing_account_id_returns_401", func(t *testing.T) {
		r2 := testRouter()
		r2.GET("/api/v1/account/me", withAccountID(0), h.GetMe) // 0 = unauthenticated
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/account/me", nil)
		r2.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
		}
	})
}

func TestAccountHandler_UpdateMe(t *testing.T) {
	as := newMockAccountStore()
	acct := as.seed(entity.Account{ZitadelSub: "sub-u", Email: "u@x.com", DisplayName: "Old"})

	h := NewAccountHandler(makeAccountServiceWith(as), makeVIPService(), makeSubService(), nil, makeReferralService())

	tests := []struct {
		name   string
		body   map[string]string
		status int
	}{
		{"update_display_name", map[string]string{"display_name": "New"}, http.StatusOK},
		{"update_locale", map[string]string{"locale": "zh-CN"}, http.StatusOK},
		{"empty_body_still_ok", map[string]string{}, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := testRouter()
			r.PUT("/api/v1/account/me", withAccountID(acct.ID), h.UpdateMe)
			body, _ := json.Marshal(tt.body)
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPut, "/api/v1/account/me", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			if w.Code != tt.status {
				t.Errorf("status = %d, want %d, body: %s", w.Code, tt.status, w.Body.String())
			}
		})
	}
}

func TestAccountHandler_GetServices(t *testing.T) {
	as := newMockAccountStore()
	acct := as.seed(entity.Account{ZitadelSub: "sub-s", Email: "s@x.com"})

	h := NewAccountHandler(makeAccountServiceWith(as), makeVIPService(), makeSubService(), nil, makeReferralService())
	r := testRouter()
	r.GET("/api/v1/account/me/services", withAccountID(acct.ID), h.GetServices)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/account/me/services", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	// "services" key must exist in response (value can be null for empty list)
	if _, ok := resp["services"]; !ok {
		t.Error("response missing 'services' key")
	}
}

func TestAccountHandler_AdminListAccounts(t *testing.T) {
	as := newMockAccountStore()
	as.seed(entity.Account{ZitadelSub: "sub-1", Email: "a1@x.com"})
	as.seed(entity.Account{ZitadelSub: "sub-2", Email: "a2@x.com"})

	h := NewAccountHandler(makeAccountServiceWith(as), makeVIPService(), makeSubService(), nil, makeReferralService())

	tests := []struct {
		name     string
		query    string
		status   int
	}{
		{"default_pagination", "", http.StatusOK},
		{"custom_page", "?page=1&page_size=10", http.StatusOK},
		{"negative_page_normalized", "?page=-1&page_size=200", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := testRouter()
			r.GET("/admin/v1/accounts", h.AdminListAccounts)
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/admin/v1/accounts"+tt.query, nil)
			r.ServeHTTP(w, req)
			if w.Code != tt.status {
				t.Errorf("status = %d, want %d", w.Code, tt.status)
			}
			var resp map[string]interface{}
			json.Unmarshal(w.Body.Bytes(), &resp)
			if resp["data"] == nil {
				t.Error("response missing 'data' field")
			}
			// Verify page normalization
			if tt.name == "negative_page_normalized" {
				if page, ok := resp["page"].(float64); ok && page < 1 {
					t.Errorf("page should be normalized to >= 1, got %v", page)
				}
			}
		})
	}
}

func TestAccountHandler_AdminGetAccount(t *testing.T) {
	as := newMockAccountStore()
	acct := as.seed(entity.Account{ZitadelSub: "sub-admin", Email: "admin@x.com"})

	h := NewAccountHandler(makeAccountServiceWith(as), makeVIPService(), makeSubService(), nil, makeReferralService())
	r := testRouter()
	r.GET("/admin/v1/accounts/:id", h.AdminGetAccount)

	tests := []struct {
		name   string
		id     string
		status int
	}{
		{"found", "1", http.StatusOK},
		{"not_found", "999", http.StatusNotFound},
		{"invalid_id", "abc", http.StatusBadRequest},
	}

	_ = acct
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/admin/v1/accounts/"+tt.id, nil)
			r.ServeHTTP(w, req)
			if w.Code != tt.status {
				t.Errorf("status = %d, want %d", w.Code, tt.status)
			}
		})
	}
}

func TestAccountHandler_GetMeReferral(t *testing.T) {
	as := newMockAccountStore()
	acct := as.seed(entity.Account{
		ZitadelSub:  "sub-ref",
		Email:       "ref@x.com",
		DisplayName: "RefUser",
		AffCode:     "deadbeef",
	})

	h := NewAccountHandler(makeAccountServiceWith(as), makeVIPService(), makeSubService(), nil, makeReferralService())

	t.Run("returns_aff_code_and_url", func(t *testing.T) {
		r := testRouter()
		r.GET("/api/v1/account/me/referral", withAccountID(acct.ID), h.GetMeReferral)

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/account/me/referral", nil)
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatal("invalid JSON response")
		}
		if resp["aff_code"] != "deadbeef" {
			t.Errorf("aff_code = %v, want deadbeef", resp["aff_code"])
		}
		if url, ok := resp["referral_url"].(string); !ok || url == "" {
			t.Error("referral_url missing or empty")
		}
		if stats, ok := resp["stats"].(map[string]interface{}); !ok || stats == nil {
			t.Error("stats field missing")
		}
	})

	t.Run("unknown_account_returns_404", func(t *testing.T) {
		r := testRouter()
		r.GET("/api/v1/account/me/referral", withAccountID(9999), h.GetMeReferral)

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/account/me/referral", nil)
		r.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", w.Code)
		}
	})
}

func TestAccountHandler_GetMeOverview(t *testing.T) {
	as := newMockAccountStore()
	acct := as.seed(entity.Account{ZitadelSub: "sub-ov", Email: "ov@x.com", DisplayName: "OvUser"})

	h := NewAccountHandler(makeAccountServiceWith(as), makeVIPService(), makeSubService(), makeOverviewServiceWithAccounts(as), makeReferralService())

	t.Run("ok", func(t *testing.T) {
		r := testRouter()
		r.GET("/api/v1/account/me/overview", withAccountID(acct.ID), h.GetMeOverview)

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/account/me/overview", nil)
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
		}
		var resp map[string]any
		json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["topup_url"] == nil {
			t.Error("response missing topup_url")
		}
	})

	t.Run("with_product_id", func(t *testing.T) {
		r := testRouter()
		r.GET("/api/v1/account/me/overview", withAccountID(acct.ID), h.GetMeOverview)

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/account/me/overview?product_id=llm-api", nil)
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
		}
	})
}

func TestAccountHandler_AdminGrantEntitlement(t *testing.T) {
	as := newMockAccountStore()
	acct := as.seed(entity.Account{ZitadelSub: "sub-grant", Email: "grant@x.com"})

	h := NewAccountHandler(makeAccountServiceWith(as), makeVIPService(), makeSubService(), nil, makeReferralService())
	r := testRouter()
	r.POST("/admin/v1/accounts/:id/grant", h.AdminGrantEntitlement)

	tests := []struct {
		name   string
		id     string
		body   map[string]string
		status int
	}{
		{
			"valid_grant",
			"1",
			map[string]string{"product_id": "lurus_api", "key": "rate_limit", "value": "500"},
			http.StatusOK,
		},
		{
			"missing_key",
			"1",
			map[string]string{"product_id": "lurus_api", "value": "500"},
			http.StatusBadRequest,
		},
		{
			"account_not_found",
			"999",
			map[string]string{"product_id": "lurus_api", "key": "rate_limit", "value": "500"},
			http.StatusNotFound,
		},
		{
			"invalid_id",
			"abc",
			map[string]string{"product_id": "lurus_api", "key": "rate_limit", "value": "500"},
			http.StatusBadRequest,
		},
	}

	_ = acct
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.body)
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/admin/v1/accounts/"+tt.id+"/grant", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			if w.Code != tt.status {
				t.Errorf("status = %d, want %d, body: %s", w.Code, tt.status, w.Body.String())
			}
		})
	}
}

func TestAccountHandler_UpdateMe_NotFound(t *testing.T) {
	h := NewAccountHandler(makeAccountService(), makeVIPService(), makeSubService(), nil, makeReferralService())
	r := testRouter()
	// withAccountID(999) → GetByID(999) returns nil from empty store → 404
	r.PUT("/api/v1/account/me", withAccountID(999), h.UpdateMe)

	body, _ := json.Marshal(map[string]string{"display_name": "Test"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/account/me", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404; body: %s", w.Code, w.Body.String())
	}
}

func TestAccountHandler_UpdateMe_AllFields(t *testing.T) {
	as := newMockAccountStore()
	acct := as.seed(entity.Account{ZitadelSub: "sub-all", Email: "all@x.com"})
	h := NewAccountHandler(makeAccountServiceWith(as), makeVIPService(), makeSubService(), nil, makeReferralService())
	r := testRouter()
	r.PUT("/api/v1/account/me", withAccountID(acct.ID), h.UpdateMe)

	body, _ := json.Marshal(map[string]string{
		"display_name": "Full Name",
		"avatar_url":   "https://example.com/avatar.png",
		"username":     "fulluser",
		"locale":       "en-US",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/account/me", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestAccountHandler_GetServices_Error(t *testing.T) {
	subSvc := app.NewSubscriptionService(&errSubStoreH{*newMockSubStore()}, newMockPlanStore(), makeEntitlementService(), 3)
	h := NewAccountHandler(makeAccountService(), makeVIPService(), subSvc, nil, makeReferralService())
	r := testRouter()
	r.GET("/api/v1/account/me/services", withAccountID(1), h.GetServices)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/account/me/services", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500; body: %s", w.Code, w.Body.String())
	}
}

// TestAccountHandler_GetMeOverview_Error verifies 500 when the overview service fails.
func TestAccountHandler_GetMeOverview_Error(t *testing.T) {
	errStore := &errAccountStoreH{*newMockAccountStore()}
	errOvSvc := app.NewOverviewService(
		errStore, makeVIPService(), newMockWalletStore(), makeSubService(), newMockPlanStore(), &mockOverviewCacheH{},
	)
	h := NewAccountHandler(makeAccountService(), makeVIPService(), makeSubService(), errOvSvc, makeReferralService())
	r := testRouter()
	r.GET("/api/v1/account/me/overview", withAccountID(1), h.GetMeOverview)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/account/me/overview", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500; body: %s", w.Code, w.Body.String())
	}
}

// TestAccountHandler_GetMeOverview_AccountNotFound verifies 500 when account is not in store.
func TestAccountHandler_GetMeOverview_AccountNotFound(t *testing.T) {
	// Empty account store: GetByID(9999) returns nil,nil → compute sees a==nil → error.
	ovSvc := makeOverviewServiceH()
	h := NewAccountHandler(makeAccountService(), makeVIPService(), makeSubService(), ovSvc, makeReferralService())
	r := testRouter()
	r.GET("/api/v1/account/me/overview", withAccountID(9999), h.GetMeOverview)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/account/me/overview", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500; body: %s", w.Code, w.Body.String())
	}
}
