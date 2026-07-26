package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/specgate/doc-registry/internal/integrations"
	"gorm.io/gorm"
)

func (r *IntegrationRepository) ListResources(ctx context.Context, integrationID string) ([]integrations.Resource, error) {
	var rows []integrations.Resource
	q := r.db.WithContext(ctx).Where("integration_id = ?", integrationID)
	q = scopeIntegrationRows(q, ctx, "integration_resources.integration_id")
	if err := q.
		Order("resource_type ASC, external_key ASC, created_at ASC").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list resources %s: %w", integrationID, err)
	}
	for i := range rows {
		rows[i].HasWebhookSecret = rows[i].WebhookSecretEncrypted != ""
	}
	return rows, nil
}

func (r *IntegrationRepository) GetResource(ctx context.Context, integrationID string, resourceID string) (*integrations.Resource, error) {
	var row integrations.Resource
	q := r.db.WithContext(ctx).Where("integration_id = ? AND id = ?", integrationID, resourceID)
	q = scopeIntegrationRows(q, ctx, "integration_resources.integration_id")
	if err := q.
		First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, integrations.ErrNotFound
		}
		return nil, fmt.Errorf("get resource %s: %w", resourceID, err)
	}
	row.HasWebhookSecret = row.WebhookSecretEncrypted != ""
	return &row, nil
}

func (r *IntegrationRepository) CreateResource(ctx context.Context, in integrations.Resource) (*integrations.Resource, error) {
	if integrations.WorkspaceID(ctx) != "" {
		if _, err := r.GetIntegration(ctx, in.IntegrationID); err != nil {
			return nil, err
		}
	}
	now := time.Now().UTC()
	if in.ID == "" {
		in.ID = uuid.NewString()
	}
	if in.CreatedAt.IsZero() {
		in.CreatedAt = now
	}
	in.UpdatedAt = now
	if err := r.db.WithContext(ctx).Create(&in).Error; err != nil {
		if isUniqueViolation(err) {
			return nil, integrations.ErrConflict
		}
		return nil, fmt.Errorf("create resource: %w", err)
	}
	in.HasWebhookSecret = in.WebhookSecretEncrypted != ""
	return &in, nil
}

func (r *IntegrationRepository) UpdateResourceWebhookSecretEncrypted(ctx context.Context, integrationID string, resourceID string, encrypted string) error {
	db := r.db.WithContext(ctx).Model(&integrations.Resource{}).
		Where("integration_id = ? AND id = ?", integrationID, resourceID)
	db = scopeIntegrationRows(db, ctx, "integration_resources.integration_id")
	res := db.
		Updates(map[string]any{
			"webhook_secret_encrypted": encrypted,
			"updated_at":               time.Now().UTC(),
		})
	if res.Error != nil {
		return fmt.Errorf("update resource webhook secret encrypted %s: %w", resourceID, res.Error)
	}
	if res.RowsAffected == 0 {
		return integrations.ErrNotFound
	}
	return nil
}

func (r *IntegrationRepository) UpdateResourceConfigJSON(ctx context.Context, integrationID string, resourceID string, configJSON string) error {
	db := r.db.WithContext(ctx).Model(&integrations.Resource{}).
		Where("integration_id = ? AND id = ?", integrationID, resourceID)
	db = scopeIntegrationRows(db, ctx, "integration_resources.integration_id")
	res := db.
		Updates(map[string]any{
			"config_json": configJSON,
			"updated_at":  time.Now().UTC(),
		})
	if res.Error != nil {
		return fmt.Errorf("update resource config json %s: %w", resourceID, res.Error)
	}
	if res.RowsAffected == 0 {
		return integrations.ErrNotFound
	}
	return nil
}

func (r *IntegrationRepository) DeleteResource(ctx context.Context, integrationID string, resourceID string) error {
	db := r.db.WithContext(ctx).
		Where("integration_id = ? AND id = ?", integrationID, resourceID)
	db = scopeIntegrationRows(db, ctx, "integration_resources.integration_id")
	res := db.
		Delete(&integrations.Resource{})
	if res.Error != nil {
		return fmt.Errorf("delete resource %s: %w", resourceID, res.Error)
	}
	if res.RowsAffected == 0 {
		return integrations.ErrNotFound
	}
	return nil
}

func (r *IntegrationRepository) FindResourceByProvider(ctx context.Context, provider string, resourceType string, externalID string, externalKey string) (*integrations.Integration, *integrations.Resource, error) {
	base := func() *gorm.DB {
		q := r.db.WithContext(ctx).
			Table("integration_resources AS r").
			Select("r.*").
			Joins("JOIN integrations AS i ON i.id = r.integration_id").
			Where("i.provider = ? AND r.resource_type = ?", provider, resourceType)
		if workspaceID := integrations.WorkspaceID(ctx); workspaceID != "" {
			q = q.Where("i.workspace_id = ?", workspaceID)
		}
		return q
	}
	loadIntegration := func(resource integrations.Resource) (*integrations.Integration, *integrations.Resource, error) {
		integration, getErr := r.GetIntegration(ctx, resource.IntegrationID)
		return integration, &resource, getErr
	}
	if externalID != "" {
		var resource integrations.Resource
		err := base().Where("r.external_id = ?", externalID).Order("r.created_at ASC").First(&resource).Error
		if err == nil {
			return loadIntegration(resource)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, fmt.Errorf("find resource by provider external id %s: %w", externalID, err)
		}
	}
	if externalKey != "" {
		var resource integrations.Resource
		err := base().Where("r.external_key = ?", externalKey).Order("r.created_at ASC").First(&resource).Error
		if err == nil {
			return loadIntegration(resource)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, fmt.Errorf("find resource by provider external key %s: %w", externalKey, err)
		}
	}
	return nil, nil, integrations.ErrNotFound
}
