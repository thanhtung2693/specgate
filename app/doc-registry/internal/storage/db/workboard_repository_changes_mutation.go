package db

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/specgate/doc-registry/internal/workboard"
)

func (r *WorkBoardRepository) UpdateChangeRequest(
	ctx context.Context,
	in workboard.ChangeRequest,
) (*workboard.ChangeRequest, error) {
	in.UpdatedAt = time.Now().UTC()
	// PATCH body comes from the UI as a sparse partial; only persist columns
	// the caller actually populated so missing fields are not silently blanked.
	// To explicitly clear a field, set it to its zero value AND include the
	// column name here — we currently have no use case that needs that.
	updates := map[string]any{"updated_at": in.UpdatedAt}
	if in.Title != "" {
		updates["title"] = in.Title
	}
	if in.IntentMD != "" {
		updates["intent_md"] = in.IntentMD
	}
	if in.WorkType != "" {
		updates["work_type"] = in.WorkType
	}
	if in.Archived {
		updates["archived"] = true
		updates["archived_at"] = in.UpdatedAt
		if in.ArchivedBy != "" {
			updates["archived_by"] = in.ArchivedBy
		}
		if in.ArchiveReason != "" {
			updates["archive_reason"] = in.ArchiveReason
		}
	}
	if in.AcceptanceCriteria != "" {
		updates["acceptance_criteria_json"] = in.AcceptanceCriteria
	}
	if err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing workboard.ChangeRequest
		if err := scopeWorkBoardQuery(tx, ctx).First(&existing, "id = ?", in.ID).Error; err != nil {
			return mapWorkBoardNotFound(err)
		}
		res := scopeWorkBoardQuery(tx.Model(&workboard.ChangeRequest{}), ctx).Where("id = ?", in.ID).Updates(updates)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return workboard.ErrNotFound
		}
		if in.AcceptanceCriteria != "" {
			if err := replaceAcceptanceCriteria(tx, in.ID, in.AcceptanceCriteria, workboard.AcceptanceCriterionSourceHuman, in.UpdatedAt); err != nil {
				return err
			}
			if existing.LeadArtifactID == "" {
				return nil
			}
			return insertWorkBoardEvent(tx, "change_request.acceptance_criteria_changed", existing.LeadArtifactID, map[string]any{
				"change_request_id": in.ID,
				"feature_id":        existing.FeatureID,
			}, in.UpdatedAt)
		}
		if !existing.Archived && in.Archived {
			return insertWorkBoardLifecycleEvent(tx, existing.WorkspaceID, "change_request", existing.ID, "change_request.archived", in.ArchivedBy, map[string]any{
				"change_request_id":  existing.ID,
				"change_request_key": existing.Key,
				"feature_id":         existing.FeatureID,
				"archive_reason":     in.ArchiveReason,
				"changed_at":         in.UpdatedAt,
			}, in.UpdatedAt)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return r.GetChangeRequest(ctx, in.ID)
}

// UnarchiveChangeRequest restores an archived ChangeRequest. A sparse PATCH
// cannot clear the flag (a false bool is indistinguishable from omitted), so
// unarchive is its own explicit, audited operation that mirrors the archive
// lifecycle event.
func (r *WorkBoardRepository) UnarchiveChangeRequest(
	ctx context.Context,
	id string,
	actor string,
) (*workboard.ChangeRequest, error) {
	now := time.Now().UTC()
	if err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing workboard.ChangeRequest
		if err := scopeWorkBoardQuery(tx, ctx).First(&existing, "id = ?", id).Error; err != nil {
			return mapWorkBoardNotFound(err)
		}
		if err := scopeWorkBoardQuery(tx.Model(&workboard.ChangeRequest{}), ctx).Where("id = ?", id).Updates(map[string]any{
			"archived":       false,
			"archived_at":    nil,
			"archived_by":    "",
			"archive_reason": "",
			"updated_at":     now,
		}).Error; err != nil {
			return err
		}
		if !existing.Archived {
			return nil
		}
		return insertWorkBoardLifecycleEvent(tx, existing.WorkspaceID, "change_request", existing.ID, "change_request.unarchived", actor, map[string]any{
			"change_request_id":  existing.ID,
			"change_request_key": existing.Key,
			"feature_id":         existing.FeatureID,
			"changed_at":         now,
		}, now)
	}); err != nil {
		return nil, err
	}
	return r.GetChangeRequest(ctx, id)
}

// DeleteChangeRequestChildRows removes the child rows that reference the given
// change-request ids without FK cascade: gate runs, lifecycle events, feedback
// events, and tracker/delivery link rows. acceptance_criteria cascades from the
// change_requests delete itself. Shared by the archived purge and demo removal
// so the child-table list cannot diverge.
func DeleteChangeRequestChildRows(tx *gorm.DB, ids []string, workspaceID string) error {
	if len(ids) == 0 {
		return nil
	}
	// Integration children derive their workspace from integrations. Keep the
	// ownership predicate here too: work-item ids are not FK constrained.
	for _, stmt := range []string{
		"DELETE FROM tracker_links WHERE change_request_id IN ? AND EXISTS (SELECT 1 FROM integrations WHERE integrations.id = tracker_links.integration_id AND integrations.workspace_id = ?)",
		"DELETE FROM integration_delivery_links WHERE change_request_id IN ? AND EXISTS (SELECT 1 FROM integrations WHERE integrations.id = integration_delivery_links.integration_id AND integrations.workspace_id = ?)",
	} {
		if err := tx.Exec(stmt, ids, workspaceID).Error; err != nil {
			return err
		}
	}
	for _, stmt := range []string{
		"DELETE FROM gate_runs WHERE workspace_id = ? AND subject_id IN ?",
		"DELETE FROM workboard_lifecycle_events WHERE workspace_id = ? AND entity_id IN ?",
		"DELETE FROM governance_feedback_events WHERE workspace_id = ? AND change_request_id IN ?",
	} {
		if err := tx.Exec(stmt, workspaceID, ids).Error; err != nil {
			return err
		}
	}
	return nil
}

// PurgeArchivedChangeRequests hard-deletes every archived ChangeRequest along
// with its gate runs, lifecycle events, feedback events, and tracker/delivery
// link rows. Archived is the user-facing soft-delete end state; this purge is
// the explicit workspace-cleanup action that empties it. Active (non-archived)
// rows are never touched: the archived set is locked inside the transaction so
// a concurrent unarchive cannot land between selection and deletion.
func (r *WorkBoardRepository) PurgeArchivedChangeRequests(ctx context.Context) (int, error) {
	var purged int64
	workspaceID := workboard.WorkspaceID(ctx)
	if workspaceID == "" {
		return 0, workboard.ErrWorkspaceRequired
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var ids []string
		q := tx.Model(&workboard.ChangeRequest{}).
			Select("id").
			Where("archived = TRUE AND workspace_id = ?", workspaceID)
		if err := q.Clauses(clause.Locking{Strength: "UPDATE"}).Scan(&ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		if err := DeleteChangeRequestChildRows(tx, ids, workspaceID); err != nil {
			return err
		}
		q = tx.Where("id IN ? AND archived = TRUE AND workspace_id = ?", ids, workspaceID)
		res := q.Delete(&workboard.ChangeRequest{}) // cascades acceptance_criteria
		if res.Error != nil {
			return res.Error
		}
		purged = res.RowsAffected
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("purge archived change requests: %w", err)
	}
	return int(purged), nil
}
