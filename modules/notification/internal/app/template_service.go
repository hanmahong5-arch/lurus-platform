package app

import (
	"context"

	"github.com/hanmahong5-arch/lurus-platform/modules/notification/internal/adapter/repo"
	"github.com/hanmahong5-arch/lurus-platform/modules/notification/internal/domain/entity"
)

// TemplateService manages notification templates (admin CRUD).
type TemplateService struct {
	repo *repo.TemplateRepo
}

// NewTemplateService creates a TemplateService.
func NewTemplateService(repo *repo.TemplateRepo) *TemplateService {
	return &TemplateService{repo: repo}
}

// List returns all templates.
func (s *TemplateService) List(ctx context.Context) ([]entity.Template, error) {
	return s.repo.List(ctx)
}

// Upsert creates or updates a template.
func (s *TemplateService) Upsert(ctx context.Context, t *entity.Template) error {
	return s.repo.Upsert(ctx, t)
}

// Delete removes a template by ID.
func (s *TemplateService) Delete(ctx context.Context, id int64) error {
	return s.repo.Delete(ctx, id)
}
