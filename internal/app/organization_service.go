// Package app contains use case orchestration — no framework types allowed here.
package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"regexp"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// slugRegexp validates org slugs: 3-32 lowercase alphanumeric or hyphen characters.
var slugRegexp = regexp.MustCompile(`^[a-z0-9-]{3,32}$`)

// OrganizationService orchestrates organization lifecycle, membership, API key, and wallet operations.
type OrganizationService struct {
	store orgStore
}

func NewOrganizationService(store orgStore) *OrganizationService {
	return &OrganizationService{store: store}
}

// Create registers a new organization and bootstraps owner membership and wallet.
func (s *OrganizationService) Create(ctx context.Context, name, slug string, ownerID int64) (*entity.Organization, error) {
	if !slugRegexp.MatchString(slug) {
		return nil, fmt.Errorf("invalid slug: must match ^[a-z0-9-]{3,32}$ (got %q)", slug)
	}
	org := &entity.Organization{
		Name:           name,
		Slug:           slug,
		OwnerAccountID: ownerID,
		Status:         "active",
		Plan:           "free",
	}
	if err := s.store.Create(ctx, org); err != nil {
		return nil, fmt.Errorf("create org: %w", err)
	}
	if err := s.store.AddMember(ctx, &entity.OrgMember{
		OrgID:     org.ID,
		AccountID: ownerID,
		Role:      "owner",
	}); err != nil {
		return nil, fmt.Errorf("add owner member: %w", err)
	}
	if _, err := s.store.GetOrCreateWallet(ctx, org.ID); err != nil {
		return nil, fmt.Errorf("create org wallet: %w", err)
	}
	return org, nil
}

// Get returns an organization by ID, enforcing that callerID is a member.
func (s *OrganizationService) Get(ctx context.Context, id, callerID int64) (*entity.Organization, error) {
	m, err := s.store.GetMember(ctx, id, callerID)
	if err != nil {
		return nil, fmt.Errorf("get membership: %w", err)
	}
	if m == nil {
		return nil, fmt.Errorf("permission denied: not a member of this organization")
	}
	return s.store.GetByID(ctx, id)
}

// ListMine returns all organizations the given account belongs to.
func (s *OrganizationService) ListMine(ctx context.Context, accountID int64) ([]entity.Organization, error) {
	return s.store.ListByAccountID(ctx, accountID)
}

// IsOwnerOrAdmin reports whether callerID is currently a member of orgID with
// role "owner" or "admin". Used by gates that need to pre-flight a mutation
// (e.g. QR join_org create) without the caller having to synthesize an error.
//
// Returns (false, nil) when there is no membership row — callers should treat
// this as "permission denied" rather than an error.
func (s *OrganizationService) IsOwnerOrAdmin(ctx context.Context, orgID, callerID int64) (bool, error) {
	m, err := s.store.GetMember(ctx, orgID, callerID)
	if err != nil {
		return false, fmt.Errorf("get membership: %w", err)
	}
	if m == nil {
		return false, nil
	}
	return m.Role == "owner" || m.Role == "admin", nil
}

// AddMember adds targetAccountID to the organization. callerID must be owner or admin.
func (s *OrganizationService) AddMember(ctx context.Context, orgID, callerID, targetAccountID int64, role string) error {
	caller, err := s.store.GetMember(ctx, orgID, callerID)
	if err != nil {
		return fmt.Errorf("get caller membership: %w", err)
	}
	if caller == nil || (caller.Role != "owner" && caller.Role != "admin") {
		return fmt.Errorf("permission denied: must be owner or admin to add members")
	}
	return s.store.AddMember(ctx, &entity.OrgMember{
		OrgID:     orgID,
		AccountID: targetAccountID,
		Role:      role,
	})
}

