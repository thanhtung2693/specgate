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
	"github.com/specgate/doc-registry/internal/workboard"
)

var ErrNotFound = artifact.ErrNotFound

var ErrArtifactReferenced = errors.New("artifact is referenced by governed work")

// Repository persists artifacts and events in the configured Postgres database via GORM.
type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// Insert persists a new artifact together with its child rows in a single
// transaction (spec §14).
func (r *Repository) Insert(ctx context.Context, a *artifact.Artifact) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Session(&gorm.Session{FullSaveAssociations: true}).Create(a).Error; err != nil {
			if isUniqueViolation(err) {
				return artifact.ErrConflict
			}
			return err
		}
		return nil
	})
}

// InsertWithEvent persists a new artifact and its publish event in one
// transaction so metadata and audit history cannot drift.
func (r *Repository) InsertWithEvent(ctx context.Context, a *artifact.Artifact, e artifact.Event) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Session(&gorm.Session{FullSaveAssociations: true}).Create(a).Error; err != nil {
			if isUniqueViolation(err) {
				return artifact.ErrConflict
			}
			return err
		}
		return createChainedEvent(tx, &e)
	})
}

// createChainedEvent inserts an artifact event linked into its artifact's
// tamper-evidence chain (spec §8). It locks the chain head so concurrent
// writers serialize, truncates CreatedAt to Postgres timestamptz precision so
// the stored row re-verifies bit-for-bit, and must be the ONLY way event rows
// are created.
func createChainedEvent(tx *gorm.DB, e *artifact.Event) error {
	var head struct{ Hash *string }
	if err := tx.Raw(
		`SELECT hash FROM artifact_events WHERE artifact_id = ? ORDER BY created_at DESC, id DESC LIMIT 1 FOR UPDATE`,
		e.ArtifactID,
	).Scan(&head).Error; err != nil {
		return err
	}
	e.CreatedAt = e.CreatedAt.UTC().Truncate(time.Microsecond)
	if head.Hash != nil {
		e.PrevHash = *head.Hash
	} else {
		e.PrevHash = ""
	}
	e.Hash = artifact.EventHash(e.PrevHash, *e)
	return tx.Create(e).Error
}

// Get loads an artifact by ID with all associations preloaded.
func (r *Repository) Get(ctx context.Context, id string) (*artifact.Artifact, error) {
	if workspaceID, ok := artifact.WorkspaceFromContext(ctx); ok {
		return r.GetInWorkspace(ctx, workspaceID, id)
	}
	var a artifact.Artifact
	err := r.db.WithContext(ctx).
		Preload(clause.Associations).
		First(&a, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *Repository) GetInWorkspace(ctx context.Context, workspaceID, id string) (*artifact.Artifact, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return nil, ErrNotFound
	}
	var a artifact.Artifact
	err := r.db.WithContext(ctx).
		Preload(clause.Associations).
		First(&a, "workspace_id = ? AND id = ?", workspaceID, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *Repository) List(ctx context.Context, f artifact.ListFilter) ([]artifact.Artifact, error) {
	q := r.filterQuery(ctx, f).Preload(clause.Associations)
	if f.Limit > 0 {
		q = q.Limit(f.Limit)
	}
	if f.Offset > 0 {
		q = q.Offset(f.Offset)
	}
	var out []artifact.Artifact
	return out, q.Order("created_at DESC").Find(&out).Error
}

// Count returns the total number of artifacts matching the filter, ignoring
// Limit/Offset — callers use this to populate an absolute `total` alongside a
// paged `List` response.
func (r *Repository) Count(ctx context.Context, f artifact.ListFilter) (int64, error) {
	var n int64
	err := r.filterQuery(ctx, f).Count(&n).Error
	return n, err
}

// filterQuery applies the shared WHERE/JOIN clauses used by List and Count so
// pagination metadata stays in sync with the page contents.
func (r *Repository) filterQuery(ctx context.Context, f artifact.ListFilter) *gorm.DB {
	q := r.db.WithContext(ctx).Model(&artifact.Artifact{})
	if workspaceID, ok := artifact.WorkspaceFromContext(ctx); ok {
		if f.WorkspaceID != "" && f.WorkspaceID != workspaceID {
			return q.Where("1 = 0")
		}
		f.WorkspaceID = workspaceID
	}
	if f.WorkspaceID != "" {
		q = q.Where("workspace_id = ?", f.WorkspaceID)
	}
	if f.FeatureID != "" {
		q = q.Where("feature_id = ?", f.FeatureID)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	} else if f.ExcludeStatus != "" {
		q = q.Where("status <> ?", f.ExcludeStatus)
	}
	if f.Service != "" {
		q = q.Joins("JOIN artifact_services s ON s.artifact_id = artifacts.id").
			Where("s.name = ?", f.Service)
	}
	return q
}

// UpdateStatus transitions the artifact and appends an event row in the same TX
// (spec §14).
func (r *Repository) UpdateStatus(
	ctx context.Context,
	id string,
	status artifact.Status,
	approvedBy string,
	event artifact.Event,
) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		updates := map[string]any{
			"status":     status,
			"updated_at": now,
		}
		if status == artifact.StatusApproved {
			updates["approved_by"] = approvedBy
			updates["approved_at"] = now
		}
		res := scopeArtifactQuery(tx.Model(&artifact.Artifact{}), ctx).Where("id = ?", id).Updates(updates)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		if err := createChainedEvent(tx, &event); err != nil {
			return err
		}
		// Approving a version supersedes the feature's other non-superseded
		// versions in the same transaction, so stale draft/approved rows do not
		// accumulate under the feature.
		if status == artifact.StatusApproved {
			return supersedePriorVersions(ctx, tx, id, now)
		}
		return nil
	})
}

