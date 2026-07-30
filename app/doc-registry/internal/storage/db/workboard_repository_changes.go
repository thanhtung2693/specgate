package db

import (
	"context"
	"encoding/json"
	"errors"
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
	// Mirrors ChangeRequest.DerivePhase, including its work-type proxy for
	// quick-route readiness and the reason that proxy is still here.
	if cr.LeadArtifactID == "" {
		if cr.WorkType == workboard.WorkTypeBugFix {
			return workboard.BoardPhaseReady, nil
		}
		return workboard.BoardPhaseIntake, nil
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
