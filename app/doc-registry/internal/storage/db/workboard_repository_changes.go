package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/governanceprofile"
	"github.com/specgate/doc-registry/internal/integrations"
	"github.com/specgate/doc-registry/internal/workboard"
)

func (r *WorkBoardRepository) CreateChangeRequest(
	ctx context.Context,
	in workboard.ChangeRequest,
) (*workboard.ChangeRequest, error) {
	workspaceID, err := resolveWorkBoardWorkspace(ctx, in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	in.WorkspaceID = workspaceID
	now := time.Now().UTC()
	normalizeChangeRequest(&in, now)
	if err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := validateChangeRequestWorkspaceLinks(tx, in); err != nil {
			return err
		}
		if err := tx.Create(&in).Error; err != nil {
			return err
		}
		return replaceAcceptanceCriteria(tx, in.ID, in.AcceptanceCriteria, workboard.AcceptanceCriterionSourceHuman, now)
	}); err != nil {
		return nil, err
	}
	return &in, nil
}

// validateChangeRequestWorkspaceLinks runs inside the create transaction. A
// link is accepted only when its parent already belongs to the request's
// workspace; a mismatch is deliberately indistinguishable from not found.
func validateChangeRequestWorkspaceLinks(tx *gorm.DB, in workboard.ChangeRequest) error {
	workspaceID := strings.TrimSpace(in.WorkspaceID)
	if workspaceID == "" {
		return nil
	}
	if featureID := strings.TrimSpace(in.FeatureID); featureID != "" {
		var feature workboard.Feature
		if err := tx.Where("id = ? AND workspace_id = ?", featureID, workspaceID).First(&feature).Error; err != nil {
			return mapWorkBoardNotFound(err)
		}
	}
	if artifactID := strings.TrimSpace(in.LeadArtifactID); artifactID != "" {
		var row artifact.Artifact
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where(
			"id = ? AND workspace_id = ? AND feature_id = ? AND status IN ?",
			artifactID,
			workspaceID,
			strings.TrimSpace(in.FeatureID),
			[]artifact.Status{artifact.StatusApproved, artifact.StatusSuperseded},
		).First(&row).Error; err != nil {
			return mapWorkBoardNotFound(err)
		}
	}
	return nil
}

func (r *WorkBoardRepository) ListChangeRequests(ctx context.Context, includeArchived bool) ([]workboard.ChangeRequest, error) {
	return r.listChangeRequests(ctx, workboard.WorkspaceID(ctx), includeArchived)
}

// ListChangeRequestsInWorkspace returns only requests owned by workspaceID.
// HTTP callers use this boundary instead of loading every workspace then
// filtering response data in memory.
func (r *WorkBoardRepository) ListChangeRequestsInWorkspace(
	ctx context.Context,
	workspaceID string,
	includeArchived bool,
) ([]workboard.ChangeRequest, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return nil, workboard.ErrNotFound
	}
	return r.listChangeRequests(ctx, workspaceID, includeArchived)
}

