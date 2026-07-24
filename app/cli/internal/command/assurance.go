package command

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
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
	ID            string
	Name          string
	LocalState    string
	LocalReason   string
	FullDetailKey string
}

var capabilityCatalog = []capabilityDefinition{
	{ID: "core", Name: "Spec and work tracking", LocalState: "available"},
	{ID: "artifact_versions", Name: "Immutable spec versions", LocalState: "available"},
	{ID: "governed_delivery", Name: "Acceptance-criterion delivery evidence", LocalState: "available"},
	{ID: "workspace_coverage", Name: "Workspace specification coverage", LocalState: "available"},
	{ID: "portable_transfer", Name: "Portable Local-to-Full transfer", LocalState: "available"},
	{ID: "ide_agent_gates", Name: "IDE-agent gate tasks", LocalState: "available"},
	{ID: "web_ui", Name: "Browser UI", LocalState: "unavailable", LocalReason: "Local mode has no server or browser UI", FullDetailKey: "web_ui"},
	{ID: "governance_chat", Name: "Governance chat", LocalState: "unavailable", LocalReason: "Local mode has no agents service", FullDetailKey: "governance_chat"},
	{ID: "platform_model", Name: "Server-side governance model", LocalState: "unavailable", LocalReason: "Local mode delegates model work to the IDE agent", FullDetailKey: "platform_model"},
	{ID: "integrations", Name: "Team integrations", LocalState: "unavailable", LocalReason: "Local mode has no integration service", FullDetailKey: "integrations"},
	{ID: "knowledge", Name: "Workspace knowledge registry", LocalState: "unavailable", LocalReason: "Local mode stores governed specs, not a knowledge service", FullDetailKey: "knowledge"},
	{ID: "semantic_search", Name: "Semantic knowledge search", LocalState: "unavailable", LocalReason: "Local mode has no embedding service", FullDetailKey: "semantic_search"},
}

// localCommandCapabilities is the command-to-capability portion of the same
// manifest used by `specgate capabilities`.
var localCommandCapabilities = map[string]string{
	"version":                   "core",
	"user current":              "core",
	"user list":                 "core",
	"user login":                "core",
	"user logout":               "core",
	"workspace bind":            "core",
	"workspace create":          "core",
	"workspace current":         "core",
	"workspace list":            "core",
	"workspace select":          "core",
	"artifact approve":          "artifact_versions",
	"artifact coverage":         "artifact_versions",
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
	"change accept":             "governed_delivery",
	"change approve":            "artifact_versions",
	"change request-changes":    "governed_delivery",
	"change status":             "governed_delivery",
	"change submit":             "governed_delivery",
	"audit":                     "core",
	"stats":                     "core",
	"status":                    "core",
	"doctor":                    "core",
	"update":                    "core",
	"uninstall":                 "core",
	"capabilities":              "core",
	"coverage":                  "workspace_coverage",
	"verify":                    "governed_delivery",
	"plugins doctor":            "core",
	"plugins install":           "core",
	"portable export":           "portable_transfer",
}

func newCapabilitiesCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "capabilities",
		Short: "Show what this SpecGate mode can do now",
		RunE: func(cmd *cobra.Command, _ []string) error {
			manifest := localCapabilityManifest()
			if deps.Topology == config.ModeFull {
				meta, err := deps.Client.Meta(cmd.Context())
				if err != nil {
					return apiExitError(deps, "capabilities", err)
				}
				manifest = fullCapabilityManifest(meta)
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("capabilities", manifest)
				return nil
			}
			modeName := "Full"
			if manifest.Mode == config.ModeLocal {
				modeName = "Local"
			}
			fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "Mode:"), modeName)
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

