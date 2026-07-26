package command

import (
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/local"
)

// `criteria[].why` is the line a human reads before accepting delivery. Because
// `--run-checks` is opt-in, the common case is a status the coding agent claimed
// and nothing re-ran. Calling that "deterministic" presented an unexecuted
// command as a platform result, so the reason must name who observed it.
func TestBoundCriterionOutcomeNamesWhoObservedTheCheck(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		check   client.CheckResult
		verdict string
		reason  string
	}{
		{
			name:    "agent-claimed pass says nothing re-ran it",
			check:   client.CheckResult{Name: "unit", Status: "pass"},
			verdict: "pass",
			reason:  "reported by the coding agent, not re-run",
		},
		{
			name:    "re-executed pass names the CLI",
			check:   client.CheckResult{Name: "unit", Status: "pass", Source: "specgate_cli"},
			verdict: "pass",
			reason:  "observed by the SpecGate CLI",
		},
		{
			name:    "re-executed failure names the CLI",
			check:   client.CheckResult{Name: "unit", Status: "fail", Source: "specgate_cli"},
			verdict: "fail",
			reason:  "observed by the SpecGate CLI",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			verdict, why := boundCriterionOutcome("unit", []client.CheckResult{tc.check})
			if verdict != tc.verdict {
				t.Fatalf("verdict = %q, want %q", verdict, tc.verdict)
			}
			if !strings.Contains(why, tc.reason) {
				t.Fatalf("why = %q, want it to contain %q", why, tc.reason)
			}
			if strings.Contains(why, "(deterministic)") {
				t.Fatalf("why = %q still claims determinism without naming the observer", why)
			}
		})
	}
}

func TestLocalReportEvidenceCarriesCheckProvenance(t *testing.T) {
	t.Parallel()
	reviews, checks := localReportEvidence(
		[]string{"Tags persist @check:unit"},
		local.DeliveryReport{Body: map[string]any{
			"checks": []any{map[string]any{
				"name": "unit", "status": "pass",
				"detail": "executed by specgate: exit 0", "source": "specgate_cli",
			}},
			"criteria": []any{map[string]any{"criterion_id": "local-1", "claim": "satisfied"}},
		}},
	)

	if len(checks) != 1 || checks[0].Source != "specgate_cli" {
		t.Fatalf("checks = %+v, want the CLI source preserved", checks)
	}
	if len(reviews) != 1 || !strings.Contains(reviews[0].Why, "observed by the SpecGate CLI") {
		t.Fatalf("reviews = %+v, want the observed origin in why", reviews)
	}
}