func (r *WorkBoardRepository) listChangeRequests(
	ctx context.Context,
	workspaceID string,
	includeArchived bool,
) ([]workboard.ChangeRequest, error) {
	var out []workboard.ChangeRequest
	q := r.db.WithContext(ctx).Order("created_at DESC")
	if workspaceID != "" {
		q = q.Where("workspace_id = ?", workspaceID)
	}
	if !includeArchived {
		q = q.Where("archived = ?", false)
	}
	if err := q.Find(&out).Error; err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(out))
	for i := range out {
		ids = append(ids, out[i].ID)
	}
	deliveryReviews, err := r.latestDeliveryReviewSnapshots(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range out {
		if err := r.deriveChangeRequestReadFields(ctx, &out[i], deliveryReviews[out[i].ID]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (r *WorkBoardRepository) GetChangeRequest(
	ctx context.Context,
	id string,
) (*workboard.ChangeRequest, error) {
	var out workboard.ChangeRequest
	if err := scopeWorkBoardQuery(r.db.WithContext(ctx), ctx).First(&out, "id = ?", id).Error; err != nil {
		return nil, mapWorkBoardNotFound(err)
	}
	deliveryReviews, err := r.latestDeliveryReviewSnapshots(ctx, []string{out.ID})
	if err != nil {
		return nil, err
	}
	if err := r.deriveChangeRequestReadFields(ctx, &out, deliveryReviews[out.ID]); err != nil {
		return nil, err
	}
	return &out, nil
}

// SetChangeRequestAttribution is used by idempotent demo seeding to attach
// demo work to the selected local identity without broadening the public sparse
// PATCH path.
func (r *WorkBoardRepository) SetChangeRequestAttribution(
	ctx context.Context,
	id string,
	workspaceID string,
	createdBy string,
) (*workboard.ChangeRequest, error) {
	updates := map[string]any{"updated_at": time.Now().UTC()}
	if workspaceID != "" {
		updates["workspace_id"] = workspaceID
	}
	if createdBy != "" {
		updates["created_by"] = createdBy
	}
	res := scopeWorkBoardQuery(r.db.WithContext(ctx).Model(&workboard.ChangeRequest{}), ctx).Where("id = ?", id).Updates(updates)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, workboard.ErrNotFound
	}
	return r.GetChangeRequest(ctx, id)
}

// deriveChangeRequestReadFields fills the read-only derived fields on a change
// request: the board phase (from artifact pointers, overridden to Delivered
// when the current completion has an authoritative human approval) and the
// latest inbound tracker status. Neither is persisted; both are computed per read. The
// deliveryReview is batch-loaded by the caller (latestDeliveryReviewSnapshots)
// to avoid an N+1 gate-run query on the list path.
func (r *WorkBoardRepository) deriveChangeRequestReadFields(
	ctx context.Context,
	cr *workboard.ChangeRequest,
	deliveryReview *workboard.DeliveryReviewSnapshot,
) error {
	phase, err := r.derivedChangeRequestPhase(ctx, *cr)
	if err != nil {
		return err
	}
	cr.DeliveryReview = deliveryReview
	if deliveryReview != nil &&
		deliveryReview.Verdict == string(workboard.NextActionStatePass) &&
		deliveryReview.Executor == workboard.GateRunExecutorHuman {
		phase = workboard.BoardPhaseDelivered
	}
	cr.Phase = phase
	tracker, err := r.latestTrackerStatus(ctx, *cr)
	if err != nil {
		return err
	}
	cr.TrackerStatus = tracker
	return nil
}

func (r *WorkBoardRepository) derivedChangeRequestPhase(
	ctx context.Context,
	cr workboard.ChangeRequest,
) (workboard.BoardPhase, error) {
	if cr.IsQuickRoute() {
		return workboard.BoardPhaseReady, nil
	}
	if cr.LeadArtifactID != "" {
		var lead artifact.Artifact
		leadQuery := r.db.WithContext(ctx).Where("id = ?", cr.LeadArtifactID)
		if cr.WorkspaceID != "" {
			leadQuery = leadQuery.Where("workspace_id = ?", cr.WorkspaceID)
		}
		if err := leadQuery.First(&lead).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return workboard.BoardPhaseReady, nil
			}
			return "", err
		}
		if lead.Status == artifact.StatusApproved {
			return workboard.BoardPhaseReady, nil
		}
		return workboard.BoardPhaseReview, nil
	}
	return workboard.BoardPhaseIntake, nil
}

