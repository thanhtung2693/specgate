package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/specgate/doc-registry/internal/governanceprofile"
	"github.com/specgate/doc-registry/internal/integrations"
	"github.com/specgate/doc-registry/internal/workboard"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *IntegrationRepository) CreateGovernanceFeedbackEvent(ctx context.Context, in integrations.GovernanceFeedbackEvent) (*integrations.GovernanceFeedbackEvent, error) {
	now := time.Now().UTC()
	in.WorkspaceID = strings.TrimSpace(in.WorkspaceID)
	in.IntegrationID = strings.TrimSpace(in.IntegrationID)
	in.ResourceID = strings.TrimSpace(in.ResourceID)
	in.WebhookEventID = strings.TrimSpace(in.WebhookEventID)
	in.DeliveryLinkID = strings.TrimSpace(in.DeliveryLinkID)
	in.FeatureID = strings.TrimSpace(in.FeatureID)
	in.ChangeRequestID = strings.TrimSpace(in.ChangeRequestID)
	in.ArtifactID = strings.TrimSpace(in.ArtifactID)
	in.EventType = strings.TrimSpace(in.EventType)
	in.PayloadJSON = strings.TrimSpace(in.PayloadJSON)
	in.Reason = strings.TrimSpace(in.Reason)
	in.Status = strings.TrimSpace(in.Status)
	if workspaceID := integrations.WorkspaceID(ctx); workspaceID != "" {
		if in.WorkspaceID != "" && in.WorkspaceID != workspaceID {
			return nil, fmt.Errorf("%w: workspace_id", integrations.ErrValidation)
		}
		in.WorkspaceID = workspaceID
		if in.IntegrationID == "" {
			if in.WorkspaceID == "" {
				return nil, fmt.Errorf("%w: workspace_id is required", integrations.ErrValidation)
			}
		} else if _, err := r.GetIntegration(ctx, in.IntegrationID); err != nil {
			return nil, err
		}
		if in.ResourceID != "" {
			if in.IntegrationID == "" {
				return nil, fmt.Errorf("%w: integration_id is required when resource_id is set", integrations.ErrValidation)
			}
			if _, err := r.GetResource(ctx, in.IntegrationID, in.ResourceID); err != nil {
				return nil, err
			}
		}
	} else if in.IntegrationID != "" {
		integration, err := r.GetIntegration(ctx, in.IntegrationID)
		if err != nil {
			return nil, err
		}
		in.WorkspaceID = strings.TrimSpace(integration.WorkspaceID)
	}
	if in.WorkspaceID == "" {
		return nil, fmt.Errorf("%w: workspace_id is required", integrations.ErrValidation)
	}
	if in.Status == "" {
		in.Status = integrations.FeedbackStatusReceived
	}
	if in.ID == "" {
		in.ID = uuid.NewString()
	}
	if in.CreatedAt.IsZero() {
		in.CreatedAt = now
	}
	in.UpdatedAt = now
	create := func(tx *gorm.DB) error {
		if in.EventType == integrations.FeedbackEventCodingAgentCompleted &&
			in.ChangeRequestID != "" {
			var cr workboard.ChangeRequest
			q := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("id = ? AND workspace_id = ?", in.ChangeRequestID, in.WorkspaceID)
			err := q.First(&cr).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if err == nil {
				var accepted int64
				if err := tx.Model(&workboard.GateRun{}).
					Where(
						"workspace_id = ? AND subject_kind = ? AND subject_id = ? AND gate = ? AND executor = ? AND state = ?",
						in.WorkspaceID,
						workboard.GateRunSubjectChangeRequest,
						in.ChangeRequestID,
						governanceprofile.DeliveryReviewGateKey,
						workboard.GateRunExecutorHuman,
						workboard.NextActionStatePass,
					).
					Count(&accepted).Error; err != nil {
					return err
				}
				if cr.Archived || accepted > 0 {
					return fmt.Errorf("%w: delivery is already accepted; create a new work item", integrations.ErrValidation)
				}
			}
		}
		return tx.Create(&in).Error
	}
	if err := r.db.WithContext(ctx).Transaction(create); err != nil {
		return nil, fmt.Errorf("create governance feedback event: %w", err)
	}
	return &in, nil
}

func (r *IntegrationRepository) ListGovernanceFeedbackEvents(ctx context.Context, filter integrations.GovernanceFeedbackFilter) ([]integrations.GovernanceFeedbackEvent, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	q := r.db.WithContext(ctx).Model(&integrations.GovernanceFeedbackEvent{})
	if filter.Status != "" {
		q = q.Where("status = ?", filter.Status)
	}
	if filter.ChangeRequestID != "" {
		q = q.Where("change_request_id = ?", strings.TrimSpace(filter.ChangeRequestID))
	}
	if filter.ArtifactID != "" {
		q = q.Where("artifact_id = ?", strings.TrimSpace(filter.ArtifactID))
	}
	if filter.EventType != "" {
		q = q.Where("event_type = ?", strings.TrimSpace(filter.EventType))
	}
	if workspaceID := integrations.WorkspaceID(ctx); workspaceID != "" {
		q = q.Where("workspace_id = ?", workspaceID)
	}
	var rows []integrations.GovernanceFeedbackEvent
	if err := q.Order("created_at DESC, id DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list governance feedback events: %w", err)
	}
	return rows, nil
}

func (r *IntegrationRepository) UpdateGovernanceFeedbackEventStatus(
	ctx context.Context,
	id string,
	status string,
	reason string,
) (*integrations.GovernanceFeedbackEvent, error) {
	now := time.Now().UTC()
	db := r.db.WithContext(ctx).
		Model(&integrations.GovernanceFeedbackEvent{}).
		Where("id = ?", id)
	if workspaceID := integrations.WorkspaceID(ctx); workspaceID != "" {
		db = db.Where("workspace_id = ?", workspaceID)
	}
	res := db.
		Updates(map[string]any{"status": status, "reason": reason, "updated_at": now})
	if res.Error != nil {
		return nil, fmt.Errorf("update governance feedback event status %s: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	var row integrations.GovernanceFeedbackEvent
	q := r.db.WithContext(ctx).Where("id = ?", id)
	if workspaceID := integrations.WorkspaceID(ctx); workspaceID != "" {
		q = q.Where("workspace_id = ?", workspaceID)
	}
	if err := q.First(&row).Error; err != nil {
		return nil, fmt.Errorf("get governance feedback event %s: %w", id, err)
	}
	return &row, nil
}
