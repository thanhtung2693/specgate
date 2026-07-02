package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/specgate/doc-registry/internal/agentsclient"
	"github.com/specgate/doc-registry/internal/governanceops"
)

// readinessTestRunner implements governanceops.AgentsRunner for thin wiring tests.
type readinessTestRunner struct {
	verdict *agentsclient.Verdict
}

func (r *readinessTestRunner) RunReadiness(_ context.Context, artifactID string) (*agentsclient.Verdict, error) {
	if r.verdict == nil {
		return &agentsclient.Verdict{ArtifactID: artifactID}, nil
	}
	return r.verdict, nil
}
func (r *readinessTestRunner) RunLLMGates(_ context.Context, _ string) (map[string]any, error) {
	return nil, nil
}
func (r *readinessTestRunner) ReviewDelivery(_ context.Context, _ string) (map[string]any, error) {
	return nil, nil
}
func (r *readinessTestRunner) CreateQuickWorkItem(_ context.Context, _, _, _, _, _, _ string, _ []string, _, _ string) (map[string]any, error) {
	return nil, nil
}

// TestSpecgateCheckReadinessHandler_JSONOutput verifies the thin adapter
// marshals the service result into valid JSON with aggregate.
func TestSpecgateCheckReadinessHandler_JSONOutput(t *testing.T) {
	t.Parallel()
	runner := &readinessTestRunner{
		verdict: &agentsclient.Verdict{
			ArtifactID:        "art-1",
			EvaluationsPosted: 1,
			ReadinessRuns: []agentsclient.ReadinessRun{
				{Gate: "spec_completeness", State: "warn", CreatedAt: "2026-01-01T00:00:00Z"},
			},
		},
	}
	svc := &governanceops.Service{AgentsRunner: runner}
	handler := NewSpecgateCheckReadinessHandler(svc)

	out, err := handler(context.Background(), SpecgateCheckReadinessInput{ArtifactID: "art-1"})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	for _, want := range []string{`"artifact_id"`, `"readiness_runs"`, `"aggregate":"warn"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %s: %s", want, out)
		}
	}
}

// TestSpecgateCheckReadinessHandler_PropagatesError verifies errors from the
// service are returned without modification.
func TestSpecgateCheckReadinessHandler_PropagatesError(t *testing.T) {
	t.Parallel()
	// nil AgentsRunner → service returns error
	svc := &governanceops.Service{}
	handler := NewSpecgateCheckReadinessHandler(svc)
	_, err := handler(context.Background(), SpecgateCheckReadinessInput{ArtifactID: "art-1"})
	if err == nil {
		t.Fatal("expected error when AgentsRunner is nil")
	}
}
