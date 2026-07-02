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

// SetAutoArchiveOnDeliveryPass configures whether a passing delivery_review
// gate run should archive its ChangeRequest. The function form lets callers
// read live settings without recreating the repository.
func (r *WorkBoardRepository) SetAutoArchiveOnDeliveryPass(enabled func() bool) {
	r.autoArchiveOnDeliveryPassFunc = enabled
}

func (r *WorkBoardRepository) autoArchiveOnDeliveryPass() bool {
	return r.autoArchiveOnDeliveryPassFunc != nil && r.autoArchiveOnDeliveryPassFunc()
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
	var feature workboard.Feature
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&feature, "key = ?", key).Error; err == nil {
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		now := time.Now().UTC()
		feature = workboard.Feature{
			Key:  key,
			Name: valueOrDefault(name, key),
		}
		normalizeFeature(&feature, now)
		return tx.Create(&feature).Error
	})
	if err != nil {
		return nil, err
	}
	return &feature, nil
}

func (r *WorkBoardRepository) CreateFeature(
	ctx context.Context,
	in workboard.Feature,
) (*workboard.Feature, error) {
	now := time.Now().UTC()
	normalizeFeature(&in, now)
	if err := r.db.WithContext(ctx).Create(&in).Error; err != nil {
		return nil, err
	}
	return &in, nil
}

func (r *WorkBoardRepository) ListFeatures(ctx context.Context) ([]workboard.Feature, error) {
	var out []workboard.Feature
	err := r.db.WithContext(ctx).Order("created_at DESC").Find(&out).Error
	return out, err
}

func (r *WorkBoardRepository) GetFeature(ctx context.Context, id string) (*workboard.Feature, error) {
	var out workboard.Feature
	err := r.db.WithContext(ctx).First(&out, "id = ?", id).Error
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
		if err := tx.First(&existing, "id = ?", in.ID).Error; err != nil {
			return mapWorkBoardNotFound(err)
		}
		res := tx.Model(&workboard.Feature{}).Where("id = ?", in.ID).Updates(updates)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return workboard.ErrNotFound
		}
		if in.Status != "" && existing.Status != in.Status {
			return insertWorkBoardLifecycleEvent(tx, "feature", existing.ID, "feature.status_changed", "", map[string]any{
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

// DeleteFeature stays disabled because removing product capability records has
// broad audit impact. Quick-route CRs may exist without a Feature, so callers
// should deprecate product Features instead of deleting them.
func (r *WorkBoardRepository) DeleteFeature(ctx context.Context, id string) error {
	_ = ctx
	_ = id
	return fmt.Errorf("%w: delete feature disabled in development; use PATCH status=deprecated", workboard.ErrValidation)
}

// promoteStatusOnCanonical returns the new FeatureStatus a feature should
// transition to when an approved canonical artifact is set, or "" to leave
// status unchanged. Curator-controlled terminal states (rejected,
// deprecated) and the already-promoted active state are preserved.
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
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&feature, "id = ?", featureID).Error; err != nil {
			return mapWorkBoardNotFound(err)
		}
		if err := validateCanonicalArtifact(tx, artifactID); err != nil {
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
		if err := tx.Model(&workboard.Feature{}).Where("id = ?", featureID).Updates(updates).Error; err != nil {
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
		return tx.First(&feature, "id = ?", featureID).Error
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
		"subject_kind = ? AND subject_id = ? AND executor = ?",
		workboard.GateRunSubjectArtifact, artifactID, workboard.GateRunExecutorPlatform,
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

// SetFeatureSummary persists the LLM-generated feature Overview narrative and
// the canonical artifact version it was generated from. Returns the updated
// feature row, or ErrNotFound when the feature does not exist.
func (r *WorkBoardRepository) SetFeatureSummary(
	ctx context.Context,
	featureID string,
	summaryMD string,
	sourceVersion string,
) (*workboard.Feature, error) {
	var feature workboard.Feature
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&feature, "id = ?", featureID).Error; err != nil {
			return mapWorkBoardNotFound(err)
		}
		updates := map[string]any{
			"summary_md":             summaryMD,
			"summary_source_version": sourceVersion,
			"updated_at":             time.Now().UTC(),
		}
		if err := tx.Model(&workboard.Feature{}).Where("id = ?", featureID).Updates(updates).Error; err != nil {
			return err
		}
		return tx.First(&feature, "id = ?", featureID).Error
	})
	if err != nil {
		return nil, err
	}
	return &feature, nil
}

func (r *WorkBoardRepository) CreateChangeRequest(
	ctx context.Context,
	in workboard.ChangeRequest,
) (*workboard.ChangeRequest, error) {
	now := time.Now().UTC()
	normalizeChangeRequest(&in, now)
	if err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&in).Error; err != nil {
			return err
		}
		return replaceAcceptanceCriteria(tx, in.ID, in.AcceptanceCriteria, workboard.AcceptanceCriterionSourceHuman, now)
	}); err != nil {
		return nil, err
	}
	return &in, nil
}

