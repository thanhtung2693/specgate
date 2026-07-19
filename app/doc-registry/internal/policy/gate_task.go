package policy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrGateTaskNotFound    = errors.New("gate task not found")
	ErrWorkspaceMismatch   = errors.New("gate task workspace mismatch")
	ErrStaleDigest         = errors.New("stale gate digest")
	ErrInputDigestMismatch = errors.New("input digest mismatch")
	ErrGateTaskExpired     = errors.New("gate task expired")
	ErrExecutorMismatch    = errors.New("executor mismatch")
	ErrExpiresAtRequired   = errors.New("expires_at is required")
)

// ValidateAndStampResult applies binding validation (gate_digest + executor match)
// and stamps the Trust level on r in place, then fills r.TaskID and r.SubmittedAt if unset.
// It returns the stamped copy.
func ValidateAndStampResult(task *GateTaskRecord, r GateResultRecord) (GateResultRecord, error) {
	if !task.ExpiresAt.After(time.Now().UTC()) {
		return GateResultRecord{}, ErrGateTaskExpired
	}
	if strings.TrimSpace(r.InputDigest) == "" || r.InputDigest != task.ArtifactDigest {
		return GateResultRecord{}, fmt.Errorf("%w: task has %s, result submitted %s", ErrInputDigestMismatch, task.ArtifactDigest, r.InputDigest)
	}
	if r.GateDigest != task.GateDigest {
		return GateResultRecord{}, fmt.Errorf("%w: task has %s, result submitted %s", ErrStaleDigest, task.GateDigest, r.GateDigest)
	}
	if r.Executor != string(task.Executor) {
		return GateResultRecord{}, fmt.Errorf("%w: task assigned to %q, got %q", ErrExecutorMismatch, task.Executor, r.Executor)
	}
	switch r.Executor {
	case string(ExecutorIDEAgent):
		r.Trust = TrustAgentAttested
	case string(ExecutorPlatformLLM):
		r.Trust = TrustPlatformEvaluated
	case string(ExecutorHuman):
		r.Trust = TrustHumanDecision
	default:
		r.Trust = TrustAgentAttested
	}
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	if r.SubmittedAt.IsZero() {
		r.SubmittedAt = time.Now()
	}
	r.TaskID = task.ID
	// Ensure JSONB/JSON columns are non-nil for storage constraints and callers.
	if len(r.EvaluatorJSON) == 0 {
		r.EvaluatorJSON = json.RawMessage(`{}`)
	}
	if len(r.FindingsJSON) == 0 {
		r.FindingsJSON = json.RawMessage(`[]`)
	}
	return r, nil
}

// GateTaskStore manages gate tasks and submitted results.
type GateTaskStore interface {
	CreateTask(ctx context.Context, t GateTaskRecord) (*GateTaskRecord, error)
	CreateTaskIfCurrentMissing(ctx context.Context, t GateTaskRecord, now time.Time) (*GateTaskRecord, bool, error)
	GetTask(ctx context.Context, id string) (*GateTaskRecord, error)
	ListTasksForArtifact(ctx context.Context, artifactID string) ([]GateTaskRecord, error)
	SubmitResult(ctx context.Context, taskID string, r GateResultRecord) (*GateResultRecord, error)
}
