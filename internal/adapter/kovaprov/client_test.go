package kovaprov

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestClient_MockMode_ReturnsSyntheticCredentials(t *testing.T) {
	c := New("", "")
	if !c.IsMock() {
		t.Fatalf("empty baseURL should yield mock client")
	}
	resp, err := c.Provision(context.Background(), ProvisionRequest{
		TesterName: "acme",
		OrgID:      42,
		AccountID:  7,
	})
	if err != nil {
		t.Fatalf("mock provision failed: %v", err)
	}
	if resp.TesterName != "acme" {
		t.Errorf("tester name not echoed: got %q", resp.TesterName)
	}
	if !strings.HasPrefix(resp.AdminKey, "sk-kova-") {
		t.Errorf("admin key not prefixed sk-kova-: got %q", resp.AdminKey)
	}
	if resp.BaseURL == "" {
		t.Error("base url empty")
	}
	if resp.Port != -1 {
		t.Errorf("mock port should be sentinel -1, got %d", resp.Port)
	}
}

func TestClient_LiveMode_RoundTrip(t *testing.T) {
	called := atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Add(1)
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/internal/provision" {
			t.Errorf("expected /internal/provision, got %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("missing or wrong bearer: %q", r.Header.Get("Authorization"))
		}
		var req ProvisionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if req.TesterName != "acme" {
			t.Errorf("body tester_name=%q", req.TesterName)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ProvisionResponse{
			TesterName: req.TesterName,
			BaseURL:    "http://r6:3015",
			AdminKey:   "sk-kova-livedeadbeef",
			Port:       3015,
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "secret")
	if c.IsMock() {
		t.Fatal("non-empty baseURL should not be mock")
	}
	resp, err := c.Provision(context.Background(), ProvisionRequest{
		TesterName: "acme", OrgID: 1, AccountID: 1,
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	if called.Load() != 1 {
		t.Errorf("expected 1 call, got %d", called.Load())
	}
	if resp.AdminKey != "sk-kova-livedeadbeef" {
		t.Errorf("admin key mismatch: %q", resp.AdminKey)
	}
	if resp.Port != 3015 {
		t.Errorf("port mismatch: %d", resp.Port)
	}
}

func TestClient_RetriesTransientFailures(t *testing.T) {
	called := atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := called.Add(1)
		if n < 3 {
			http.Error(w, "transient", http.StatusBadGateway)
			return
		}
		_ = json.NewEncoder(w).Encode(ProvisionResponse{
			TesterName: "x", BaseURL: "http://r6:3010", AdminKey: "sk-kova-xx", Port: 3010,
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "k")
	c.sleepFn = func(time.Duration) {} // skip backoff

	resp, err := c.Provision(context.Background(), ProvisionRequest{
		TesterName: "x", OrgID: 1, AccountID: 1,
	})
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if resp.AdminKey == "" {
		t.Error("empty admin key on retried success")
	}
	if got := called.Load(); got != 3 {
		t.Errorf("expected 3 attempts, got %d", got)
	}
}

func TestClient_DoesNotRetry4xx(t *testing.T) {
	called := atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called.Add(1)
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer srv.Close()

	c := New(srv.URL, "k")
	c.sleepFn = func(time.Duration) {}

	_, err := c.Provision(context.Background(), ProvisionRequest{
		TesterName: "x", OrgID: 1, AccountID: 1,
	})
	if err == nil {
		t.Fatal("expected error from 400")
	}
	if got := called.Load(); got != 1 {
		t.Errorf("4xx should be hit exactly once, got %d", got)
	}
}

func TestClient_RejectsInvalidRequest(t *testing.T) {
	c := New("http://x", "k")
	cases := []ProvisionRequest{
		{TesterName: "", OrgID: 1, AccountID: 1},
		{TesterName: strings.Repeat("a", 33), OrgID: 1, AccountID: 1},
		{TesterName: "ok", OrgID: 0, AccountID: 1},
	}
	for i, req := range cases {
		if _, err := c.Provision(context.Background(), req); err == nil {
			t.Errorf("case %d: expected validation error", i)
		}
	}
}

func TestClient_HonoursContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer srv.Close()

	c := New(srv.URL, "k")
	c.sleepFn = func(time.Duration) {}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	_, err := c.Provision(ctx, ProvisionRequest{
		TesterName: "x", OrgID: 1, AccountID: 1,
	})
	if err == nil {
		t.Fatal("expected error when context cancelled")
	}
}
