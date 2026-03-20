// Package app — ServiceKeyStore provides a cached, in-memory resolver for
// service API keys. Loaded once at startup, then resolved in O(1) per request.
package app

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// ServiceKeyRepo is the persistence interface for service API keys.
type ServiceKeyRepo interface {
	ListActive(ctx context.Context) ([]entity.ServiceAPIKey, error)
	TouchLastUsed(ctx context.Context, id int64)
}

// ServiceKeyStore resolves bearer tokens to service identities with scopes.
// Thread-safe for concurrent use by the HTTP middleware.
type ServiceKeyStore struct {
	mu        sync.RWMutex
	byHash    map[string]*entity.ServiceAPIKey // SHA-256(raw) → key
	repo      ServiceKeyRepo
	legacyKey string // fallback: the old INTERNAL_API_KEY (full access)
}

// NewServiceKeyStore creates a store and loads all active keys into memory.
// legacyKey is the old INTERNAL_API_KEY — used as fallback during migration.
// Pass "" to disable the legacy key.
func NewServiceKeyStore(repo ServiceKeyRepo, legacyKey string) *ServiceKeyStore {
	return &ServiceKeyStore{
		byHash:    make(map[string]*entity.ServiceAPIKey),
		repo:      repo,
		legacyKey: legacyKey,
	}
}

// LoadAll fetches all active keys from the database into memory.
// Call once at startup. Can be called again to hot-reload keys.
func (s *ServiceKeyStore) LoadAll(ctx context.Context) error {
	keys, err := s.repo.ListActive(ctx)
	if err != nil {
		return fmt.Errorf("load service keys: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.byHash = make(map[string]*entity.ServiceAPIKey, len(keys))
	for i := range keys {
		s.byHash[keys[i].KeyHash] = &keys[i]
	}
	slog.Info("service key store loaded", "active_keys", len(keys))
	return nil
}

// ResolveResult holds the result of resolving a bearer token.
type ResolveResult struct {
	ServiceName  string
	Scopes       []string
	RateLimitRPM int
	KeyID        int64
	IsLegacy     bool // true if resolved via the old shared INTERNAL_API_KEY
}

// Resolve looks up a bearer token and returns the service identity.
// Returns nil if the token is invalid or the key is not active.
//
// Resolution order:
// 1. Hash the raw token → look up in the in-memory cache
// 2. If not found and legacyKey matches → return a full-access legacy result
// 3. Otherwise → nil (unauthorized)
func (s *ServiceKeyStore) Resolve(rawToken string) *ResolveResult {
	if rawToken == "" {
		return nil
	}

	// Hash and lookup.
	hash := hashKey(rawToken)
	s.mu.RLock()
	key := s.byHash[hash]
	s.mu.RUnlock()

	if key != nil {
		// Async touch last_used_at (fire-and-forget).
		if s.repo != nil {
			go s.repo.TouchLastUsed(context.Background(), key.ID)
		}
		return &ResolveResult{
			ServiceName:  key.ServiceName,
			Scopes:       key.Scopes,
			RateLimitRPM: key.RateLimitRPM,
			KeyID:        key.ID,
		}
	}

	// Legacy fallback: the old shared INTERNAL_API_KEY.
	if s.legacyKey != "" && subtle.ConstantTimeCompare([]byte(rawToken), []byte(s.legacyKey)) == 1 {
		return &ResolveResult{
			ServiceName:  "legacy",
			Scopes:       entity.AllScopes(), // full access
			RateLimitRPM: 0,                  // unlimited
			IsLegacy:     true,
		}
	}

	return nil
}

// HasScope checks if a resolved result has the required scope.
func (r *ResolveResult) HasScope(scope string) bool {
	for _, s := range r.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// hashKey computes the SHA-256 hex digest of a raw API key.
func hashKey(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}

// HashKey is exported for key generation (admin API).
func HashKey(raw string) string { return hashKey(raw) }
