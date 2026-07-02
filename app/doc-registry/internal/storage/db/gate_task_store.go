package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/specgate/doc-registry/internal/policy"
	"github.com/specgate/doc-registry/internal/workboard"
)

const postgresTimestampPrecision = time.Microsecond

// gateTaskRow maps the gate_tasks table.
type gateTaskRow struct {
	ID             string    `gorm:"column:id;primaryKey"`
	ArtifactID     string    `gorm:"column:artifact_id"`
	GateKey        string    `gorm:"column:gate_key"`
	GateVersion    string    `gorm:"column:gate_version"`
	GateDigest     string    `gorm:"column:gate_digest"`
	ArtifactDigest string    `gorm:"column:artifact_digest"`
	ProfileDigest  string    `gorm:"column:profile_digest"`
	Executor       string    `gorm:"column:executor"`
	SkillContent   string    `gorm:"column:skill_content"`
	ExpiresAt      time.Time `gorm:"column:expires_at"`
	CreatedAt      time.Time `gorm:"column:created_at"`
}

func (gateTaskRow) TableName() string { return "gate_tasks" }

// PGGateTaskStore is a Postgres-backed GateTaskStore.
type PGGateTaskStore struct {
	db *gorm.DB
}

// NewPGGateTaskStore returns a Postgres-backed GateTaskStore using the given GORM DB handle.
func NewPGGateTaskStore(db *gorm.DB) *PGGateTaskStore {
	return &PGGateTaskStore{db: db}
}

func (s *PGGateTaskStore) CreateTask(ctx context.Context, t policy.GateTaskRecord) (*policy.GateTaskRecord, error) {
	if t.ExpiresAt.IsZero() {
		return nil, fmt.Errorf("%w: ExpiresAt must be set", policy.ErrExpiresAtRequired)
	}
	if t.ID == "" {
		t.ID = uuid.NewString()
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now().UTC()
	}
	row := toGateTaskRow(t)
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return nil, fmt.Errorf("gate_tasks insert: %w", err)
	}
	out := fromGateTaskRow(row)
	return &out, nil
}

func (s *PGGateTaskStore) GetTask(ctx context.Context, id string) (*policy.GateTaskRecord, error) {
	var row gateTaskRow
	err := s.db.WithContext(ctx).Where("id = ?", id).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("%w: %q", policy.ErrGateTaskNotFound, id)
	}
	if err != nil {
		return nil, fmt.Errorf("gate_tasks get: %w", err)
	}
	out := fromGateTaskRow(row)
	return &out, nil
}

func (s *PGGateTaskStore) ListTasksForArtifact(ctx context.Context, artifactID string) ([]policy.GateTaskRecord, error) {
	// A task is pending until a result run exists for it. Submitted results live
	// in the unified gate_runs table keyed by the task id (per spec §3.2).
	var rows []gateTaskRow
	if err := s.db.WithContext(ctx).
		Where("artifact_id = ? AND NOT EXISTS (SELECT 1 FROM gate_runs WHERE gate_runs.id = gate_tasks.id::text)", artifactID).
		Order("created_at ASC").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("gate_tasks list: %w", err)
	}
	out := make([]policy.GateTaskRecord, len(rows))
	for i, r := range rows {
		out[i] = fromGateTaskRow(r)
	}
	return out, nil
}

func (s *PGGateTaskStore) SubmitResult(ctx context.Context, taskID string, r policy.GateResultRecord) (*policy.GateResultRecord, error) {
	// Wrap the read-validate-write in a single transaction and lock the task row
	// so concurrent submissions for the same taskID serialize (per review Fix 1).
	var out policy.GateResultRecord
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var taskRow gateTaskRow
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", taskID).First(&taskRow).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("%w: %q", policy.ErrGateTaskNotFound, taskID)
		}
		if err != nil {
			return fmt.Errorf("gate_tasks get: %w", err)
		}
		task := fromGateTaskRow(taskRow)

		stamped, err := policy.ValidateAndStampResult(&task, r)
		if err != nil {
			// Return validation errors unwrapped so errors.Is works for callers.
			return err
		}

		// A submitted result IS a gate run (per spec §3.2): persist it as an
		// artifact-scoped row in the unified gate_runs table, keyed by the task
		// id so a resubmission replaces the prior result (one result per task,
		// last-wins) instead of accumulating rows. The result-only fields ride
		// evidence_json.
		evidence, err := json.Marshal(map[string]any{
			"task_id":     stamped.TaskID,
			"result_id":   stamped.ID,
			"gate_digest": stamped.GateDigest,
			"trust":       string(stamped.Trust),
			"evaluator":   stamped.EvaluatorJSON,
			"findings":    stamped.FindingsJSON,
		})
		if err != nil {
			return fmt.Errorf("gate result evidence: %w", err)
		}
		run := workboard.GateRun{
			ID:           stamped.TaskID,
			SubjectKind:  workboard.GateRunSubjectArtifact,
			SubjectID:    task.ArtifactID,
			Gate:         task.GateKey,
			State:        workboard.NextActionState(stamped.State),
			Hint:         "",
			Executor:     stamped.Executor,
			EvidenceJSON: string(evidence),
			CreatedAt:    normalizePostgresTimestamp(stamped.SubmittedAt),
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "id"}},
			UpdateAll: true,
		}).Create(&run).Error; err != nil {
			return fmt.Errorf("gate result run upsert: %w", err)
		}
		out = stamped
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// toGateTaskRow converts a policy record to the GORM row type.
func toGateTaskRow(t policy.GateTaskRecord) gateTaskRow {
	return gateTaskRow{
		ID:             t.ID,
		ArtifactID:     t.ArtifactID,
		GateKey:        t.GateKey,
		GateVersion:    t.GateVersion,
		GateDigest:     t.GateDigest,
		ArtifactDigest: t.ArtifactDigest,
		ProfileDigest:  t.ProfileDigest,
		Executor:       string(t.Executor),
		SkillContent:   t.SkillContent,
		ExpiresAt:      normalizePostgresTimestamp(t.ExpiresAt),
		CreatedAt:      normalizePostgresTimestamp(t.CreatedAt),
	}
}

func normalizePostgresTimestamp(t time.Time) time.Time {
	if t.IsZero() {
		return t
	}
	return t.UTC().Truncate(postgresTimestampPrecision)
}

// fromGateTaskRow converts a GORM row back to the policy record.
func fromGateTaskRow(r gateTaskRow) policy.GateTaskRecord {
	return policy.GateTaskRecord{
		ID:             r.ID,
		ArtifactID:     r.ArtifactID,
		GateKey:        r.GateKey,
		GateVersion:    r.GateVersion,
		GateDigest:     r.GateDigest,
		ArtifactDigest: r.ArtifactDigest,
		ProfileDigest:  r.ProfileDigest,
		Executor:       policy.Executor(r.Executor),
		SkillContent:   r.SkillContent,
		ExpiresAt:      r.ExpiresAt,
		CreatedAt:      r.CreatedAt,
	}
}
