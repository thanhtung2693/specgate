package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/specgate/doc-registry/internal/integrations"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *IntegrationRepository) UpsertDeliveryLink(ctx context.Context, in integrations.DeliveryLink) (*integrations.DeliveryLink, error) {
	if integrations.WorkspaceID(ctx) != "" {
		if _, err := r.GetIntegration(ctx, in.IntegrationID); err != nil {
			return nil, err
		}
		if in.ResourceID != "" {
			if _, err := r.GetResource(ctx, in.IntegrationID, in.ResourceID); err != nil {
				return nil, err
			}
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
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "resource_id"},
			{Name: "external_type"},
			{Name: "external_iid"},
		},
		DoUpdates: clause.AssignmentColumns([]string{
			"integration_id",
			"feature_id",
			"change_request_id",
			"external_id",
			"external_key",
			"url",
			"title",
			"state",
			"source_branch",
			"target_branch",
			"head_sha",
			"merge_commit_sha",
			"last_event_id",
			"updated_at",
		}),
	}).Create(&in).Error; err != nil {
		return nil, fmt.Errorf("upsert delivery link: %w", err)
	}
	var out integrations.DeliveryLink
	q := r.db.WithContext(ctx)
	q = scopeIntegrationRows(q, ctx, "integration_delivery_links.integration_id")
	if err := q.
		Where("resource_id = ? AND external_type = ? AND external_iid = ?", in.ResourceID, in.ExternalType, in.ExternalIID).
		First(&out).Error; err != nil {
		return nil, fmt.Errorf("get delivery link after upsert: %w", err)
	}
	return &out, nil
}

// ListDeliveryLinksByChangeRequest returns one work item's delivery links,
// constrained by the integration-owned workspace boundary, newest first.
func (r *IntegrationRepository) ListDeliveryLinksByChangeRequest(ctx context.Context, changeRequestID string) ([]integrations.DeliveryLink, error) {
	changeRequestID = strings.TrimSpace(changeRequestID)
	if changeRequestID == "" {
		return nil, nil
	}
	var out []integrations.DeliveryLink
	q := r.db.WithContext(ctx).Where("change_request_id = ?", changeRequestID)
	q = scopeIntegrationRows(q, ctx, "integration_delivery_links.integration_id")
	if err := q.Order("updated_at DESC").Find(&out).Error; err != nil {
		return nil, fmt.Errorf("list delivery links by change request %s: %w", changeRequestID, err)
	}
	return out, nil
}

// UpsertTrackerLink persists a work item's one primary Linear handoff link.
func (r *IntegrationRepository) UpsertTrackerLink(ctx context.Context, in integrations.TrackerLink) (*integrations.TrackerLink, error) {
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
	if in.State == "" {
		in.State = integrations.TrackerStateOpened
	}
	if strings.TrimSpace(in.ChangeRequestID) == "" || strings.TrimSpace(in.ResourceID) == "" {
		return nil, fmt.Errorf("%w: change_request_id and resource_id are required", integrations.ErrValidation)
	}
	resource, err := r.GetResource(ctx, in.IntegrationID, in.ResourceID)
	if err != nil {
		return nil, err
	}
	if resource.IntegrationID != in.IntegrationID {
		return nil, fmt.Errorf("%w: tracker resource does not belong to integration", integrations.ErrValidation)
	}
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "change_request_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"integration_id", "resource_id", "feature_id", "external_id", "external_key", "url", "title", "state", "tracker_state", "updated_at",
		}),
		Where: clause.Where{Exprs: []clause.Expression{gorm.Expr("tracker_links.integration_id = EXCLUDED.integration_id AND tracker_links.resource_id = EXCLUDED.resource_id")}},
	}).Create(&in).Error; err != nil {
		return nil, fmt.Errorf("upsert tracker link: %w", err)
	}
	var out integrations.TrackerLink
	q := r.db.WithContext(ctx)
	q = scopeIntegrationRows(q, ctx, "tracker_links.integration_id")
	if err := q.
		Where("change_request_id = ?", in.ChangeRequestID).
		First(&out).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			var existing integrations.TrackerLink
			if lookupErr := r.db.WithContext(ctx).Where("change_request_id = ?", in.ChangeRequestID).First(&existing).Error; lookupErr == nil {
				return nil, integrations.ErrConflict
			}
		}
		return nil, fmt.Errorf("get tracker link after upsert: %w", err)
	}
	if out.IntegrationID != in.IntegrationID || out.ResourceID != in.ResourceID {
		return nil, integrations.ErrConflict
	}
	return &out, nil
}

// TrackerLinkByExternal resolves a handoff-created issue link by its immutable
// external id or human key (newest wins). Returns (nil, nil) on no match so the
// caller can fall back to the exact SpecGate work-reference marker.
func (r *IntegrationRepository) TrackerLinkByExternal(ctx context.Context, integrationID, externalID, externalKey string) (*integrations.TrackerLink, error) {
	externalID = strings.TrimSpace(externalID)
	externalKey = strings.TrimSpace(externalKey)
	if integrationID == "" || (externalID == "" && externalKey == "") {
		return nil, nil
	}
	q := r.db.WithContext(ctx).Where("integration_id = ?", integrationID)
	q = scopeIntegrationRows(q, ctx, "tracker_links.integration_id")
	switch {
	case externalID != "" && externalKey != "":
		q = q.Where("external_id = ? OR external_key = ?", externalID, externalKey)
	case externalID != "":
		q = q.Where("external_id = ?", externalID)
	default:
		q = q.Where("external_key = ?", externalKey)
	}
	var out integrations.TrackerLink
	if err := q.Order("updated_at DESC").First(&out).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("tracker link by external: %w", err)
	}
	return &out, nil
}

// ListTrackerLinksByChangeRequest returns a work item's primary tracker issue.
func (r *IntegrationRepository) ListTrackerLinksByChangeRequest(ctx context.Context, changeRequestID string) ([]integrations.TrackerLink, error) {
	changeRequestID = strings.TrimSpace(changeRequestID)
	if changeRequestID == "" {
		return nil, nil
	}
	var out []integrations.TrackerLink
	q := r.db.WithContext(ctx).Where("change_request_id = ?", changeRequestID)
	q = scopeIntegrationRows(q, ctx, "tracker_links.integration_id")
	if err := q.
		Order("updated_at DESC").
		Find(&out).Error; err != nil {
		return nil, fmt.Errorf("list tracker links by change request %s: %w", changeRequestID, err)
	}
	return out, nil
}
