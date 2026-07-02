package db

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/specgate/doc-registry/internal/governancefiles"
)

// GovernanceFilesRepository is a GORM-backed implementation of governancefiles.Store.
type GovernanceFilesRepository struct {
	db *gorm.DB
}

// NewGovernanceFilesRepository creates a GovernanceFilesRepository backed by gdb.
func NewGovernanceFilesRepository(db *gorm.DB) *GovernanceFilesRepository {
	return &GovernanceFilesRepository{db: db}
}

// Compile-time assertion that GovernanceFilesRepository satisfies governancefiles.Store.
var _ governancefiles.Store = (*GovernanceFilesRepository)(nil)

// Create inserts a new governance_files row (typically with status=pending).
func (r *GovernanceFilesRepository) Create(ctx context.Context, f governancefiles.File) (*governancefiles.File, error) {
	if err := r.db.WithContext(ctx).Create(&f).Error; err != nil {
		return nil, err
	}
	return &f, nil
}

// Commit flips status from pending to ready and refreshes last_used_at.
// Returns governancefiles.ErrNotFound if the row does not exist or is not pending.
func (r *GovernanceFilesRepository) Commit(ctx context.Context, id string, now time.Time) (*governancefiles.File, error) {
	var out governancefiles.File
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&out, "id = ? AND status = ?", id, governancefiles.StatusPending).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return governancefiles.ErrNotFound
			}
			return err
		}
		out.Status = governancefiles.StatusReady
		out.LastUsedAt = now
		return tx.Save(&out).Error
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// Get returns a single ready row; governancefiles.ErrNotFound otherwise.
func (r *GovernanceFilesRepository) Get(ctx context.Context, id string) (*governancefiles.File, error) {
	var out governancefiles.File
	err := r.db.WithContext(ctx).First(&out, "id = ? AND status = ?", id, governancefiles.StatusReady).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, governancefiles.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// List returns ready rows ordered by last_used_at DESC plus the filtered total count.
func (r *GovernanceFilesRepository) List(ctx context.Context, f governancefiles.ListFilter) ([]governancefiles.File, int64, error) {
	q := r.db.WithContext(ctx).
		Model(&governancefiles.File{}).
		Where("status = ?", governancefiles.StatusReady)

	if s := strings.TrimSpace(f.Q); s != "" {
		q = q.Where("LOWER(name) LIKE ?", "%"+strings.ToLower(s)+"%")
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}

	var items []governancefiles.File
	if err := q.Order("last_used_at DESC").Limit(limit).Offset(offset).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// Touch refreshes last_used_at on a ready row.
// Returns governancefiles.ErrNotFound if the row is missing or not ready.
func (r *GovernanceFilesRepository) Touch(ctx context.Context, id string, now time.Time) (*governancefiles.File, error) {
	var out governancefiles.File
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&out, "id = ? AND status = ?", id, governancefiles.StatusReady).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return governancefiles.ErrNotFound
			}
			return err
		}
		out.LastUsedAt = now
		return tx.Save(&out).Error
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// Delete removes the row regardless of status and returns the deleted row's object_key.
// Returns governancefiles.ErrNotFound if the row does not exist.
func (r *GovernanceFilesRepository) Delete(ctx context.Context, id string) (string, error) {
	var existing governancefiles.File
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&existing, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return governancefiles.ErrNotFound
			}
			return err
		}
		return tx.Delete(&existing).Error
	})
	if err != nil {
		return "", err
	}
	return existing.ObjectKey, nil
}

// DeleteStaleReady deletes ready rows with last_used_at < cutoff that are NOT
// referenced by any published artifact. Returns the deleted rows' object_keys
// for S3 cleanup.
//
// File-ref artifacts (POST /artifacts with `file_refs`) point at the same S3
// object as the governance_files row, so without this guard the TTL purger would
// delete the S3 object out from under the artifact and break GET
// /artifacts/{id}/files/{key}. The NOT EXISTS subqueries treat artifact_files
// (by object_key) and artifact_attachments (by governance_files.id) as permanent
// "pins" on the underlying object — a feature reference attachment must survive
// the TTL window the same way an artifact file does.
func (r *GovernanceFilesRepository) DeleteStaleReady(ctx context.Context, cutoff time.Time) ([]string, error) {
	return r.deleteWhere(ctx,
		"status = ? AND last_used_at < ? "+
			"AND NOT EXISTS (SELECT 1 FROM artifact_files af WHERE af.s3_path = governance_files.object_key) "+
			"AND NOT EXISTS (SELECT 1 FROM artifact_attachments aa WHERE aa.governance_file_id = governance_files.id)",
		governancefiles.StatusReady, cutoff,
	)
}

// DeleteStalePending deletes pending rows with created_at < cutoff.
// Returns the deleted rows' object_keys for S3 cleanup.
func (r *GovernanceFilesRepository) DeleteStalePending(ctx context.Context, cutoff time.Time) ([]string, error) {
	return r.deleteWhere(ctx,
		"status = ? AND created_at < ?",
		governancefiles.StatusPending, cutoff,
	)
}

// deleteWhere is a shared helper that finds rows matching where/args, collects
// their object_keys, then deletes them — all inside a single transaction.
func (r *GovernanceFilesRepository) deleteWhere(ctx context.Context, where string, args ...any) ([]string, error) {
	var keys []string
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []governancefiles.File
		if err := tx.Where(where, args...).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		ids := make([]string, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.ID)
			keys = append(keys, row.ObjectKey)
		}
		return tx.Where("id IN ?", ids).Delete(&governancefiles.File{}).Error
	})
	if err != nil {
		return nil, err
	}
	return keys, nil
}
