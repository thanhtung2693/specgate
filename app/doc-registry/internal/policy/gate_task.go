package policy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

var (
	ErrGateTaskNotFound  = errors.New("gate task not found")
	ErrStaleDigest       = errors.New("stale gate digest")
	ErrExecutorMismatch  = errors.New("executor mismatch")
	ErrExpiresAtRequired = errors.New("expires_at is required")
)

// ValidateAndStampResult applies binding validation (gate_digest + executor match)
// and stamps the Trust level on r in place, then fills r.TaskID and r.SubmittedAt if unset.
// It returns the stamped copy. Callers (both in-memory and Postgres implementations)
// call this before persisting so the rules are identical.
func ValidateAndStampResult(task *GateTaskRecord, r GateResultRecord) (GateResultRecord, error) {
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
	// Ensure JSONB/JSON columns are non-nil so both stores satisfy NOT NULL constraints
	// and callers always receive consistent empty-object/array defaults.
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
	GetTask(ctx context.Context, id string) (*GateTaskRecord, error)
	ListTasksForArtifact(ctx context.Context, artifactID string) ([]GateTaskRecord, error)
	SubmitResult(ctx context.Context, taskID string, r GateResultRecord) (*GateResultRecord, error)
}

// NewInMemGateTaskStore returns a thread-safe in-memory GateTaskStore for tests.
func NewInMemGateTaskStore() GateTaskStore {
	return &inMemGateTaskStore{
		tasks:   map[string]*GateTaskRecord{},
		results: map[string]*GateResultRecord{},
	}
}

type inMemGateTaskStore struct {
	mu      sync.RWMutex
	tasks   map[string]*GateTaskRecord
	results map[string]*GateResultRecord
}

func (s *inMemGateTaskStore) CreateTask(ctx context.Context, t GateTaskRecord) (*GateTaskRecord, error) {
	if t.ExpiresAt.IsZero() {
		return nil, fmt.Errorf("%w: ExpiresAt must be set", ErrExpiresAtRequired)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if t.ID == "" {
		t.ID = uuid.NewString()
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now()
	}
	s.tasks[t.ID] = &t
	cp := t
	return &cp, nil
}

func (s *inMemGateTaskStore) GetTask(ctx context.Context, id string) (*GateTaskRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tasks[id]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrGateTaskNotFound, id)
	}
	cp := *t
	return &cp, nil
}

func (s *inMemGateTaskStore) ListTasksForArtifact(ctx context.Context, artifactID string) ([]GateTaskRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []GateTaskRecord
	for _, t := range s.tasks {
		if t.ArtifactID == artifactID && s.results[t.ID] == nil {
			out = append(out, *t)
		}
	}
	return out, nil
}

func (s *inMemGateTaskStore) SubmitResult(ctx context.Context, taskID string, r GateResultRecord) (*GateResultRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[taskID]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrGateTaskNotFound, taskID)
	}
	stamped, err := ValidateAndStampResult(task, r)
	if err != nil {
		return nil, err
	}
	// One result per task (last-wins): key by TaskID, not the result ID, so a
	// resubmission replaces the prior result rather than accumulating — matching
	// the Postgres store's delete-then-insert behavior.
	s.results[stamped.TaskID] = &stamped
	cp := stamped
	return &cp, nil
}
