package artifact

import (
	"context"
	"testing"
)

func TestRefreshReadinessRuns_PersistsRows(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)
	ctx := context.Background()

	a, err := svc.Publish(ctx, PublishInput{
		FeatureID:   "checkout-loyalty",
		Version:     "v0.1",
		RequestType: RequestTypeNewFeature,
		ImpactLevel: ImpactLevelHigh,
		Documents: []DocumentInput{
			{Path: "spec.md", Role: "spec", Content: []byte("# Spec")},
		},
	})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	rows, err := svc.RefreshReadinessRuns(ctx, a.ID, []ReadinessEvaluation{
		{
			Gate:             "spec_completeness",
			State:            ReadinessStateWarn,
			Hint:             "missing constraints",
			Confidence:       0.8,
			JudgeModel:       "governance-gate-judge",
			EvalSuiteVersion: "spec-completeness-v1",
			Evidence:         `{"topics":[{"topic":"constraints","status":"missing"}]}`,
		},
	})
	if err != nil {
		t.Fatalf("RefreshReadinessRuns: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if rows[0].ArtifactID != a.ID {
		t.Fatalf("ArtifactID = %q, want %q", rows[0].ArtifactID, a.ID)
	}
}
