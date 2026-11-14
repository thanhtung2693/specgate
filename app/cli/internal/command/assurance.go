package command

import (
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
	"github.com/specgate/specgate/app/cli/internal/workflow"
)

type capabilityRow struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	State       string `json:"state"`
	Reason      string `json:"reason,omitempty"`
	NextCommand string `json:"next_command,omitempty"`
}

type capabilityManifest struct {
	Mode         config.Mode     `json:"mode"`
	Capabilities []capabilityRow `json:"capabilities"`
}

type capabilityDefinition struct {
	ID          string
	Name        string
	LocalState  string
	LocalReason string
}

var capabilityCatalog = []capabilityDefinition{
	{ID: "core", Name: "Spec and work tracking", LocalState: "available"},
	{ID: "artifact_versions", Name: "Immutable spec versions", LocalState: "available"},
	{ID: "governed_delivery", Name: "Acceptance-criterion delivery evidence", LocalState: "available"},
	{ID: "workspace_coverage", Name: "Workspace specification coverage", LocalState: "available"},
	{ID: "ide_agent_gates", Name: "IDE-agent gate tasks", LocalState: "available"},
	{ID: "web_ui", Name: "Browser inspection and human review", LocalState: "available"},
}

// localCommandCapabilities is the command-to-capability portion of the same
// manifest used by `specgate capabilities`.
var localCommandCapabilities = map[string]string{
	"version":                   "core",
	"ui":                        "web_ui",
	"open":                      "web_ui",
	"user current":              "core",
	"user list":                 "core",
	"user login":                "core",
	"user logout":               "core",
	"workspace bind":            "core",
	"workspace create":          "core",
	"workspace current":         "core",
	"workspace list":            "core",
	"workspace select":          "core",
	"workspace unbind":          "core",
	"artifact approve":          "artifact_versions",
	"artifact coverage":         "artifact_versions",
	"artifact impact":           "artifact_versions",
	"artifact list":             "artifact_versions",
	"artifact promote":          "artifact_versions",
	"artifact publish":          "artifact_versions",
	"artifact show":             "artifact_versions",
	"feature list":              "artifact_versions",
	"feature show":              "artifact_versions",
	"gates check":               "ide_agent_gates",
	"gates results":             "ide_agent_gates",
	"gates tasks list":          "ide_agent_gates",
	"gates tasks show":          "ide_agent_gates",
	"gates tasks submit-result": "ide_agent_gates",
	"gates tasks dispatch":      "ide_agent_gates",
	"work context":              "core",
	"work verification":         "core",
	"work checkpoint":           "core",
	"work resume":               "core",
	"work create":               "core",
	"work create-quick":         "core",
	"work list":                 "core",
	"work show":                 "core",
	"delivery approve":          "governed_delivery",
	"delivery peer-review":      "governed_delivery",
	"delivery reject":           "governed_delivery",
	"delivery report":           "governed_delivery",
	"delivery review":           "governed_delivery",
	"delivery status":           "governed_delivery",
	"delivery submit":           "governed_delivery",
	"delivery handoff export":   "governed_delivery",
	"delivery handoff show":     "governed_delivery",
	"change accept":             "governed_delivery",
	"change approve":            "artifact_versions",
	"change request-changes":    "governed_delivery",
	"change status":             "governed_delivery",
	"change submit":             "governed_delivery",
	"audit":                     "core",
	"status":                    "core",
	"doctor":                    "core",
	"update":                    "core",
	"uninstall":                 "core",
	"capabilities":              "core",
	"coverage":                  "workspace_coverage",
	"verify":                    "governed_delivery",
	"plugins doctor":            "core",
	"plugins install":           "core",
}

func newCapabilitiesCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "capabilities",
		Short: "Show what this SpecGate mode can do now",
		RunE: func(cmd *cobra.Command, _ []string) error {
			manifest := localCapabilityManifest()

			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("capabilities", manifest)
				return nil
			}
			fmt.Fprintf(deps.Stdout, "%s Local\n", label(deps, "Mode:"))
			for _, capability := range manifest.Capabilities {
				line := fmt.Sprintf("%-22s %s", capability.ID, capability.State)
				if capability.Reason != "" {
					line += " — " + capability.Reason
				}
				fmt.Fprintln(deps.Stdout, line)
				if capability.NextCommand != "" {
					fmt.Fprintf(deps.Stdout, "  Next: %s\n", capability.NextCommand)
				}
			}
			return nil
		},
	}
}

