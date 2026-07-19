package db

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/specgate/doc-registry/internal/governancethreads"
)

// GovernanceThreadsRepository is a GORM-backed implementation of governancethreads.Store.
type GovernanceThreadsRepository struct {
	db *gorm.DB
}

func NewGovernanceThreadsRepository(db *gorm.DB) *GovernanceThreadsRepository {
	return &GovernanceThreadsRepository{db: db}
}

var _ governancethreads.Store = (*GovernanceThreadsRepository)(nil)

func (r *GovernanceThreadsRepository) Upsert(
	ctx context.Context,
	thread governancethreads.Thread,
) (*governancethreads.Thread, error) {
	if thread.WorkspaceID == "" {
		return nil, governancethreads.ErrNotFound
	}
	var existing governancethreads.Thread
	if err := r.db.WithContext(ctx).Where("thread_id = ?", thread.ThreadID).First(&existing).Error; err == nil {
		if existing.WorkspaceID != thread.WorkspaceID {
			return nil, governancethreads.ErrNotFound
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "thread_id"}},
		Where: clause.Where{Exprs: []clause.Expression{
			clause.Expr{SQL: "governance_threads.workspace_id = EXCLUDED.workspace_id"},
		}},
		DoUpdates: clause.AssignmentColumns([]string{
			"title",
			"preview",
			"archived",
			"updated_at",
		}),
	}).Create(&thread).Error; err != nil {
		return nil, err
	}
	var out governancethreads.Thread
	if err := r.db.WithContext(ctx).First(&out, "thread_id = ? AND workspace_id = ?", thread.ThreadID, thread.WorkspaceID).Error; err != nil {
		return nil, err
	}
	return &out, nil
}

func (r *GovernanceThreadsRepository) List(
	ctx context.Context,
	filter governancethreads.ListFilter,
) ([]governancethreads.Thread, int64, error) {
	q := r.db.WithContext(ctx).Model(&governancethreads.Thread{})
	if filter.WorkspaceID == "" {
		return nil, 0, governancethreads.ErrNotFound
	}
	q = q.Where("workspace_id = ?", filter.WorkspaceID)
	if !filter.IncludeArchived {
		q = q.Where("archived = ?", false)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	var items []governancethreads.Thread
	if err := q.Order("updated_at DESC").Limit(limit).Offset(offset).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (r *GovernanceThreadsRepository) Delete(
	ctx context.Context,
	workspaceID string,
	threadID string,
	now time.Time,
) error {
	res := r.db.WithContext(ctx).
		Model(&governancethreads.Thread{}).
		Where("workspace_id = ? AND thread_id = ?", workspaceID, threadID).
		Updates(map[string]any{
			"archived":   true,
			"updated_at": now,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 || errors.Is(res.Error, gorm.ErrRecordNotFound) {
		return governancethreads.ErrNotFound
	}
	return nil
}
