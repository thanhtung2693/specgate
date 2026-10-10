package command

import (
	"context"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/local"
)

func TestAcceptanceRisksKeepFailedMissingStaleAndUnknownDistinct(t *testing.T) {
	status := changeStatusResult{
		Stale: true, PeerState: "failed", Receipt: "No Git receipt recorded",
		CriterionEvidence: []local.AcceptanceCriterionEvidence{
			{ID: "local-1", Claim: "satisfied", CheckName: "unit", CheckStatus: "fail", CheckSource: "specgate_cli", Gaps: []string{"bound check unit: fail"}},
			{ID: "local-2", Claim: "missing", CheckName: "regression", Gaps: []string{"claim: missing", "bound check regression: "}},
		},
		AcceptanceBasis: &local.AcceptanceBasis{Watched: []local.WatchedPathDrift{{Path: "go.mod", State: "changed"}}},
	}
	risks := acceptanceRisks(status)
	for _, category := range []string{"failed", "missing", "stale", "unknown"} {
		found := false
		for _, risk := range risks {
			if risk.Category == category && risk.Count > 0 {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s risk hidden: %#v", category, risks)
		}
	}
}

func TestAcceptanceRisksExposeUncheckedLegacyFreshness(t *testing.T) {
	for _, basis := range []*local.AcceptanceBasis{nil, {}} {
		status := changeStatusResult{
			CompletionID: "completion-1", Receipt: "Recorded Git receipt", PeerState: "passed",
			CriterionEvidence: []local.AcceptanceCriterionEvidence{{ID: "local-1", Claim: "satisfied", CheckProvenance: "manual"}},
			AcceptanceBasis:   basis,
		}
		// Observe a real non-Git directory, not a manually authored message.
		status = applyCheckoutFreshness(context.Background(), &Deps{WorkingDir: t.TempDir()}, status, gitReceipt{Availability: "available", HeadRevision: "head-1"})
		for _, risk := range acceptanceRisks(status) {
			if risk.Category == "unknown" && risk.Count != 1 {
				t.Fatalf("basis %#v, unchecked observation %q: %#v", basis, status.Freshness, risk)
			}
		}
	}
}

func TestAcceptanceRisksExposeUnavailableSelectedSnapshots(t *testing.T) {
	status := changeStatusResult{
		SelectedCheckpointDelta: &checkpointDelta{State: "unavailable"},
		SelectedImpact:          &local.ArtifactImpact{RequirementImpact: "unavailable", RequirementReason: "base requirement inventory is unknown", SourceOverlapState: "unknown", PathOverlapState: "noncomparable"},
	}
	for _, risk := range acceptanceRisks(status) {
		if risk.Category == "unknown" && risk.Count == 3 {
			return
		}
	}
	t.Fatalf("unavailable comparison risks were hidden: %#v", acceptanceRisks(status))
}

func TestAcceptanceRisksExposeNormalizedCurrentFreshness(t *testing.T) {
	for _, test := range []struct {
		state       string
		wantUnknown int
	}{
		{"unavailable", 1},
		{"noncomparable", 1},
		{"matching_endpoints", 0},
	} {
		t.Run(test.state, func(t *testing.T) {
			status := changeStatusResult{
				CompletionID: "completion-1", Receipt: "Recorded Git receipt", PeerState: "passed",
				Freshness:         "Could not compare the stored receipt with the current checkout.",
				CriterionEvidence: []local.AcceptanceCriterionEvidence{{ID: "local-1", Claim: "satisfied", CheckStatus: "pass", CheckProvenance: "selected_test_observed"}},
				AcceptanceBasis:   &local.AcceptanceBasis{Freshness: test.state, Watched: []local.WatchedPathDrift{{Path: "go.mod", State: "unchanged"}}},
			}
			for _, risk := range acceptanceRisks(status) {
				want := 0
				if risk.Category == "unknown" {
					want = test.wantUnknown
				}
				if risk.Count != want {
					t.Fatalf("%s risk = %#v, want count %d", test.state, risk, want)
				}
			}
			if status.CriterionEvidence[0].CheckStatus != "pass" {
				t.Fatal("historical test outcome was relabeled")
			}
		})
	}
}
