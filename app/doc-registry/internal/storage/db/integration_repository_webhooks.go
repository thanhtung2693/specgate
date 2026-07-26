package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/specgate/doc-registry/internal/integrations"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *IntegrationRepository) findWebhookEventByExternalID(ctx context.Context, integrationID string, externalEventID string) (*integrations.WebhookEvent, error) {
	var event integrations.WebhookEvent
	q := r.db.WithContext(ctx).Where("integration_id = ? AND external_event_id = ?", integrationID, externalEventID)
	q = scopeIntegrationRows(q, ctx, "integration_webhook_events.integration_id")
	if err := q.
		First(&event).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, integrations.ErrNotFound
		}
		return nil, fmt.Errorf("find webhook event by external id %s: %w", externalEventID, err)
	}
	return &event, nil
}

// RecordWebhookEvent performs an INSERT and returns whether the row is fresh.
// When ExternalEventID is set the partial unique index
// `uq_integration_webhook_events_external` enforces dedup at the DB level.
// On conflict we re-read the existing row and return created=false so the
// caller can short-circuit any side effects — this is the only TOCTOU-safe
// shape; the prior SELECT-then-INSERT pattern raced under concurrent
// deliveries from GitLab's redrive system.
func (r *IntegrationRepository) RecordWebhookEvent(ctx context.Context, in integrations.WebhookEvent) (bool, *integrations.WebhookEvent, error) {
	if integrations.WorkspaceID(ctx) != "" {
		if _, err := r.GetIntegration(ctx, in.IntegrationID); err != nil {
			return false, nil, err
		}
		if in.ResourceID != "" {
			if _, err := r.GetResource(ctx, in.IntegrationID, in.ResourceID); err != nil {
				return false, nil, err
			}
		}
	}
	now := time.Now().UTC()
	if in.ID == "" {
		in.ID = uuid.NewString()
	}
	// Single source of truth for the body hash: every recorded signal — webhook
	// pipeline or public inbox — flows through here, so compute it once.
	if in.PayloadHash == "" {
		sum := sha256.Sum256([]byte(in.PayloadJSON))
		in.PayloadHash = hex.EncodeToString(sum[:])
	}
	if in.ReceivedAt.IsZero() {
		in.ReceivedAt = now
	}
	if in.CreatedAt.IsZero() {
		in.CreatedAt = now
	}
	in.UpdatedAt = now
	// ON CONFLICT DO NOTHING rather than letting the unique-index violation
	// raise: on Postgres a raised 23505 poisons the surrounding transaction
	// (this method runs inside WithTx in the webhook pipelines), so the
	// dedup re-read below would then fail with "current transaction is
	// aborted". DoNothing returns no error and RowsAffected=0 on a duplicate,
	// keeping the transaction usable. Empty external_event_id has no
	// unique index, so it always inserts (RowsAffected=1, created=true).
	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&in)
	if res.Error != nil {
		return false, nil, fmt.Errorf("record webhook event: %w", res.Error)
	}
	if res.RowsAffected == 1 {
		copy := in
		return true, &copy, nil
	}
	// RowsAffected == 0 → external_event_id already present; re-read the
	// canonical row so the caller has both id and status for the dedup branch.
	existing, findErr := r.findWebhookEventByExternalID(ctx, in.IntegrationID, in.ExternalEventID)
	if findErr != nil {
		return false, nil, findErr
	}
	return false, existing, nil
}

// ClaimFailedWebhookEvent atomically returns a failed delivery to pending so a
// queue retry may process it. The status predicate is the concurrency guard:
// only one redelivery can claim a failed row, while processed or already
// pending duplicates remain no-ops.
func (r *IntegrationRepository) ClaimFailedWebhookEvent(ctx context.Context, id string) (bool, *integrations.WebhookEvent, error) {
	now := time.Now().UTC()
	db := r.db.WithContext(ctx).Model(&integrations.WebhookEvent{}).
		Where("id = ? AND status = ?", id, integrations.WebhookStatusFailed)
	if workspaceID := integrations.WorkspaceID(ctx); workspaceID != "" {
		db = db.Where("EXISTS (SELECT 1 FROM integrations AS scope_integration WHERE scope_integration.id = integration_webhook_events.integration_id AND scope_integration.workspace_id = ?)", workspaceID)
	}
	res := db.Updates(map[string]any{
		"status":       integrations.WebhookStatusPending,
		"error":        "",
		"processed_at": nil,
		"updated_at":   now,
	})
	if res.Error != nil {
		return false, nil, fmt.Errorf("claim failed webhook event %s: %w", id, res.Error)
	}

	var event integrations.WebhookEvent
	q := r.db.WithContext(ctx).Where("id = ?", id)
	q = scopeIntegrationRows(q, ctx, "integration_webhook_events.integration_id")
	if err := q.First(&event).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil, integrations.ErrNotFound
		}
		return false, nil, fmt.Errorf("get claimed webhook event %s: %w", id, err)
	}
	return res.RowsAffected == 1, &event, nil
}

func (r *IntegrationRepository) UpdateWebhookEventStatus(ctx context.Context, id string, status string, errorMessage string) (*integrations.WebhookEvent, error) {
	now := time.Now().UTC()
	processedAt := &now
	db := r.db.WithContext(ctx).Model(&integrations.WebhookEvent{}).Where("id = ?", id)
	if workspaceID := integrations.WorkspaceID(ctx); workspaceID != "" {
		db = db.Where("EXISTS (SELECT 1 FROM integrations AS scope_integration WHERE scope_integration.id = integration_webhook_events.integration_id AND scope_integration.workspace_id = ?)", workspaceID)
	}
	res := db.Updates(map[string]any{
		"status":       status,
		"error":        errorMessage,
		"processed_at": processedAt,
		"updated_at":   now,
	})
	if res.Error != nil {
		return nil, fmt.Errorf("update webhook event status %s: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, integrations.ErrNotFound
	}
	var event integrations.WebhookEvent
	q := r.db.WithContext(ctx).Where("id = ?", id)
	q = scopeIntegrationRows(q, ctx, "integration_webhook_events.integration_id")
	if err := q.First(&event).Error; err != nil {
		return nil, fmt.Errorf("get webhook event %s: %w", id, err)
	}
	return &event, nil
}

func (r *IntegrationRepository) ListWebhookEvents(ctx context.Context, filter integrations.WebhookEventFilter) ([]integrations.WebhookEvent, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	q := r.db.WithContext(ctx).Model(&integrations.WebhookEvent{})
	if filter.IntegrationID != "" {
		q = q.Where("integration_id = ?", filter.IntegrationID)
	}
	q = scopeIntegrationRows(q, ctx, "integration_webhook_events.integration_id")
	if filter.ResourceID != "" {
		q = q.Where("resource_id = ?", filter.ResourceID)
	}
	if filter.Status != "" {
		q = q.Where("status = ?", filter.Status)
	}
	var rows []integrations.WebhookEvent
	if err := q.Order("received_at DESC, created_at DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list webhook events: %w", err)
	}
	return rows, nil
}
