package db

import (
	"context"
	"time"

	"github.com/specgate/doc-registry/internal/settings"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SettingsRepository persists settings in the configured Postgres database via GORM.
type SettingsRepository struct {
	db *gorm.DB
}

func NewSettingsRepository(db *gorm.DB) *SettingsRepository {
	return &SettingsRepository{db: db}
}

// GetAll returns every setting row.
func (r *SettingsRepository) GetAll(ctx context.Context) ([]settings.Setting, error) {
	var out []settings.Setting
	return out, r.db.WithContext(ctx).Order("key ASC").Find(&out).Error
}

// PutBatch upserts a batch of settings in a single transaction.
func (r *SettingsRepository) PutBatch(ctx context.Context, items []settings.Setting) error {
	if len(items) == 0 {
		return nil
	}
	now := time.Now().UTC()
	for i := range items {
		items[i].UpdatedAt = now
	}
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "key"}},
			DoUpdates: clause.AssignmentColumns([]string{"value", "encrypted", "updated_at"}),
		}).
		Create(&items).Error
}
