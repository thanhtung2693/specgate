package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/specgate/doc-registry/internal/artifact"
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
