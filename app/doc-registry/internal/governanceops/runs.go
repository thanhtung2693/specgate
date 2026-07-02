package governanceops

import (
	"context"
	"fmt"
	"strings"

	"github.com/specgate/doc-registry/internal/agentsclient"
	"github.com/specgate/doc-registry/internal/governanceprofile"
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
			if snap, snapErr := governanceprofile.ParseSnapshot(art.GatesProfileSnapshotJSON); snapErr == nil {
				governanceLevel = string(snap.GovernanceLevel)
			}
		}
	}

	return &ReadinessResult{
		ArtifactID:        verdict.ArtifactID,
		EvaluationsPosted: verdict.EvaluationsPosted,
		ReadinessRuns:     verdict.ReadinessRuns,
		Aggregate:         aggregateReadiness(verdict.ReadinessRuns),
		GovernanceLevel:   governanceLevel,
	}, nil
}

// RunLLMGates triggers LLM quality gates for a change request via the agents
// service. The raw result map is returned for the caller to marshal as needed.
func (s *Service) RunLLMGates(ctx context.Context, changeRequestID string) (map[string]any, error) {
	if s.AgentsRunner == nil {
		return nil, fmt.Errorf("%w: agents service not configured", ErrUnavailable)
	}
	id := strings.TrimSpace(changeRequestID)
	if id == "" {
		return nil, fmt.Errorf("change_request_id is required")
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
	acs := make([]string, 0, len(in.AcceptanceCriteria))
	for _, ac := range in.AcceptanceCriteria {
		if trimmed := strings.TrimSpace(ac); trimmed != "" {
			acs = append(acs, trimmed)
		}
	}
	return s.AgentsRunner.CreateQuickWorkItem(ctx, title, description, in.IssueURL, in.IssueKey, in.FeatureKey, in.FeatureName, acs, strings.TrimSpace(in.CreatedBy), strings.TrimSpace(in.WorkspaceID))
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