func localCapabilityManifest() capabilityManifest {
	rows := make([]capabilityRow, 0, len(capabilityCatalog))
	for _, definition := range capabilityCatalog {
		rows = append(rows, capabilityRow{
			ID:     definition.ID,
			Name:   definition.Name,
			State:  definition.LocalState,
			Reason: definition.LocalReason,
		})
	}
	return capabilityManifest{Mode: config.ModeLocal, Capabilities: rows}
}

type closeoutWork struct {
	ID    string `json:"id"`
	Key   string `json:"key"`
	Title string `json:"title"`
	Phase string `json:"phase"`
}

type closeoutArtifact struct {
	ID             string `json:"id"`
	Version        string `json:"version"`
	Status         string `json:"status"`
	SnapshotDigest string `json:"snapshot_digest"`
}

type closeoutResult struct {
	Mode            config.Mode                `json:"mode"`
	Work            closeoutWork               `json:"work"`
	Artifact        *closeoutArtifact          `json:"artifact,omitempty"`
	QuickRoute      bool                       `json:"quick_route"`
	Criteria        []workflow.CriterionReview `json:"criteria"`
	Checks          []workflow.CheckResult     `json:"checks"`
	DeliveryFound   bool                       `json:"delivery_found"`
	DeliveryVerdict string                     `json:"delivery_verdict"`
	DeliverySummary string                     `json:"delivery_summary,omitempty"`
	CleanupEligible bool                       `json:"cleanup_eligible"`
	NextCommand     string                     `json:"next_command"`
}

func newVerifyCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "verify <work-ref>",
		Short: "Verify spec-to-delivery evidence and cleanup readiness",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var (
				result closeoutResult
				err    error
			)
			result, err = verifyLocal(cmd, deps, args[0])
			if err != nil {
				return localExitError(deps, "verify", err)
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("verify", result)
			} else {
				printCloseout(deps, result)
			}
			if !result.CleanupEligible {
				return &output.ExitError{Code: output.ExitGovernanceFailed}
			}
			return nil
		},
	}
}

func verifyLocal(cmd *cobra.Command, deps *Deps, ref string) (closeoutResult, error) {
	store, err := openLocalStore(deps)
	if err != nil {
		return closeoutResult{}, err
	}
	defer store.Close()
	selection, err := localSelection(cmd.Context(), deps, store)
	if err != nil {
		return closeoutResult{}, err
	}
	work, err := store.GetWork(cmd.Context(), selection.Workspace.ID, ref)
	if err != nil {
		return closeoutResult{}, err
	}
	commandPrefix := "specgate"
	if cmd.Flags().Changed("workspace") && deps.WorkspaceOverride != "" {
		commandPrefix += " --workspace " + shellQuote(deps.WorkspaceOverride)
	}
	result := closeoutResult{
		Mode:            config.ModeLocal,
		Work:            closeoutWork{ID: work.ID, Key: work.Key, Title: work.Title, Phase: work.Phase},
		QuickRoute:      work.ArtifactID == "",
		Criteria:        localPendingCriteria(work.AcceptanceCriteria),
		Checks:          []workflow.CheckResult{},
		DeliveryVerdict: "not_reviewed",
		NextCommand:     commandPrefix + " change status " + work.Key + " --json",
	}
	if work.ArtifactID != "" {
		artifact, err := store.GetArtifact(cmd.Context(), selection.Workspace.ID, work.ArtifactID)
		if err != nil {
			return closeoutResult{}, err
		}
		result.Artifact = &closeoutArtifact{
			ID: artifact.ID, Version: "v" + strconv.Itoa(artifact.Version), Status: artifact.Status, SnapshotDigest: artifact.SnapshotDigest,
		}
	}
	review, err := store.DeliveryStatus(cmd.Context(), selection.Workspace.ID, work.Key)
	if err != nil && err != sql.ErrNoRows {
		return closeoutResult{}, err
	}
	if err == nil {
		result.DeliveryFound = true
		result.DeliveryVerdict = review.Verdict
		result.DeliverySummary = review.Summary
		report, reportErr := store.DeliveryReportForReview(cmd.Context(), selection.Workspace.ID, review)
		if reportErr != nil {
			return closeoutResult{}, reportErr
		}
		result.Criteria, result.Checks = localReportEvidence(work.AcceptanceCriteria, report)
	}
	result.CleanupEligible = workPhaseDelivered(work.Phase) && review.HumanDecision == "approve"
	if result.CleanupEligible {
		result.NextCommand = "specgate cleanup --work --dry-run"
	}
	return result, nil
}

