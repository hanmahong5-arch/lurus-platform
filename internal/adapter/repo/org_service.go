package repo

import (
	"context"
	"errors"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// OrgServiceRepo persists per-org provisioned services (migration 029).
//
// All methods are admin-path: there is no RLS gating because callers are
// already authenticated either as the org owner (for the user-facing GET)
// or as an internal service key holder (for the provisioning POST).
type OrgServiceRepo struct {
	db *gorm.DB
}

// NewOrgServiceRepo wires a repo against a live GORM handle.
func NewOrgServiceRepo(db *gorm.DB) *OrgServiceRepo { return &OrgServiceRepo{db: db} }

// Get returns the (orgID, service) row, or (nil, nil) when not yet provisioned.
// Caller-side gates decide whether "not provisioned" maps to 404 or "trigger".
func (r *OrgServiceRepo) Get(ctx context.Context, orgID int64, service string) (*entity.OrgService, error) {
	var s entity.OrgService
	err := r.db.WithContext(ctx).
		Where("org_id = ? AND service = ?", orgID, service).
		First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// ListByOrg returns every service currently provisioned for the given org.
// Used by the user-facing "show me everything I'm paying for" view (future
// dashboard tile); keeping it in the repo today so handlers can compose.
func (r *OrgServiceRepo) ListByOrg(ctx context.Context, orgID int64) ([]entity.OrgService, error) {
	var out []entity.OrgService
	err := r.db.WithContext(ctx).
		Where("org_id = ?", orgID).
		Order("service ASC").
		Find(&out).Error
	return out, err
}

// Upsert installs or refreshes the row for (orgID, service). Existing columns
// are overwritten with the supplied values — callers are expected to load the
// previous state, mutate the desired fields, and pass the merged record back.
//
// We deliberately collide on the composite primary key (org_id, service) so a
// failed → pending → active transition is one round-trip, not three.
func (r *OrgServiceRepo) Upsert(ctx context.Context, s *entity.OrgService) error {
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "org_id"}, {Name: "service"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"status", "base_url", "key_hash", "key_prefix",
				"tester_name", "port", "metadata", "provisioned_at", "updated_at",
			}),
		}).
		Create(s).Error
}

// CreateUsageEvent appends a usage row. Returns the assigned ID via the
// supplied pointer (GORM convention).
func (r *OrgServiceRepo) CreateUsageEvent(ctx context.Context, ev *entity.UsageEvent) error {
	return r.db.WithContext(ctx).Create(ev).Error
}
