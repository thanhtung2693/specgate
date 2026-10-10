package local_test

import (
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/local"
)

func TestAcceptanceEvidenceSeparatesObservedTestsFromClaimAndGroundedCitation(t *testing.T) {
	body := map[string]any{
		"criteria": []any{map[string]any{"criterion_id": "local-1", "claim": "satisfied", "evidence": map[string]any{
			"kind": "file", "path": "handler.go", "line": float64(3), "grounding": map[string]any{"status": "grounded"},
		}}, map[string]any{"criterion_id": "local-2", "claim": "satisfied"}},
		"checks": []any{map[string]any{"name": "unit", "status": "pass", "source": "specgate_cli", "test_observation": map[string]any{
			"report_digest": "sha256:report", "selected": []any{map[string]any{"classname": "pkg", "name": "works", "outcome": "passed"}, map[string]any{"classname": "pkg", "name": "old", "outcome": "passed"}},
		}}},
	}
	contract := local.VerificationContract{Checks: []local.VerificationCheck{{Name: "unit", TestReport: &local.JUnitTestReport{Selectors: map[string][]local.SelectedTestCase{
		"local-1": {{ClassName: "pkg", Name: "works"}}, "local-2": {{ClassName: "pkg", Name: "old"}},
	}}}}}
	rows, gaps := local.ProjectAcceptanceEvidence([]string{"Works @check:unit", "Old behavior @check:unit"}, body, contract)
	if len(rows) != 2 || rows[0].CheckSource != "specgate_cli" || rows[0].EvidenceKind != "file" || rows[0].Citation != "line: 3" || len(rows[0].Selected) != 1 || rows[0].Selected[0].Name != "works" || len(rows[1].Selected) != 1 || rows[1].Selected[0].Name != "old" || rows[0].ReportDigest != "sha256:report" || len(gaps) != 0 {
		t.Fatalf("wrong evidence projection: rows=%#v gaps=%#v", rows, gaps)
	}
}

func TestAcceptanceEvidenceKeepsMissingAndFailedCategories(t *testing.T) {
	rows, gaps := local.ProjectAcceptanceEvidence([]string{"Preserve old behavior @check:regression", "Manual review"}, map[string]any{
		"checks": []any{map[string]any{"name": "regression", "status": "fail"}},
	})
	if len(rows) != 2 || rows[0].CheckSource != "agent_attested" || rows[0].CheckStatus != "fail" || rows[1].Claim != "missing" || len(gaps) < 2 || !strings.Contains(strings.Join(gaps, " "), "bound check regression: fail") {
		t.Fatalf("hidden acceptance gaps: rows=%#v gaps=%#v", rows, gaps)
	}
}

func TestAcceptanceEvidenceFlagsSatisfiedClaimWithoutEvidence(t *testing.T) {
	rows, gaps := local.ProjectAcceptanceEvidence([]string{"The manual behavior works"}, map[string]any{
		"criteria": []any{map[string]any{"criterion_id": "local-1", "claim": "satisfied"}},
	})
	if len(rows) != 1 || rows[0].Claim != "satisfied" || len(gaps) != 1 || !strings.Contains(gaps[0], "evidence: missing") {
		t.Fatalf("evidence-free claim was presented as complete: rows=%#v gaps=%#v", rows, gaps)
	}
}