func localPendingCriteria(criteria []string) []workflow.CriterionReview {
	result := make([]workflow.CriterionReview, 0, len(criteria))
	for index, raw := range criteria {
		text, binding := parseAcceptanceCriterionBinding(raw)
		result = append(result, workflow.CriterionReview{
			CriterionID:         fmt.Sprintf("local-%d", index+1),
			Text:                text,
			VerificationBinding: binding,
			Verdict:             "pending",
			Why:                 "no delivery evidence",
		})
	}
	return result
}

func localReportEvidence(criteria []string, report local.DeliveryReport) ([]workflow.CriterionReview, []workflow.CheckResult) {
	reviews := localPendingCriteria(criteria)
	byID := make(map[string]int, len(reviews))
	for index, review := range reviews {
		byID[review.CriterionID] = index
	}
	rawChecks, _ := report.Body["checks"].([]any)
	checks := make([]workflow.CheckResult, 0, len(rawChecks))
	for _, raw := range rawChecks {
		row, _ := raw.(map[string]any)
		checks = append(checks, workflow.CheckResult{
			Name: strings.TrimSpace(fmt.Sprint(row["name"])), Status: strings.TrimSpace(fmt.Sprint(row["status"])), Detail: strings.TrimSpace(fmt.Sprint(row["detail"])),
			Source: checkSource(row["source"]),
		})
	}
	rawCriteria, _ := report.Body["criteria"].([]any)
	citations := make(map[int]string, len(rawCriteria))
	for _, raw := range rawCriteria {
		row, _ := raw.(map[string]any)
		id := strings.TrimSpace(fmt.Sprint(row["criterion_id"]))
		index, ok := byID[id]
		if !ok {
			continue
		}
		claim := strings.TrimSpace(fmt.Sprint(row["claim"]))
		verdict := "unclear"
		if claim == "satisfied" {
			verdict = "met"
		} else if claim == "partial" || claim == "not_done" {
			verdict = "unmet"
		}
		reviews[index].Verdict = verdict
		reviews[index].Why = evidenceSummary(row)
		citations[index] = citationWeakness(row)
	}
	// A bound criterion reads its verdict from the named check, matching the
	// review that produced the stored verdict; the claim never overrides it.
	for index, review := range reviews {
		if review.VerificationBinding == "" {
			continue
		}
		verdict, why := boundCriterionOutcome(review.VerificationBinding, checks)
		if diagnostic := selectedJUnitDiagnostic(review.VerificationBinding, checks); diagnostic != "" {
			why += "; " + diagnostic
		}
		// The bound check decides, so a bad citation must not flip the verdict —
		// a wrong pointer to the proof is not a broken feature. It still belongs
		// on this line: overwriting `why` wholesale hid a fabricated citation
		// behind a clean pass, which is what the human reads before accepting.
		if note := citations[index]; note != "" {
			why += "; " + note
		}
		reviews[index].Verdict, reviews[index].Why = verdict, why
	}
	return reviews, checks
}

func selectedJUnitDiagnostic(binding string, checks []workflow.CheckResult) string {
	const marker = " — selected-test report: "
	for _, check := range checks {
		if check.Name != binding || check.Source != "specgate_cli" {
			continue
		}
		if _, diagnostic, ok := strings.Cut(check.Detail, marker); ok {
			return strings.TrimSpace(diagnostic)
		}
	}
	return ""
}

