package repo

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"github.com/hanmahong5-arch/lurus-platform/modules/notification/internal/domain/entity"
)

// TemplateRepo manages notification templates.
type TemplateRepo struct {
	db *gorm.DB
}

// NewTemplateRepo creates a new TemplateRepo.
func NewTemplateRepo(db *gorm.DB) *TemplateRepo {
	return &TemplateRepo{db: db}
}

// FindByEventAndChannel returns the template for a specific event type and channel.
func (r *TemplateRepo) FindByEventAndChannel(ctx context.Context, eventType string, channel entity.Channel) (*entity.Template, error) {
	var t entity.Template
	err := r.db.WithContext(ctx).
		Where("event_type = ? AND channel = ? AND enabled = true", eventType, channel).
		First(&t).Error
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// List returns all templates.
func (r *TemplateRepo) List(ctx context.Context) ([]entity.Template, error) {
	var items []entity.Template
	if err := r.db.WithContext(ctx).Order("event_type, channel").Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list templates: %w", err)
	}
	return items, nil
}

// Upsert creates or updates a template.
func (r *TemplateRepo) Upsert(ctx context.Context, t *entity.Template) error {
	result := r.db.WithContext(ctx).
		Where("event_type = ? AND channel = ?", t.EventType, t.Channel).
		Assign(entity.Template{
			Title:    t.Title,
			Body:     t.Body,
			Priority: t.Priority,
			Enabled:  t.Enabled,
		}).
		FirstOrCreate(t)
	if result.Error != nil {
		return fmt.Errorf("upsert template: %w", result.Error)
	}
	return nil
}

// Delete removes a template by ID.
func (r *TemplateRepo) Delete(ctx context.Context, id int64) error {
	if err := r.db.WithContext(ctx).Delete(&entity.Template{}, id).Error; err != nil {
		return fmt.Errorf("delete template: %w", err)
	}
	return nil
}
