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
			verdict: "met",
			reason:  "reported by the coding agent, not re-run",
		},
		{
			name:    "re-executed pass names the CLI",
			check:   client.CheckResult{Name: "unit", Status: "pass", Source: "specgate_cli"},
			verdict: "met",
			reason:  "observed by the SpecGate CLI",
		},
		{
			name:    "re-executed failure names the CLI",
			check:   client.CheckResult{Name: "unit", Status: "fail", Source: "specgate_cli"},
			verdict: "unmet",
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

func TestLocalReportEvidenceUsesCanonicalCriterionVerdicts(t *testing.T) {
	t.Parallel()
	reviews, _ := localReportEvidence(
		[]string{
			"Claim satisfied",
			"Claim partial",
			"Claim not done",
			"Bound pass @check:passing",
			"Bound fail @check:failing",
			"Bound skipped @check:skipped",
			"Bound missing @check:missing",
		},
		local.DeliveryReport{Body: map[string]any{
			"checks": []any{
				map[string]any{"name": "passing", "status": "pass"},
				map[string]any{"name": "failing", "status": "fail"},
				map[string]any{"name": "skipped", "status": "skipped"},
			},
			"criteria": []any{
				map[string]any{"criterion_id": "local-1", "claim": "satisfied"},
				map[string]any{"criterion_id": "local-2", "claim": "partial"},
				map[string]any{"criterion_id": "local-3", "claim": "not_done"},
				map[string]any{"criterion_id": "local-4", "claim": "satisfied"},
				map[string]any{"criterion_id": "local-5", "claim": "satisfied"},
				map[string]any{"criterion_id": "local-6", "claim": "satisfied"},
				map[string]any{"criterion_id": "local-7", "claim": "satisfied"},
			},
		}},
	)

	want := []string{"met", "unmet", "unmet", "met", "unmet", "unclear", "unclear"}
	if len(reviews) != len(want) {
		t.Fatalf("reviews = %+v, want %d", reviews, len(want))
	}
	for index, verdict := range want {
		if reviews[index].Verdict != verdict {
			t.Fatalf("criterion %d verdict = %q, want %q", index+1, reviews[index].Verdict, verdict)
		}
	}
}

// A bound criterion takes its verdict from the named check, so a bad citation
// must not flip it — a wrong pointer to the proof is not a broken feature. It
// must still reach the decision line, because overwriting `why` with the check
// outcome alone hid a fabricated citation behind a clean pass.
func TestLocalReportEvidenceShowsCitationWeaknessWithoutFlippingTheVerdict(t *testing.T) {
	t.Parallel()
	report := func(evidence map[string]any) local.DeliveryReport {
		return local.DeliveryReport{Body: map[string]any{
			"checks": []any{map[string]any{"name": "unit", "status": "pass"}},
			"criteria": []any{map[string]any{
				"criterion_id": "local-1", "claim": "satisfied", "evidence": evidence,
			}},
		}}
	}

	for _, tc := range []struct {
		name     string
		evidence map[string]any
		note     string
	}{
		{
			name: "a fabricated heading is named",
			evidence: map[string]any{
				"path": "test_notes.py", "heading": "test_invented",
				"grounding": map[string]any{"status": "heading_not_found"},
			},
			note: `cited heading "test_invented" is not in test_notes.py`,
		},
		{
			name: "a path-only citation is named",
			evidence: map[string]any{
				"path": "notes.py", "grounding": map[string]any{"status": "unanchored"},
			},
			note: "citation names notes.py with no line or heading",
		},
		{
			name: "an anchored citation adds nothing",
			evidence: map[string]any{
				"path": "test_notes.py", "heading": "test_real",
				"grounding": map[string]any{"status": "grounded", "excerpt": "def test_real():"},
			},
			note: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reviews, _ := localReportEvidence([]string{"Tags persist @check:unit"}, report(tc.evidence))
			if len(reviews) != 1 {
				t.Fatalf("reviews = %+v, want one", reviews)
			}
			if reviews[0].Verdict != "met" {
				t.Fatalf("verdict = %q, want met — the bound check decides", reviews[0].Verdict)
			}
			if tc.note == "" {
				if strings.Contains(reviews[0].Why, ";") {
					t.Fatalf("why = %q, want no citation note for anchored evidence", reviews[0].Why)
				}
				return
			}
			if !strings.Contains(reviews[0].Why, tc.note) {
				t.Fatalf("why = %q, want it to contain %q", reviews[0].Why, tc.note)
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