// latestDeliveryReviewSnapshots returns each change request's authoritative
// delivery_review gate run for its latest completion cycle. One query for the
// whole id set keeps the list path free of N+1 lookups.
func (r *WorkBoardRepository) latestDeliveryReviewSnapshots(
	ctx context.Context,
	ids []string,
) (map[string]*workboard.DeliveryReviewSnapshot, error) {
	snapshots := make(map[string]*workboard.DeliveryReviewSnapshot, len(ids))
	if len(ids) == 0 {
		return snapshots, nil
	}
	latestCompletions := r.db.WithContext(ctx).
		Model(&integrations.GovernanceFeedbackEvent{}).
		Select(
			"id, workspace_id, change_request_id, ROW_NUMBER() OVER (PARTITION BY workspace_id, change_request_id ORDER BY created_at DESC, id DESC) AS completion_rank",
		).
		Where(
			"event_type = ? AND change_request_id IN ?",
			integrations.FeedbackEventCodingAgentCompleted,
			ids,
		)
	if workspaceID := workboard.WorkspaceID(ctx); workspaceID != "" {
		latestCompletions = latestCompletions.Where("workspace_id = ?", workspaceID)
	}
	rankedRuns := r.db.WithContext(ctx).
		Table("gate_runs AS gr").
		Select(`gr.*,
			ROW_NUMBER() OVER (
				PARTITION BY gr.workspace_id, gr.subject_id,
					CASE WHEN gr.executor = ? THEN 'human' ELSE 'platform' END
				ORDER BY gr.created_at DESC, gr.id DESC
			) AS review_rank`, workboard.GateRunExecutorHuman).
		Joins(
			`LEFT JOIN (?) AS lc
			 ON lc.workspace_id = gr.workspace_id
			AND lc.change_request_id = gr.subject_id
			AND lc.completion_rank = 1`,
			latestCompletions,
		).
		Where(
			"gr.subject_kind = ? AND gr.gate = ? AND gr.subject_id IN ?",
			workboard.GateRunSubjectChangeRequest,
			governanceprofile.DeliveryReviewGateKey,
			ids,
		).
		Where(`lc.id IS NULL
			OR gr.completion_feedback_event_id = lc.id`)
	if workspaceID := workboard.WorkspaceID(ctx); workspaceID != "" {
		rankedRuns = rankedRuns.Where("gr.workspace_id = ?", workspaceID)
	}
	var rows []workboard.GateRun
	if err := r.db.WithContext(ctx).
		Table("(?) AS ranked_reviews", rankedRuns).
		Where("review_rank = 1").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	runsBySubject := make(map[string][]workboard.GateRun, len(ids))
	for _, row := range rows {
		runsBySubject[row.SubjectID] = append(runsBySubject[row.SubjectID], row)
	}
	for subjectID, subjectRuns := range runsBySubject {
		latest := authoritativeDeliveryReviewFromRuns(subjectRuns)
		if latest == nil || strings.TrimSpace(subjectID) == "" {
			continue
		}
		actor, note, summary := deliveryRunAuditFields(*latest)
		snapshots[subjectID] = &workboard.DeliveryReviewSnapshot{
			Verdict:    string(latest.State),
			Hint:       latest.Hint,
			ReviewedAt: latest.CreatedAt,
			Executor:   latest.Executor,
			Actor:      actor,
			Note:       note,
			Summary:    summary,
		}
	}
	return snapshots, nil
}

// latestTrackerFeedback returns the provider and raw tracker state of the most
// recent delivery.tracker_status_changed feedback event correlated to this
// change request. Returns ("", "", nil) when none is found. Tracker feedback
// rows carry no change_request_id; the link is the payload `correlation_id`
// (SPECGATE-{key|id}) matched against the CR id or key — structural identifiers,
// not pattern matching over user content.
func (r *WorkBoardRepository) latestTrackerFeedback(
	ctx context.Context,
	cr workboard.ChangeRequest,
) (provider, trackerState string, err error) {
	var rows []integrations.GovernanceFeedbackEvent
	query := r.db.WithContext(ctx).Where("event_type = ?", integrations.FeedbackEventTrackerStatusChanged)
	if cr.WorkspaceID != "" {
		query = query.Where("workspace_id = ?", cr.WorkspaceID)
	}
	if err := query.
		Order("created_at DESC").
		Find(&rows).Error; err != nil {
		return "", "", err
	}
	for _, row := range rows {
		var payload struct {
			CorrelationID string `json:"correlation_id"`
			TrackerState  string `json:"tracker_state"`
			Provider      string `json:"provider"`
		}
		if err := json.Unmarshal([]byte(row.PayloadJSON), &payload); err != nil {
			continue
		}
		corr := strings.TrimSpace(payload.CorrelationID)
		if corr == "" {
			continue
		}
		if strings.EqualFold(corr, cr.ID) || strings.EqualFold(corr, cr.Key) {
			return strings.TrimSpace(payload.Provider), strings.TrimSpace(payload.TrackerState), nil
		}
	}
	return "", "", nil
}

// latestTrackerStatus returns the raw tracker state.type of the most recent
// delivery.tracker_status_changed feedback event correlated to this change
// request, or "" if none. Kept for existing callers that do not need the
// provider. Delegates to latestTrackerFeedback.
func (r *WorkBoardRepository) latestTrackerStatus(
	ctx context.Context,
	cr workboard.ChangeRequest,
) (string, error) {
	_, state, err := r.latestTrackerFeedback(ctx, cr)
	return state, err
}

