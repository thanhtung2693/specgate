package api

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/specgate/doc-registry/internal/policy"
)

type gateTaskTestStore struct {
	tasks   map[string]policy.GateTaskRecord
	results map[string]bool
}

func newGateTaskTestStore() *gateTaskTestStore {
	return &gateTaskTestStore{
		tasks:   map[string]policy.GateTaskRecord{},
		results: map[string]bool{},
	}
}

func (s *gateTaskTestStore) CreateTask(ctx context.Context, task policy.GateTaskRecord) (*policy.GateTaskRecord, error) {
	return s.createTask(ctx, task)
}

func (s *gateTaskTestStore) CreateTaskIfCurrentMissing(ctx context.Context, task policy.GateTaskRecord, now time.Time) (*policy.GateTaskRecord, bool, error) {
	for _, existing := range s.tasks {
		if existing.WorkspaceID == task.WorkspaceID &&
			existing.ArtifactID == task.ArtifactID &&
			existing.ArtifactDigest == task.ArtifactDigest &&
			existing.GateKey == task.GateKey &&
			existing.GateDigest == task.GateDigest &&
			(s.results[existing.ID] || existing.ExpiresAt.After(now)) {
			return nil, false, nil
		}
	}
	created, err := s.createTask(ctx, task)
	return created, err == nil, err
}

func (s *gateTaskTestStore) createTask(ctx context.Context, task policy.GateTaskRecord) (*policy.GateTaskRecord, error) {
	if task.ExpiresAt.IsZero() {
		return nil, policy.ErrExpiresAtRequired
	}
	if workspaceID, ok := policy.WorkspaceFromContext(ctx); ok && strings.TrimSpace(task.WorkspaceID) != workspaceID {
		return nil, policy.ErrWorkspaceMismatch
	}
	if task.ID == "" {
		task.ID = uuid.NewString()
	}
	if task.CreatedAt.IsZero() {
		task.CreatedAt = time.Now().UTC()
	}
	s.tasks[task.ID] = task
	copy := task
	return &copy, nil
}

func (s *gateTaskTestStore) GetTask(ctx context.Context, id string) (*policy.GateTaskRecord, error) {
	task, ok := s.tasks[id]
	if !ok || !gateTaskVisible(ctx, task) {
		return nil, fmt.Errorf("%w: %q", policy.ErrGateTaskNotFound, id)
	}
	copy := task
	return &copy, nil
}

func (s *gateTaskTestStore) ListTasksForArtifact(ctx context.Context, artifactID string) ([]policy.GateTaskRecord, error) {
	var tasks []policy.GateTaskRecord
	for _, task := range s.tasks {
		if task.ArtifactID == artifactID && !s.results[task.ID] && task.ExpiresAt.After(time.Now().UTC()) && gateTaskVisible(ctx, task) {
			tasks = append(tasks, task)
		}
	}
	return tasks, nil
}

func (s *gateTaskTestStore) SubmitResult(ctx context.Context, taskID string, result policy.GateResultRecord) (*policy.GateResultRecord, error) {
	task, ok := s.tasks[taskID]
	if !ok || !gateTaskVisible(ctx, task) {
		return nil, fmt.Errorf("%w: %q", policy.ErrGateTaskNotFound, taskID)
	}
	stamped, err := policy.ValidateAndStampResult(&task, result)
	if err != nil {
		return nil, err
	}
	s.results[taskID] = true
	return &stamped, nil
}

func gateTaskVisible(ctx context.Context, task policy.GateTaskRecord) bool {
	workspaceID, ok := policy.WorkspaceFromContext(ctx)
	return !ok || strings.TrimSpace(task.WorkspaceID) == workspaceID
}
