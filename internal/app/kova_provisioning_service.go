package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/adapter/kovaprov"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// KovaProvisioningService glues together the org store, the org-services
// store, and the kovaprov client to give handlers a single entry point for
// "give org X a kova workspace".
//
// All three dependencies are interfaces so unit tests can sub the persistence
// in-memory and the provisioner with a stub.
type KovaProvisioningService struct {
	orgs        kovaOrgLookup
	services    orgServiceStore
	usage       usageEventStore
	provisioner kovaProvisioner

	// keyPrefixLen is how many leading chars of the raw admin key we keep
	// for log triage. 8 is the convention shared with org_api_keys.
	keyPrefixLen int
}

// NewKovaProvisioningService wires the service. orgs / services / usage are
// the persistence interfaces, provisioner is the R6 client (or its mock).
func NewKovaProvisioningService(
	orgs kovaOrgLookup,
	services orgServiceStore,
	usage usageEventStore,
	provisioner kovaProvisioner,
) *KovaProvisioningService {
	return &KovaProvisioningService{
		orgs:         orgs,
		services:     services,
		usage:        usage,
		provisioner:  provisioner,
		keyPrefixLen: 8,
	}
}

// ProvisionResult is what the caller gets back: the freshly minted credentials
// PLUS the persisted row. The raw admin key lives ONLY here — it is never
// stored, never logged in clear text, and never returned again.
type ProvisionResult struct {
	Service  *entity.OrgService
	AdminKey string
	IsMock   bool
}

// ProvisionKovaTester triggers an R6 provisioning request for the org and
// records the outcome. It is idempotent on (org_id, "kova"): re-invoking
// after a successful provision returns the existing record + an empty
// AdminKey (the original key has been forgotten by the platform, so the
// caller must rotate if they lost it).
//
// Status transitions:
//
//	(no row)      → POST attempted → success → row inserted as 'active'
//	(no row)      → POST attempted → failure → row inserted as 'failed'
//	'failed' row  → re-POST → success         → row updated to 'active'
//	'active' row  → re-POST → no-op           → returns existing row
func (s *KovaProvisioningService) ProvisionKovaTester(ctx context.Context, orgID int64) (*ProvisionResult, error) {
	if orgID <= 0 {
		return nil, errors.New("kovaprov: org_id must be positive")
	}

	org, err := s.orgs.GetByID(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("kovaprov: lookup org: %w", err)
	}
	if org == nil {
		return nil, ErrOrgNotFound
	}

	// Idempotency: do not re-provision a healthy active row. Surfacing the
	// existing record is more useful than 409 because the caller almost
	// always wants the base_url next.
	existing, err := s.services.Get(ctx, orgID, entity.OrgServiceKova)
	if err != nil {
		return nil, fmt.Errorf("kovaprov: lookup existing service: %w", err)
	}
	if existing != nil && existing.Status == entity.OrgServiceStatusActive {
		return &ProvisionResult{
			Service:  existing,
			AdminKey: "", // forgotten on purpose; rotate if lost
			IsMock:   s.provisioner.IsMock(),
		}, nil
	}

	// Trigger the R6 RPC. mock-mode short-circuits inside the client; we
	// intentionally do not branch on IsMock here so there is one execution
	// path for tests and prod.
	provisionReq := kovaprov.ProvisionRequest{
		TesterName: org.Slug,
		OrgID:      org.ID,
		AccountID:  org.OwnerAccountID,
	}
	resp, err := s.provisioner.Provision(ctx, provisionReq)
	if err != nil {
		// Best-effort persist of the failure so operators can see it.
		// Errors here are not propagated — the original RPC error is more
		// actionable than "I failed twice".
		failedRow := s.buildRowFromFailure(orgID, provisionReq, err, existing)
		_ = s.services.Upsert(ctx, failedRow)
		return nil, fmt.Errorf("kovaprov: r6 provision: %w", err)
	}

	row := s.buildRowFromSuccess(orgID, resp, existing)
	if err := s.services.Upsert(ctx, row); err != nil {
		// We have a live tester on R6 but failed to persist its key locally.
		// Surface the error rather than swallowing — the caller can retry
		// the RPC (the R6 side is idempotent on tester_name).
		return nil, fmt.Errorf("kovaprov: persist provision result: %w", err)
	}

	return &ProvisionResult{
		Service:  row,
		AdminKey: resp.AdminKey,
		IsMock:   s.provisioner.IsMock(),
	}, nil
}

// GetKovaService returns the persisted service row for an org's kova service.
// Returns (nil, ErrOrgServiceNotProvisioned) when the org has not yet been
// provisioned — handlers should map this to 404 rather than 500.
func (s *KovaProvisioningService) GetKovaService(ctx context.Context, orgID, callerID int64) (*entity.OrgService, error) {
	if orgID <= 0 {
		return nil, errors.New("kovaprov: org_id must be positive")
	}
	// Membership check — only members of the org may read its provisioning
	// info. We rely on orgs.GetMember rather than reusing OrganizationService
	// to keep the dependency graph linear (no app→app cycle).
	member, err := s.orgs.GetMember(ctx, orgID, callerID)
	if err != nil {
		return nil, fmt.Errorf("kovaprov: lookup membership: %w", err)
	}
	if member == nil {
		return nil, ErrPermissionDenied
	}
	row, err := s.services.Get(ctx, orgID, entity.OrgServiceKova)
	if err != nil {
		return nil, fmt.Errorf("kovaprov: lookup service: %w", err)
	}
	if row == nil {
		return nil, ErrOrgServiceNotProvisioned
	}
	return row, nil
}

