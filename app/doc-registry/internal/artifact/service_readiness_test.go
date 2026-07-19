package artifact

import (
	"context"
	"encoding/json"
	"testing"
)

func TestRefreshReadinessRuns_PersistsRows(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)
	ctx := context.Background()

	a, err := svc.Publish(ctx, PublishInput{
		WorkspaceID: "test-workspace",
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

func TestRefreshReadinessRunsWrapsJudgeMetadataIntoEnvelope(t *testing.T) {
	t.Parallel()
	svc, _, _ := newTestService(t)
	art, err := svc.Publish(context.Background(), PublishInput{
		WorkspaceID: "test-workspace",
		FeatureID:   "envelope-wrap",
		Version:     "v0.1",
		RequestType: RequestTypeNewFeature,
		ImpactLevel: ImpactLevelLow,
		Documents:   []DocumentInput{{Path: "spec.md", Role: "spec", Content: []byte("# Spec")}},
	})
	if err != nil {
		t.Fatal(err)
	}

	runs, err := svc.RefreshReadinessRuns(context.Background(), art.ID, []ReadinessEvaluation{
		{
			Gate:             "scope_clear",
			State:            ReadinessStatePass,
			Hint:             "Scope bounded with explicit non-goals",
			Confidence:       0.88,
			JudgeModel:       "governance-gate-judge",
			EvalSuiteVersion: "quality-gate-v1",
			Evidence:         "Non-goals section names three exclusions",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(runs))
	}

	var envelope map[string]any
	if err := json.Unmarshal([]byte(runs[0].EvidenceJSON), &envelope); err != nil {
		t.Fatalf("evidence_json is not an envelope: %v (%q)", err, runs[0].EvidenceJSON)
	}
	if envelope["evidence_contract_version"] != "gate-run-v1" {
		t.Errorf("contract = %v", envelope["evidence_contract_version"])
	}
	evaluator, _ := envelope["evaluator"].(map[string]any)
	if evaluator["judge_model"] != "governance-gate-judge" {
		t.Errorf("judge_model = %v", evaluator["judge_model"])
	}
	if envelope["confidence"] != 0.88 {
		t.Errorf("confidence = %v", envelope["confidence"])
	}
	if envelope["evidence"] != "Non-goals section names three exclusions" {
		t.Errorf("evidence = %v", envelope["evidence"])
	}
}

func TestRefreshReadinessRunsLeavesEnvelopeAndPlainEvidenceAlone(t *testing.T) {
	t.Parallel()
	svc, _, _ := newTestService(t)
	art, err := svc.Publish(context.Background(), PublishInput{
		WorkspaceID: "test-workspace",
		FeatureID:   "envelope-passthrough",
		Version:     "v0.1",
		RequestType: RequestTypeNewFeature,
		ImpactLevel: ImpactLevelLow,
		Documents:   []DocumentInput{{Path: "spec.md", Role: "spec", Content: []byte("# Spec")}},
	})
	if err != nil {
		t.Fatal(err)
	}

	preWrapped := `{"evidence_contract_version":"gate-run-v1","evidence":"already wrapped"}`
	runs, err := svc.RefreshReadinessRuns(context.Background(), art.ID, []ReadinessEvaluation{
		// Already-enveloped evidence passes through untouched.
		{Gate: "spec_completeness", State: ReadinessStateWarn, Evidence: preWrapped, JudgeModel: "governance-gate-judge"},
		// No judge metadata: plain evidence stays plain.
		{Gate: "required_roles", State: ReadinessStatePass, Evidence: "all roles present"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if runs[0].EvidenceJSON != preWrapped {
		t.Errorf("pre-wrapped evidence mutated: %q", runs[0].EvidenceJSON)
	}
	if runs[1].EvidenceJSON != "all roles present" {
		t.Errorf("plain evidence mutated: %q", runs[1].EvidenceJSON)
	}
}

func TestRefreshReadinessRunsDoesNotTreatEmbeddedFieldNameAsEnvelope(t *testing.T) {
	t.Parallel()
	evidence := `{"note":"mentions evidence_contract_version but is not a gate envelope"}`

	got := readinessEvidenceJSON(ReadinessEvaluation{
		Gate:       "scope_clear",
		State:      ReadinessStateWarn,
		JudgeModel: "governance-gate-judge",
		Evidence:   evidence,
	}, "scope_clear")

	var envelope map[string]any
	if err := json.Unmarshal([]byte(got), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope["evidence_contract_version"] != "gate-run-v1" {
		t.Fatalf("contract = %v, want gate-run-v1; got %s", envelope["evidence_contract_version"], got)
	}
	if envelope["evidence"] != evidence {
		t.Fatalf("evidence = %v, want original payload", envelope["evidence"])
	}
}
