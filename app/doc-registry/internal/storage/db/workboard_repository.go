package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/governanceprofile"
	"github.com/specgate/doc-registry/internal/governancethreads"
	"github.com/specgate/doc-registry/internal/integrations"
	"github.com/specgate/doc-registry/internal/knowledge"
	"github.com/specgate/doc-registry/internal/workboard"
)

type WorkBoardRepository struct {
	db                            *gorm.DB
	deliverySLADays               int
	autoArchiveOnDeliveryPassFunc func() bool
}

func NewWorkBoardRepository(db *gorm.DB) *WorkBoardRepository {
	return &WorkBoardRepository{db: db, deliverySLADays: 7}
}

// SetDeliverySLADays overrides the default 7-day SLA threshold for the
// delivery_stale warning. Values <= 0 mean any failing delivery review is
// immediately stale.
func (r *WorkBoardRepository) SetDeliverySLADays(n int) {
	r.deliverySLADays = n
}

// SetAutoArchiveOnDeliveryPass configures whether an explicit human delivery
// approval should archive its ChangeRequest. The function form lets callers
// read live settings without recreating the repository.
func (r *WorkBoardRepository) SetAutoArchiveOnDeliveryPass(enabled func() bool) {
	r.autoArchiveOnDeliveryPassFunc = enabled
}

func (r *WorkBoardRepository) autoArchiveOnDeliveryPass() bool {
	return r.autoArchiveOnDeliveryPassFunc != nil && r.autoArchiveOnDeliveryPassFunc()
}

// scopeWorkBoardQuery narrows root workboard rows when the caller has selected
// a workspace. Background maintenance may intentionally omit that context.
func scopeWorkBoardQuery(q *gorm.DB, ctx context.Context) *gorm.DB {
	if workspaceID := workboard.WorkspaceID(ctx); workspaceID != "" {
		return q.Where("workspace_id = ?", workspaceID)
	}
	return q
}

func resolveWorkBoardWorkspace(ctx context.Context, supplied string) (string, error) {
	supplied = strings.TrimSpace(supplied)
	if selected := workboard.WorkspaceID(ctx); selected != "" {
		if supplied != "" && supplied != selected {
			return "", workboard.ErrNotFound
		}
		return selected, nil
	}
	return supplied, nil
}

// UpsertFeatureByKey returns the existing Feature with the given key, or
// creates a new one if none exists. The call is idempotent: calling it twice
// with the same key always returns the same row. When a new Feature is
// created it receives the same initial status as CreateFeature (candidate),
// and name falls back to key when empty.
func (r *WorkBoardRepository) UpsertFeatureByKey(
	ctx context.Context,
	key string,
	name string,
) (*workboard.Feature, error) {
	feature, _, err := r.UpsertFeatureByKeyForPublish(ctx, key, name)
	return feature, err
}

func (r *WorkBoardRepository) UpsertFeatureByKeyForPublish(
	ctx context.Context,
	key string,
	name string,
) (*workboard.Feature, bool, error) {
	return r.upsertFeatureByKeyForPublish(ctx, workboard.WorkspaceID(ctx), key, name)
}

// UpsertFeatureByKeyInWorkspaceForPublish is the workspace-bound publish path.
// A key may be reused in another workspace, but never resolves across one.
func (r *WorkBoardRepository) UpsertFeatureByKeyInWorkspaceForPublish(
	ctx context.Context,
	workspaceID, key, name string,
) (*workboard.Feature, bool, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return nil, false, workboard.ErrNotFound
	}
	return r.upsertFeatureByKeyForPublish(ctx, workspaceID, key, name)
}

func (r *WorkBoardRepository) upsertFeatureByKeyForPublish(
	ctx context.Context,
	workspaceID, key, name string,
) (*workboard.Feature, bool, error) {
	var feature workboard.Feature
	created := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := "key = ?"
		args := []any{key}
		if workspaceID != "" {
			query = "workspace_id = ? AND key = ?"
			args = []any{workspaceID, key}
		}
		if err := tx.First(&feature, append([]any{query}, args...)...).Error; err == nil {
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		now := time.Now().UTC()
		feature = workboard.Feature{
			WorkspaceID: workspaceID,
			Key:         key,
			Name:        valueOrDefault(name, key),
		}
		normalizeFeature(&feature, now)
		if err := tx.Create(&feature).Error; err != nil {
			if isUniqueViolation(err) {
				return tx.First(&feature, append([]any{query}, args...)...).Error
			}
			return err
		}
		created = true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return &feature, created, nil
}

func (r *WorkBoardRepository) CreateFeature(
	ctx context.Context,
	in workboard.Feature,
) (*workboard.Feature, error) {
	workspaceID, err := resolveWorkBoardWorkspace(ctx, in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	in.WorkspaceID = workspaceID
	now := time.Now().UTC()
	normalizeFeature(&in, now)
	if err := r.db.WithContext(ctx).Create(&in).Error; err != nil {
		return nil, err
	}
	return &in, nil
}

func (r *WorkBoardRepository) ListFeatures(ctx context.Context) ([]workboard.Feature, error) {
	var out []workboard.Feature
	err := scopeWorkBoardQuery(r.db.WithContext(ctx), ctx).Order("created_at DESC").Find(&out).Error
	return out, err
}

func (r *WorkBoardRepository) ListFeaturesInWorkspace(ctx context.Context, workspaceID string) ([]workboard.Feature, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return nil, workboard.ErrNotFound
	}
	var out []workboard.Feature
	err := r.db.WithContext(ctx).Where("workspace_id = ?", workspaceID).Order("created_at DESC").Find(&out).Error
	return out, err
}

func (r *WorkBoardRepository) GetFeature(ctx context.Context, id string) (*workboard.Feature, error) {
	var out workboard.Feature
	err := scopeWorkBoardQuery(r.db.WithContext(ctx), ctx).First(&out, "id = ?", id).Error
	return &out, mapWorkBoardNotFound(err)
}

func (r *WorkBoardRepository) GetFeatureInWorkspace(ctx context.Context, workspaceID, id string) (*workboard.Feature, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return nil, workboard.ErrNotFound
	}
	var out workboard.Feature
	err := r.db.WithContext(ctx).First(&out, "workspace_id = ? AND id = ?", workspaceID, id).Error
	return &out, mapWorkBoardNotFound(err)
}

// GetFeatureByKey resolves a feature by its stable key (case-insensitive),
// for CLI refs like `specgate work create --feature my-feature`.
func (r *WorkBoardRepository) GetFeatureByKey(ctx context.Context, key string) (*workboard.Feature, error) {
	var out workboard.Feature
	err := scopeWorkBoardQuery(r.db.WithContext(ctx), ctx).First(&out, "LOWER(key) = LOWER(?)", key).Error
	return &out, mapWorkBoardNotFound(err)
}

