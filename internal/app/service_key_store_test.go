package app

import (
	"context"
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// ── Mock repo ───────────────────────────────────────────────────────────────

type mockServiceKeyRepo struct {
	keys []entity.ServiceAPIKey
}

func (m *mockServiceKeyRepo) ListActive(_ context.Context) ([]entity.ServiceAPIKey, error) {
	return m.keys, nil
}

func (m *mockServiceKeyRepo) TouchLastUsed(_ context.Context, _ int64) {}

// ── Tests ───────────────────────────────────────────────────────────────────

func TestServiceKeyStore_Resolve_Scoped(t *testing.T) {
	rawKey := "sk-test-lurus-api-key-2026"
	hash := HashKey(rawKey)

	repo := &mockServiceKeyRepo{
		keys: []entity.ServiceAPIKey{
			{
				ID:           1,
				KeyHash:      hash,
				KeyPrefix:    rawKey[:8],
				ServiceName:  "lurus-api",
				Scopes:       entity.StringList{"account:read", "wallet:debit", "entitlement"},
				RateLimitRPM: 1000,
				Status:       entity.ServiceKeyActive,
			},
		},
	}

	store := NewServiceKeyStore(repo, "legacy-key")
	if err := store.LoadAll(context.Background()); err != nil {
		t.Fatalf("LoadAll: %v", err)
	}

	// Resolve with the correct key.
	result := store.Resolve(rawKey)
	if result == nil {
		t.Fatal("expected non-nil result for valid key")
	}
	if result.ServiceName != "lurus-api" {
		t.Errorf("ServiceName = %q, want 'lurus-api'", result.ServiceName)
	}
	if result.IsLegacy {
		t.Error("should not be legacy")
	}
	if !result.HasScope("wallet:debit") {
		t.Error("expected wallet:debit scope")
	}
	if result.HasScope("wallet:credit") {
		t.Error("should NOT have wallet:credit scope")
	}
}

func TestServiceKeyStore_Resolve_Legacy(t *testing.T) {
	store := NewServiceKeyStore(&mockServiceKeyRepo{}, "my-legacy-key")
	store.LoadAll(context.Background())

	result := store.Resolve("my-legacy-key")
	if result == nil {
		t.Fatal("expected non-nil result for legacy key")
	}
	if !result.IsLegacy {
		t.Error("should be marked as legacy")
	}
	if result.ServiceName != "legacy" {
		t.Errorf("ServiceName = %q, want 'legacy'", result.ServiceName)
	}
	// Legacy key has all scopes.
	if !result.HasScope("wallet:credit") {
		t.Error("legacy should have all scopes including wallet:credit")
	}
}

func TestServiceKeyStore_Resolve_InvalidKey(t *testing.T) {
	store := NewServiceKeyStore(&mockServiceKeyRepo{}, "legacy-key")
	store.LoadAll(context.Background())

	result := store.Resolve("wrong-key")
	if result != nil {
		t.Error("expected nil for invalid key")
	}
}

func TestServiceKeyStore_Resolve_EmptyToken(t *testing.T) {
	store := NewServiceKeyStore(&mockServiceKeyRepo{}, "legacy-key")
	result := store.Resolve("")
	if result != nil {
		t.Error("expected nil for empty token")
	}
}

func TestServiceKeyStore_Resolve_NoLegacy(t *testing.T) {
	store := NewServiceKeyStore(&mockServiceKeyRepo{}, "") // no legacy key
	store.LoadAll(context.Background())

	result := store.Resolve("any-key")
	if result != nil {
		t.Error("expected nil when no keys and no legacy")
	}
}

func TestServiceKeyStore_ScopeCheck(t *testing.T) {
	result := &ResolveResult{
		ServiceName: "test",
		Scopes:      []string{"account:read", "entitlement"},
	}

	tests := []struct {
		scope string
		want  bool
	}{
		{"account:read", true},
		{"entitlement", true},
		{"wallet:debit", false},
		{"wallet:credit", false},
		{"", false},
	}
	for _, tc := range tests {
		if got := result.HasScope(tc.scope); got != tc.want {
			t.Errorf("HasScope(%q) = %v, want %v", tc.scope, got, tc.want)
		}
	}
}

func TestHashKey_Deterministic(t *testing.T) {
	h1 := HashKey("test-key")
	h2 := HashKey("test-key")
	if h1 != h2 {
		t.Error("HashKey should be deterministic")
	}
	if len(h1) != 64 { // SHA-256 hex = 64 chars
		t.Errorf("hash length = %d, want 64", len(h1))
	}
}

func TestHashKey_DifferentKeys(t *testing.T) {
	h1 := HashKey("key-a")
	h2 := HashKey("key-b")
	if h1 == h2 {
		t.Error("different keys should produce different hashes")
	}
}

func TestServiceAPIKey_HasScope(t *testing.T) {
	key := entity.ServiceAPIKey{
		Scopes: entity.StringList{"account:read", "wallet:debit"},
	}
	if !key.HasScope("account:read") {
		t.Error("should have account:read")
	}
	if key.HasScope("wallet:credit") {
		t.Error("should NOT have wallet:credit")
	}
}

func TestServiceAPIKey_IsActive(t *testing.T) {
	active := entity.ServiceAPIKey{Status: entity.ServiceKeyActive}
	suspended := entity.ServiceAPIKey{Status: entity.ServiceKeySuspended}
	revoked := entity.ServiceAPIKey{Status: entity.ServiceKeyRevoked}

	if !active.IsActive() {
		t.Error("active key should be active")
	}
	if suspended.IsActive() {
		t.Error("suspended key should not be active")
	}
	if revoked.IsActive() {
		t.Error("revoked key should not be active")
	}
}
