package command

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func localDeliveryReviewView(review local.DeliveryReview) map[string]any {
	return map[string]any{"found": true, "id": review.ID, "work_id": review.WorkID, "report_id": review.ReportID, "verdict": review.Verdict, "summary": review.Summary, "human_decision": review.HumanDecision, "note": review.Note, "created_at": review.CreatedAt}
}

func printLocalDeliveryStatus(cmd *cobra.Command, deps *Deps, ref, commandName string) error {
	return printLocalDeliveryStatusWithUpgrade(cmd, deps, ref, commandName, nil)
}

func printLocalDeliveryStatusWithUpgrade(cmd *cobra.Command, deps *Deps, ref, commandName string, upgrade *local.StoreUpgrade) error {
	store, err := openLocalStore(deps)
	if err != nil {
		return localExitError(deps, commandName, err)
	}
	defer store.Close()
	selection, err := localSelection(cmd.Context(), deps, store)
	if err != nil {
		return localExitError(deps, commandName, err)
	}
	work, err := store.GetWork(cmd.Context(), selection.Workspace.ID, ref)
	if err != nil {
		return localExitError(deps, commandName, err)
	}
	contract, err := store.GetVerificationContract(cmd.Context(), selection.Workspace.ID, ref)
	if err != nil {
		return localExitError(deps, commandName, err)
	}
	review, err := store.DeliveryStatus(cmd.Context(), selection.Workspace.ID, ref)
	if errors.Is(err, sql.ErrNoRows) {
		next := "specgate delivery report " + work.Key + " --init"
		if deps.Printer.Mode() == output.ModeJSON {
			deps.Printer.Success(commandName, map[string]any{"found": false, "work_id": work.ID, "work_key": work.Key, "next": next, "verification_contract": contract.Status})
			return nil
		}
		fmt.Fprintf(deps.Stdout, "Verification contract: %s\nNo delivery review found for %s.\nNext: %s\n", contract.Status, work.Key, next)
		return nil
	}
	if err != nil {
		return localExitError(deps, commandName, err)
	}
	peer, err := store.PeerReviewStatus(cmd.Context(), selection.Workspace.ID, ref)
	if err != nil {
		return localExitError(deps, commandName, err)
	}
	report, err := store.DeliveryReportForReview(cmd.Context(), selection.Workspace.ID, review)
	if err != nil {
		return localExitError(deps, commandName, err)
	}
	status := deriveLocalChangeStatus(work, &review, &report, peer)
	status.VerificationContract = contract.Status
	status.CriterionEvidence, _ = local.ProjectAcceptanceEvidence(work.AcceptanceCriteria, report.Body, contract)
	status, err = augmentAcceptanceStatus(cmd, deps, store, selection.Workspace.ID, ref, status)
	if err != nil {
		return localExitError(deps, commandName, err)
	}
	if view := status.Inspection; view != nil {
		work, review, report, peer, contract = view.Work, view.Review, view.Report, view.Peer, view.Contract
	}
	receiptLabel := localDeliveryReceiptLabel(report.Body)
	if deps.Printer.Mode() == output.ModeJSON {
		data := localDeliveryReviewView(review)
		data["verification_contract"] = contract.Status
		data["peer_review"] = peer
		data["evidence_assessment"] = deliveryEvidenceLabel(review.Verdict, "")
		data["assurance_source"] = status.Assurance
		data["freshness"] = status.Freshness
		if status.HistoricalVerification {
			data["historical_verification"] = true
		}
		data["decision_state"] = localDeliveryDecisionLabel(review.HumanDecision)
		data["receipt"] = receiptLabel
		if status.BasisDigest != "" {
			data["basis_digest"] = status.BasisDigest
		}
		if status.AcceptanceBasis != nil {
			data["acceptance_basis"] = status.AcceptanceBasis
		}
		if status.RecordedBasis != nil {
			data["recorded_acceptance_basis"] = status.RecordedBasis
		}
		data["next_command"] = status.NextCommand
		if upgrade != nil {
			data["store_upgrade"] = upgrade
		}
		deps.Printer.Success(commandName, data)
		return nil
	}
	fmt.Fprintf(deps.Stdout, "Verification contract: %s\n", contract.Status)
	fmt.Fprintf(deps.Stdout, "Evidence: %s\n", deliveryEvidenceLabel(review.Verdict, ""))
	fmt.Fprintf(deps.Stdout, "Assurance: %s\n", status.Assurance)
	fmt.Fprintf(deps.Stdout, "Freshness: %s\n", status.Freshness)
	fmt.Fprintf(deps.Stdout, "Decision: %s\n", localDeliveryDecisionLabel(review.HumanDecision))
	fmt.Fprintf(deps.Stdout, "Receipt: %s\n", receiptLabel)
	fmt.Fprintf(deps.Stdout, "Stored verdict: %s\n", review.Verdict)
	fmt.Fprintf(deps.Stdout, "Peer review: %s\n", peer.State)
	fmt.Fprintln(deps.Stdout, review.Summary)
	if review.HumanDecision == "" && status.BasisDigest != "" {
		printLocalBasisDecisionCommands(deps, work.Key, review.ID, status.AcceptanceBasis)
	} else if review.HumanDecision == "" {
		fmt.Fprintln(deps.Stdout, "\nDecision commands:")
		fmt.Fprintf(deps.Stdout, "  specgate --yes change accept %s --review-id %s\n", work.Key, review.ID)
		fmt.Fprintf(deps.Stdout, "  specgate --yes change request-changes %s --review-id %s --note \"<reason>\"\n", work.Key, review.ID)
	}
	return nil
}