// citationWeakness describes a cited-evidence problem worth showing next to a
// verdict. It returns empty when the citation is anchored or absent, because a
// criterion with no citation already reads as "no evidence supplied".
func citationWeakness(row map[string]any) string {
	evidence, _ := row["evidence"].(map[string]any)
	grounding, _ := evidence["grounding"].(map[string]any)
	status, _ := grounding["status"].(string)
	switch strings.TrimSpace(status) {
	case "heading_not_found":
		heading, _ := evidence["heading"].(string)
		return fmt.Sprintf("cited heading %q is not in %v", heading, evidence["path"])
	case "line_out_of_range":
		return fmt.Sprintf("cited line is past the end of %v", evidence["path"])
	case "unanchored":
		return fmt.Sprintf("citation names %v with no line or heading", evidence["path"])
	case "empty_at_anchor":
		return fmt.Sprintf("cited location in %v is blank", evidence["path"])
	default:
		return ""
	}
}

// checkSource reads the CLI-stamped provenance of a check row. Only
// `--run-checks` sets it, so a missing value means nothing re-ran the command.
func checkSource(raw any) string {
	source, _ := raw.(string)
	return strings.TrimSpace(source)
}

func boundCriterionOutcome(binding string, checks []workflow.CheckResult) (string, string) {
	for _, check := range checks {
		if check.Name != binding {
			continue
		}
		// A human deciding acceptance reads this line, not the completion file.
		// Saying a check "passed" without saying who observed it presents an
		// unrun command as a deterministic result; `--run-checks` is opt-in, so
		// the common case is the agent's own claim.
		origin := "reported by the coding agent, not re-run"
		if check.Source == "specgate_cli" {
			origin = "observed by the SpecGate CLI"
		}
		switch check.Status {
		case "pass":
			return "met", fmt.Sprintf("check %q passed (%s)", binding, origin)
		case "fail":
			return "unmet", fmt.Sprintf("check %q failed (%s)", binding, origin)
		default:
			return "unclear", fmt.Sprintf("check %q reported %q — cannot verify deterministically", binding, check.Status)
		}
	}
	return "unclear", fmt.Sprintf("check %q not found in report — cannot verify deterministically", binding)
}

func evidenceSummary(row map[string]any) string {
	if evidence, ok := row["evidence"].(map[string]any); ok {
		keys := make([]string, 0, len(evidence))
		for key, value := range evidence {
			if strings.TrimSpace(fmt.Sprint(value)) != "" {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		if len(keys) > 0 {
			return "evidence: " + strings.Join(keys, ", ")
		}
	}
	return "no evidence supplied"
}

func workPhaseDelivered(phase string) bool {
	return strings.EqualFold(strings.TrimSpace(phase), "delivered")
}

func printCloseout(deps *Deps, result closeoutResult) {
	fmt.Fprintf(deps.Stdout, "%s %s — %s\n", label(deps, "Work:"), result.Work.Key, result.Work.Phase)
	if result.Artifact != nil {
		fmt.Fprintf(deps.Stdout, "%s %s %s (%s)\n", label(deps, "Artifact:"), result.Artifact.ID, result.Artifact.Version, result.Artifact.SnapshotDigest)
	} else {
		fmt.Fprintf(deps.Stdout, "%s none\n", label(deps, "Artifact:"))
	}
	for _, criterion := range result.Criteria {
		fmt.Fprintf(deps.Stdout, "  [%s] %s — %s\n", criterion.Verdict, criterion.Text, criterion.Why)
	}
	for _, check := range result.Checks {
		fmt.Fprintf(deps.Stdout, "  [%s] %s — %s\n", check.Status, check.Name, check.Detail)
	}
	fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "Delivery:"), result.DeliveryVerdict)
	fmt.Fprintf(deps.Stdout, "%s %t\n", label(deps, "Cleanup eligible:"), result.CleanupEligible)
	fmt.Fprintf(deps.Stdout, "Next: %s\n", result.NextCommand)
}
