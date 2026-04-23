package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestNewJWKSCache_DefaultTTL covers the ttl<=0 branch of the constructor,
// which existing tests don't hit (they always pass a positive TTL).
func TestNewJWKSCache_DefaultTTL(t *testing.T) {
	for _, ttl := range []time.Duration{0, -1 * time.Minute, -1 * time.Second} {
		c := NewJWKSCache("http://example/keys", ttl)
		if c.ttl != defaultJWKSTTL {
			t.Errorf("ttl=%v → got c.ttl=%v, want defaultJWKSTTL (%v)", ttl, c.ttl, defaultJWKSTTL)
		}
	}
}

// TestJWKSCache_RefreshPublic_ForcesFetch covers the exported Refresh method
// (distinct from the internal refresh): existing tests exercise refresh only
// indirectly via GetKey. Refresh is for operators who want to force a pull
// without waiting for TTL to expire.
func TestJWKSCache_RefreshPublic_ForcesFetch(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		// Empty key set — parseable, refreshes successfully.
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{}})
	}))
	defer srv.Close()

	c := NewJWKSCache(srv.URL, 1*time.Hour) // long TTL so stale path doesn't trigger
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("first Refresh: %v", err)
	}
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("second Refresh: %v", err)
	}
	if calls != 2 {
		t.Errorf("expected 2 fetches (Refresh bypasses TTL), got %d", calls)
	}
}

// TestJWKSCache_RefreshPublic_HTTPError surfaces non-2xx responses as errors.
func TestJWKSCache_RefreshPublic_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	c := NewJWKSCache(srv.URL, time.Minute)
	if err := c.Refresh(context.Background()); err == nil {
		t.Fatal("expected error for HTTP 502, got nil")
	}
}

// TestParseJWK_UnknownKty covers the default branch that rejects kty values
// other than RSA/EC.
func TestParseJWK_UnknownKty(t *testing.T) {
	// Provide a JWKS with an unsupported key type (symmetric).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]any{{
				"kty": "oct", // symmetric — not supported by parseJWK
				"kid": "sym-1",
				"k":   "AAAA",
			}},
		})
	}))
	defer srv.Close()

	c := NewJWKSCache(srv.URL, time.Minute)
	// Refresh should succeed overall (unsupported keys are skipped with a log,
	// not a hard error) — or return an error depending on implementation.
	// Either way, the kid must NOT be cached.
	_ = c.Refresh(context.Background())

	_, err := c.GetKey(context.Background(), "sym-1")
	if err == nil {
		t.Error("expected error looking up 'oct' key (should have been skipped), got nil")
	}
}

// TestVerifySignature_UnknownAlg exercises the default branch of the algorithm
// dispatch — an unknown alg must error cleanly, not panic.
func TestVerifySignature_UnknownAlg(t *testing.T) {
	for _, alg := range []string{"", "HS256", "none", "XYZ123", "PS256"} {
		t.Run(alg, func(t *testing.T) {
			err := verifySignature(alg, nil, []byte("sig"), []byte("input"))
			if err == nil {
				t.Errorf("alg=%q: want error, got nil", alg)
			}
		})
	}
}

// TestVerifyRSA_WrongKeyType covers the "key type mismatch" guard when a
// caller hands an EC key to the RSA verifier — a realistic JWKS-mixup case.
func TestVerifyRSA_WrongKeyType(t *testing.T) {
	// Build an EC public key via the existing JWKS path so we don't duplicate key-gen logic.
	// Simplest: pass a type that definitely isn't *rsa.PublicKey.
	type notRSA struct{}
	err := verifyRSA(&notRSA{}, []byte("input"), []byte("sig"), nil, "sha256")
	if err == nil {
		t.Error("expected 'key type mismatch' error, got nil")
	}
}