func (r *WorkBoardRepository) UpdateFeature(ctx context.Context, in workboard.Feature) (*workboard.Feature, error) {
	in.UpdatedAt = time.Now().UTC()
	// Sparse PATCH: only persist columns the caller populated. The UI sends
	// `{status: "active"}` to mark a feature as Live; we must not blank
	// `name` / `summary` along with it.
	updates := map[string]any{"updated_at": in.UpdatedAt}
	if in.Name != "" {
		updates["name"] = in.Name
	}
	if in.Summary != "" {
		updates["summary"] = in.Summary
	}
	if in.Status != "" {
		updates["status"] = in.Status
	}
	if err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing workboard.Feature
		if err := scopeWorkBoardQuery(tx, ctx).First(&existing, "id = ?", in.ID).Error; err != nil {
			return mapWorkBoardNotFound(err)
		}
		res := scopeWorkBoardQuery(tx.Model(&workboard.Feature{}), ctx).Where("id = ?", in.ID).Updates(updates)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return workboard.ErrNotFound
		}
		if in.Status != "" && existing.Status != in.Status {
			return insertWorkBoardLifecycleEvent(tx, existing.WorkspaceID, "feature", existing.ID, "feature.status_changed", "", map[string]any{
				"feature_id":  existing.ID,
				"feature_key": existing.Key,
				"previous":    existing.Status,
				"next":        in.Status,
				"changed_at":  in.UpdatedAt,
			}, in.UpdatedAt)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return r.GetFeature(ctx, in.ID)
}

// ListReferencedArtifactIDs returns the artifact IDs still referenced by
// workboard rows: feature canonical artifacts plus change-request lead
// artifacts. The retention sweeper uses it to protect referenced
// artifacts from deletion (per spec §9).
func (r *WorkBoardRepository) ListReferencedArtifactIDs(ctx context.Context) (map[string]bool, error) {
	var ids []string
	workspaceID := workboard.WorkspaceID(ctx)
	query := `
		SELECT canonical_artifact_id AS id FROM features WHERE canonical_artifact_id IS NOT NULL AND canonical_artifact_id <> ''
		UNION
		SELECT lead_artifact_id AS id FROM change_requests WHERE lead_artifact_id IS NOT NULL AND lead_artifact_id <> ''
	`
	args := []any(nil)
	if workspaceID != "" {
		query = `
			SELECT canonical_artifact_id AS id FROM features WHERE workspace_id = ? AND canonical_artifact_id IS NOT NULL AND canonical_artifact_id <> ''
			UNION
			SELECT lead_artifact_id AS id FROM change_requests WHERE workspace_id = ? AND lead_artifact_id IS NOT NULL AND lead_artifact_id <> ''
		`
		args = []any{workspaceID, workspaceID}
	}
	err := r.db.WithContext(ctx).Raw(query, args...).Scan(&ids).Error
	if err != nil {
		return nil, fmt.Errorf("list referenced artifact ids: %w", err)
	}
	referenced := make(map[string]bool, len(ids))
	for _, id := range ids {
		referenced[id] = true
	}
	return referenced, nil
}

func (r *WorkBoardRepository) DeleteCandidateFeatureIfUnreferenced(ctx context.Context, id, key string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var feature workboard.Feature
		if err := scopeWorkBoardQuery(tx.Clauses(clause.Locking{Strength: "UPDATE"}), ctx).
			First(&feature, "id = ? AND key = ?", id, key).Error; err != nil {
			return mapWorkBoardNotFound(err)
		}
		if feature.Status != workboard.FeatureStatusCandidate || feature.CanonicalArtifactID != "" {
			return nil
		}
		var refs int64
		if err := tx.Model(&artifact.Artifact{}).Where("feature_id = ?", id).Count(&refs).Error; err != nil {
			return err
		}
		if refs > 0 {
			return nil
		}
		if err := tx.Model(&workboard.ChangeRequest{}).Where("feature_id = ?", id).Count(&refs).Error; err != nil {
			return err
		}
		if refs > 0 {
			return nil
		}
		return scopeWorkBoardQuery(tx, ctx).Delete(&workboard.Feature{}, "id = ? AND key = ?", id, key).Error
	})
}

// promoteStatusOnCanonical returns the new FeatureStatus a feature should
// transition to when an approved canonical artifact is set, or "" to leave
// status unchanged. Curator-controlled terminal states (rejected,
// deprecated, archived) and the already-promoted active state are preserved.
func promoteStatusOnCanonical(current workboard.FeatureStatus) workboard.FeatureStatus {
	switch current {
	case workboard.FeatureStatusCandidate, workboard.FeatureStatusPlanned:
		return workboard.FeatureStatusActive
	}
	return ""
}

func (r *WorkBoardRepository) SetFeatureCanonicalArtifact(
	ctx context.Context,
	featureID string,
	artifactID string,
	approvedBy string,
) (*workboard.Feature, error) {
	var feature workboard.Feature
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Resolve by id or key: promotion accepts both stable feature keys and UUIDs.
		// Keys and UUIDs never collide; feature.ID is used thereafter.
		if err := scopeWorkBoardQuery(tx.Clauses(clause.Locking{Strength: "UPDATE"}), ctx).First(&feature, "id = ? OR key = ?", featureID, featureID).Error; err != nil {
			return mapWorkBoardNotFound(err)
		}
		if err := validateCanonicalArtifact(tx, artifactID, feature.WorkspaceID); err != nil {
			return err
		}
		version := feature.Version
		if feature.CanonicalArtifactID != "" && feature.CanonicalArtifactID != artifactID {
			version++
		}
		now := time.Now().UTC()
		updates := map[string]any{
			"canonical_artifact_id": artifactID,
			"version":               version,
			"updated_at":            now,
		}
		// Promote candidate / planned features to active when they get their
		// first approved canonical artifact (per spec §lifecycle: a Feature
		// becomes a real product capability once it has an approved spec).
		if newStatus := promoteStatusOnCanonical(feature.Status); newStatus != "" {
			updates["status"] = newStatus
		}
		if err := scopeWorkBoardQuery(tx.Model(&workboard.Feature{}), ctx).Where("id = ?", feature.ID).Updates(updates).Error; err != nil {
			return err
		}
		if feature.CanonicalArtifactID != artifactID {
			readinessState, readinessOverridden := latestReadinessAggregate(tx, artifactID)
			if err := insertWorkBoardEvent(tx, "feature.canonical_changed", artifactID, map[string]any{
				"feature_id":            feature.ID,
				"previous_artifact_id":  feature.CanonicalArtifactID,
				"canonical_artifact_id": artifactID,
				"approved_by":           approvedBy,
				"readiness_state":       readinessState,
				"readiness_overridden":  readinessOverridden,
			}, now); err != nil {
				return err
			}
		}
		return tx.First(&feature, "id = ?", feature.ID).Error
	})
	if err != nil {
		return nil, err
	}
	return &feature, nil
}

// latestReadinessAggregate derives the readiness state for an artifact from its
// persisted readiness runs (latest run per gate). Precedence:
// fail > needs_human_review > warn > pass. not_applicable counts as evaluated-
// and-fine (pass-equivalent); no runs => "not_run". overridden is true whenever
// the state is not "pass" — so a promote without a clean readiness pass leaves a
// durable trace on the canonical event. Kept identical to the UI's
// aggregateReadiness so the audit matches what the reviewer saw.
func latestReadinessAggregate(tx *gorm.DB, artifactID string) (state string, overridden bool) {
	var runs []workboard.GateRun
	if err := tx.Where(
		"subject_kind = ? AND subject_id = ? AND executor IN ?",
		workboard.GateRunSubjectArtifact, artifactID,
		[]string{workboard.GateRunExecutorPlatform, workboard.GateRunExecutorIDEAgent},
	).Order("created_at DESC").Find(&runs).Error; err != nil || len(runs) == 0 {
		return "not_run", true
	}
	seen := map[string]struct{}{}
	hasFail, hasNHR, hasWarn, hasClean := false, false, false, false
	for _, r := range runs { // DESC, so the first row seen per gate is the latest
		if _, ok := seen[r.Gate]; ok {
			continue
		}
		seen[r.Gate] = struct{}{}
		switch artifact.ReadinessState(r.State) {
		case artifact.ReadinessStateFail:
			hasFail = true
		case artifact.ReadinessStateNeedsHumanReview:
			hasNHR = true
		case artifact.ReadinessStateWarn:
			hasWarn = true
		case artifact.ReadinessStatePass, artifact.ReadinessStateNotApplicable:
			hasClean = true
		}
	}
	switch {
	case hasFail:
		return "fail", true
	case hasNHR:
		return "needs_human_review", true
	case hasWarn:
		return "warn", true
	case hasClean:
		return "pass", false
	default:
		return "not_run", true
	}
}

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
	if err := validateGovernanceThreadWorkspace(tx, workspaceID, in.GovernanceThreadID); err != nil {
		return err
	}
	return nil
}

