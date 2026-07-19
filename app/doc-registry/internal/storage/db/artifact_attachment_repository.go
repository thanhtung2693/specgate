package db

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	"github.com/specgate/doc-registry/internal/artifactattachment"
)

// ArtifactAttachmentRepository is a GORM-backed artifactattachment.Store.
type ArtifactAttachmentRepository struct {
	db *gorm.DB
}

func NewArtifactAttachmentRepository(db *gorm.DB) *ArtifactAttachmentRepository {
	return &ArtifactAttachmentRepository{db: db}
}

var _ artifactattachment.Store = (*ArtifactAttachmentRepository)(nil)

// Create inserts a new artifact_attachments row.
func (r *ArtifactAttachmentRepository) Create(
	ctx context.Context,
	a artifactattachment.Attachment,
) (*artifactattachment.Attachment, error) {
	if strings.TrimSpace(a.WorkspaceID) == "" {
		return nil, artifactattachment.ErrNotFound
	}
	if err := r.db.WithContext(ctx).Create(&a).Error; err != nil {
		return nil, err
	}
	return &a, nil
}

// ListByFeature returns a feature's attachments, newest first.
func (r *ArtifactAttachmentRepository) ListByFeature(
	ctx context.Context,
	workspaceID string,
	featureID string,
) ([]artifactattachment.Attachment, error) {
	var items []artifactattachment.Attachment
	err := r.db.WithContext(ctx).
		Where("workspace_id = ? AND feature_id = ?", workspaceID, featureID).
		Order("created_at DESC").
		Find(&items).Error
	if err != nil {
		return nil, err
	}
	return items, nil
}

// Get returns a single attachment; artifactattachment.ErrNotFound otherwise.
func (r *ArtifactAttachmentRepository) Get(
	ctx context.Context,
	workspaceID string,
	id string,
) (*artifactattachment.Attachment, error) {
	var out artifactattachment.Attachment
	err := r.db.WithContext(ctx).First(&out, "workspace_id = ? AND id = ?", workspaceID, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, artifactattachment.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// Delete removes the row. Returns artifactattachment.ErrNotFound if missing.
func (r *ArtifactAttachmentRepository) Delete(ctx context.Context, workspaceID, id string) error {
	res := r.db.WithContext(ctx).Delete(&artifactattachment.Attachment{}, "workspace_id = ? AND id = ?", workspaceID, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return artifactattachment.ErrNotFound
	}
	return nil
}