// RecordUsage appends a usage event reported by a kova worker. The caller is
// responsible for authn (this is /internal/v1/*); we only validate field
// shapes here.
func (s *KovaProvisioningService) RecordUsage(ctx context.Context, ev *entity.UsageEvent) error {
	if ev == nil {
		return errors.New("kovaprov: nil usage event")
	}
	if ev.OrgID <= 0 {
		return errors.New("kovaprov: usage event org_id must be positive")
	}
	if ev.Service == "" {
		ev.Service = entity.OrgServiceKova
	}
	if ev.OccurredAt.IsZero() {
		ev.OccurredAt = time.Now().UTC()
	}
	if ev.TokensIn < 0 || ev.TokensOut < 0 || ev.CostMicros < 0 {
		return errors.New("kovaprov: usage event counters must be non-negative")
	}
	if ev.Metadata == nil {
		ev.Metadata = entity.OrgServiceMeta{}
	}
	return s.usage.CreateUsageEvent(ctx, ev)
}

// buildRowFromSuccess produces the row to persist after a successful provision.
// keyPrefix uses the un-trimmed first N chars of the raw key so log scans
// match up with what the customer sees.
func (s *KovaProvisioningService) buildRowFromSuccess(orgID int64, resp *kovaprov.ProvisionResponse, existing *entity.OrgService) *entity.OrgService {
	now := time.Now().UTC()
	hash := sha256.Sum256([]byte(resp.AdminKey))
	prefix := resp.AdminKey
	if len(prefix) > s.keyPrefixLen {
		prefix = prefix[:s.keyPrefixLen]
	}
	meta := entity.OrgServiceMeta{
		"provisioner": map[string]any{
			"mode": s.provisionerMode(),
		},
	}
	if existing != nil {
		// Preserve any operator-set keys (e.g. budget overrides) that
		// existed pre-rotation.
		for k, v := range existing.Metadata {
			if _, conflict := meta[k]; !conflict {
				meta[k] = v
			}
		}
	}
	return &entity.OrgService{
		OrgID:         orgID,
		Service:       entity.OrgServiceKova,
		Status:        entity.OrgServiceStatusActive,
		BaseURL:       resp.BaseURL,
		KeyHash:       hex.EncodeToString(hash[:]),
		KeyPrefix:     prefix,
		TesterName:    resp.TesterName,
		Port:          resp.Port,
		Metadata:      meta,
		ProvisionedAt: &now,
	}
}

// buildRowFromFailure produces the row to persist after a failed provision.
// We carry the error message in metadata so operators can tell pending /
// failed apart without grepping logs.
func (s *KovaProvisioningService) buildRowFromFailure(orgID int64, req kovaprov.ProvisionRequest, provErr error, existing *entity.OrgService) *entity.OrgService {
	now := time.Now().UTC()
	meta := entity.OrgServiceMeta{
		"provisioner": map[string]any{
			"mode":     s.provisionerMode(),
			"error":    provErr.Error(),
			"attempted_at": now.Format(time.RFC3339),
		},
	}
	row := &entity.OrgService{
		OrgID:      orgID,
		Service:    entity.OrgServiceKova,
		Status:     entity.OrgServiceStatusFailed,
		TesterName: req.TesterName,
		Metadata:   meta,
	}
	// Preserve previously-stored credentials on a failed re-provision so the
	// customer's old (still-valid) key keeps working until the next success.
	if existing != nil {
		row.BaseURL = existing.BaseURL
		row.KeyHash = existing.KeyHash
		row.KeyPrefix = existing.KeyPrefix
		row.Port = existing.Port
		row.ProvisionedAt = existing.ProvisionedAt
	}
	return row
}

func (s *KovaProvisioningService) provisionerMode() string {
	if s.provisioner.IsMock() {
		return "mock"
	}
	return "live"
}

// ----- dependency interfaces -----

// kovaOrgLookup is the slice of orgStore that this service needs. We do not
// reuse the full orgStore interface to keep the dependency surface narrow
// and the mock implementations small.
type kovaOrgLookup interface {
	GetByID(ctx context.Context, id int64) (*entity.Organization, error)
	GetMember(ctx context.Context, orgID, accountID int64) (*entity.OrgMember, error)
}

// orgServiceStore persists per-org provisioned-service rows.
type orgServiceStore interface {
	Get(ctx context.Context, orgID int64, service string) (*entity.OrgService, error)
	Upsert(ctx context.Context, s *entity.OrgService) error
}

// usageEventStore persists worker-reported usage events.
type usageEventStore interface {
	CreateUsageEvent(ctx context.Context, ev *entity.UsageEvent) error
}

// kovaProvisioner is the R6 RPC contract — implemented by *kovaprov.Client.
type kovaProvisioner interface {
	Provision(ctx context.Context, req kovaprov.ProvisionRequest) (*kovaprov.ProvisionResponse, error)
	IsMock() bool
}

// ----- sentinel errors -----

// ErrOrgNotFound is returned when ProvisionKovaTester / GetKovaService hits
// an unknown org_id. Handlers map this to 404.
var ErrOrgNotFound = errorString("organization not found")

// ErrOrgServiceNotProvisioned is returned by GetKovaService when the org
// exists but no provisioning has run yet. Handlers map this to 404 with a
// distinct error code so the dashboard can render "click to provision".
var ErrOrgServiceNotProvisioned = errorString("org service not provisioned")

// ErrPermissionDenied is returned when the caller is not a member of the
// requested org. Handlers map this to 403.
var ErrPermissionDenied = errorString("permission denied: not a member of this organization")
