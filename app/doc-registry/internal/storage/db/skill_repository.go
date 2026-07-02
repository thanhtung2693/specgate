package db

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// SkillRow maps the skills table.
type SkillRow struct {
	ID          string    `gorm:"column:id;primaryKey"`
	Name        string    `gorm:"column:name"`
	Description string    `gorm:"column:description"`
	Prompt      string    `gorm:"column:prompt"`
	CreatedAt   time.Time `gorm:"column:created_at"`
	UpdatedAt   time.Time `gorm:"column:updated_at"`
}

func (SkillRow) TableName() string { return "skills" }

// SkillRepository persists skill definitions.
type SkillRepository struct {
	db *gorm.DB
}

func NewSkillRepository(db *gorm.DB) *SkillRepository {
	return &SkillRepository{db: db}
}

// List returns all skills ordered by updated_at descending.
func (r *SkillRepository) List(ctx context.Context) ([]SkillRow, error) {
	var rows []SkillRow
	err := r.db.WithContext(ctx).Order("updated_at DESC").Find(&rows).Error
	return rows, err
}

// Get returns one row or ErrRecordNotFound.
func (r *SkillRepository) Get(ctx context.Context, id string) (*SkillRow, error) {
	var row SkillRow
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *SkillRepository) Create(ctx context.Context, row *SkillRow) error {
	now := time.Now().UTC()
	if row.CreatedAt.IsZero() {
		row.CreatedAt = now
	}
	row.UpdatedAt = now
	return r.db.WithContext(ctx).Create(row).Error
}

func (r *SkillRepository) Update(ctx context.Context, row *SkillRow) error {
	row.UpdatedAt = time.Now().UTC()
	return r.db.WithContext(ctx).
		Model(&SkillRow{}).
		Where("id = ?", row.ID).
		Select("name", "description", "prompt", "updated_at").
		Updates(SkillRow{
			Name:        row.Name,
			Description: row.Description,
			Prompt:      row.Prompt,
			UpdatedAt:   row.UpdatedAt,
		}).Error
}

func (r *SkillRepository) Delete(ctx context.Context, id string) error {
	res := r.db.WithContext(ctx).Where("id = ?", id).Delete(&SkillRow{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
