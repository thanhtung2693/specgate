package governanceops

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/specgate/doc-registry/internal/agentsclient"
	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/governanceprofile"
	"github.com/specgate/doc-registry/internal/workboard"
)

// RunReadiness runs the readiness gates for an artifact via the agents service
// and returns the verdict plus a derived aggregate. Requires AgentsRunner.
func (s *Service) RunReadiness(ctx context.Context, artifactID string) (*ReadinessResult, error) {
	if s.AgentsRunner == nil {
		return nil, fmt.Errorf("%w: agents service not configured", ErrUnavailable)
	}
	artifactID = strings.TrimSpace(artifactID)
	if artifactID == "" {
		return nil, fmt.Errorf("artifact_id is required")
	}
	if s.Artifacts != nil {
		art, err := s.Artifacts.Get(ctx, artifactID)
		if err != nil {
			return nil, err
		}
		if selected := trustedWorkspace(ctx); selected != "" && strings.TrimSpace(art.WorkspaceID) != selected {
			return nil, ErrNotFound
		}
	}
	verdict, err := s.AgentsRunner.RunReadiness(ctx, artifactID)
	if err != nil {
		return nil, err
	}
	if verdict == nil {
		return nil, fmt.Errorf("readiness unavailable: empty verdict")
	}

	// Read governance level from the artifact snapshot when the reader is available.
	// per spec §8: GovernanceLevel is informational — snapshot read failure does not
	// block the readiness result.
	var governanceLevel string
	if s.Artifacts != nil {
		if art, artErr := s.Artifacts.Get(ctx, artifactID); artErr == nil {
			if snap, snapErr := governanceprofile.ParseSnapshot(art.PolicySnapshotJSON); snapErr == nil {
				governanceLevel = string(snap.GovernanceLevel)
			}
		}
	}

	readinessRuns := verdict.ReadinessRuns
	if s.ReadinessRuns != nil {
		stored, listErr := s.ReadinessRuns.ListReadinessRuns(ctx, artifactID, 500)
		if listErr != nil {
			return nil, fmt.Errorf("%w: stored readiness results unavailable: %v", ErrUnavailable, listErr)
		}
		readinessRuns = mergeLatestReadinessRuns(readinessRuns, stored)
	}

	aggregate := aggregateReadiness(readinessRuns)
	if verdict.DispatchedToIDEAgent != nil && len(verdict.DispatchedToIDEAgent.PendingTaskIDs) > 0 && (aggregate == "pass" || aggregate == "warn") {
		aggregate = "not_run"
	}
	return &ReadinessResult{
		ArtifactID:           verdict.ArtifactID,
		EvaluationsPosted:    verdict.EvaluationsPosted,
		ReadinessRuns:        readinessRuns,
		DispatchedToIDEAgent: verdict.DispatchedToIDEAgent,
		Aggregate:            aggregate,
		GovernanceLevel:      governanceLevel,
	}, nil
}

func mergeLatestReadinessRuns(current []agentsclient.ReadinessRun, stored []artifact.ReadinessRun) []agentsclient.ReadinessRun {
	latest := make(map[string]agentsclient.ReadinessRun, len(current)+len(stored))
	for _, run := range current {
		latest[run.Gate] = run
	}
	for _, run := range stored {
		if run.Executor != workboard.GateRunExecutorPlatform && run.Executor != workboard.GateRunExecutorIDEAgent {
			continue
		}
		candidate := agentsclient.ReadinessRun{
			Gate:         run.Gate,
			State:        string(run.State),
			Hint:         run.Hint,
			Executor:     run.Executor,
			EvidenceJSON: run.EvidenceJSON,
			CreatedAt:    run.CreatedAt.UTC().Format(time.RFC3339Nano),
		}
		previous, ok := latest[candidate.Gate]
		previousAt, parseErr := time.Parse(time.RFC3339Nano, previous.CreatedAt)
		if !ok || parseErr != nil || run.CreatedAt.After(previousAt) {
			latest[candidate.Gate] = candidate
		}
	}

	gates := make([]string, 0, len(latest))
	for gate := range latest {
		gates = append(gates, gate)
	}
	sort.Strings(gates)
	result := make([]agentsclient.ReadinessRun, 0, len(gates))
	for _, gate := range gates {
		result = append(result, latest[gate])
	}
	return result
}