// supersedePriorVersions marks every other non-superseded artifact sharing the
// approved artifact's feature as superseded, writing the transition event for
// each (spec §14). Quick-route artifacts without a feature are left untouched.
func supersedePriorVersions(ctx context.Context, tx *gorm.DB, approvedID string, now time.Time) error {
	var approved artifact.Artifact
	if err := scopeArtifactQuery(tx.Select("id", "feature_id", "version"), ctx).Where("id = ?", approvedID).First(&approved).Error; err != nil {
		return err
	}
	if approved.FeatureID == "" {
		return nil
	}
	var priors []artifact.Artifact
	if err := scopeArtifactQuery(tx, ctx).Where("feature_id = ? AND id <> ? AND status <> ?",
		approved.FeatureID, approvedID, artifact.StatusSuperseded).Find(&priors).Error; err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]string{"superseded_by": approvedID})
	if err != nil {
		return err
	}
	for _, p := range priors {
		// Only supersede OLDER versions. Approving a version replaces the ones it
		// descends from — never a newer version. Without this, re-approving an
		// older (or already-superseded) version would flip the current latest to
		// superseded, corrupting the feature's lifecycle.
		if artifact.CompareVersion(p.Version, approved.Version) >= 0 {
			continue
		}
		if err := scopeArtifactQuery(tx.Model(&artifact.Artifact{}), ctx).Where("id = ?", p.ID).
			Updates(map[string]any{"status": artifact.StatusSuperseded, "updated_at": now}).Error; err != nil {
			return err
		}
		supersededEvent := artifact.Event{
			ID:         uuid.NewString(),
			ArtifactID: p.ID,
			EventType:  artifact.EventSuperseded,
			Payload:    string(payload),
			CreatedAt:  now,
		}
		if err := createChainedEvent(tx, &supersededEvent); err != nil {
			return err
		}
	}
	return nil
}

// FindOverlappingServices returns artifacts (excluding the candidate feature)
// whose impacted services overlap the given list and are still active
// (spec §10, §14).
func (r *Repository) FindOverlappingServices(
	ctx context.Context,
	services []string,
	excludeFeatureID string,
) ([]artifact.Artifact, error) {
	var out []artifact.Artifact
	q := r.db.WithContext(ctx).
		Distinct("artifacts.*").
		Preload("Services").
		Joins("JOIN artifact_services s ON s.artifact_id = artifacts.id").
		Where("s.name IN ?", services).
		Where("artifacts.status IN ?", []artifact.Status{artifact.StatusDraft, artifact.StatusApproved}).
		Where("artifacts.feature_id <> ?", excludeFeatureID)
	if workspaceID, ok := artifact.WorkspaceFromContext(ctx); ok {
		q = q.Where("artifacts.workspace_id = ?", workspaceID)
	}
	err := q.Find(&out).Error
	return out, err
}

