package db

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/workboard"
)

var ErrNotFound = artifact.ErrNotFound

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
		return tx.Create(&e).Error
	})
}

// Get loads an artifact by ID with all associations preloaded.
func (r *Repository) Get(ctx context.Context, id string) (*artifact.Artifact, error) {
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
	if f.FeatureID != "" {
		q = q.Where("feature_id = ?", f.FeatureID)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
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
		res := tx.Model(&artifact.Artifact{}).Where("id = ?", id).Updates(updates)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return tx.Create(&event).Error
	})
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
	err := r.db.WithContext(ctx).
		Distinct("artifacts.*").
		Preload("Services").
		Joins("JOIN artifact_services s ON s.artifact_id = artifacts.id").
		Where("s.name IN ?", services).
		Where("artifacts.status IN ?", []artifact.Status{artifact.StatusDraft, artifact.StatusApproved}).
		Where("artifacts.feature_id <> ?", excludeFeatureID).
		Find(&out).Error
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

// Delete removes an artifact and cascades to child tables via FK.
func (r *Repository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&artifact.Artifact{}, "id = ?", id).Error
}

// ListEvents returns event log rows in chronological order after the optional
// cutoff. A zero limit defaults to 100.
func (r *Repository) ListEvents(ctx context.Context, f artifact.EventFilter) ([]artifact.Event, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	q := r.db.WithContext(ctx).Model(&artifact.Event{}).Order("created_at ASC").Limit(limit)
	if f.EventType != "" {
		q = q.Where("event_type = ?", f.EventType)
	}
	if f.ArtifactID != "" {
		q = q.Where("artifact_id = ?", f.ArtifactID)
	}
	if !f.After.IsZero() {
		q = q.Where("created_at > ?", f.After)
	}
	var out []artifact.Event
	return out, q.Find(&out).Error
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
	for i, row := range rows {
		runs[i] = workboard.GateRun{
			ID:           row.ID,
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
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	var runs []workboard.GateRun
	err := r.db.WithContext(ctx).
		Where(
			"subject_kind = ? AND subject_id = ? AND executor = ?",
			workboard.GateRunSubjectArtifact, artifactID, workboard.GateRunExecutorPlatform,
		).
		Order("created_at DESC").
		Limit(limit).
		Find(&runs).Error
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
			EvidenceJSON: run.EvidenceJSON,
			CreatedAt:    run.CreatedAt,
		}
	}
	return rows, nil
}
