package newapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 用 httptest 起一个 mock NewAPI server，覆盖正常 + 异常路径。
// 不联真实 NewAPI；调用次数和 header 都校验。

func newMockServer(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c, err := New(srv.URL, "test-admin-token", "1")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c, srv
}

func TestNew_MissingConfig(t *testing.T) {
	cases := []struct{ url, tok, uid string }{
		{"", "tok", "1"},
		{"http://x", "", "1"},
		{"http://x", "tok", ""},
	}
	for _, tc := range cases {
		if _, err := New(tc.url, tc.tok, tc.uid); err == nil {
			t.Errorf("New(%v) expected error, got nil", tc)
		}
	}
}

func TestCreateUser_Headers(t *testing.T) {
	called := false
	c, _ := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.Method != http.MethodPost || r.URL.Path != "/api/user/" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "test-admin-token" {
			t.Errorf("Authorization = %q, want %q", got, "test-admin-token")
		}
		if got := r.Header.Get("New-Api-User"); got != "1" {
			t.Errorf("New-Api-User = %q, want %q", got, "1")
		}
		// Body should have username + display_name
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["username"] != "lurus_42" {
			t.Errorf("username = %v", body["username"])
		}
		_, _ = w.Write([]byte(`{"success":true,"message":""}`))
	})
	if err := c.CreateUser(context.Background(), "lurus_42", "Lurus 42"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if !called {
		t.Error("server never called")
	}
}

func TestCreateUser_FailureBubblesUp(t *testing.T) {
	c, _ := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":false,"message":"username already exists"}`))
	})
	err := c.CreateUser(context.Background(), "dup", "Dup")
	if err == nil || !strings.Contains(err.Error(), "username already exists") {
		t.Errorf("expected error mentioning duplicate, got %v", err)
	}
}

func TestFindUserByUsername_Found(t *testing.T) {
	c, _ := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		// envelope shape with data array
		_, _ = w.Write([]byte(`{"success":true,"data":[{"id":7,"username":"lurus_42"},{"id":8,"username":"other_user"}]}`))
	})
	id, err := c.FindUserByUsername(context.Background(), "lurus_42")
	if err != nil {
		t.Fatalf("FindUserByUsername: %v", err)
	}
	if id != 7 {
		t.Errorf("id = %d, want 7", id)
	}
}

func TestFindUserByUsername_NotFound(t *testing.T) {
	c, _ := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"data":[]}`))
	})
	_, err := c.FindUserByUsername(context.Background(), "ghost")
	if !errors.Is(err, ErrUserNotFound) {
		t.Errorf("expected ErrUserNotFound, got %v", err)
	}
}

func TestFindUserByUsername_PrefixMatchIgnored(t *testing.T) {
	// NewAPI search is fuzzy; we want exact match only.
	c, _ := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"data":[{"id":7,"username":"lurus_4"},{"id":8,"username":"lurus_42_admin"}]}`))
	})
	_, err := c.FindUserByUsername(context.Background(), "lurus_42")
	if !errors.Is(err, ErrUserNotFound) {
		t.Errorf("expected ErrUserNotFound on no exact match, got %v", err)
	}
}

func TestGetUser_OK(t *testing.T) {
	c, _ := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/user/7" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"success":true,"data":{"id":7,"username":"lurus_42","display_name":"L42","quota":50000000}}`))
	})
	u, err := c.GetUser(context.Background(), 7)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if u.Quota != 50000000 || u.Username != "lurus_42" {
		t.Errorf("got %+v", u)
	}
}

func TestSetUserQuota_PutsAbsoluteValue(t *testing.T) {
	var receivedBody map[string]any
	c, _ := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/user/" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&receivedBody)
		_, _ = w.Write([]byte(`{"success":true}`))
	})
	if err := c.SetUserQuota(context.Background(), 7, 99999); err != nil {
		t.Fatalf("SetUserQuota: %v", err)
	}
	if got, ok := receivedBody["quota"].(float64); !ok || int64(got) != 99999 {
		t.Errorf("quota in PUT body = %v, want 99999", receivedBody["quota"])
	}
	if got, ok := receivedBody["id"].(float64); !ok || int(got) != 7 {
		t.Errorf("id in PUT body = %v, want 7", receivedBody["id"])
	}
}

func TestIncrementUserQuota_ReadModifyWrite(t *testing.T) {
	gets := 0
	puts := 0
	var lastQuota int64
	c, _ := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			gets++
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":7,"quota":1000}}`))
		case http.MethodPut:
			puts++
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			lastQuota = int64(body["quota"].(float64))
			_, _ = w.Write([]byte(`{"success":true}`))
		}
	})
	got, err := c.IncrementUserQuota(context.Background(), 7, 500)
	if err != nil {
		t.Fatalf("IncrementUserQuota: %v", err)
	}
	if got != 1500 {
		t.Errorf("returned new quota = %d, want 1500", got)
	}
	if gets != 1 || puts != 1 {
		t.Errorf("gets=%d puts=%d, expected 1/1", gets, puts)
	}
	if lastQuota != 1500 {
		t.Errorf("PUT quota = %d, want 1500", lastQuota)
	}
}

func TestDo_HTTP404IsStatusErr(t *testing.T) {
	c, _ := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"success":false,"message":"not found"}`))
	})
	_, err := c.GetUser(context.Background(), 99)
	if !errors.Is(err, ErrUserNotFound) {
		t.Errorf("expected ErrUserNotFound, got %v", err)
	}
}