// RetentionBucket pairs a status with the cutoff timestamp below which rows
// in that status are eligible for cleanup. Spec §9 defines different retention
// windows per status (approved: 180d, superseded: 90d, others: 30d), so the
// repository takes a slice of buckets rather than a single cutoff.
type RetentionBucket struct {
	Status artifact.Status
	Cutoff time.Time
}

// ListExpiredCandidates returns IDs of artifacts past retention, across all
// provided buckets (spec §9, §14). An empty input returns no IDs without
// hitting the database.
func (r *Repository) ListExpiredCandidates(ctx context.Context, buckets []RetentionBucket) ([]string, error) {
	if len(buckets) == 0 {
		return nil, nil
	}
	q := r.db.WithContext(ctx).Model(&artifact.Artifact{})
	if workspaceID, ok := artifact.WorkspaceFromContext(ctx); ok {
		q = q.Where("workspace_id = ?", workspaceID)
	}
	var (
		conds []string
		args  []any
	)
	for _, b := range buckets {
		conds = append(conds, "(status = ? AND updated_at < ?)")
		args = append(args, b.Status, b.Cutoff)
	}
	var ids []string
	err := q.Where(strings.Join(conds, " OR "), args...).Pluck("id", &ids).Error
	return ids, err
}

// DeleteArtifactGateRows removes gate runs and gate tasks that reference the
// given artifact ids. Artifact deletion does not cascade these tables (no FK);
// the retention sweeper calls this after deleting expired artifacts so
// readiness evidence rows do not orphan (per spec §9).
func (r *Repository) DeleteArtifactGateRows(ctx context.Context, artifactIDs []string) error {
	if len(artifactIDs) == 0 {
		return nil
	}
	q := r.db.WithContext(ctx).Where("subject_id IN ?", artifactIDs)
	if workspaceID, ok := artifact.WorkspaceFromContext(ctx); ok {
		q = q.Where("workspace_id = ?", workspaceID)
	}
	if err := q.Delete(&workboard.GateRun{}).Error; err != nil {
		return fmt.Errorf("delete artifact gate runs: %w", err)
	}
	if workspaceID, ok := artifact.WorkspaceFromContext(ctx); ok {
		if err := r.db.WithContext(ctx).Exec("DELETE FROM gate_tasks WHERE artifact_id::text IN ? AND workspace_id = ?", artifactIDs, workspaceID).Error; err != nil {
			return fmt.Errorf("delete artifact gate tasks: %w", err)
		}
	} else if err := r.db.WithContext(ctx).Exec("DELETE FROM gate_tasks WHERE artifact_id::text IN ?", artifactIDs).Error; err != nil {
		return fmt.Errorf("delete artifact gate tasks: %w", err)
	}
	return nil
}

// Delete removes an unreferenced artifact and cascades to child tables via FK.
func (r *Repository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row artifact.Artifact
		err := scopeArtifactQuery(tx, ctx).
			Select("id", "workspace_id").
			Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&row, "id = ?", id).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}

		var referenced bool
		if err := tx.Raw(`
			SELECT EXISTS (
				SELECT 1 FROM features
				WHERE workspace_id = ? AND canonical_artifact_id = ?
				UNION ALL
				SELECT 1 FROM change_requests
				WHERE workspace_id = ? AND lead_artifact_id = ?
			)
		`, row.WorkspaceID, row.ID, row.WorkspaceID, row.ID).Scan(&referenced).Error; err != nil {
			return err
		}
		if referenced {
			return ErrArtifactReferenced
		}

		res := scopeArtifactQuery(tx, ctx).Delete(&artifact.Artifact{}, "id = ?", id)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// ListEvents returns event log rows in chronological order after the optional
