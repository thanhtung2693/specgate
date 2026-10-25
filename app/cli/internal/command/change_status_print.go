package command

import (
	"fmt"
	"strings"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/local"
)

func printChangeStatus(deps *Deps, result changeStatusResult, detailed ...bool) {
	if result.VerificationContract != "" {
		fmt.Fprintf(deps.Stdout, "Verification contract: %s\n", result.VerificationContract)
	}
	fmt.Fprintf(deps.Stdout, "Change: %s — %s\n", result.Ref, result.Title)
	fmt.Fprintf(deps.Stdout, "State: %s\n", result.State)
	fmt.Fprintf(deps.Stdout, "Evidence: %s\n", result.Evidence)
	fmt.Fprintf(deps.Stdout, "Assurance: %s\n", result.Assurance)
	fmt.Fprintf(deps.Stdout, "Decision: %s\n", result.Decision)
	fmt.Fprintf(deps.Stdout, "Receipt: %s\n", result.Receipt)
	fmt.Fprintf(deps.Stdout, "Freshness: %s\n", result.Freshness)
	if result.Mode == config.ModeLocal {
		if len(result.Risks) == 0 {
			result.Risks = acceptanceRisks(result)
		}
		if result.VerificationDigest != "" {
			fmt.Fprintf(deps.Stdout, "Pin: v%d %s\n", result.VerificationVersion, result.VerificationDigest)
		}
		if result.ArtifactID == "" {
			fmt.Fprintln(deps.Stdout, "Scope: quick work (no artifact)")
		} else {
			fmt.Fprintf(deps.Stdout, "Scope: %s v%d (%s)\n", result.ArtifactID, result.ArtifactVersion, result.ArtifactDigest)
			fmt.Fprintf(deps.Stdout, "Source coverage: %s\n", result.SourceCoverage)
		}
		fmt.Fprintf(deps.Stdout, "Context Pack: %s\n", result.ContextDigest)
		if result.CompletionID != "" {
			fmt.Fprintf(deps.Stdout, "Completion: %s; review: %s; peer: %s\n", result.CompletionID, result.ReviewID, result.PeerState)
		}
		if result.SelectedCheckpoint != nil {
			fmt.Fprintf(deps.Stdout, "Selected checkpoint: %s (%s)\n", result.SelectedCheckpoint.ID, result.SelectedCheckpoint.CreatedAt)
			if result.SelectedCheckpointDelta != nil {
				delta := result.SelectedCheckpointDelta
				fmt.Fprintf(deps.Stdout, "Checkpoint comparison: %s; added=%d removed=%d modified=%d\n", delta.State, delta.AddedCount, delta.RemovedCount, delta.ModifiedCount)
				if delta.Limit != "" {
					fmt.Fprintf(deps.Stdout, "Checkpoint comparison limit: %s\n", delta.Limit)
				}
				if len(detailed) > 0 && detailed[0] {
					for _, paths := range []struct {
						label string
						items []string
					}{{"added", delta.Added}, {"removed", delta.Removed}, {"modified", delta.Modified}} {
						for _, path := range paths.items {
							fmt.Fprintf(deps.Stdout, "  checkpoint %s: %s\n", paths.label, path)
						}
					}
				}
			}
		}
		if result.AcceptanceBasis != nil {
			fmt.Fprintf(deps.Stdout, "Acceptance basis: %s\n", result.AcceptanceBasis.Digest)
		}
		fmt.Fprintln(deps.Stdout, "Risks (recorded issues, not a score):")
		for _, risk := range result.Risks {
			fmt.Fprintf(deps.Stdout, "  %s: %d", risk.Category, risk.Count)
			if len(risk.Criteria) > 0 {
				fmt.Fprintf(deps.Stdout, " (%s)", strings.Join(risk.Criteria, ", "))
			}
			fmt.Fprintln(deps.Stdout)
		}
		if len(detailed) > 0 && detailed[0] {
			for _, risk := range result.Risks {
				for _, detail := range risk.Details {
					fmt.Fprintf(deps.Stdout, "  %s risk: %s\n", risk.Category, detail)
				}
			}
			if result.RecordedBasis != nil {
				fmt.Fprintf(deps.Stdout, "Recorded decision: %s by %s at %s; note: %s\n", result.RecordedBasis.Decision, result.RecordedBasis.Actor, result.RecordedBasis.ObservedAt, result.RecordedBasis.Note)
			}
			for _, requirement := range result.SourceRequirements {
				if requirement.State == "deferred" || requirement.State == "unassigned" {
					fmt.Fprintf(deps.Stdout, "  source %s: %s (%s)\n", requirement.ID, requirement.State, requirement.DeferredReason)
				}
			}
			if result.SelectedImpact != nil {
				printArtifactImpact(deps, *result.SelectedImpact)
			}
		}
	}
	// Same shape as `verify`: the criterion text and the reason, so a human can
	// see which one is weak without opening the completion file.
	if result.Mode == config.ModeLocal && len(result.Criteria) == 0 {
		for _, row := range result.CriterionEvidence {
			result.Criteria = append(result.Criteria, client.CriterionReview{CriterionID: row.ID, Text: row.Text, Verdict: "not reviewed"})
		}
	}
	if len(result.Criteria) > 0 {
		evidence := make(map[string]local.AcceptanceCriterionEvidence, len(result.CriterionEvidence))
		for _, row := range result.CriterionEvidence {
			evidence[row.ID] = row
		}
		fmt.Fprintln(deps.Stdout, "Criteria:")
		for _, criterion := range result.Criteria {
			fmt.Fprintf(deps.Stdout, "  [%s] %s — %s\n", criterion.Verdict, criterion.Text, criterion.Why)
			if result.Mode != config.ModeLocal {
				continue
			}
			row, ok := evidence[criterion.CriterionID]
			if !ok {
				fmt.Fprintln(deps.Stdout, "    gap: criterion evidence missing")
				continue
			}
			provenance := row.CheckProvenance
			switch provenance {
			case "agent_attested":
				provenance += " (agent-reported)"
			case "command_only":
				provenance += " (CLI observed command; no selected testcase observation)"
			case "selected_test_observed":
				provenance += " (CLI observed selected testcases)"
			case "":
				provenance = "unknown"
			}
			status, grounding := row.CheckStatus, row.GroundingStatus
			if status == "" {
				status = "missing"
			}
			if grounding == "" {
				grounding = "unknown"
			}
			if row.CheckName == "" {
				fmt.Fprintf(deps.Stdout, "    Check: none bound; provenance: %s\n", provenance)
			} else {
				fmt.Fprintf(deps.Stdout, "    Check: %s (%s); provenance: %s\n", row.CheckName, status, provenance)
			}
			fmt.Fprintf(deps.Stdout, "    Claim: %s; citation grounding: %s\n", row.Claim, grounding)
			for _, gap := range row.Gaps {
				fmt.Fprintf(deps.Stdout, "    gap: %s\n", gap)
			}
			if len(detailed) > 0 && detailed[0] {
				fmt.Fprintf(deps.Stdout, "    Evidence: %s %s %s\n", row.EvidenceKind, row.EvidencePath, row.Citation)
				for _, selected := range row.Selected {
					fmt.Fprintf(deps.Stdout, "    selected test: %s/%s %s\n", selected.ClassName, selected.Name, selected.Outcome)
				}
			}
		}
	}
	fmt.Fprintf(deps.Stdout, "Next actor: %s\n", result.NextActor)
	missing := strings.Join(result.Missing, ", ")
	if missing == "" {
		missing = "none"
	}
	fmt.Fprintf(deps.Stdout, "Missing: %s\n", missing)
	if result.Guidance != "" {
		fmt.Fprintf(deps.Stdout, "Requested changes: %s\n", result.Guidance)
	}
	fmt.Fprintf(deps.Stdout, "Stale: %t\n", result.Stale)
	if result.StaleReason != "" {
		fmt.Fprintf(deps.Stdout, "Stale reason: %s\n", result.StaleReason)
	}
	fmt.Fprintf(deps.Stdout, "Next: %s\n", result.NextCommand)
}