// RunLLMGates triggers model-judged quality gates for a change request via the
// agents service. The raw result map is returned for the caller to marshal as
// needed.
func (s *Service) RunLLMGates(ctx context.Context, changeRequestID string) (map[string]any, error) {
	if s.AgentsRunner == nil {
		return nil, fmt.Errorf("%w: agents service not configured", ErrUnavailable)
	}
	id := strings.TrimSpace(changeRequestID)
	if id == "" {
		return nil, fmt.Errorf("change_request_id is required")
	}
	if s.WorkBoard != nil {
		cr, err := s.WorkBoard.GetChangeRequest(ctx, id)
		if err != nil {
			return nil, err
		}
		if err := requireChangeRequestWorkspace(ctx, cr); err != nil {
			return nil, ErrNotFound
		}
	}
	return s.AgentsRunner.RunLLMGates(ctx, id)
}

// ReviewDelivery triggers the delivery review for a change request via the
// agents service. The raw result map is returned for the caller to marshal.
func (s *Service) ReviewDelivery(ctx context.Context, changeRequestID string) (map[string]any, error) {
	if s.AgentsRunner == nil {
		return nil, fmt.Errorf("%w: agents service not configured", ErrUnavailable)
	}
	id := strings.TrimSpace(changeRequestID)
	if id == "" {
		return nil, fmt.Errorf("change_request_id is required")
	}
	if s.WorkBoard != nil {
		cr, err := s.WorkBoard.GetChangeRequest(ctx, id)
		if err != nil {
			return nil, err
		}
		if err := requireChangeRequestWorkspace(ctx, cr); err != nil {
			return nil, ErrNotFound
		}
	}
	return s.AgentsRunner.ReviewDelivery(ctx, id)
}

// CreateQuickWorkItem creates a quick-route change request from issue content.
// The raw result map is returned for the caller to marshal as needed.
func (s *Service) CreateQuickWorkItem(ctx context.Context, in CreateQuickWorkItemInput) (map[string]any, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return nil, fmt.Errorf("title is required")
	}
	description := strings.TrimSpace(in.Description)
	if description == "" {
		return nil, fmt.Errorf("description is required")
	}
	if s.AgentsRunner == nil {
		return nil, fmt.Errorf("%w: agents service not configured", ErrUnavailable)
	}
	workspaceID := strings.TrimSpace(in.WorkspaceID)
	if selected := trustedWorkspace(ctx); selected != "" {
		if workspaceID != "" && workspaceID != selected {
			return nil, fmt.Errorf("%w: workspace mismatch", ErrValidation)
		}
		workspaceID = selected
	}
	acs := make([]AcceptanceCriterionInput, 0, len(in.AcceptanceCriteria))
	for _, ac := range in.AcceptanceCriteria {
		trimmed := AcceptanceCriterionInput{
			Text:                strings.TrimSpace(ac.Text),
			VerificationBinding: strings.TrimSpace(ac.VerificationBinding),
		}
		if trimmed.Text != "" {
			acs = append(acs, trimmed)
		}
	}
	result, err := s.AgentsRunner.CreateQuickWorkItem(ctx, title, description, in.IssueURL, in.IssueKey, in.FeatureKey, in.FeatureName, acs, strings.TrimSpace(in.CreatedBy), workspaceID)
	if err != nil {
		var responseErr *agentsclient.ResponseError
		if errors.As(err, &responseErr) && responseErr.StatusCode == http.StatusBadRequest {
			detail := responseErr.Detail()
			if detail == "" {
				detail = responseErr.Error()
			}
			return nil, fmt.Errorf("%w: %s", ErrValidation, detail)
		}
		return nil, err
	}
	return result, nil
}

// aggregateReadiness reduces the latest run per gate to a single verdict.
// Precedence: fail > needs_human_review > warn > pass > not_run.
// not_applicable counts as pass-equivalent; pending does not contribute.
// No runs (or only pending) => not_run.
// Ported from ui/src/lib/readiness.ts (aggregateReadiness).
func aggregateReadiness(runs []agentsclient.ReadinessRun) string {
	latest := make(map[string]agentsclient.ReadinessRun, len(runs))
	for _, r := range runs {
		prev, ok := latest[r.Gate]
		if !ok || r.CreatedAt >= prev.CreatedAt {
			latest[r.Gate] = r
		}
	}
	var hasFail, hasNeedsHuman, hasWarn, hasClean bool
	for _, r := range latest {
		switch r.State {
		case "fail":
			hasFail = true
		case "needs_human_review":
			hasNeedsHuman = true
		case "warn":
			hasWarn = true
		case "pass", "not_applicable":
			hasClean = true
		}
	}
	switch {
	case hasFail:
		return "fail"
	case hasNeedsHuman:
		return "needs_human_review"
	case hasWarn:
		return "warn"
	case hasClean:
		return "pass"
	default:
		return "not_run"
	}
}