func (r *WorkBoardRepository) ListChangeRequests(ctx context.Context, includeArchived bool) ([]workboard.ChangeRequest, error) {
	var out []workboard.ChangeRequest
	q := r.db.WithContext(ctx).Order("created_at DESC")
	if !includeArchived {
		q = q.Where("archived = ?", false)
	}
	if err := q.Find(&out).Error; err != nil {
		return nil, err
	}
	for i := range out {
		if err := r.deriveChangeRequestReadFields(ctx, &out[i]); err != nil {
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
	if err := r.db.WithContext(ctx).First(&out, "id = ?", id).Error; err != nil {
		return nil, mapWorkBoardNotFound(err)
	}
	if err := r.deriveChangeRequestReadFields(ctx, &out); err != nil {
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
	res := r.db.WithContext(ctx).Model(&workboard.ChangeRequest{}).Where("id = ?", id).Updates(updates)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, workboard.ErrNotFound
	}
	return r.GetChangeRequest(ctx, id)
}

// deriveChangeRequestReadFields fills the read-only derived fields on a change
// request: the board phase (from artifact pointers) and the latest inbound
// tracker status. Neither is persisted; both are computed per read.
func (r *WorkBoardRepository) deriveChangeRequestReadFields(
	ctx context.Context,
	cr *workboard.ChangeRequest,
) error {
	phase, err := r.derivedChangeRequestPhase(ctx, *cr)
	if err != nil {
		return err
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
	if cr.ContextPackArtifactID != "" {
		return workboard.BoardPhaseHandoff, nil
	}
	if cr.LeadArtifactID != "" {
		var lead artifact.Artifact
		if err := r.db.WithContext(ctx).First(&lead, "id = ?", cr.LeadArtifactID).Error; err != nil {
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
	if err := r.db.WithContext(ctx).
		Where("event_type = ?", integrations.FeedbackEventTrackerStatusChanged).
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
// link — the git/MCP evidence that delivery actually landed. Keyed by
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
	if err := r.db.WithContext(ctx).
		Model(&integrations.DeliveryLink{}).
		Where("change_request_id = ? AND external_type = ? AND state = ?",
			changeRequestID, integrations.ExternalTypeMergeRequest, integrations.DeliveryStateMerged).
		Count(&count).Error; err != nil {
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
		if err := tx.First(&existing, "id = ?", in.ID).Error; err != nil {
			return mapWorkBoardNotFound(err)
		}
		res := tx.Model(&workboard.ChangeRequest{}).Where("id = ?", in.ID).Updates(updates)
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
			return insertWorkBoardLifecycleEvent(tx, "change_request", existing.ID, "change_request.archived", in.ArchivedBy, map[string]any{
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
		if err := tx.First(&existing, "id = ?", id).Error; err != nil {
			return mapWorkBoardNotFound(err)
		}
		if err := tx.Model(&workboard.ChangeRequest{}).Where("id = ?", id).Updates(map[string]any{
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
		return insertWorkBoardLifecycleEvent(tx, "change_request", existing.ID, "change_request.unarchived", actor, map[string]any{
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

// DeleteChangeRequest removes the CR and its acceptance_criteria rows in one
// transaction. Implementation runs are NOT cascaded — they are runtime
// artifacts and operators should review them before wiping.
func (r *WorkBoardRepository) DeleteChangeRequest(ctx context.Context, id string) error {
	_ = ctx
	_ = id
	return fmt.Errorf("%w: delete change request disabled in development; use PATCH archived=true", workboard.ErrValidation)
}

func (r *WorkBoardRepository) PatchLeadArtifact(
	ctx context.Context,
	id string,
	artifactID string,
) (*workboard.ChangeRequest, error) {
	return r.patchChangeRequestArtifact(ctx, id, "lead_artifact_id", artifactID)
}

func (r *WorkBoardRepository) PatchContextPackArtifact(
	ctx context.Context,
	id string,
	artifactID string,
) (*workboard.ChangeRequest, error) {
	return r.patchChangeRequestArtifact(ctx, id, "context_pack_artifact_id", artifactID)
}

func (r *WorkBoardRepository) patchChangeRequestArtifact(
	ctx context.Context,
	id string,
	column string,
	artifactID string,
) (*workboard.ChangeRequest, error) {
	res := r.db.WithContext(ctx).Model(&workboard.ChangeRequest{}).Where("id = ?", id).Updates(map[string]any{
		column:       artifactID,
		"updated_at": time.Now().UTC(),
	})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, workboard.ErrNotFound
	}
	return r.GetChangeRequest(ctx, id)
}

func (r *WorkBoardRepository) ListStaleWarnings(
	ctx context.Context,
	filter workboard.StaleWarningFilter,
) ([]workboard.StaleWarning, error) {
	if filter.FeatureID == "" && filter.ChangeRequestID == "" {
		features, err := r.ListFeatures(ctx)
		if err != nil {
			return nil, err
		}
		featureByID := make(map[string]workboard.Feature, len(features))
		var warnings []workboard.StaleWarning
		for _, item := range features {
			featureByID[item.ID] = item
			itemWarnings, err := r.listStaleWarningsForFeatureAndCR(ctx, item, workboard.ChangeRequest{}, true)
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
				itemWarnings, err := r.listStaleWarningsForFeatureAndCR(ctx, workboard.Feature{}, cr, false)
				if err != nil {
					return nil, err
				}
				warnings = append(warnings, itemWarnings...)
				continue
			}
			feature, ok := featureByID[cr.FeatureID]
			if !ok {
				if err := r.db.WithContext(ctx).First(&feature, "id = ?", cr.FeatureID).Error; err != nil {
					return nil, mapWorkBoardNotFound(err)
				}
				featureByID[cr.FeatureID] = feature
			}
			itemWarnings, err := r.listStaleWarningsForFeatureAndCR(ctx, feature, cr, false)
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
		if err := r.db.WithContext(ctx).First(&cr, "id = ?", filter.ChangeRequestID).Error; err != nil {
			return nil, mapWorkBoardNotFound(err)
		}
		filter.FeatureID = cr.FeatureID
	}
	if filter.FeatureID == "" {
		return nil, nil
	}
	if err := r.db.WithContext(ctx).First(&feature, "id = ?", filter.FeatureID).Error; err != nil {
		return nil, mapWorkBoardNotFound(err)
	}
	return r.listStaleWarningsForFeatureAndCR(ctx, feature, cr, true)
}

func (r *WorkBoardRepository) listStaleWarningsForFeatureAndCR(
	ctx context.Context,
	feature workboard.Feature,
	cr workboard.ChangeRequest,
	includeFeatureWarnings bool,
) ([]workboard.StaleWarning, error) {
	var warnings []workboard.StaleWarning
	if includeFeatureWarnings {
		if feature.Status == workboard.FeatureStatusDeprecated {
			warnings = append(warnings, staleWarning(workboard.WarningFeatureDeprecated, feature.ID, cr.ID, "", "Feature is deprecated."))
		}
		if feature.CanonicalArtifactID == "" {
			warnings = append(warnings, staleWarning(workboard.WarningCanonicalArtifactMissing, feature.ID, cr.ID, "", "Feature has no canonical artifact."))
		} else {
			var a artifact.Artifact
			if err := r.db.WithContext(ctx).First(&a, "id = ?", feature.CanonicalArtifactID).Error; err == nil {
				if a.Status != artifact.StatusApproved {
					warnings = append(warnings, staleWarning(workboard.WarningCanonicalArtifactUnapproved, feature.ID, cr.ID, a.ID, "Canonical artifact is not approved."))
				}
				if a.Status == artifact.StatusSuperseded {
					warnings = append(warnings, staleWarning(workboard.WarningCanonicalArtifactSuperseded, feature.ID, cr.ID, a.ID, "Canonical artifact is superseded."))
				}
				// The persisted feature Overview is generated from a specific
				// canonical artifact version; warn when that version no longer
				// matches the current canonical artifact. artifact.Version is a
				// string (e.g. "v0.1"), so compare directly.
				if feature.SummaryMD != "" && feature.SummarySourceVersion != "" && a.Version != feature.SummarySourceVersion {
					warnings = append(warnings, staleWarning(workboard.WarningFeatureSummaryOutdated, feature.ID, cr.ID, a.ID, "Feature summary is older than the current canonical artifact."))
				}
				linkedKnowledgeWarning, err := r.linkedKnowledgeNewerWarning(ctx, feature, a, cr.ID)
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
		if err := r.db.WithContext(ctx).First(&lead, "id = ?", cr.LeadArtifactID).Error; err == nil {
			if lead.Status == artifact.StatusSuperseded {
				warnings = append(warnings, staleWarning(workboard.WarningLeadArtifactSuperseded, feature.ID, cr.ID, lead.ID, "Lead artifact is superseded."))
			}
			if lead.Status == artifact.StatusApproved && feature.CanonicalArtifactID != lead.ID {
				warnings = append(warnings, staleWarning(workboard.WarningCanonicalPromotionAvailable, feature.ID, cr.ID, lead.ID, "Approved lead artifact can be promoted to canonical."))
			}
		}
	}
	// Tracker status augments the derived phase but must not override the
	// git/MCP delivery evidence; a clear contradiction surfaces as a warning.
	// Only meaningful for a loaded CR (the feature-level board loop passes no
	// cr.ID, so this never fires there).
	if cr.ID != "" {
		stalePack, err := r.contextPackStaleWarning(ctx, feature, cr)
		if err != nil {
			return nil, err
		}
		if stalePack != nil {
			warnings = append(warnings, *stalePack)
		}
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
	if err := r.db.WithContext(ctx).
		Model(&integrations.DeliveryLink{}).
		Where("feature_id IN ? AND external_type = ? AND state = ?", featureRefs, integrations.ExternalTypeMergeRequest, integrations.DeliveryStateOpened).
		Count(&openLinkCount).Error; err != nil {
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
	if err := r.db.WithContext(ctx).First(&cr, "id = ?", changeRequestID).Error; err != nil {
		return nil, mapWorkBoardNotFound(err)
	}
	// Quick-route change requests may have no feature; a zero-value feature
	// behaves like one without a canonical artifact.
	var feature workboard.Feature
	if cr.FeatureID != "" {
		if err := r.db.WithContext(ctx).First(&feature, "id = ?", cr.FeatureID).Error; err != nil {
			return nil, mapWorkBoardNotFound(err)
		}
	}
	var lead *artifact.Artifact
	if cr.LeadArtifactID != "" {
		var a artifact.Artifact
		if err := r.db.WithContext(ctx).First(&a, "id = ?", cr.LeadArtifactID).Error; err == nil {
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
	return []workboard.NextAction{
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
		{
			// Full-route CRs (lead artifact present) assemble the context pack on-demand
			// via the MCP resource; no stored artifact is required. Quick-route CRs have no
			// lead artifact and must persist a pack artifact so the agent can read it.
			Gate:  "delivery_pack",
			State: stateForDeliveryPackGate(cr.ContextPackArtifactID, cr.LeadArtifactID),
			Hint:  hintForDeliveryPackGate(cr.ContextPackArtifactID, cr.LeadArtifactID),
		},
	}, nil
}

func (r *WorkBoardRepository) RefreshGateRuns(
	ctx context.Context,
	in workboard.RefreshGateRunsInput,
) ([]workboard.GateRun, error) {
	changeRequestID := strings.TrimSpace(in.ChangeRequestID)
	if changeRequestID == "" {
		return nil, workboard.ErrValidation
	}
	actions, err := r.NextActions(ctx, changeRequestID)
	if err != nil {
		return nil, err
	}
	var cr workboard.ChangeRequest
	if err := r.db.WithContext(ctx).First(&cr, "id = ?", changeRequestID).Error; err != nil {
		return nil, mapWorkBoardNotFound(err)
	}
	// Quick-route change requests may have no feature (see NextActions).
	var feature workboard.Feature
	if cr.FeatureID != "" {
		if err := r.db.WithContext(ctx).First(&feature, "id = ?", cr.FeatureID).Error; err != nil {
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
	linkedKnowledgeRows, err := r.listLinkedKnowledgeEvidence(ctx, feature)
	if err != nil {
		return nil, err
	}
	proposalRef := ""
	baseArtifactID := strings.TrimSpace(cr.LeadArtifactID)
	if baseArtifactID == "" {
		baseArtifactID = strings.TrimSpace(feature.CanonicalArtifactID)
	}
	if baseArtifactID != "" {
		proposalRef = "/artifact-edit/sessions?base_artifact_id=" + baseArtifactID
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
		evidencePayload, _ := json.Marshal(map[string]any{
			"evidence_contract_version": "gate-run-v1",
			"gate":                      action.Gate,
			"evaluator": map[string]any{
				"type":               evaluatorType(hasEval),
				"judge_model":        judgeModel,
				"config_version":     "workboard-next-actions-v1",
				"eval_suite_version": evalSuiteVersion,
			},
			"verdict":                  verdict,
			"confidence":               confidence,
			"evidence":                 evidence,
			"change_request_id":        changeRequestID,
			"feature_id":               feature.ID,
			"source_artifact_id":       baseArtifactID,
			"lead_artifact_id":         cr.LeadArtifactID,
			"canonical_artifact_id":    feature.CanonicalArtifactID,
			"context_pack_artifact_id": cr.ContextPackArtifactID,
			"linked_knowledge":         linkedKnowledgeRows,
			"warnings":                 warningRows,
		})
		if len(evidencePayload) > 0 {
			evidenceJSON = string(evidencePayload)
		}
		run := workboard.GateRun{
			ID:           uuid.NewString(),
			SubjectKind:  workboard.GateRunSubjectChangeRequest,
			SubjectID:    changeRequestID,
			Gate:         action.Gate,
			State:        state,
			Hint:         hint,
			Executor:     workboard.GateRunExecutorPlatform,
			EvidenceJSON: evidenceJSON,
			CreatedAt:    now,
		}
		if proposalRef != "" && (action.Gate == "knowledge_fresh" || action.Gate == "canonical_spec") &&
			(action.State == workboard.NextActionStateWarn || action.State == workboard.NextActionStatePending) {
			run.ProposalRef = proposalRef
		}
		rows = append(rows, run)
	}
	// Eval-only gates — the LLM-judged ones — have no deterministic next-action,
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
		evidencePayload, _ := json.Marshal(map[string]any{
			"evidence_contract_version": "gate-run-v1",
			"gate":                      gate,
			"evaluator": map[string]any{
				"type":               "agent_judge",
				"judge_model":        strings.TrimSpace(eval.JudgeModel),
				"config_version":     "workboard-next-actions-v1",
				"eval_suite_version": strings.TrimSpace(eval.EvalSuiteVersion),
			},
			"verdict":            string(eval.State),
			"confidence":         eval.Confidence,
			"evidence":           strings.TrimSpace(eval.Evidence),
			"change_request_id":  changeRequestID,
			"feature_id":         feature.ID,
			"source_artifact_id": baseArtifactID,
		})
		rows = append(rows, workboard.GateRun{
			ID:           uuid.NewString(),
			SubjectKind:  workboard.GateRunSubjectChangeRequest,
			SubjectID:    changeRequestID,
			Gate:         gate,
			State:        eval.State,
			Hint:         strings.TrimSpace(eval.Hint),
			Executor:     workboard.GateRunExecutorPlatform,
			EvidenceJSON: string(evidencePayload),
			CreatedAt:    now,
		})
	}
	if len(rows) == 0 {
		return rows, nil
	}
	if err := r.db.WithContext(ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	if r.autoArchiveOnDeliveryPass() && hasPassedDeliveryReview(rows) {
		if _, err := r.UpdateChangeRequest(ctx, workboard.ChangeRequest{
			ID:            changeRequestID,
			Archived:      true,
			ArchivedBy:    "specgate-auto-archive",
			ArchiveReason: "delivery review passed",
		}); err != nil {
			return nil, err
		}
	}
	return rows, nil
}

func hasPassedDeliveryReview(rows []workboard.GateRun) bool {
	for _, row := range rows {
		if row.Gate == governanceprofile.DeliveryReviewGateKey && row.State == workboard.NextActionStatePass {
			return true
		}
	}
	return false
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
	feature workboard.Feature,
) ([]map[string]any, error) {
	featureRefs := []string{feature.ID}
	if strings.TrimSpace(feature.Key) != "" && feature.Key != feature.ID {
		featureRefs = append(featureRefs, feature.Key)
	}
	var docs []knowledge.Document
	if err := r.db.WithContext(ctx).
		Where("is_latest = ? AND status = ? AND linked_feature_id IN ?", true, knowledge.StatusIndexed, featureRefs).
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
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	var rows []workboard.GateRun
	err := r.db.WithContext(ctx).
		Where("subject_kind = ? AND subject_id = ?", workboard.GateRunSubjectChangeRequest, changeRequestID).
		Order("created_at DESC").
		Limit(limit).
		Find(&rows).Error
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
	err := r.db.WithContext(ctx).
		Where("change_request_id = ?", changeRequestID).
		Order("sort_order ASC, created_at ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	if len(rows) > 0 {
		return rows, nil
	}
	var cr workboard.ChangeRequest
	if err := r.db.WithContext(ctx).First(&cr, "id = ?", changeRequestID).Error; err != nil {
		return nil, mapWorkBoardNotFound(err)
	}
	now := time.Now().UTC()
	if err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return replaceAcceptanceCriteria(tx, changeRequestID, cr.AcceptanceCriteria, workboard.AcceptanceCriterionSourceLLM, now)
	}); err != nil {
		return nil, err
	}
	return r.ListAcceptanceCriteria(ctx, changeRequestID)
}

func (r *WorkBoardRepository) linkedKnowledgeNewerWarning(
	ctx context.Context,
	feature workboard.Feature,
	canonical artifact.Artifact,
	changeRequestID string,
) (*workboard.StaleWarning, error) {
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
		Where("is_latest = ? AND status = ? AND linked_feature_id IN ?", true, knowledge.StatusIndexed, featureRefs).
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
// tracker status contradicts the git/MCP merge evidence for the change request.
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

func (r *WorkBoardRepository) contextPackStaleWarning(
	ctx context.Context,
	feature workboard.Feature,
	cr workboard.ChangeRequest,
) (*workboard.StaleWarning, error) {
	if cr.ContextPackArtifactID == "" {
		return nil, nil
	}
	var row integrations.GovernanceFeedbackEvent
	err := r.db.WithContext(ctx).
		Where("change_request_id = ? AND event_type = ?", cr.ID, integrations.FeedbackEventContextPackStale).
		Order("created_at DESC").
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	message := strings.TrimSpace(row.Reason)
	if message == "" {
		message = "Delivery changed after the Context Pack was handed off."
	}
	w := staleWarning(workboard.WarningContextPackStale, feature.ID, cr.ID, cr.ContextPackArtifactID, message)
	return &w, nil
}

// trackerPriorityUrgentWarning emits tracker_priority_urgent when the latest
// tracker feedback event for the CR carries a Linear priority of 1 (urgent) or
// 2 (high) AND the CR has not yet been handed off (context_pack_artifact_id is
// empty). Priority 0 (no priority set) never fires this warning; priorities 3–4
// (normal, low) never fire it. Tracker feedback rows carry no change_request_id;
// the link is the payload `correlation_id` matched against the CR id or key
// (same convention as latestTrackerStatus). per spec §14.
func (r *WorkBoardRepository) trackerPriorityUrgentWarning(
	ctx context.Context,
	feature workboard.Feature,
	cr workboard.ChangeRequest,
) (*workboard.StaleWarning, error) {
	if cr.ContextPackArtifactID != "" {
		// Already handed off — warning is not applicable.
		return nil, nil
	}
	var rows []integrations.GovernanceFeedbackEvent
	if err := r.db.WithContext(ctx).
		Where("event_type = ?", integrations.FeedbackEventTrackerStatusChanged).
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

// deliveryStaleWarning emits delivery_stale when the most recent delivery_review
// gate run for the change request has state=fail and was created more than
// r.deliverySLADays days ago. Returns nil when no delivery review history exists,
// when the latest review passed, or when the failure is within the threshold.
// per spec §delivery-sla.
func (r *WorkBoardRepository) deliveryStaleWarning(
	ctx context.Context,
	feature workboard.Feature,
	cr workboard.ChangeRequest,
) (*workboard.StaleWarning, error) {
	var latest workboard.GateRun
	err := r.db.WithContext(ctx).
		Where(
			"subject_kind = ? AND subject_id = ? AND gate = ?",
			workboard.GateRunSubjectChangeRequest, cr.ID, governanceprofile.DeliveryReviewGateKey,
		).
		Order("created_at DESC").
		First(&latest).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil // no delivery review yet — no warning
	}
	if err != nil {
		return nil, err
	}
	if latest.State != workboard.NextActionStateFail {
		return nil, nil // latest review passed or is pending — no warning
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

func validateCanonicalArtifact(tx *gorm.DB, artifactID string) error {
	var a artifact.Artifact
	if err := tx.First(&a, "id = ?", artifactID).Error; err != nil {
		return mapWorkBoardNotFound(err)
	}
	if a.Status != artifact.StatusApproved {
		return fmt.Errorf("canonical artifact must be approved")
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
	return tx.Create(&artifact.Event{
		ID:         uuid.NewString(),
		ArtifactID: artifactID,
		EventType:  eventType,
		Payload:    string(body),
		CreatedAt:  now,
	}).Error
}

func insertWorkBoardLifecycleEvent(
	tx *gorm.DB,
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

func stateForDeliveryPackGate(contextPackArtifactID, leadArtifactID string) workboard.NextActionState {
	if contextPackArtifactID != "" {
		return workboard.NextActionStatePass
	}
	if leadArtifactID != "" {
		// Full-route: pack assembled on-demand via MCP, no stored artifact needed.
		return workboard.NextActionStateNotApplicable
	}
	return workboard.NextActionStatePending
}

func hintForDeliveryPackGate(contextPackArtifactID, leadArtifactID string) string {
	if contextPackArtifactID != "" {
		return contextPackArtifactID
	}
	if leadArtifactID != "" {
		return "Context pack assembled on-demand from lead artifact via MCP resource"
	}
	return "No delivery context pack attached"
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
