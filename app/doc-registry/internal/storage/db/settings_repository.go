package db

import (
	"context"
	"errors"
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

// Get returns a single setting by key or nil if not found.
func (r *SettingsRepository) Get(ctx context.Context, key string) (*settings.Setting, error) {
	var s settings.Setting
	err := r.db.WithContext(ctx).Where("key = ?", key).First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &s, err
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

// Count returns the number of rows in the settings table.
func (r *SettingsRepository) Count(ctx context.Context) (int64, error) {
	var count int64
	return count, r.db.WithContext(ctx).Model(&settings.Setting{}).Count(&count).Error
}
