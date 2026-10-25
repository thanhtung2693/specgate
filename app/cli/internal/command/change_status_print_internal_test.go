package command

import (
	"bytes"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/local"
)

func TestLocalCriterionOutputKeepsProvenanceAndGapsVisible(t *testing.T) {
	for _, test := range []struct {
		name, provenance, status, grounding string
		gaps                                []string
		want                                []string
	}{
		{"attested", "agent_attested", "pass", "grounded", nil, []string{"agent_attested (agent-reported)"}},
		{"command", "command_only", "pass", "grounded", nil, []string{"command_only (CLI observed command; no selected testcase observation)"}},
		{"selected", "selected_test_observed", "pass", "grounded", nil, []string{"selected_test_observed (CLI observed selected testcases)"}},
		{"legacy", "", "pass", "", nil, []string{"provenance: unknown", "citation grounding: unknown"}},
		{"failed", "command_only", "fail", "unanchored", []string{"bound check unit: fail", "citation: unanchored"}, []string{"unit (fail)", "gap: bound check unit: fail", "gap: citation: unanchored"}},
		{"skipped", "agent_attested", "skipped", "heading_not_found", []string{"bound check unit: skipped", "citation: heading_not_found"}, []string{"unit (skipped)", "gap: bound check unit: skipped", "gap: citation: heading_not_found"}},
		{"missing", "", "", "unverified", []string{"bound check unit: ", "citation: unverified"}, []string{"unit (missing)", "gap: bound check unit:", "gap: citation: unverified"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var out bytes.Buffer
			status := changeStatusResult{
				Mode: config.ModeLocal, Decision: "Accepted by human", Freshness: "unavailable", Stale: true,
				Criteria:          []client.CriterionReview{{CriterionID: "local-1", Text: "Works", Verdict: "unclear", Why: "required testcase is missing"}},
				CriterionEvidence: []local.AcceptanceCriterionEvidence{{ID: "local-1", Claim: "satisfied", CheckName: "unit", CheckStatus: test.status, CheckSource: "specgate_cli", CheckProvenance: test.provenance, GroundingStatus: test.grounding, Gaps: test.gaps}},
				NextActor:         "human_reviewer", NextCommand: "specgate change accept LOCAL-1",
			}
			printChangeStatus(&Deps{Stdout: &out}, status)
			for _, want := range append(test.want, "[unclear] Works — required testcase is missing", "Decision: Accepted by human", "Freshness: unavailable", "Stale: true", "Next actor: human_reviewer", "Next: specgate change accept LOCAL-1") {
				if !strings.Contains(out.String(), want) {
					t.Fatalf("missing %q:\n%s", want, out.String())
				}
			}
			if strings.Count(out.String(), "Freshness:") != 1 {
				t.Fatalf("global freshness attributed to a criterion:\n%s", out.String())
			}
		})
	}
}

func TestLocalCriterionDetailJoinsByIDWithoutDuplicateTable(t *testing.T) {
	status := changeStatusResult{
		Mode: config.ModeLocal,
		Criteria: []client.CriterionReview{
			{CriterionID: "local-1", Text: "First", Verdict: "met", Why: "first reason"},
			{CriterionID: "local-2", Text: "Second", Verdict: "unmet", Why: "second reason"},
			{CriterionID: "local-3", Text: "Missing", Verdict: "unclear", Why: "no evidence"},
		},
		CriterionEvidence: []local.AcceptanceCriterionEvidence{
			{ID: "local-2", Claim: "partial", CheckName: "second", CheckStatus: "fail", CheckProvenance: "command_only", GroundingStatus: "grounded", EvidencePath: "second_test.go", Citation: "line: 22", Selected: []local.JUnitCaseObservation{{ClassName: "suite", Name: "second", Outcome: "failed"}}},
			{ID: "local-1", Claim: "satisfied", CheckName: "first", CheckStatus: "pass", CheckProvenance: "agent_attested", GroundingStatus: "grounded", EvidencePath: "first_test.go", Citation: "line: 11"},
		},
	}
	for _, detail := range []bool{false, true} {
		var out bytes.Buffer
		printChangeStatus(&Deps{Stdout: &out}, status, detail)
		body := out.String()
		first, second := strings.Index(body, "[met] First"), strings.Index(body, "[unmet] Second")
		if first < 0 || second <= first || !strings.Contains(body[first:second], "first (pass)") || strings.Contains(body[first:second], "second (fail)") {
			t.Fatalf("criterion evidence joined by position:\n%s", body)
		}
		if strings.Count(body, "Criteria:") != 1 || strings.Contains(body, "Criterion evidence:") || !strings.Contains(body, "gap: criterion evidence missing") {
			t.Fatalf("missing facts or duplicate criterion table:\n%s", body)
		}
		for _, value := range []string{"first_test.go", "line: 11", "selected test: suite/second failed"} {
			if strings.Contains(body, value) != detail {
				t.Fatalf("detail=%t, unexpected presence of %q:\n%s", detail, value, body)
			}
		}
	}
}

func TestFullCriterionOutputRemainsVerdictAndWhy(t *testing.T) {
	var out bytes.Buffer
	printChangeStatus(&Deps{Stdout: &out}, changeStatusResult{
		Mode:              config.ModeFull,
		Criteria:          []client.CriterionReview{{CriterionID: "ac-1", Text: "Works", Verdict: "met", Why: "observed"}},
		CriterionEvidence: []local.AcceptanceCriterionEvidence{{ID: "ac-1", CheckProvenance: "command_only"}},
	}, true)
	if !strings.Contains(out.String(), "  [met] Works — observed\nNext actor:") || strings.Contains(out.String(), "provenance:") {
		t.Fatalf("Full output changed:\n%s", out.String())
	}
}

func TestLocalUnreviewedCriterionEvidenceStaysVisible(t *testing.T) {
	for _, detail := range []bool{false, true} {
		var out bytes.Buffer
		printChangeStatus(&Deps{Stdout: &out}, changeStatusResult{
			Mode:              config.ModeLocal,
			CriterionEvidence: []local.AcceptanceCriterionEvidence{{ID: "local-1", Text: "Unreviewed", Claim: "missing", CheckName: "unit", Gaps: []string{"claim: missing"}, EvidencePath: "evidence.go"}},
		}, detail)
		for _, want := range []string{"[not reviewed] Unreviewed", "claim: missing", "unit (missing)"} {
			if !strings.Contains(out.String(), want) {
				t.Fatalf("detail=%t missing %q:\n%s", detail, want, out.String())
			}
		}
		if strings.Contains(out.String(), "evidence.go") != detail {
			t.Fatalf("detail=%t paths visibility changed:\n%s", detail, out.String())
		}
	}
}