// validateGovernanceThreadWorkspace permits a not-yet-indexed LangGraph thread,
// but never allows a known thread from a different workspace to be linked.
func validateGovernanceThreadWorkspace(tx *gorm.DB, workspaceID, threadID string) error {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil
	}
	var thread governancethreads.Thread
	err := tx.Where("thread_id = ?", threadID).First(&thread).Error
	if err == nil {
		if thread.WorkspaceID != workspaceID {
			return workboard.ErrNotFound
		}
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	return err
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
	if strings.TrimSpace(cr.GovernanceThreadID) != "" {
		return workboard.BoardPhaseDraft, nil
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
	if in.GovernanceThreadID != "" {
		updates["governance_thread_id"] = in.GovernanceThreadID
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
		if err := validateGovernanceThreadWorkspace(tx, existing.WorkspaceID, in.GovernanceThreadID); err != nil {
			return err
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

func (r *WorkBoardRepository) RefreshGateRuns(
	ctx context.Context,
	in workboard.RefreshGateRunsInput,
) ([]workboard.GateRun, error) {
	changeRequestID := strings.TrimSpace(in.ChangeRequestID)
	if changeRequestID == "" {
		return nil, workboard.ErrValidation
	}
	var cr workboard.ChangeRequest
	if err := scopeWorkBoardQuery(r.db.WithContext(ctx), ctx).First(&cr, "id = ?", changeRequestID).Error; err != nil {
		return nil, mapWorkBoardNotFound(err)
	}
	var actions []workboard.NextAction
	if !in.EvaluationsOnly {
		nextActions, err := r.NextActions(ctx, changeRequestID)
		if err != nil {
			if errors.Is(err, workboard.ErrNotFound) {
				rows, fallbackErr := r.persistPolicyUnavailableEvaluations(ctx, cr, in.Evaluations)
				if fallbackErr != nil {
					return nil, fallbackErr
				}
				if len(rows) > 0 {
					return rows, nil
				}
			}
			return nil, err
		}
		actions = nextActions
	}
	// Quick-route change requests may have no feature (see NextActions).
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
	warnings, err := r.ListStaleWarnings(ctx, workboard.StaleWarningFilter{ChangeRequestID: changeRequestID})
	if err != nil {
		return nil, err
	}
	warningRows := make([]map[string]any, 0, len(warnings))
	for _, warning := range warnings {
		warningRows = append(warningRows, map[string]any{
			"code":        warning.Code,
			"message":     warning.Message,
			"artifact_id": warning.ArtifactID,
		})
	}
	linkedKnowledgeRows, err := r.listLinkedKnowledgeEvidence(ctx, cr.WorkspaceID, feature)
	if err != nil {
		return nil, err
	}
	baseArtifactID := strings.TrimSpace(cr.LeadArtifactID)
	if baseArtifactID == "" {
		baseArtifactID = strings.TrimSpace(feature.CanonicalArtifactID)
	}
	now := time.Now().UTC()
	rows := make([]workboard.GateRun, 0, len(actions))
	evalsByGate := map[string]workboard.GateEvaluation{}
	for _, eval := range in.Evaluations {
		gate := strings.TrimSpace(eval.Gate)
		if gate == "" {
			continue
		}
		if eval.Confidence < 0 || eval.Confidence > 1 {
			return nil, workboard.ErrValidation
		}
		evalsByGate[gate] = eval
	}
	for _, action := range actions {
		eval, hasEval := evalsByGate[action.Gate]
		state := action.State
		hint := action.Hint
		confidence := gateConfidenceFromState(action.State)
		judgeModel := "deterministic-v1"
		evalSuiteVersion := "none"
		evidence := ""
		if hasEval {
			if eval.State != "" {
				state = eval.State
			}
			if strings.TrimSpace(eval.Hint) != "" {
				hint = strings.TrimSpace(eval.Hint)
			}
			if eval.Confidence >= 0 {
				confidence = eval.Confidence
			}
			if strings.TrimSpace(eval.JudgeModel) != "" {
				judgeModel = strings.TrimSpace(eval.JudgeModel)
			}
			if strings.TrimSpace(eval.EvalSuiteVersion) != "" {
				evalSuiteVersion = strings.TrimSpace(eval.EvalSuiteVersion)
			}
			evidence = strings.TrimSpace(eval.Evidence)
		}
		evidenceJSON := `{}`
		verdict := gateVerdictFromState(state)
		completionFeedbackEventID := deliveryEvaluationCompletionID(evidence)
		evidencePayload, _ := json.Marshal(map[string]any{
			"evidence_contract_version": "gate-run-v1",
			"gate":                      action.Gate,
			"evaluator": map[string]any{
				"type":               evaluatorType(hasEval),
				"judge_model":        judgeModel,
				"config_version":     "workboard-next-actions-v1",
				"eval_suite_version": evalSuiteVersion,
			},
			"verdict":                      verdict,
			"confidence":                   confidence,
			"evidence":                     evidence,
			"completion_feedback_event_id": completionFeedbackEventID,
			"change_request_id":            changeRequestID,
			"feature_id":                   feature.ID,
			"source_artifact_id":           baseArtifactID,
			"lead_artifact_id":             cr.LeadArtifactID,
			"canonical_artifact_id":        feature.CanonicalArtifactID,
			"linked_knowledge":             linkedKnowledgeRows,
			"warnings":                     warningRows,
		})
		if len(evidencePayload) > 0 {
			evidenceJSON = string(evidencePayload)
		}
		run := workboard.GateRun{
			ID:                        uuid.NewString(),
			WorkspaceID:               cr.WorkspaceID,
			SubjectKind:               workboard.GateRunSubjectChangeRequest,
			SubjectID:                 changeRequestID,
			Gate:                      action.Gate,
			State:                     state,
			Hint:                      hint,
			Executor:                  workboard.GateRunExecutorPlatform,
			EvidenceJSON:              evidenceJSON,
			CompletionFeedbackEventID: completionFeedbackEventID,
			CreatedAt:                 now,
		}
		rows = append(rows, run)
	}
	// Eval-only gates — the model-judged ones — have no deterministic next-action,
	// so the loop above never emits a row for them. Persist them straight from
	// the evaluations so the review UI can show their verdicts.
	actionGates := make(map[string]bool, len(actions))
	for _, action := range actions {
		actionGates[action.Gate] = true
	}
	for _, eval := range in.Evaluations {
		gate := strings.TrimSpace(eval.Gate)
		if gate == "" || actionGates[gate] {
			continue
		}
		completionFeedbackEventID := deliveryEvaluationCompletionID(eval.Evidence)
		evidencePayload, _ := json.Marshal(map[string]any{
			"evidence_contract_version": "gate-run-v1",
			"gate":                      gate,
			"evaluator": map[string]any{
				"type":               "agent_judge",
				"judge_model":        strings.TrimSpace(eval.JudgeModel),
				"config_version":     "workboard-next-actions-v1",
				"eval_suite_version": strings.TrimSpace(eval.EvalSuiteVersion),
			},
			"verdict":                      string(eval.State),
			"confidence":                   eval.Confidence,
			"evidence":                     strings.TrimSpace(eval.Evidence),
			"completion_feedback_event_id": completionFeedbackEventID,
			"change_request_id":            changeRequestID,
			"feature_id":                   feature.ID,
			"source_artifact_id":           baseArtifactID,
		})
		rows = append(rows, workboard.GateRun{
			ID:                        uuid.NewString(),
			WorkspaceID:               cr.WorkspaceID,
			SubjectKind:               workboard.GateRunSubjectChangeRequest,
			SubjectID:                 changeRequestID,
			Gate:                      gate,
			State:                     eval.State,
			Hint:                      strings.TrimSpace(eval.Hint),
			Executor:                  workboard.GateRunExecutorPlatform,
			EvidenceJSON:              string(evidencePayload),
			CompletionFeedbackEventID: completionFeedbackEventID,
			CreatedAt:                 now,
		})
	}
	if len(rows) == 0 {
		return rows, nil
	}
	if err := r.createGateRunsWithChangeRequestLock(ctx, changeRequestID, rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// persistPolicyUnavailableEvaluations keeps the deterministic fail-closed
// delivery guard durable even when a dangling policy dependency prevents the
// normal deterministic gate refresh from being assembled.
func (r *WorkBoardRepository) persistPolicyUnavailableEvaluations(
	ctx context.Context,
	cr workboard.ChangeRequest,
	evaluations []workboard.GateEvaluation,
) ([]workboard.GateRun, error) {
	now := time.Now().UTC()
	rows := make([]workboard.GateRun, 0, 1)
	for _, eval := range evaluations {
		if eval.Confidence < 0 || eval.Confidence > 1 {
			return nil, workboard.ErrValidation
		}
		if strings.TrimSpace(eval.Gate) != governanceprofile.DeliveryReviewGateKey ||
			eval.State != workboard.NextActionStateNeedsHumanReview {
			continue
		}
		var detail struct {
			ReasonCode string `json:"reason_code"`
		}
		if json.Unmarshal([]byte(strings.TrimSpace(eval.Evidence)), &detail) != nil ||
			detail.ReasonCode != "policy_unavailable" {
			continue
		}
		completionFeedbackEventID := deliveryEvaluationCompletionID(eval.Evidence)
		evidencePayload, _ := json.Marshal(map[string]any{
			"evidence_contract_version": "gate-run-v1",
			"gate":                      governanceprofile.DeliveryReviewGateKey,
			"evaluator": map[string]any{
				"type":               "deterministic_policy_guard",
				"judge_model":        strings.TrimSpace(eval.JudgeModel),
				"config_version":     "workboard-next-actions-v1",
				"eval_suite_version": strings.TrimSpace(eval.EvalSuiteVersion),
			},
			"verdict":                      string(eval.State),
			"confidence":                   eval.Confidence,
			"evidence":                     strings.TrimSpace(eval.Evidence),
			"completion_feedback_event_id": completionFeedbackEventID,
			"change_request_id":            cr.ID,
			"feature_id":                   cr.FeatureID,
			"source_artifact_id":           cr.LeadArtifactID,
		})
		rows = append(rows, workboard.GateRun{
			ID:                        uuid.NewString(),
			WorkspaceID:               cr.WorkspaceID,
			SubjectKind:               workboard.GateRunSubjectChangeRequest,
			SubjectID:                 cr.ID,
			Gate:                      governanceprofile.DeliveryReviewGateKey,
			State:                     eval.State,
			Hint:                      strings.TrimSpace(eval.Hint),
			Executor:                  workboard.GateRunExecutorPlatform,
			EvidenceJSON:              string(evidencePayload),
			CompletionFeedbackEventID: completionFeedbackEventID,
			CreatedAt:                 now,
		})
	}
	if len(rows) == 0 {
		return nil, nil
	}
	if err := r.createGateRunsWithChangeRequestLock(ctx, cr.ID, rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *WorkBoardRepository) createGateRunsWithChangeRequestLock(
	ctx context.Context,
	changeRequestID string,
	rows []workboard.GateRun,
) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var cr workboard.ChangeRequest
		if err := scopeWorkBoardQuery(
			tx.Clauses(clause.Locking{Strength: "UPDATE"}),
			ctx,
		).Select("id").First(&cr, "id = ?", changeRequestID).Error; err != nil {
			return mapWorkBoardNotFound(err)
		}
		return tx.Create(&rows).Error
	})
}

func deliveryEvaluationCompletionID(evidence string) string {
	var detail struct {
		CompletionFeedbackEventID string `json:"completion_feedback_event_id"`
	}
	_ = json.Unmarshal([]byte(strings.TrimSpace(evidence)), &detail)
	return strings.TrimSpace(detail.CompletionFeedbackEventID)
}

// AuthoritativeDeliveryReviewRun returns the human-authoritative delivery run
// for the latest completion cycle without first truncating mixed gate history.
func (r *WorkBoardRepository) AuthoritativeDeliveryReviewRun(
	ctx context.Context,
	changeRequestID string,
) (*workboard.GateRun, error) {
	completionID, err := r.latestCompletionFeedbackEventID(ctx, changeRequestID)
	if err != nil {
		return nil, err
	}
	if completionID == "" {
		human, err := r.latestDeliveryReviewRunByExecutor(ctx, changeRequestID, true, "")
		if err != nil {
			return nil, err
		}
		platform, err := r.latestDeliveryReviewRunByExecutor(ctx, changeRequestID, false, "")
		if err != nil {
			return nil, err
		}
		if human == nil {
			return platform, nil
		}
		if platform == nil {
			return human, nil
		}
		return authoritativeDeliveryReviewCycle(human, platform), nil
	}
	human, err := r.latestDeliveryReviewRunByExecutor(ctx, changeRequestID, true, completionID)
	if err != nil {
		return nil, err
	}
	if human != nil {
		return human, nil
	}
	platform, err := r.latestDeliveryReviewRunByExecutor(ctx, changeRequestID, false, completionID)
	if err != nil {
		return nil, err
	}
	if platform != nil {
		return platform, nil
	}
	return nil, nil
}

// CurrentGateRuns returns one current row per non-delivery gate plus the
// completion-bound authoritative delivery review. Append-only delivery history
// cannot crowd an unresolved quality gate out of a Context Pack.
func (r *WorkBoardRepository) CurrentGateRuns(
	ctx context.Context,
	changeRequestID string,
) ([]workboard.GateRun, error) {
	ranked := r.db.WithContext(ctx).
		Table("gate_runs AS gr").
		Select(`gr.*,
			ROW_NUMBER() OVER (
				PARTITION BY gr.workspace_id, gr.subject_id, gr.gate
				ORDER BY gr.created_at DESC, gr.id DESC
			) AS gate_rank`).
		Where(
			"gr.subject_kind = ? AND gr.subject_id = ? AND gr.gate <> ?",
			workboard.GateRunSubjectChangeRequest,
			changeRequestID,
			governanceprofile.DeliveryReviewGateKey,
		)
	if workspaceID := workboard.WorkspaceID(ctx); workspaceID != "" {
		ranked = ranked.Where("gr.workspace_id = ?", workspaceID)
	}
	var rows []workboard.GateRun
	if err := r.db.WithContext(ctx).
		Table("(?) AS ranked_gates", ranked).
		Where("gate_rank = 1").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	delivery, err := r.AuthoritativeDeliveryReviewRun(ctx, changeRequestID)
	if err != nil {
		return nil, err
	}
	if delivery != nil {
		rows = append(rows, *delivery)
	}
	return rows, nil
}

func (r *WorkBoardRepository) latestCompletionFeedbackEventID(
	ctx context.Context,
	changeRequestID string,
) (string, error) {
	var row integrations.GovernanceFeedbackEvent
	q := r.db.WithContext(ctx).Where(
		"change_request_id = ? AND event_type = ?",
		changeRequestID,
		integrations.FeedbackEventCodingAgentCompleted,
	)
	if workspaceID := workboard.WorkspaceID(ctx); workspaceID != "" {
		q = q.Where("workspace_id = ?", workspaceID)
	}
	err := q.Order("created_at DESC, id DESC").First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return row.ID, nil
}

func authoritativeDeliveryReviewFromRuns(runs []workboard.GateRun) *workboard.GateRun {
	var human, platform *workboard.GateRun
	for i := range runs {
		run := &runs[i]
		if run.Executor == workboard.GateRunExecutorHuman {
			if human == nil || deliveryGateRunNewer(*run, *human) {
				human = run
			}
		} else if platform == nil || deliveryGateRunNewer(*run, *platform) {
			platform = run
		}
	}
	if human == nil {
		return platform
	}
	if platform == nil {
		return human
	}
	return authoritativeDeliveryReviewCycle(human, platform)
}

func authoritativeDeliveryReviewCycle(
	human *workboard.GateRun,
	platform *workboard.GateRun,
) *workboard.GateRun {
	type completionBinding struct {
		CompletionFeedbackEventID string `json:"completion_feedback_event_id"`
	}
	var humanBinding, platformBinding completionBinding
	_ = json.Unmarshal([]byte(human.EvidenceJSON), &humanBinding)
	_ = json.Unmarshal([]byte(platform.EvidenceJSON), &platformBinding)
	if deliveryGateRunNewer(*platform, *human) &&
		strings.TrimSpace(humanBinding.CompletionFeedbackEventID) != "" &&
		strings.TrimSpace(platformBinding.CompletionFeedbackEventID) != "" &&
		humanBinding.CompletionFeedbackEventID != platformBinding.CompletionFeedbackEventID {
		return platform
	}
	return human
}

func deliveryGateRunNewer(candidate, current workboard.GateRun) bool {
	return candidate.CreatedAt.After(current.CreatedAt) ||
		(candidate.CreatedAt.Equal(current.CreatedAt) && candidate.ID > current.ID)
}

func (r *WorkBoardRepository) latestDeliveryReviewRunByExecutor(
	ctx context.Context,
	changeRequestID string,
	human bool,
	completionID string,
) (*workboard.GateRun, error) {
	var latest workboard.GateRun
	q := r.db.WithContext(ctx).Where(
		"subject_kind = ? AND subject_id = ? AND gate = ?",
		workboard.GateRunSubjectChangeRequest, changeRequestID, governanceprofile.DeliveryReviewGateKey,
	)
	if workspaceID := workboard.WorkspaceID(ctx); workspaceID != "" {
		q = q.Where("workspace_id = ?", workspaceID)
	}
	if human {
		q = q.Where("executor = ?", workboard.GateRunExecutorHuman)
	} else {
		q = q.Where("executor <> ?", workboard.GateRunExecutorHuman)
	}
	if completionID != "" {
		q = q.Where("completion_feedback_event_id = ?", completionID)
	}
	err := q.Order("created_at DESC, id DESC").First(&latest).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &latest, nil
}

func (r *WorkBoardRepository) RecordDeliveryDecision(
	ctx context.Context,
	in workboard.DeliveryDecisionInput,
) (*workboard.GateRun, error) {
	changeRequestID := strings.TrimSpace(in.ChangeRequestID)
	reviewedGateRunID := strings.TrimSpace(in.ReviewedGateRunID)
	completionFeedbackEventID := strings.TrimSpace(in.CompletionFeedbackEventID)
	actor := strings.TrimSpace(in.Actor)
	note := strings.TrimSpace(in.Note)
	if changeRequestID == "" || actor == "" {
		return nil, workboard.ErrValidation
	}
	decision := in.Decision
	state := workboard.NextActionStateFail
	hint := "delivery rejected"
	if decision == workboard.DeliveryDecisionApprove {
		state = workboard.NextActionStatePass
		hint = "delivery accepted"
	} else if decision != workboard.DeliveryDecisionReject {
		return nil, workboard.ErrValidation
	}
	if actor != "" {
		hint += " by " + actor
	}
	if note != "" {
		hint += ": " + note
	}
	now := time.Now().UTC()
	evidence := map[string]any{
		"evidence_contract_version": "gate-run-v1",
		"gate":                      governanceprofile.DeliveryReviewGateKey,
		"verdict":                   string(state),
		"confidence":                1.0,
		"decision":                  string(decision),
		"note":                      note,
		"change_request_id":         changeRequestID,
		"evaluator": map[string]any{
			"type":  "human_decision",
			"actor": actor,
			"trust": "human_decision",
		},
	}
	run := workboard.GateRun{
		ID:                        uuid.NewString(),
		SubjectKind:               workboard.GateRunSubjectChangeRequest,
		SubjectID:                 changeRequestID,
		Gate:                      governanceprofile.DeliveryReviewGateKey,
		State:                     state,
		Hint:                      hint,
		Executor:                  workboard.GateRunExecutorHuman,
		CompletionFeedbackEventID: completionFeedbackEventID,
		CreatedAt:                 now,
	}
	if err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing workboard.ChangeRequest
		if err := scopeWorkBoardQuery(tx.Clauses(clause.Locking{Strength: "UPDATE"}), ctx).First(&existing, "id = ?", changeRequestID).Error; err != nil {
			return mapWorkBoardNotFound(err)
		}
		run.WorkspaceID = existing.WorkspaceID
		if reviewedGateRunID == "" || completionFeedbackEventID == "" {
			return workboard.ErrValidation
		}
		var latestCompletion integrations.GovernanceFeedbackEvent
		if err := tx.Where(
			"workspace_id = ? AND change_request_id = ? AND event_type = ?",
			existing.WorkspaceID,
			changeRequestID,
			integrations.FeedbackEventCodingAgentCompleted,
		).Order("created_at DESC, id DESC").First(&latestCompletion).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return workboard.ErrValidation
			}
			return err
		}
		if latestCompletion.ID != completionFeedbackEventID {
			return workboard.ErrValidation
		}
		var previous workboard.GateRun
		err := tx.
			Where(
				"id = ? AND workspace_id = ? AND subject_kind = ? AND subject_id = ? AND gate = ? AND executor <> ? AND completion_feedback_event_id = ?",
				reviewedGateRunID,
				existing.WorkspaceID,
				workboard.GateRunSubjectChangeRequest,
				changeRequestID,
				governanceprofile.DeliveryReviewGateKey,
				workboard.GateRunExecutorHuman,
				completionFeedbackEventID,
			).
			First(&previous).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return workboard.ErrValidation
		}
		if err != nil {
			return err
		}
		var latestPlatform workboard.GateRun
		if err := tx.Where(
			"workspace_id = ? AND subject_kind = ? AND subject_id = ? AND gate = ? AND executor <> ? AND completion_feedback_event_id = ?",
			existing.WorkspaceID,
			workboard.GateRunSubjectChangeRequest,
			changeRequestID,
			governanceprofile.DeliveryReviewGateKey,
			workboard.GateRunExecutorHuman,
			completionFeedbackEventID,
		).Order("created_at DESC, id DESC").First(&latestPlatform).Error; err != nil {
			return err
		}
		if latestPlatform.ID != reviewedGateRunID {
			return workboard.ErrValidation
		}
		var humanCount int64
		if err := tx.Model(&workboard.GateRun{}).Where(
			"workspace_id = ? AND subject_kind = ? AND subject_id = ? AND gate = ? AND executor = ? AND completion_feedback_event_id = ?",
			existing.WorkspaceID,
			workboard.GateRunSubjectChangeRequest,
			changeRequestID,
			governanceprofile.DeliveryReviewGateKey,
			workboard.GateRunExecutorHuman,
			completionFeedbackEventID,
		).Count(&humanCount).Error; err != nil {
			return err
		}
		if humanCount > 0 {
			return workboard.ErrValidation
		}
		var previousEvidence struct {
			Evidence                  string   `json:"evidence"`
			CompletionFeedbackEventID string   `json:"completion_feedback_event_id"`
			Confidence                *float64 `json:"confidence"`
			JudgeModel                string   `json:"judge_model"`
			EvalSuiteVersion          string   `json:"eval_suite_version"`
			Evaluator                 struct {
				JudgeModel       string `json:"judge_model"`
				EvalSuiteVersion string `json:"eval_suite_version"`
			} `json:"evaluator"`
		}
		if json.Unmarshal([]byte(previous.EvidenceJSON), &previousEvidence) == nil {
			if strings.TrimSpace(previousEvidence.Evidence) != "" {
				evidence["evidence"] = previousEvidence.Evidence
			}
			if strings.TrimSpace(previousEvidence.CompletionFeedbackEventID) != "" {
				evidence["completion_feedback_event_id"] = previousEvidence.CompletionFeedbackEventID
			}
			judgeModel := strings.TrimSpace(previousEvidence.Evaluator.JudgeModel)
			if judgeModel == "" {
				judgeModel = strings.TrimSpace(previousEvidence.JudgeModel)
			}
			if judgeModel != "" {
				evidence["evidence_judge_model"] = judgeModel
			}
			evalSuiteVersion := strings.TrimSpace(previousEvidence.Evaluator.EvalSuiteVersion)
			if evalSuiteVersion == "" {
				evalSuiteVersion = strings.TrimSpace(previousEvidence.EvalSuiteVersion)
			}
			if evalSuiteVersion != "" {
				evidence["evidence_eval_suite_version"] = evalSuiteVersion
			}
			if previousEvidence.Confidence != nil {
				evidence["evidence_confidence"] = *previousEvidence.Confidence
			}
		}
		evidence["evidence_verdict"] = string(previous.State)
		evidence["reviewed_gate_run_id"] = previous.ID
		evidencePayload, err := json.Marshal(evidence)
		if err != nil {
			return err
		}
		run.EvidenceJSON = string(evidencePayload)
		if err := tx.Create(&run).Error; err != nil {
			return err
		}
		if state == workboard.NextActionStatePass && r.autoArchiveOnDeliveryPass() {
			updates := map[string]any{
				"archived":       true,
				"archived_at":    now,
				"archived_by":    actor,
				"archive_reason": "delivery accepted by human reviewer",
				"updated_at":     now,
			}
			if err := scopeWorkBoardQuery(tx.Model(&workboard.ChangeRequest{}), ctx).Where("id = ?", changeRequestID).Updates(updates).Error; err != nil {
				return err
			}
			if !existing.Archived {
				if err := insertWorkBoardLifecycleEvent(tx, existing.WorkspaceID, "change_request", existing.ID, "change_request.archived", actor, map[string]any{
					"change_request_id":  existing.ID,
					"change_request_key": existing.Key,
					"feature_id":         existing.FeatureID,
					"archive_reason":     "delivery accepted by human reviewer",
					"delivery_decision":  string(decision),
					"changed_at":         now,
				}, now); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return &run, nil
}

func deliveryRunAuditFields(run workboard.GateRun) (actor string, note string, summary string) {
	var wrapper struct {
		Decision  string `json:"decision,omitempty"`
		Note      string `json:"note,omitempty"`
		Evaluator struct {
			Actor string `json:"actor,omitempty"`
			Trust string `json:"trust,omitempty"`
			Type  string `json:"type,omitempty"`
		} `json:"evaluator,omitempty"`
	}
	_ = json.Unmarshal([]byte(strings.TrimSpace(run.EvidenceJSON)), &wrapper)
	actor = strings.TrimSpace(wrapper.Evaluator.Actor)
	note = strings.TrimSpace(wrapper.Note)
	summary = workboard.DeliveryDecisionSummary(run, actor, note)
	return actor, note, summary
}

func evaluatorType(hasEval bool) string {
	if hasEval {
		return "agent_judge"
	}
	return "deterministic"
}

func gateVerdictFromState(state workboard.NextActionState) string {
	switch state {
	case workboard.NextActionStatePass:
		return "pass"
	case workboard.NextActionStateWarn:
		return "warn"
	case workboard.NextActionStateNotApplicable:
		return "not_applicable"
	default:
		return "pending"
	}
}

func gateConfidenceFromState(state workboard.NextActionState) float64 {
	switch state {
	case workboard.NextActionStatePass:
		return 0.95
	case workboard.NextActionStateWarn:
		return 0.75
	default:
		return 0.60
	}
}

func (r *WorkBoardRepository) listLinkedKnowledgeEvidence(
	ctx context.Context,
	workspaceID string,
	feature workboard.Feature,
) ([]map[string]any, error) {
	// Gate evidence is workspace-scoped like the freshness warning; without a
	// workspace there is nothing safely in scope.
	if strings.TrimSpace(workspaceID) == "" {
		return []map[string]any{}, nil
	}
	featureRefs := []string{feature.ID}
	if strings.TrimSpace(feature.Key) != "" && feature.Key != feature.ID {
		featureRefs = append(featureRefs, feature.Key)
	}
	var docs []knowledge.Document
	if err := r.db.WithContext(ctx).
		Where("workspace_id = ? AND is_latest = ? AND status = ? AND linked_feature_id IN ?", workspaceID, true, knowledge.StatusIndexed, featureRefs).
		Order("updated_at DESC").
		Limit(5).
		Find(&docs).Error; err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(docs))
	for _, doc := range docs {
		out = append(out, map[string]any{
			"document_id": doc.DocumentID,
			"version":     doc.Version,
			"title":       doc.Title,
			"updated_at":  doc.UpdatedAt,
		})
	}
	return out, nil
}

func (r *WorkBoardRepository) ListGateRuns(
	ctx context.Context,
	changeRequestID string,
	limit int,
) ([]workboard.GateRun, error) {
	if limit == 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	var rows []workboard.GateRun
	q := scopeWorkBoardQuery(r.db.WithContext(ctx), ctx).
		Where("subject_kind = ? AND subject_id = ?", workboard.GateRunSubjectChangeRequest, changeRequestID).
		Order("created_at DESC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	err := q.Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// ListLifecycleEvents returns the workboard_lifecycle_events rows for one entity
// (entity_kind, entity_id) ordered by created_at ascending — chronological for
// the governance audit trail.
func (r *WorkBoardRepository) ListLifecycleEvents(
	ctx context.Context,
	entityKind string,
	entityID string,
	limit int,
) ([]workboard.LifecycleEvent, error) {
	if limit == 0 {
		limit = 200
	}
	if limit > 500 {
		limit = 500
	}
	var rows []workboard.LifecycleEvent
	q := r.db.WithContext(ctx).Where("entity_kind = ? AND entity_id = ?", entityKind, entityID)
	if workspaceID := workboard.WorkspaceID(ctx); workspaceID != "" {
		q = q.Where("workspace_id = ?", workspaceID)
	}
	q = q.Order("created_at ASC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	err := q.Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *WorkBoardRepository) ListAcceptanceCriteria(
	ctx context.Context,
	changeRequestID string,
) ([]workboard.AcceptanceCriterion, error) {
	var rows []workboard.AcceptanceCriterion
	q := r.db.WithContext(ctx).Where("change_request_id = ?", changeRequestID)
	if workspaceID := workboard.WorkspaceID(ctx); workspaceID != "" {
		q = q.Where("change_request_id IN (SELECT id FROM change_requests WHERE workspace_id = ?)", workspaceID)
	}
	err := q.
		Order("sort_order ASC, created_at ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	if len(rows) > 0 {
		return rows, nil
	}
	var cr struct{ ID string }
	if err := scopeWorkBoardQuery(r.db.WithContext(ctx), ctx).
		Model(&workboard.ChangeRequest{}).
		Select("id").
		First(&cr, "id = ?", changeRequestID).Error; err != nil {
		return nil, mapWorkBoardNotFound(err)
	}
	return rows, nil
}

func (r *WorkBoardRepository) linkedKnowledgeNewerWarning(
	ctx context.Context,
	workspaceID string,
	feature workboard.Feature,
	canonical artifact.Artifact,
	changeRequestID string,
) (*workboard.StaleWarning, error) {
	// Knowledge freshness is workspace-scoped; with no known workspace there is
	// no safe scope, so do not raise (avoids cross-workspace false positives).
	if strings.TrimSpace(workspaceID) == "" {
		return nil, nil
	}
	canonicalAt := canonical.UpdatedAt
	if canonical.ApprovedAt != nil {
		canonicalAt = *canonical.ApprovedAt
	}
	featureRefs := []string{feature.ID}
	if strings.TrimSpace(feature.Key) != "" && feature.Key != feature.ID {
		featureRefs = append(featureRefs, feature.Key)
	}

	var doc knowledge.Document
	err := r.db.WithContext(ctx).
		Where("workspace_id = ? AND is_latest = ? AND status = ? AND linked_feature_id IN ?", workspaceID, true, knowledge.StatusIndexed, featureRefs).
		Order("updated_at DESC").
		First(&doc).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !doc.UpdatedAt.After(canonicalAt) {
		return nil, nil
	}
	message := fmt.Sprintf("Linked knowledge %q (%s) is newer than the feature source of truth.", doc.Title, doc.Version)
	warning := staleWarning(workboard.WarningLinkedKnowledgeNewer, feature.ID, changeRequestID, canonical.ID, message)
	return &warning, nil
}

// trackerStatusConflictWarning emits tracker_status_conflict when the inbound
// tracker status contradicts the git merge evidence for the change request.
// Conservative by design: a missing tracker event (or any non-contradicting
// combination) yields no warning.
func (r *WorkBoardRepository) trackerStatusConflictWarning(
	ctx context.Context,
	feature workboard.Feature,
	cr workboard.ChangeRequest,
) (*workboard.StaleWarning, error) {
	provider, tracker, err := r.latestTrackerFeedback(ctx, cr)
	if err != nil {
		return nil, err
	}
	if tracker == "" {
		return nil, nil
	}
	merged, err := r.hasMergedDelivery(ctx, cr.ID)
	if err != nil {
		return nil, err
	}
	trackerLabel := trackerProviderLabel(provider)
	switch {
	case tracker == trackerStateCompleted && !merged:
		msg := trackerLabel + " marked this done, but no merge was detected."
		w := staleWarning(workboard.WarningTrackerStatusConflict, feature.ID, cr.ID, "", msg)
		return &w, nil
	case merged && tracker != trackerStateCompleted && tracker != trackerStateCanceled:
		msg := "A merge was detected, but " + trackerLabel + " hasn't marked this done."
		w := staleWarning(workboard.WarningTrackerStatusConflict, feature.ID, cr.ID, "", msg)
		return &w, nil
	}
	return nil, nil
}

// trackerProviderLabel returns a human-readable label for the provider to use
// in warning messages, e.g. "GitHub" or "Linear". Falls back to "Tracker" when
// the provider string is empty or unrecognised.
func trackerProviderLabel(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "github":
		return "GitHub"
	case "gitlab":
		return "GitLab"
	case "linear":
		return "Linear"
	default:
		return "Tracker"
	}
}

// trackerPriorityUrgentWarning emits tracker_priority_urgent when the latest
// tracker feedback event for the CR carries a Linear priority of 1 (urgent) or
// 2 (high). Priority 0 (no priority set) never fires this warning; priorities 3–4
// (normal, low) never fire it. Tracker feedback rows carry no change_request_id;
// the link is the payload `correlation_id` matched against the CR id or key
// (same convention as latestTrackerStatus). per spec §14.
func (r *WorkBoardRepository) trackerPriorityUrgentWarning(
	ctx context.Context,
	feature workboard.Feature,
	cr workboard.ChangeRequest,
) (*workboard.StaleWarning, error) {
	var rows []integrations.GovernanceFeedbackEvent
	query := r.db.WithContext(ctx).Where("event_type = ?", integrations.FeedbackEventTrackerStatusChanged)
	if cr.WorkspaceID != "" {
		query = query.Where("workspace_id = ?", cr.WorkspaceID)
	}
	if err := query.
		Order("created_at DESC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		var payload struct {
			CorrelationID string `json:"correlation_id"`
			Priority      int    `json:"priority"`
		}
		if err := json.Unmarshal([]byte(row.PayloadJSON), &payload); err != nil {
			continue
		}
		corr := strings.TrimSpace(payload.CorrelationID)
		if corr == "" {
			continue
		}
		if !strings.EqualFold(corr, cr.ID) && !strings.EqualFold(corr, cr.Key) {
			continue
		}
		// Priority 1 = urgent, 2 = high. 0 means no priority was set — skip.
		if payload.Priority == 1 || payload.Priority == 2 {
			w := workboard.StaleWarning{
				Code:            workboard.WarningTrackerPriorityUrgent,
				Severity:        "warn",
				Message:         "Linked tracker issue is marked urgent/high priority but this work item has not been handed off yet.",
				FeatureID:       feature.ID,
				ChangeRequestID: cr.ID,
			}
			return &w, nil
		}
		return nil, nil
	}
	return nil, nil
}

// deliveryStaleWarning emits delivery_stale when the authoritative
// delivery_review gate run for the change request has state=fail and was created
// more than r.deliverySLADays days ago. Human decisions outrank later platform
// runs. Returns nil when no delivery review history exists, when the
// authoritative review passed, or when the failure is within the threshold.
// per spec §delivery-sla.
func (r *WorkBoardRepository) deliveryStaleWarning(
	ctx context.Context,
	feature workboard.Feature,
	cr workboard.ChangeRequest,
) (*workboard.StaleWarning, error) {
	latest, err := r.AuthoritativeDeliveryReviewRun(ctx, cr.ID)
	if err != nil {
		return nil, err
	}
	if latest == nil {
		return nil, nil // no delivery review yet — no warning
	}
	if latest.State != workboard.NextActionStateFail {
		return nil, nil // authoritative review passed or is pending — no warning
	}
	days := int(time.Since(latest.CreatedAt).Hours() / 24)
	if days < r.deliverySLADays {
		return nil, nil
	}
	msg := fmt.Sprintf("Delivery review has been failing for %d day(s). Last failed on %s.", days, latest.CreatedAt.UTC().Format("2006-01-02"))
	w := staleWarning(workboard.WarningDeliveryStale, feature.ID, cr.ID, "", msg)
	return &w, nil
}

// Linear workflow state.type values that matter to the conflict check. These
// are structural tracker enum keys, not user-supplied content.
const (
	trackerStateCompleted = "completed"
	trackerStateCanceled  = "canceled"
)

func validateCanonicalArtifact(tx *gorm.DB, artifactID string, workspaceID string) error {
	var a artifact.Artifact
	q := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ?", artifactID)
	if workspaceID != "" {
		q = q.Where("workspace_id = ?", workspaceID)
	}
	if err := q.First(&a).Error; err != nil {
		return mapWorkBoardNotFound(err)
	}
	if a.Status != artifact.StatusApproved {
		return fmt.Errorf("%w: canonical artifact must be approved", workboard.ErrValidation)
	}
	return nil
}

func staleWarning(code workboard.WarningCode, featureID, crID, artifactID, message string) workboard.StaleWarning {
	return workboard.StaleWarning{
		Code:            code,
		Severity:        "warning",
		Message:         message,
		FeatureID:       featureID,
		ChangeRequestID: crID,
		ArtifactID:      artifactID,
	}
}

func replaceAcceptanceCriteria(
	tx *gorm.DB,
	changeRequestID string,
	raw string,
	source workboard.AcceptanceCriterionSource,
	now time.Time,
) error {
	rows := parseAcceptanceCriteriaRows(changeRequestID, raw, source, now)
	if err := tx.Where("change_request_id = ?", changeRequestID).Delete(&workboard.AcceptanceCriterion{}).Error; err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	return tx.Create(&rows).Error
}

func parseAcceptanceCriteriaRows(
	changeRequestID string,
	raw string,
	defaultSource workboard.AcceptanceCriterionSource,
	now time.Time,
) []workboard.AcceptanceCriterion {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var items []any
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil
	}
	rows := make([]workboard.AcceptanceCriterion, 0, len(items))
	for i, item := range items {
		row := workboard.AcceptanceCriterion{
			ID:              uuid.NewString(),
			ChangeRequestID: changeRequestID,
			Source:          defaultSource,
			SortOrder:       i,
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		switch v := item.(type) {
		case string:
			row.Text = strings.TrimSpace(v)
		case map[string]any:
			if id, ok := v["id"].(string); ok && strings.TrimSpace(id) != "" {
				row.ID = strings.TrimSpace(id)
			}
			if text, ok := v["text"].(string); ok {
				row.Text = strings.TrimSpace(text)
			}
			if done, ok := v["done"].(bool); ok {
				row.Done = done
			}
			if binding, ok := v["verification_binding"].(string); ok {
				row.VerificationBinding = strings.TrimSpace(binding)
			}
			if src, ok := v["source"].(string); ok {
				switch workboard.AcceptanceCriterionSource(src) {
				case workboard.AcceptanceCriterionSourceHuman, workboard.AcceptanceCriterionSourceLLM:
					row.Source = workboard.AcceptanceCriterionSource(src)
				}
			}
		}
		if row.Text != "" {
			rows = append(rows, row)
		}
	}
	return rows
}

func insertWorkBoardEvent(tx *gorm.DB, eventType, artifactID string, payload map[string]any, now time.Time) error {
	if artifactID == "" {
		artifactID = valueOrDefault(fmt.Sprint(payload["feature_id"]), "workboard")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	// Route through the tamper-evidence chain (spec §8.2) — every
	// artifact_events row must link to its artifact's chain, or verification
	// flags the unchained row as tampering.
	e := artifact.Event{
		ID:         uuid.NewString(),
		ArtifactID: artifactID,
		EventType:  eventType,
		Payload:    string(body),
		CreatedAt:  now,
	}
	return createChainedEvent(tx, &e)
}

func insertWorkBoardLifecycleEvent(
	tx *gorm.DB,
	workspaceID string,
	entityKind string,
	entityID string,
	eventType string,
	actor string,
	payload map[string]any,
	now time.Time,
) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return tx.Create(&workboard.LifecycleEvent{
		ID:          uuid.NewString(),
		WorkspaceID: strings.TrimSpace(workspaceID),
		EntityKind:  entityKind,
		EntityID:    entityID,
		EventType:   eventType,
		Actor:       actor,
		PayloadJSON: string(body),
		CreatedAt:   now,
	}).Error
}

func stateIf(ok bool, yes, no workboard.NextActionState) workboard.NextActionState {
	if ok {
		return yes
	}
	return no
}

func hintForArtifactStatus(lead *artifact.Artifact, fallback string) string {
	if lead == nil {
		return fallback
	}
	return string(lead.Status)
}

func hintForConflictGate(lead *artifact.Artifact) string {
	if lead != nil {
		return "No blocking conflicts on impacted services"
	}
	return "No working spec attached"
}

func hintForKnowledgeGate(hasWarning bool) string {
	if hasWarning {
		return "Linked knowledge is newer than the canonical spec"
	}
	return "No newer linked knowledge"
}

func hintForCanonicalGate(isCanonical, drifted bool, canonicalArtifactID string) string {
	if isCanonical {
		return "This spec is the feature canonical document"
	}
	if drifted {
		return "Feature canonical is a different artifact (" + canonicalArtifactID + "). Promote to take over."
	}
	return "Working spec is not yet promoted"
}

func normalizeFeature(feature *workboard.Feature, now time.Time) {
	feature.ID = idOrNew(feature.ID)
	if feature.Status == "" {
		feature.Status = workboard.FeatureStatusCandidate
	}
	if feature.Version == 0 {
		feature.Version = 1
	}
	if feature.SourceDocumentIDs == "" {
		feature.SourceDocumentIDs = "[]"
	}
	if feature.SourceArtifactIDs == "" {
		feature.SourceArtifactIDs = "[]"
	}
	if feature.CreatedAt.IsZero() {
		feature.CreatedAt = now
	}
	if feature.UpdatedAt.IsZero() {
		feature.UpdatedAt = now
	}
}

func normalizeChangeRequest(cr *workboard.ChangeRequest, now time.Time) {
	cr.ID = idOrNew(cr.ID)
	if cr.Key == "" {
		cr.Key = "CR-" + shortID()
	}
	if cr.WorkType == "" {
		cr.WorkType = workboard.WorkTypeNewFeature
	}
	if cr.AcceptanceCriteria == "" {
		cr.AcceptanceCriteria = "[]"
	}
	if cr.NonGoals == "" {
		cr.NonGoals = "[]"
	}
	if cr.OpenQuestions == "" {
		cr.OpenQuestions = "[]"
	}
	if cr.SourceRefs == "" {
		cr.SourceRefs = "[]"
	}
	if cr.CreatedAt.IsZero() {
		cr.CreatedAt = now
	}
	if cr.UpdatedAt.IsZero() {
		cr.UpdatedAt = now
	}
}

func idOrNew(id string) string {
	if id != "" {
		return id
	}
	return uuid.NewString()
}

func shortID() string {
	return strings.ToUpper(strings.ReplaceAll(uuid.NewString()[:8], "-", ""))
}

func valueOrDefault(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func mapWorkBoardNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return workboard.ErrNotFound
	}
	return err
}
