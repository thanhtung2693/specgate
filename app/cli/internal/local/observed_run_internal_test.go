package local

import (
	"strings"
	"testing"
)

func TestBindObservedChecksPreservesOnlyCLISelectedTestDiagnostic(t *testing.T) {
	t.Parallel()
	contract := VerificationContract{Checks: []VerificationCheck{{
		Name: "unit",
		TestReport: &JUnitTestReport{Format: "junit", Selectors: map[string][]SelectedTestCase{
			"local-1": {{ClassName: "csv", Name: "required"}},
		}},
	}}}
	for _, tc := range []struct {
		name, source, detail string
		want                 string
	}{
		{
			name:   "CLI-observed parser reason survives binding",
			source: "specgate_cli",
			detail: "executed by specgate: exit 0 — selected-test report: invalid verification contract or report: selected testcase is missing",
			want:   "selected testcase is missing",
		},
		{
			name:   "agent-supplied detail is replaced",
			detail: "executed by specgate: exit 0 — selected-test report: invalid verification contract or report: selected testcase is missing",
			want:   "selected-test observation missing, insufficient, or bound to another run",
		},
		{
			name:   "insufficient CLI receipt still fails with generic reason",
			source: "specgate_cli",
			detail: "executed by specgate: exit 0",
			want:   "selected-test observation missing, insufficient, or bound to another run",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := map[string]any{"checks": []any{map[string]any{
				"name": "unit", "status": "fail", "source": tc.source, "detail": tc.detail,
			}}}
			bindObservedChecks(body, contract, "completion")
			got := body["checks"].([]any)[0].(map[string]any)["detail"].(string)
			if !strings.Contains(got, tc.want) {
				t.Fatalf("detail = %q, want it to contain %q", got, tc.want)
			}
		})
	}
}