// hasMergedDelivery reports whether the change request has a merged-PR delivery
// link — the git evidence that delivery actually landed. Keyed by
// change_request_id (set on the link in commitDelivery), unlike the
// feature-scoped delivery_in_progress check.
func (r *WorkBoardRepository) hasMergedDelivery(
	ctx context.Context,
	changeRequestID string,
) (bool, error) {
	if strings.TrimSpace(changeRequestID) == "" {
		return false, nil
	}
	var count int64
	links := r.db.WithContext(ctx).
		Model(&integrations.DeliveryLink{}).
		Where("change_request_id = ? AND external_type = ? AND state = ?",
			changeRequestID, integrations.ExternalTypeMergeRequest, integrations.DeliveryStateMerged)
	if workspaceID := workboard.WorkspaceID(ctx); workspaceID != "" {
		links = links.Joins("JOIN integrations ON integrations.id = integration_delivery_links.integration_id").Where("integrations.workspace_id = ?", workspaceID)
	}
	if err := links.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

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

func (r *WorkBoardRepository) ListStaleWarnings(
	ctx context.Context,
	filter workboard.StaleWarningFilter,
) ([]workboard.StaleWarning, error) {
	if workspaceID := workboard.WorkspaceID(ctx); workspaceID != "" {
		filter.WorkspaceID = workspaceID
	} else if workspaceID := strings.TrimSpace(filter.WorkspaceID); workspaceID != "" {
		filter.WorkspaceID = workspaceID
		ctx = workboard.WithWorkspace(ctx, workspaceID)
	}
	if filter.FeatureID == "" && filter.ChangeRequestID == "" {
		features, err := r.ListFeatures(ctx)
		if err != nil {
			return nil, err
		}
		featureByID := make(map[string]workboard.Feature, len(features))
		var warnings []workboard.StaleWarning
		for _, item := range features {
			featureByID[item.ID] = item
			itemWarnings, err := r.listStaleWarningsForFeatureAndCR(ctx, filter.WorkspaceID, item, workboard.ChangeRequest{}, true)
			if err != nil {
				return nil, err
			}
			warnings = append(warnings, itemWarnings...)
		}
		changeRequests, err := r.ListChangeRequests(ctx, false)
		if err != nil {
			return nil, err
		}
		for _, cr := range changeRequests {
			// Quick-route change requests may have no feature; evaluate their
			// CR-scoped warnings without a feature lookup.
			if cr.FeatureID == "" {
				itemWarnings, err := r.listStaleWarningsForFeatureAndCR(ctx, filter.WorkspaceID, workboard.Feature{}, cr, false)
				if err != nil {
					return nil, err
				}
				warnings = append(warnings, itemWarnings...)
				continue
			}
			feature, ok := featureByID[cr.FeatureID]
			if !ok {
				if err := scopeWorkBoardQuery(r.db.WithContext(ctx), ctx).First(&feature, "id = ?", cr.FeatureID).Error; err != nil {
					return nil, mapWorkBoardNotFound(err)
				}
				featureByID[cr.FeatureID] = feature
			}
			itemWarnings, err := r.listStaleWarningsForFeatureAndCR(ctx, filter.WorkspaceID, feature, cr, false)
			if err != nil {
				return nil, err
			}
			warnings = append(warnings, itemWarnings...)
		}
		return warnings, nil
	}
	var feature workboard.Feature
	var cr workboard.ChangeRequest
	if filter.ChangeRequestID != "" {
		if err := scopeWorkBoardQuery(r.db.WithContext(ctx), ctx).First(&cr, "id = ?", filter.ChangeRequestID).Error; err != nil {
			return nil, mapWorkBoardNotFound(err)
		}
		filter.FeatureID = cr.FeatureID
	}
	if filter.FeatureID == "" {
		return nil, nil
	}
	if err := scopeWorkBoardQuery(r.db.WithContext(ctx), ctx).First(&feature, "id = ?", filter.FeatureID).Error; err != nil {
		return nil, mapWorkBoardNotFound(err)
	}
	return r.listStaleWarningsForFeatureAndCR(ctx, filter.WorkspaceID, feature, cr, true)
}

func (r *WorkBoardRepository) listStaleWarningsForFeatureAndCR(
	ctx context.Context,
	workspaceID string,
	feature workboard.Feature,
	cr workboard.ChangeRequest,
	includeFeatureWarnings bool,
) ([]workboard.StaleWarning, error) {
	// A loaded change request pins the workspace; otherwise use the caller's
	// (selected) workspace. Knowledge freshness is skipped when neither is known.
	if cr.WorkspaceID != "" {
		workspaceID = cr.WorkspaceID
	} else if workspaceID == "" {
		workspaceID = feature.WorkspaceID
	}
	var warnings []workboard.StaleWarning
	if includeFeatureWarnings {
		if feature.Status == workboard.FeatureStatusDeprecated {
			warnings = append(warnings, staleWarning(workboard.WarningFeatureDeprecated, feature.ID, cr.ID, "", "Feature is deprecated."))
		}
		if feature.CanonicalArtifactID == "" {
			warnings = append(warnings, staleWarning(workboard.WarningCanonicalArtifactMissing, feature.ID, cr.ID, "", "Feature has no canonical artifact."))
		} else {
			var a artifact.Artifact
			artifactQuery := r.db.WithContext(ctx).Where("id = ?", feature.CanonicalArtifactID)
			if workspaceID != "" {
				artifactQuery = artifactQuery.Where("workspace_id = ?", workspaceID)
			}
			if err := artifactQuery.First(&a).Error; err == nil {
				if a.Status != artifact.StatusApproved {
					warnings = append(warnings, staleWarning(workboard.WarningCanonicalArtifactUnapproved, feature.ID, cr.ID, a.ID, "Canonical artifact is not approved."))
				}
				if a.Status == artifact.StatusSuperseded {
					warnings = append(warnings, staleWarning(workboard.WarningCanonicalArtifactSuperseded, feature.ID, cr.ID, a.ID, "Canonical artifact is superseded."))
				}
				linkedKnowledgeWarning, err := r.linkedKnowledgeNewerWarning(ctx, workspaceID, feature, a, cr.ID)
				if err != nil {
					return nil, err
				}
				if linkedKnowledgeWarning != nil {
					warnings = append(warnings, *linkedKnowledgeWarning)
				}
			}
		}
	}
	if cr.ID != "" && cr.LeadArtifactID != "" {
		var lead artifact.Artifact
		artifactQuery := r.db.WithContext(ctx).Where("id = ?", cr.LeadArtifactID)
		if workspaceID != "" {
			artifactQuery = artifactQuery.Where("workspace_id = ?", workspaceID)
		}
		if err := artifactQuery.First(&lead).Error; err == nil {
			if lead.Status == artifact.StatusSuperseded {
				warnings = append(warnings, staleWarning(workboard.WarningLeadArtifactSuperseded, feature.ID, cr.ID, lead.ID, "Lead artifact is superseded."))
			}
			if lead.Status == artifact.StatusApproved && feature.CanonicalArtifactID != lead.ID {
				warnings = append(warnings, staleWarning(workboard.WarningCanonicalPromotionAvailable, feature.ID, cr.ID, lead.ID, "Approved lead artifact can be promoted to canonical."))
			}
		}
	}
	// Tracker status augments the derived phase but must not override the
	// git delivery evidence; a clear contradiction surfaces as a warning.
	// Only meaningful for a loaded CR (the feature-level board loop passes no
	// cr.ID, so this never fires there).
	if cr.ID != "" {
		conflict, err := r.trackerStatusConflictWarning(ctx, feature, cr)
		if err != nil {
			return nil, err
		}
		if conflict != nil {
			warnings = append(warnings, *conflict)
		}
		priorityUrgent, err := r.trackerPriorityUrgentWarning(ctx, feature, cr)
		if err != nil {
			return nil, err
		}
		if priorityUrgent != nil {
			warnings = append(warnings, *priorityUrgent)
		}
		deliveryStale, err := r.deliveryStaleWarning(ctx, feature, cr)
		if err != nil {
			return nil, err
		}
		if deliveryStale != nil {
			warnings = append(warnings, *deliveryStale)
		}
	}
	// Check for an open MR/PR — a delivery link in state "opened" means
	// delivery is underway. Match by feature_id (ID or Key) per spec §14. // per spec §14
	featureRefs := []string{feature.ID}
	if strings.TrimSpace(feature.Key) != "" && feature.Key != feature.ID {
		featureRefs = append(featureRefs, feature.Key)
	}
	var openLinkCount int64
	deliveryLinks := r.db.WithContext(ctx).
		Model(&integrations.DeliveryLink{}).
		Where("feature_id IN ? AND external_type = ? AND state = ?", featureRefs, integrations.ExternalTypeMergeRequest, integrations.DeliveryStateOpened)
	if workspaceID != "" {
		deliveryLinks = deliveryLinks.Joins("JOIN integrations ON integrations.id = integration_delivery_links.integration_id").Where("integrations.workspace_id = ?", workspaceID)
	}
	if err := deliveryLinks.Count(&openLinkCount).Error; err != nil {
		return nil, err
	}
	if openLinkCount > 0 {
		warnings = append(warnings, workboard.StaleWarning{
			Code:      workboard.WarningDeliveryInProgress,
			Severity:  "info",
			Message:   "Active delivery: an open MR/PR is linked to this feature.",
			FeatureID: feature.ID,
		})
	}
	return warnings, nil
}

func (r *WorkBoardRepository) NextActions(
	ctx context.Context,
	changeRequestID string,
) ([]workboard.NextAction, error) {
	var cr workboard.ChangeRequest
	if err := scopeWorkBoardQuery(r.db.WithContext(ctx), ctx).First(&cr, "id = ?", changeRequestID).Error; err != nil {
		return nil, mapWorkBoardNotFound(err)
	}
	// Quick-route change requests may have no feature; a zero-value feature
	// behaves like one without a canonical artifact.
	var feature workboard.Feature
	if cr.FeatureID != "" {
		featureQuery := r.db.WithContext(ctx).Where("id = ?", cr.FeatureID)
		if cr.WorkspaceID != "" {
			featureQuery = featureQuery.Where("workspace_id = ?", cr.WorkspaceID)
		}
		if err := featureQuery.First(&feature).Error; err != nil {
			return nil, mapWorkBoardNotFound(err)
		}
	}
	var lead *artifact.Artifact
	if cr.LeadArtifactID != "" {
		var a artifact.Artifact
		artifactQuery := r.db.WithContext(ctx).Where("id = ?", cr.LeadArtifactID)
		if cr.WorkspaceID != "" {
			artifactQuery = artifactQuery.Where("workspace_id = ?", cr.WorkspaceID)
		}
		if err := artifactQuery.First(&a).Error; err == nil {
			lead = &a
		}
	}
	warnings, err := r.ListStaleWarnings(ctx, workboard.StaleWarningFilter{ChangeRequestID: changeRequestID})
	if err != nil {
		return nil, err
	}
	knowledgeWarn := false
	for _, warning := range warnings {
		if warning.Code == workboard.WarningLinkedKnowledgeNewer {
			knowledgeWarn = true
			break
		}
	}
	isApproved := lead != nil && lead.Status == artifact.StatusApproved
	isCanonical := cr.LeadArtifactID != "" && feature.CanonicalArtifactID == cr.LeadArtifactID
	canonicalDrifted := feature.CanonicalArtifactID != "" && cr.LeadArtifactID != "" && feature.CanonicalArtifactID != cr.LeadArtifactID
	actions := []workboard.NextAction{
		{
			Gate:  "spec_drafted",
			State: stateIf(cr.LeadArtifactID != "", workboard.NextActionStatePass, workboard.NextActionStatePending),
			Hint:  valueOrDefault(cr.LeadArtifactID, "No working spec attached"),
		},
		{
			Gate:  "spec_approved",
			State: stateIf(isApproved, workboard.NextActionStatePass, workboard.NextActionStatePending),
			Hint:  hintForArtifactStatus(lead, "No working spec attached"),
		},
		{
			Gate:  "no_conflicts",
			State: stateIf(lead != nil, workboard.NextActionStatePass, workboard.NextActionStatePending),
			Hint:  hintForConflictGate(lead),
		},
		{
			Gate:  "knowledge_fresh",
			State: stateIf(!knowledgeWarn && lead != nil, workboard.NextActionStatePass, stateIf(knowledgeWarn, workboard.NextActionStateWarn, workboard.NextActionStatePending)),
			Hint:  hintForKnowledgeGate(knowledgeWarn),
		},
		{
			Gate:  "canonical_spec",
			State: stateIf(isCanonical, workboard.NextActionStatePass, stateIf(canonicalDrifted, workboard.NextActionStateWarn, workboard.NextActionStatePending)),
			Hint:  hintForCanonicalGate(isCanonical, canonicalDrifted, feature.CanonicalArtifactID),
		},
	}
	// Quick-route CRs never grow a working spec, so the full-artifact-flow
	// gates are persisted as not_applicable for audit instead of pending
	// forever. Context Packs are derived on read, so they are not a gate.
	if cr.IsQuickRoute() {
		for i := range actions {
			actions[i].State = workboard.NextActionStateNotApplicable
			actions[i].Hint = "Not required for quick-route work"
		}
	}
	return actions, nil
}