func fullCapabilityManifest(meta *client.Meta) capabilityManifest {
	rows := make([]capabilityRow, 0, len(capabilityCatalog))
	for _, definition := range capabilityCatalog {
		row := capabilityRow{ID: definition.ID, Name: definition.Name, State: "available"}
		if definition.FullDetailKey != "" {
			if detail, ok := meta.CapabilityDetails[definition.FullDetailKey]; ok {
				row.State = detail.State
				row.Reason = detail.Reason
				row.NextCommand = detail.NextCommand
			}
		}
		rows = append(rows, row)
	}
	return capabilityManifest{Mode: config.ModeFull, Capabilities: rows}
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
	Mode            config.Mode              `json:"mode"`
	Work            closeoutWork             `json:"work"`
	Artifact        *closeoutArtifact        `json:"artifact,omitempty"`
	QuickRoute      bool                     `json:"quick_route"`
	Criteria        []client.CriterionReview `json:"criteria"`
	Checks          []client.CheckResult     `json:"checks"`
	DeliveryFound   bool                     `json:"delivery_found"`
	DeliveryVerdict string                   `json:"delivery_verdict"`
	DeliverySummary string                   `json:"delivery_summary,omitempty"`
	CleanupEligible bool                     `json:"cleanup_eligible"`
	NextCommand     string                   `json:"next_command"`
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
			if deps.Topology == config.ModeLocal {
				result, err = verifyLocal(cmd, deps, args[0])
				if err != nil {
					return localExitError(deps, "verify", err)
				}
			} else {
				result, err = verifyFull(cmd, deps, args[0])
				if err != nil {
					return apiExitError(deps, "verify", err)
				}
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

func verifyFull(cmd *cobra.Command, deps *Deps, ref string) (closeoutResult, error) {
	work, err := deps.Client.ResolveWorkRef(cmd.Context(), ref)
	if err != nil {
		return closeoutResult{}, closeoutReadError("resolve work", err)
	}
	result := closeoutResult{
		Mode: config.ModeFull,
		Work: closeoutWork{
			ID: work.ChangeRequestID, Key: work.ChangeRequestKey, Title: work.Title, Phase: work.Phase,
		},
		Criteria:        []client.CriterionReview{},
		Checks:          []client.CheckResult{},
		DeliveryVerdict: "not_reviewed",
	}
	pack, err := deps.Client.ContextPack(cmd.Context(), work.ChangeRequestID)
	if err != nil {
		return closeoutResult{}, closeoutReadError("read context pack", err)
	}
	artifactID := strings.TrimSpace(pack.ArtifactID)
	if artifactID == "" {
		artifactID = strings.TrimSpace(pack.SourceArtifactID)
	}
	if artifactID != "" {
		artifact, err := deps.Client.GetArtifact(cmd.Context(), artifactID)
		if err != nil {
			return closeoutResult{}, closeoutReadError("read governing artifact", err)
		}
		result.Artifact = &closeoutArtifact{
			ID: artifact.ID, Version: artifact.Version, Status: artifact.Status, SnapshotDigest: artifact.SnapshotDigest,
		}
	}
	result.QuickRoute = artifactID == ""
	criteria, err := deps.Client.ListAcceptanceCriteria(cmd.Context(), work.ChangeRequestID)
	if err != nil {
		return closeoutResult{}, closeoutReadError("read acceptance criteria", err)
	}
	delivery, err := deps.Client.DeliveryStatus(cmd.Context(), work.ChangeRequestID, true)
	if err != nil {
		return closeoutResult{}, closeoutReadError("read delivery status", err)
	}
	result.DeliveryFound = delivery.Found
	if delivery.Found {
		result.DeliveryVerdict = delivery.Verdict
		result.DeliverySummary = delivery.Summary
		result.Checks = delivery.Checks
	}
	result.Criteria = mergeCriterionReviews(criteria, delivery.PerCriterion)
	result.CleanupEligible = workPhaseDelivered(work.Phase) && delivery.Verdict == "pass"
	result.NextCommand = closeoutNextCommand(result)
	return result, nil
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
	result := closeoutResult{
		Mode:            config.ModeLocal,
		Work:            closeoutWork{ID: work.ID, Key: work.Key, Title: work.Title, Phase: work.Phase},
		QuickRoute:      work.ArtifactID == "",
		Criteria:        localPendingCriteria(work.AcceptanceCriteria),
		Checks:          []client.CheckResult{},
		DeliveryVerdict: "not_reviewed",
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
		report, reportErr := store.LatestDeliveryReport(cmd.Context(), selection.Workspace.ID, work.Key)
		if reportErr != nil {
			return closeoutResult{}, reportErr
		}
		result.Criteria, result.Checks = localReportEvidence(work.AcceptanceCriteria, report)
	}
	result.CleanupEligible = workPhaseDelivered(work.Phase) && review.HumanDecision == "approve"
	result.NextCommand = closeoutNextCommand(result)
	return result, nil
}

func closeoutReadError(operation string, err error) error {
	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		copy := *apiErr
		copy.Message = operation
		copy.Detail = apiErr.Error()
		return &copy
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func mergeCriterionReviews(criteria []client.AcceptanceCriterion, reviews []client.CriterionReview) []client.CriterionReview {
	byID := make(map[string]client.CriterionReview, len(reviews))
	for _, review := range reviews {
		byID[review.CriterionID] = review
	}
	result := make([]client.CriterionReview, 0, len(criteria))
	for _, criterion := range criteria {
		review, ok := byID[criterion.ID]
		if !ok {
			review = client.CriterionReview{CriterionID: criterion.ID, Text: criterion.Text, Verdict: "pending", Why: "no delivery evidence"}
		}
		if review.Text == "" {
			review.Text = criterion.Text
		}
		if review.VerificationBinding == "" {
			review.VerificationBinding = criterion.VerificationBinding
		}
		result = append(result, review)
	}
	return result
}

func localPendingCriteria(criteria []string) []client.CriterionReview {
	result := make([]client.CriterionReview, 0, len(criteria))
	for index, raw := range criteria {
		text, binding := parseAcceptanceCriterionBinding(raw)
		result = append(result, client.CriterionReview{
			CriterionID:         fmt.Sprintf("local-%d", index+1),
			Text:                text,
			VerificationBinding: binding,
			Verdict:             "pending",
			Why:                 "no delivery evidence",
		})
	}
	return result
}

func localReportEvidence(criteria []string, report local.DeliveryReport) ([]client.CriterionReview, []client.CheckResult) {
	reviews := localPendingCriteria(criteria)
	byID := make(map[string]int, len(reviews))
	for index, review := range reviews {
		byID[review.CriterionID] = index
	}
	rawCriteria, _ := report.Body["criteria"].([]any)
	for _, raw := range rawCriteria {
		row, _ := raw.(map[string]any)
		id := strings.TrimSpace(fmt.Sprint(row["criterion_id"]))
		index, ok := byID[id]
		if !ok {
			continue
		}
		claim := strings.TrimSpace(fmt.Sprint(row["claim"]))
		verdict := "pending"
		if claim == "satisfied" {
			verdict = "pass"
		} else if claim != "" && claim != "not_done" {
			verdict = "fail"
		}
		reviews[index].Verdict = verdict
		reviews[index].Why = evidenceSummary(row)
	}
	rawChecks, _ := report.Body["checks"].([]any)
	checks := make([]client.CheckResult, 0, len(rawChecks))
	for _, raw := range rawChecks {
		row, _ := raw.(map[string]any)
		checks = append(checks, client.CheckResult{
			Name: strings.TrimSpace(fmt.Sprint(row["name"])), Status: strings.TrimSpace(fmt.Sprint(row["status"])), Detail: strings.TrimSpace(fmt.Sprint(row["detail"])),
		})
	}
	return reviews, checks
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

func deliveryPassed(mode config.Mode, verdict string) bool {
	if mode == config.ModeLocal {
		return verdict == "passed"
	}
	return verdict == "pass"
}

func workPhaseDelivered(phase string) bool {
	return strings.EqualFold(strings.TrimSpace(phase), "delivered")
}

func closeoutNextCommand(result closeoutResult) string {
	ref := result.Work.Key
	switch {
	case result.Artifact == nil && !result.QuickRoute:
		return "specgate work context " + ref + " --json"
	case !result.DeliveryFound:
		return "specgate delivery report " + ref + " --init"
	case result.CleanupEligible:
		return "specgate cleanup --work --dry-run"
	case !deliveryPassed(result.Mode, result.DeliveryVerdict):
		return "specgate delivery status " + ref + " --detail"
	case !workPhaseDelivered(result.Work.Phase):
		if result.Mode == config.ModeLocal {
			return "specgate --yes change accept " + ref
		}
		return "specgate change accept " + ref
	default:
		return "specgate cleanup --work --dry-run"
	}
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
