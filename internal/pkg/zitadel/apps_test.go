package zitadel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestZitadel_RotateOIDCSecret_Success verifies the happy path:
// correct method+URI, Bearer auth, JSON body, and clientSecret
// extracted from the response.
func TestZitadel_RotateOIDCSecret_Success(t *testing.T) {
	const (
		projectID = "proj-1"
		appID     = "app-1"
		newSecret = "fresh-secret-xyz"
	)
	wantPath := "/management/v1/projects/" + projectID + "/apps/" + appID + "/oidc_config/_generate_client_secret"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("want POST, got %s", r.Method)
		}
		if r.URL.Path != wantPath {
			t.Errorf("path = %q, want %q", r.URL.Path, wantPath)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-pat" {
			t.Errorf("Authorization = %q, want 'Bearer test-pat'", auth)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"clientSecret": newSecret})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	got, err := c.RotateOIDCSecret(context.Background(), projectID, appID)
	if err != nil {
		t.Fatalf("RotateOIDCSecret: %v", err)
	}
	if got != newSecret {
		t.Errorf("clientSecret = %q, want %q", got, newSecret)
	}
}

// TestZitadel_RotateOIDCSecret_EmptySecret rejects a malformed Zitadel
// response that returns 200 but no clientSecret field. We never want to
// silently propagate an empty string into a K8s Secret.
func TestZitadel_RotateOIDCSecret_EmptySecret(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"clientSecret":""}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.RotateOIDCSecret(context.Background(), "p", "a")
	if err == nil {
		t.Fatal("expected error for empty clientSecret, got nil")
	}
	if !strings.Contains(err.Error(), "empty client_secret") {
		t.Errorf("error should mention 'empty client_secret', got: %v", err)
	}
}

// TestZitadel_RotateOIDCSecret_ServerError surfaces 5xx from Zitadel as
// a wrapped error so the reconciler can log+retry without crashing.
func TestZitadel_RotateOIDCSecret_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"backend exploded"}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.RotateOIDCSecret(context.Background(), "p", "a")
	if err == nil {
		t.Fatal("expected error for 500, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention status code, got: %v", err)
	}
}

// TestZitadel_RotateOIDCSecret_InvalidJSON guards against a Zitadel
// response shape change that would otherwise silently strand callers.
func TestZitadel_RotateOIDCSecret_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{bad json"))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.RotateOIDCSecret(context.Background(), "p", "a")
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

// TestZitadel_RotateOIDCSecret_RequiresIDs validates input sanity —
// reaching the network layer with an empty projectID/appID would
// produce an opaque 404 from Zitadel and burn a request budget.
func TestZitadel_RotateOIDCSecret_RequiresIDs(t *testing.T) {
	c := newTestClient("http://127.0.0.1:1") // never dialed
	if _, err := c.RotateOIDCSecret(context.Background(), "", "app"); err == nil {
		t.Error("expected error for empty projectID")
	}
	if _, err := c.RotateOIDCSecret(context.Background(), "proj", ""); err == nil {
		t.Error("expected error for empty appID")
	}
}