// cutoff. A zero limit defaults to 100; a negative limit returns the complete
// history for internal audit verification.
func (r *Repository) ListEvents(ctx context.Context, f artifact.EventFilter) ([]artifact.Event, error) {
	limit := f.Limit
	if limit == 0 {
		limit = 100
	}
	q := r.db.WithContext(ctx).Model(&artifact.Event{}).Order("created_at ASC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	if f.EventType != "" {
		q = q.Where("event_type = ?", f.EventType)
	}
	if f.ArtifactID != "" {
		q = q.Where("artifact_id = ?", f.ArtifactID)
	}
	if f.WorkspaceID != "" {
		q = q.Where("artifact_id IN (SELECT id FROM artifacts WHERE workspace_id = ?)", f.WorkspaceID)
	}
	if workspaceID, ok := artifact.WorkspaceFromContext(ctx); ok {
		q = q.Where("artifact_id IN (SELECT id FROM artifacts WHERE workspace_id = ?)", workspaceID)
	}
	if !f.After.IsZero() {
		q = q.Where("created_at > ?", f.After)
	}
	var out []artifact.Event
	return out, q.Find(&out).Error
}

func scopeArtifactQuery(q *gorm.DB, ctx context.Context) *gorm.DB {
	if workspaceID, ok := artifact.WorkspaceFromContext(ctx); ok {
		return q.Where("workspace_id = ?", workspaceID)
	}
	return q
}

// Readiness runs persist as artifact-scoped platform rows in the unified
// gate_runs table (per spec §3.2); the ReadinessRun domain shape is converted
// at this boundary. The executor filter keeps IDE-agent gate-task result rows
// (same subject, executor "ide_agent") out of the readiness projection.

func (r *Repository) InsertReadinessRuns(ctx context.Context, rows []artifact.ReadinessRun) error {
	if len(rows) == 0 {
		return nil
	}
	runs := make([]workboard.GateRun, len(rows))
	workspaceID, _ := artifact.WorkspaceFromContext(ctx)
	if workspaceID != "" {
		var count int64
		if err := r.db.WithContext(ctx).Model(&artifact.Artifact{}).
			Where("workspace_id = ? AND id = ?", workspaceID, rows[0].ArtifactID).Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return artifact.ErrNotFound
		}
	}
	for i, row := range rows {
		runs[i] = workboard.GateRun{
			ID:           row.ID,
			WorkspaceID:  workspaceID,
			SubjectKind:  workboard.GateRunSubjectArtifact,
			SubjectID:    row.ArtifactID,
			Gate:         row.Gate,
			State:        workboard.NextActionState(row.State),
			Hint:         row.Hint,
			Executor:     workboard.GateRunExecutorPlatform,
			EvidenceJSON: row.EvidenceJSON,
			CreatedAt:    row.CreatedAt,
		}
	}
	return r.db.WithContext(ctx).Create(&runs).Error
}

func (r *Repository) ListReadinessRuns(ctx context.Context, artifactID string, limit int) ([]artifact.ReadinessRun, error) {
	if limit == 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	// History shows every executor with its origin; readiness aggregates retain
	// platform and IDE-agent verdicts while human decisions stay separate.
	var runs []workboard.GateRun
	q := r.db.WithContext(ctx)
	if workspaceID, ok := artifact.WorkspaceFromContext(ctx); ok {
		q = q.Where("workspace_id = ?", workspaceID)
	}
	q = q.
		Where(
			"subject_kind = ? AND subject_id = ?",
			workboard.GateRunSubjectArtifact, artifactID,
		).
		Order("created_at DESC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	err := q.Find(&runs).Error
	if err != nil {
		return nil, err
	}
	rows := make([]artifact.ReadinessRun, len(runs))
	for i, run := range runs {
		rows[i] = artifact.ReadinessRun{
			ID:           run.ID,
			ArtifactID:   run.SubjectID,
			Gate:         run.Gate,
			State:        artifact.ReadinessState(run.State),
			Hint:         run.Hint,
			Executor:     run.Executor,
			EvidenceJSON: run.EvidenceJSON,
			CreatedAt:    run.CreatedAt,
		}
	}
	return rows, nil
}
