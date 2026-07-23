package command

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func localDeliveryReviewView(review local.DeliveryReview) map[string]any {
	return map[string]any{"found": true, "id": review.ID, "work_id": review.WorkID, "report_id": review.ReportID, "verdict": review.Verdict, "summary": review.Summary, "human_decision": review.HumanDecision, "note": review.Note, "created_at": review.CreatedAt}
}

func printLocalDeliveryStatus(cmd *cobra.Command, deps *Deps, ref, commandName string) error {
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
	review, err := store.DeliveryStatus(cmd.Context(), selection.Workspace.ID, ref)
	if errors.Is(err, sql.ErrNoRows) {
		next := "specgate delivery report " + work.Key + " --init"
		if deps.Printer.Mode() == output.ModeJSON {
			deps.Printer.Success(commandName, map[string]any{"found": false, "work_id": work.ID, "work_key": work.Key, "next": next})
			return nil
		}
		fmt.Fprintf(deps.Stdout, "No delivery review found for %s.\nNext: %s\n", work.Key, next)
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
	receiptLabel := localDeliveryReceiptLabel(report.Body)
	if deps.Printer.Mode() == output.ModeJSON {
		data := localDeliveryReviewView(review)
		data["peer_review"] = peer
		data["evidence_assessment"] = deliveryEvidenceLabel(review.Verdict, "")
		data["assurance_source"] = localDeliveryAssuranceLabel(report.Body, peer)
		data["decision_state"] = localDeliveryDecisionLabel(review.HumanDecision)
		data["receipt"] = receiptLabel
		deps.Printer.Success(commandName, data)
		return nil
	}
	fmt.Fprintf(deps.Stdout, "Evidence: %s\n", deliveryEvidenceLabel(review.Verdict, ""))
	fmt.Fprintf(deps.Stdout, "Assurance: %s\n", localDeliveryAssuranceLabel(report.Body, peer))
	fmt.Fprintf(deps.Stdout, "Decision: %s\n", localDeliveryDecisionLabel(review.HumanDecision))
	fmt.Fprintf(deps.Stdout, "Receipt: %s\n", receiptLabel)
	fmt.Fprintf(deps.Stdout, "Stored verdict: %s\n", review.Verdict)
	fmt.Fprintf(deps.Stdout, "Peer review: %s\n", peer.State)
	fmt.Fprintln(deps.Stdout, review.Summary)
	if review.HumanDecision == "" {
		printDeliveryDecisionCommands(deps, work.Key, &client.DeliveryStatusResult{
			Found: true, Verdict: review.Verdict, Executor: "platform",
		}, true)
	}
	return nil
}

func printDeliveryDecisionCommands(deps *Deps, ref string, ds *client.DeliveryStatusResult, localMode bool) {
	if !ds.Found || strings.TrimSpace(ds.Executor) == "human" ||
		strings.TrimSpace(ds.ReasonCode) == "policy_unavailable" ||
		strings.TrimSpace(ds.ReasonCode) == "delivery_review_outdated" {
		return
	}
	prefix := "specgate "
	if localMode {
		prefix = "specgate --yes "
	}
	fmt.Fprintln(deps.Stdout, "\nDecision commands:")
	fmt.Fprintf(deps.Stdout, "  %schange accept %s\n", prefix, ref)
	fmt.Fprintf(deps.Stdout, "  %schange request-changes %s --note \"<reason>\"\n", prefix, ref)
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
	for _, raw := range checks {
		entry, _ := raw.(map[string]any)
		if strings.TrimSpace(fmt.Sprint(entry["source"])) == "specgate_cli" {
			labels = append(labels, "locally reproduced")
			break
		}
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
	return "commit " + shortGitRevision(head) + suffix
}

// printDeliveryStatus renders a delivery status result for human/plain modes.
// Shared by `delivery status` and `delivery submit`.
func printDeliveryStatus(deps *Deps, ds *client.DeliveryStatusResult, detail bool) {
	if !ds.Found {
		fmt.Fprintln(deps.Stdout, "No delivery review found for this work item.")
		return
	}

	if humanVisuals(deps) {
		printDeliveryStatusDashboard(deps, ds, detail)
		return
	}
	printDeliveryTrustSummary(deps, ds, "")
	fmt.Fprintf(deps.Stdout, "%s %s\n", styled(deps, output.StyleMuted, "Stored verdict:"), styledStatus(deps, deliveryStoredVerdictLabel(ds)))
	if warn := attestedWarning(ds); warn != "" {
		fmt.Fprintf(deps.Stdout, "%s\n", warn)
	}
	fmt.Fprintf(deps.Stdout, "%s\n", styled(deps, output.StyleMuted, selfSelectedChecksNote))
	if ds.Hint != "" {
		fmt.Fprintf(deps.Stdout, "%s     %s\n", styled(deps, output.StyleMuted, "Hint:"), ds.Hint)
	}
	if ds.Summary != "" {
		fmt.Fprintf(deps.Stdout, "%s %s\n", styled(deps, output.StyleMuted, "Review summary:"), ds.Summary)
	}
	if ds.ReviewedAt != "" {
		fmt.Fprintf(deps.Stdout, "%s %s\n", styled(deps, output.StyleMuted, "Reviewed:"), ds.ReviewedAt)
	}
	if judge := deliveryJudgeLabel(ds); judge != "" {
		fmt.Fprintf(deps.Stdout, "%s    %s\n", styled(deps, output.StyleMuted, "Judge:"), judge)
	}
	if detail {
		fmt.Fprintf(deps.Stdout, "%s %s\n", styled(deps, output.StyleMuted, "Peer review:"), ds.PeerReview.State)
	}
	if ds.OutstandingMD != "" {
		fmt.Fprintf(deps.Stdout, "\n%s\n", ds.OutstandingMD)
	}
	if detail && len(ds.PerCriterion) > 0 {
		fmt.Fprintln(deps.Stdout, "\nPer criterion:")
		for _, c := range ds.PerCriterion {
			label := c.CriterionID
			if label == "" {
				label = c.Text
			}
			fmt.Fprintf(deps.Stdout, "  %-20s  %s\n", label, styledStatus(deps, c.Verdict))
			if c.Why != "" {
				fmt.Fprintf(deps.Stdout, "    %s\n", styled(deps, output.StyleMuted, c.Why))
			}
		}
	}
}

func printDeliveryStatusDashboard(deps *Deps, ds *client.DeliveryStatusResult, detail bool) {
	fmt.Fprintln(deps.Stdout, title(deps, "Delivery Review"))
	fmt.Fprintln(deps.Stdout, visualRule(deps))
	printDeliveryTrustSummary(deps, ds, "  ")
	fmt.Fprintf(deps.Stdout, "%s %s %s\n",
		statusIcon(deps, ds.Verdict),
		styled(deps, output.StyleMuted, "Stored verdict:"),
		styledStatus(deps, deliveryStoredVerdictLabel(ds)))
	if warn := attestedWarning(ds); warn != "" {
		fmt.Fprintf(deps.Stdout, "  %s\n", styled(deps, output.StyleWarning, warn))
	}
	fmt.Fprintf(deps.Stdout, "  %s\n", styled(deps, output.StyleMuted, selfSelectedChecksNote))
	if ds.Hint != "" {
		fmt.Fprintf(deps.Stdout, "  %s %s\n", styled(deps, output.StyleMuted, "Hint:"), ds.Hint)
	}
	if ds.Summary != "" {
		fmt.Fprintf(deps.Stdout, "  %s %s\n", styled(deps, output.StyleMuted, "Review summary:"), ds.Summary)
	}
	if ds.ReviewedAt != "" {
		fmt.Fprintf(deps.Stdout, "  %s %s\n", styled(deps, output.StyleMuted, "Reviewed:"), ds.ReviewedAt)
	}
	if judge := deliveryJudgeLabel(ds); judge != "" {
		fmt.Fprintf(deps.Stdout, "  %s %s\n", styled(deps, output.StyleMuted, "Judge:"), judge)
	}
	if detail {
		fmt.Fprintf(deps.Stdout, "  %s %s\n", styled(deps, output.StyleMuted, "Peer review:"), ds.PeerReview.State)
	}
	if ds.OutstandingMD != "" {
		fmt.Fprintf(deps.Stdout, "\n%s\n", ds.OutstandingMD)
	}
	if detail && len(ds.PerCriterion) > 0 {
		printCriterionDashboard(deps, ds.PerCriterion)
	}
}

// selfSelectedChecksNote is a runtime belt-and-suspenders reminder: delivery
// review judges only the checks the coding agent chose to report, so a
// regression outside that self-selected scope can still pass. Surfacing this in
// the status/review output keeps a human reviewer from over-trusting a narrow
// check set.
const selfSelectedChecksNote = "Checks are self-selected by the coding agent; delivery review judges only the reported checks."

func deliveryStoredVerdictLabel(ds *client.DeliveryStatusResult) string {
	switch strings.TrimSpace(ds.Verdict) {
	case "pass", "passed":
		if strings.TrimSpace(ds.Executor) == "human" {
			return "Accepted"
		}
		return "Ready for human review"
	case "fail", "failed":
		return "Evidence gaps found"
	case "needs_human_review":
		switch strings.TrimSpace(ds.ReasonCode) {
		case "policy_unavailable":
			return "Policy unavailable"
		case "delivery_review_outdated":
			return "Evidence stale"
		default:
			return "Independent confirmation required"
		}
	default:
		return ds.Verdict
	}
}

func printDeliveryTrustSummary(deps *Deps, ds *client.DeliveryStatusResult, indent string) {
	fmt.Fprintf(deps.Stdout, "%s%s %s\n", indent, styled(deps, output.StyleMuted, "Evidence:"), deliveryEvidenceLabel(deliveryEvidenceVerdict(ds), ds.ReasonCode))
	fmt.Fprintf(deps.Stdout, "%s%s %s\n", indent, styled(deps, output.StyleMuted, "Assurance:"), deliveryAssuranceLabel(ds))
	fmt.Fprintf(deps.Stdout, "%s%s %s\n", indent, styled(deps, output.StyleMuted, "Decision:"), deliveryDecisionLabel(ds))
	fmt.Fprintf(deps.Stdout, "%s%s %s\n", indent, styled(deps, output.StyleMuted, "Receipt:"), deliveryReceiptLabel(ds.GitReceipt))
}

func deliveryEvidenceVerdict(ds *client.DeliveryStatusResult) string {
	if verdict := strings.TrimSpace(ds.EvidenceVerdict); verdict != "" {
		return verdict
	}
	if strings.TrimSpace(ds.Executor) == "human" {
		return ""
	}
	return ds.Verdict
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

func deliveryAssuranceLabel(ds *client.DeliveryStatusResult) string {
	labels := []string{"Agent-reported"}
	seen := map[string]bool{"Agent-reported": true}
	appendSource := func(source string) {
		label := deliveryAssuranceSourceLabel(source)
		if label != "" && !seen[label] {
			labels = append(labels, label)
			seen[label] = true
		}
	}
	for _, source := range ds.AssuranceSources {
		appendSource(source)
	}
	for _, criterion := range ds.PerCriterion {
		appendSource(criterion.TrustTier)
	}
	return strings.Join(labels, "; ")
}

func deliveryAssuranceSourceLabel(source string) string {
	switch strings.TrimSpace(source) {
	case "grounded":
		return "local citation captured"
	case "deterministic":
		return "locally reproduced"
	case "peer_reviewed":
		return "second agent affirmed"
	case "repository_observed":
		return "Submitted commit observed on merged PR/MR"
	default:
		return ""
	}
}

func deliveryDecisionLabel(ds *client.DeliveryStatusResult) string {
	if strings.TrimSpace(ds.Executor) != "human" {
		return "Awaiting human acceptance"
	}
	if passingStatus(ds.Verdict) {
		return "Accepted"
	}
	return "Rejected"
}

func deliveryReceiptLabel(receipt *client.GitReceipt) string {
	if receipt == nil {
		return "No Git receipt recorded"
	}
	suffix := receiptWarningSuffix(receipt.Warnings)
	if availability := strings.TrimSpace(receipt.Availability); availability != "" && availability != "available" {
		return "Git receipt unavailable" + suffix
	}
	if strings.TrimSpace(receipt.HeadRevision) == "" {
		return "No Git receipt recorded" + suffix
	}
	return "commit " + shortGitRevision(receipt.HeadRevision) + suffix
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

// attestedWarning flags verdicts the platform derived from the coding agent's
// own claims (no governance model configured) — the delivery was not
// independently reviewed, and a human should read the verdict accordingly.
func attestedWarning(ds *client.DeliveryStatusResult) string {
	if strings.TrimSpace(ds.JudgeModel) != "agent_attested" {
		return ""
	}
	return "Agent-reported evidence: the implementing agent supplied these claims. " +
		"A platform model can add another review of the submitted evidence; it does not verify the code or replace CI and human review."
}

func deliveryJudgeLabel(ds *client.DeliveryStatusResult) string {
	if strings.TrimSpace(ds.JudgeModel) == "" {
		return ""
	}
	label := strings.TrimSpace(ds.JudgeModel)
	switch label {
	case "agent_attested":
		label = "Agent-reported"
	case "deterministic_checks":
		label = "Locally reproduced checks"
	case "deterministic_policy_guard":
		label = "Policy guard"
	}
	if strings.TrimSpace(ds.EvalSuite) != "" {
		label += " / " + strings.TrimSpace(ds.EvalSuite)
	}
	return label
}

func printCriterionDashboard(deps *Deps, criteria []client.CriterionReview) {
	met := 0
	for _, c := range criteria {
		if passingStatus(c.Verdict) {
			met++
		}
	}
	fmt.Fprintf(deps.Stdout, "\n%s %s %s %d/%d met (%d%%)\n",
		coloredBullet(deps, output.StyleSuccess),
		styled(deps, output.StyleMuted, "Criteria:"),
		progressBar(deps, met, len(criteria), 18),
		met,
		len(criteria),
		percent(met, len(criteria)))
	fmt.Fprintln(deps.Stdout)
	for _, c := range criteria {
		label := c.CriterionID
		if label == "" {
			label = c.Text
		}
		status := styledStatus(deps, c.Verdict)
		if c.VerificationBinding != "" {
			// Deterministic verdict from a bound check.
			status += styled(deps, output.StyleMuted, fmt.Sprintf(" (via check: %s)", c.VerificationBinding))
		}
		fmt.Fprintf(deps.Stdout, "  %s %-20s %s\n",
			criterionBox(deps, c.Verdict),
			label,
			status)
		if c.Why != "" {
			fmt.Fprintf(deps.Stdout, "    %s\n", styled(deps, output.StyleMuted, c.Why))
		}
	}
}