// RemoveMember removes targetAccountID from the organization.
// callerID must be owner or admin; the last owner cannot be removed.
func (s *OrganizationService) RemoveMember(ctx context.Context, orgID, callerID, targetAccountID int64) error {
	caller, err := s.store.GetMember(ctx, orgID, callerID)
	if err != nil {
		return fmt.Errorf("get caller membership: %w", err)
	}
	if caller == nil || (caller.Role != "owner" && caller.Role != "admin") {
		return fmt.Errorf("permission denied: must be owner or admin to remove members")
	}
	target, err := s.store.GetMember(ctx, orgID, targetAccountID)
	if err != nil {
		return fmt.Errorf("get target membership: %w", err)
	}
	if target == nil {
		return fmt.Errorf("member not found")
	}
	if target.Role == "owner" {
		members, err := s.store.ListMembers(ctx, orgID)
		if err != nil {
			return fmt.Errorf("list members: %w", err)
		}
		ownerCount := 0
		for _, m := range members {
			if m.Role == "owner" {
				ownerCount++
			}
		}
		if ownerCount <= 1 {
			return fmt.Errorf("cannot remove the last owner")
		}
	}
	return s.store.RemoveMember(ctx, orgID, targetAccountID)
}

// CreateAPIKey generates a new org-scoped API key. The raw key is returned only once.
// callerID must be a member of the organization.
func (s *OrganizationService) CreateAPIKey(ctx context.Context, orgID, callerID int64, name string) (string, *entity.OrgAPIKey, error) {
	m, err := s.store.GetMember(ctx, orgID, callerID)
	if err != nil {
		return "", nil, fmt.Errorf("get membership: %w", err)
	}
	if m == nil {
		return "", nil, fmt.Errorf("permission denied: not a member of this organization")
	}

	rawBytes := make([]byte, 32)
	if _, err := rand.Read(rawBytes); err != nil {
		return "", nil, fmt.Errorf("generate key entropy: %w", err)
	}
	rawKey := base64.RawURLEncoding.EncodeToString(rawBytes)
	sum := sha256.Sum256([]byte(rawKey))
	keyHash := hex.EncodeToString(sum[:])

	key := &entity.OrgAPIKey{
		OrgID:     orgID,
		KeyHash:   keyHash,
		KeyPrefix: rawKey[:8],
		Name:      name,
		CreatedBy: callerID,
		Status:    "active",
	}
	if err := s.store.CreateAPIKey(ctx, key); err != nil {
		return "", nil, fmt.Errorf("store api key: %w", err)
	}
	return rawKey, key, nil
}

// ListAPIKeys returns all API keys for an organization (key_hash is never exposed).
func (s *OrganizationService) ListAPIKeys(ctx context.Context, orgID int64) ([]entity.OrgAPIKey, error) {
	return s.store.ListAPIKeys(ctx, orgID)
}

// RevokeAPIKey marks a key as revoked. callerID must be owner or admin.
func (s *OrganizationService) RevokeAPIKey(ctx context.Context, orgID, callerID, keyID int64) error {
	caller, err := s.store.GetMember(ctx, orgID, callerID)
	if err != nil {
		return fmt.Errorf("get membership: %w", err)
	}
	if caller == nil || (caller.Role != "owner" && caller.Role != "admin") {
		return fmt.Errorf("permission denied: must be owner or admin to revoke api keys")
	}
	return s.store.RevokeAPIKey(ctx, keyID)
}

// ResolveAPIKey looks up an organization by a raw API key (used by internal services).
// It updates last_used_at on success and returns an error for unknown or revoked keys.
func (s *OrganizationService) ResolveAPIKey(ctx context.Context, rawKey string) (*entity.Organization, error) {
	sum := sha256.Sum256([]byte(rawKey))
	hash := hex.EncodeToString(sum[:])

	k, err := s.store.GetAPIKeyByHash(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("get api key: %w", err)
	}
	if k == nil || k.Status != "active" {
		return nil, fmt.Errorf("invalid or revoked api key")
	}
	// Best-effort touch — do not fail the resolve if the update fails.
	_ = s.store.TouchAPIKey(ctx, k.ID)

	return s.store.GetByID(ctx, k.OrgID)
}

// GetWallet returns (or lazily creates) the org's shared token wallet.
func (s *OrganizationService) GetWallet(ctx context.Context, orgID int64) (*entity.OrgWallet, error) {
	return s.store.GetOrCreateWallet(ctx, orgID)
}

// ListAll returns a paginated list of all organizations (admin use).
func (s *OrganizationService) ListAll(ctx context.Context, limit, offset int) ([]entity.Organization, error) {
	return s.store.ListAll(ctx, limit, offset)
}

// UpdateStatus sets the organization status (admin use).
func (s *OrganizationService) UpdateStatus(ctx context.Context, id int64, status string) error {
	return s.store.UpdateStatus(ctx, id, status)
}