func printLocalBasisDecisionCommands(deps *Deps, ref, reviewID string, basis *local.AcceptanceBasis) {
	if basis == nil {
		return
	}
	flags := " --review-id " + shellQuote(reviewID) + " --basis-digest " + shellQuote(basis.Digest)
	if basis.Selections.CheckpointID != "" {
		flags += " --checkpoint " + shellQuote(basis.Selections.CheckpointID)
	}
	if basis.Selections.ImpactBase != "" {
		flags += " --impact-base " + shellQuote(basis.Selections.ImpactBase) + " --impact-target " + shellQuote(basis.Selections.ImpactTarget)
	}
	fmt.Fprintln(deps.Stdout, "\nDecision commands:")
	fmt.Fprintf(deps.Stdout, "  specgate --yes change accept %s%s\n", ref, flags)
	fmt.Fprintf(deps.Stdout, "  specgate --yes change request-changes %s%s --note \"<reason>\"\n", ref, flags)
}

func localDeliveryDecisionLabel(decision string) string {
	switch strings.TrimSpace(decision) {
	case "approve":
		return "Accepted"
	case "reject":
		return "Rejected"
	default:
		return "Awaiting human acceptance"
	}
}

func localDeliveryAssuranceLabel(body map[string]any, peer local.PeerReviewStatus) string {
	labels := []string{"Agent-reported"}
	checks, _ := body["checks"].([]any)
	reproduced := false
	corrected := 0
	for _, raw := range checks {
		entry, _ := raw.(map[string]any)
		if strings.TrimSpace(fmt.Sprint(entry["source"])) == "specgate_cli" {
			reproduced = true
		}
		if claimed, _ := entry["claimed_status"].(string); strings.TrimSpace(claimed) != "" {
			corrected++
		}
	}
	if reproduced {
		labels = append(labels, "locally reproduced")
	}
	if corrected > 0 {
		labels = append(labels, fmt.Sprintf("corrected %d agent-reported check %s", corrected, pluralize(corrected, "result", "results")))
	}
	switch strings.TrimSpace(peer.State) {
	case "passed":
		labels = append(labels, "second agent affirmed")
	case "failed":
		labels = append(labels, "peer review found gaps")
	}
	return strings.Join(labels, "; ")
}

func localDeliveryReceiptLabel(body map[string]any) string {
	receipt, _ := body["git_receipt"].(map[string]any)
	warnings := stringSlice(receipt["warnings"])
	suffix := receiptWarningSuffix(warnings)
	availability, _ := receipt["availability"].(string)
	if availability = strings.TrimSpace(availability); availability != "" && availability != "available" {
		return "Git receipt unavailable" + suffix
	}
	head, _ := receipt["head_revision"].(string)
	head = strings.TrimSpace(head)
	if head == "" {
		return "No Git receipt recorded" + suffix
	}
	if strings.TrimSpace(fmt.Sprint(receipt["freshness_scope"])) == "local_checkout" {
		return "local checkout commit " + shortGitRevision(head) + suffix
	}
	return "commit " + shortGitRevision(head) + suffix
}

func deliveryEvidenceLabel(verdict, reasonCode string) string {
	switch strings.TrimSpace(reasonCode) {
	case "policy_unavailable":
		return "Policy unavailable"
	case "delivery_review_outdated":
		return "Review pending for latest completion"
	}
	switch strings.TrimSpace(verdict) {
	case "pass", "passed":
		return "Ready for human review"
	case "fail", "failed", "needs_changes":
		return "Evidence gaps found"
	case "needs_human_review":
		return "Human review required"
	default:
		return "Not reviewed"
	}
}

func receiptWarningSuffix(warnings []string) string {
	clean := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		if warning = strings.TrimSpace(warning); warning != "" {
			clean = append(clean, warning)
		}
	}
	switch len(clean) {
	case 0:
		return ""
	case 1:
		return "; warning: " + clean[0]
	default:
		return "; warnings: " + strings.Join(clean, " | ")
	}
}

func stringSlice(value any) []string {
	switch items := value.(type) {
	case []string:
		return items
	case []any:
		out := make([]string, 0, len(items))
		for _, item := range items {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func shortGitRevision(revision string) string {
	revision = strings.TrimSpace(revision)
	if len(revision) <= 12 {
		return revision
	}
	return revision[:12]
}

func pluralize(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}
