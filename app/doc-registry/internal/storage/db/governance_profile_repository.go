package db

import (
	"context"

	"gorm.io/gorm"

	"github.com/specgate/doc-registry/internal/governanceprofile"
)

type GovernanceProfileRepository struct {
	db *gorm.DB
}

func NewGovernanceProfileRepository(db *gorm.DB) *GovernanceProfileRepository {
	return &GovernanceProfileRepository{db: db}
}

func (r *GovernanceProfileRepository) Insert(ctx context.Context, p *governanceprofile.Profile) error {
	if err := r.db.WithContext(ctx).Create(p).Error; err != nil {
		if isUniqueViolation(err) {
			return governanceprofile.ErrConflict
		}
		return err
	}
	return nil
}

func (r *GovernanceProfileRepository) ListActive(ctx context.Context) ([]governanceprofile.Profile, error) {
	var out []governanceprofile.Profile
	err := r.db.WithContext(ctx).
		Where("status = ?", governanceprofile.StatusActive).
		Order("namespace ASC, key ASC, created_at ASC").
		Find(&out).Error
	return out, err
}
