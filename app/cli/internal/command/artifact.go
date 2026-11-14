package command

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

const artifactSourceMaxBytes = 1 << 20

func registerArtifactCommands(root *cobra.Command, deps *Deps) {
	art := &cobra.Command{
		Use:   "artifact",
		Short: "Manage and inspect artifacts",
	}
	art.AddCommand(newArtifactListCmd(deps))
	art.AddCommand(newArtifactShowCmd(deps))
	art.AddCommand(newArtifactCoverageCmd(deps))
	art.AddCommand(newArtifactImpactCmd(deps))
	art.AddCommand(newArtifactPublishCmd(deps))
	art.AddCommand(newArtifactApproveCmd(deps))
	art.AddCommand(newArtifactPromoteCmd(deps))
	root.AddCommand(art)
}

func newArtifactImpactCmd(deps *Deps) *cobra.Command {
	var base string
	cmd := &cobra.Command{Use: "impact <target-artifact-id>", Short: "Compare an exact Local artifact pair without inferring lineage", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		const op = "artifact.impact"

		if strings.TrimSpace(base) == "" {
			return completionValidationError(deps, op, "--compare <base-artifact-id> is required")
		}
		store, err := openLocalStore(deps)
		if err != nil {
			return localExitError(deps, op, err)
		}
		defer store.Close()
		sel, err := localSelection(cmd.Context(), deps, store)
		if err != nil {
			return localExitError(deps, op, err)
		}
		impact, err := store.ArtifactImpact(cmd.Context(), sel.Workspace.ID, args[0], base)
		if err != nil {
			return localExitError(deps, op, err)
		}
		if deps.Printer.Mode() == output.ModeJSON {
			deps.Printer.Success(op, impact)
			return nil
		}
		printArtifactImpact(deps, impact)
		return nil
	}}
	cmd.Flags().StringVar(&base, "compare", "", "Exact base artifact ID")
	return cmd
}

func printArtifactImpact(deps *Deps, impact local.ArtifactImpact) {
	fmt.Fprintf(deps.Stdout, "Impact %s → %s: %s\n", impact.BaseArtifactID, impact.TargetArtifactID, impact.RequirementImpact)
	fmt.Fprintf(deps.Stdout, "Snapshots: %s (%s) → %s (%s)\n", impact.BaseStatus, impact.BaseSnapshotDigest, impact.TargetStatus, impact.TargetSnapshotDigest)
	if impact.RequirementReason != "" {
		fmt.Fprintln(deps.Stdout, impact.RequirementReason)
	}
	for _, group := range []struct {
		name  string
		paths []string
	}{{"Document added", impact.DocumentDelta.Added}, {"Document removed", impact.DocumentDelta.Removed}, {"Document modified", impact.DocumentDelta.Modified}} {
		for _, path := range group.paths {
			fmt.Fprintf(deps.Stdout, "%s: %s\n", group.name, path)
		}
	}
	for _, row := range impact.Transitions {
		fmt.Fprintf(deps.Stdout, "Source: %s → %s (%s; fields: %s)\n", row.BaseID, strings.Join(row.TargetIDs, ", "), row.Summary, strings.Join(row.ChangedFields, ", "))
		fmt.Fprintf(deps.Stdout, "  Before: %q [%s; defer: %q]\n", row.Before.Text, row.Before.SourcePath, row.Before.DeferredReason)
		for _, after := range row.After {
			fmt.Fprintf(deps.Stdout, "  After %s: %q [%s; defer: %q]\n", after.ID, after.Text, after.SourcePath, after.DeferredReason)
		}
	}
	for _, group := range []struct {
		name  string
		items []local.SourceCriterion
	}{{"Source added", impact.AddedRequirements}, {"Source removed", impact.RemovedRequirements}, {"Source deferred", impact.DeferredRequirements}, {"Target source without work", impact.UnassignedRequirements}} {
		for _, item := range group.items {
			fmt.Fprintf(deps.Stdout, "%s: %s\n", group.name, item.ID)
		}
	}
	fmt.Fprintf(deps.Stdout, "Recorded work: %d base-linked, %d target-linked, %d unlinked\n", len(impact.BaseWorkItems), len(impact.TargetWorkItems), len(impact.UnlinkedWorkItems))
	for _, group := range []struct {
		name  string
		items []local.ImpactWorkLink
	}{{"Base work", impact.BaseWorkItems}, {"Target work", impact.TargetWorkItems}, {"Unlinked work (unassessed)", impact.UnlinkedWorkItems}} {
		for _, item := range group.items {
			fmt.Fprintf(deps.Stdout, "%s: %s (%s; sources: %s; evidence: %s; inspection: %s)\n", group.name, item.Key, item.Phase, strings.Join(item.SourceCriteria, ", "), item.Evidence, item.Inspection)
		}
	}
	fmt.Fprintf(deps.Stdout, "Overlap: %s (no recorded overlap does not establish safety)\n", impact.SourceOverlapState)
	for _, overlap := range impact.SourceOverlaps {
		fmt.Fprintf(deps.Stdout, "Overlap source %s: %s (reports: %s)\n", overlap.SourceCriterion, strings.Join(overlap.WorkKeys, ", "), strings.Join(overlap.ReportIDs, ", "))
	}
	fmt.Fprintf(deps.Stdout, "Path overlap: %s (reported files only; no recorded overlap does not establish safety)\n", impact.PathOverlapState)
	for _, overlap := range impact.PathOverlaps {
		fmt.Fprintf(deps.Stdout, "Overlap path %s: %s (reports: %s)\n", overlap.Path, strings.Join(overlap.WorkKeys, ", "), strings.Join(overlap.ReportIDs, ", "))
	}
	if impact.SourceOverlapTruncated {
		fmt.Fprintln(deps.Stdout, "Overlap list capped at 100 hints")
	}
	if impact.PathOverlapTruncated {
		fmt.Fprintln(deps.Stdout, "Path overlap list capped at 100 hints")
	}
}

// specgate artifact coverage <artifact-id>
func newArtifactCoverageCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "coverage <artifact-id>",
		Short: "Show delivery coverage for this exact artifact version",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			store, err := openLocalStore(deps)
			if err != nil {
				return localExitError(deps, "artifact.coverage", err)
			}
			defer store.Close()
			selection, err := localSelection(cmd.Context(), deps, store)
			if err != nil {
				return localExitError(deps, "artifact.coverage", err)
			}
			artifact, err := store.GetArtifact(cmd.Context(), selection.Workspace.ID, id)
			if err != nil {
				return localExitError(deps, "artifact.coverage", err)
			}
			items, err := store.ListWork(cmd.Context(), selection.Workspace.ID)
			if err != nil {
				return localExitError(deps, "artifact.coverage", err)
			}
			return printArtifactCoverage(deps, localArtifactCoverageView(artifact, items))
		},
	}
}

func localWorkCoverage(items []local.WorkItem, artifactID string) []map[string]string {
	out := []map[string]string{}
	for _, item := range items {
		if item.ArtifactID == artifactID {
			out = append(out, map[string]string{"key": item.Key, "phase": item.Phase, "title": item.Title})
		}
	}
	return out
}

func localArtifactCoverageView(artifact local.Artifact, items []local.WorkItem) map[string]any {
	data := artifactCoverageView(artifact.ID, artifact.Status, localWorkCoverage(items, artifact.ID))
	work := make([]coverageWork, 0, len(items))
	for _, item := range items {
		if item.ArtifactID == artifact.ID {
			work = append(work, coverageWork{Key: item.Key, Title: item.Title, Current: true, Phase: item.Phase, AcceptanceCriteria: item.AcceptanceCriteria})
		}
	}
	criteria := sourceCriterionIDs(artifact.SourceCriteria)
	data["source_coverage"] = sourceCoverage(criteria, work)
	data["source_requirements"] = sourceRequirementRows(criteria, work)
	if hasUnassignedSourceRequirement(data["source_requirements"].([]sourceRequirementCoverage)) {
		data["source_next_action"] = fmt.Sprintf("specgate artifact show %s --json", artifact.ID)
	}
	return data
}

func artifactCoverageView(id, artifactStatus string, items []map[string]string) map[string]any {
	state := "uncovered"
	if len(items) > 0 {
		state = "delivered"
		for _, item := range items {
			if !isDeliveredPhase(item["phase"]) {
				state = "in_progress"
				break
			}
		}
	} else if artifactStatus == "superseded" {
		state = "superseded"
	}
	return map[string]any{"artifact_id": id, "state": state, "source_coverage": "unknown", "work_items": items}
}
func printArtifactCoverage(deps *Deps, data map[string]any) error {
	if deps.Printer.Mode() == output.ModeJSON {
		deps.Printer.Success("artifact.coverage", data)
		return nil
	}
	fmt.Fprintf(deps.Stdout, "%s %s\n", styled(deps, output.StyleBold, fmt.Sprint(data["artifact_id"]))+":", styledStatus(deps, fmt.Sprint(data["state"])))
	fmt.Fprintf(deps.Stdout, "  Source requirements: %s\n", styledStatus(deps, fmt.Sprint(data["source_coverage"])))
	for _, item := range data["work_items"].([]map[string]string) {
		fmt.Fprintf(deps.Stdout, "%s  [%s]  %s\n", styled(deps, output.StyleBold, item["key"]), styledStatus(deps, item["phase"]), item["title"])
	}
	if requirements, ok := data["source_requirements"].([]sourceRequirementCoverage); ok {
		for _, requirement := range requirements {
			fmt.Fprintf(deps.Stdout, "  [%s] %s — %s (%s)", terminalText(requirement.State), terminalText(requirement.ID), terminalText(requirement.Text), terminalText(requirement.SourcePath))
			if requirement.DeferredReason != "" {
				fmt.Fprintf(deps.Stdout, " — %s", terminalText(requirement.DeferredReason))
			}
			fmt.Fprintln(deps.Stdout)
			for _, item := range requirement.WorkItems {
				fmt.Fprintf(deps.Stdout, "    %s [%s] %s\n", terminalText(item.Key), terminalText(item.Phase), terminalText(item.Title))
			}
		}
	}
	if sourceNextAction, ok := data["source_next_action"].(string); ok && sourceNextAction != "" {
		fmt.Fprintf(deps.Stdout, "  Source next: %s\n", sourceNextAction)
	}
	return nil
}

// specgate artifact list [--status <s>] [--feature <f>]
func newArtifactListCmd(deps *Deps) *cobra.Command {
	var (
		status    string
		featureID string
		limit     int
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List artifacts",
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, err := openLocalStore(deps)
			if err != nil {
				return localExitError(deps, "artifact.list", err)
			}
			defer store.Close()
			selection, err := localSelection(cmd.Context(), deps, store)
			if err != nil {
				return localExitError(deps, "artifact.list", err)
			}
			artifacts, err := store.ListArtifacts(cmd.Context(), selection.Workspace.ID)
			if err != nil {
				return localExitError(deps, "artifact.list", err)
			}
			items := make([]map[string]any, 0, len(artifacts))
			for _, artifact := range artifacts {
				if status != "" && status != "all" && artifact.Status != status {
					continue
				}
				if featureID != "" && artifact.FeatureKey != featureID {
					continue
				}
				items = append(items, localArtifactView(artifact))
				if limit > 0 && len(items) == limit {
					break
				}
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("artifact.list", map[string]any{"items": items})
				return nil
			}
			if len(items) == 0 {
				fmt.Fprintln(deps.Stdout, notice(deps, output.StyleInfo, "Notice", "No artifacts found."))
				return nil
			}
			for _, item := range items {
				fmt.Fprintf(deps.Stdout, "%s  v%d  %s  %s\n", styled(deps, output.StyleBold, fmt.Sprint(item["id"])), item["version"], styledStatus(deps, fmt.Sprint(item["status"])), item["feature_key"])
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&status, "status", "", "Filter by status (draft, approved, …); default hides superseded, use 'all' to show every status")
	cmd.Flags().StringVar(&featureID, "feature", "", "Filter by feature key or ID")
	cmd.Flags().IntVar(&limit, "limit", 50, "Maximum number of results")
	return cmd
}

// specgate artifact show <id>
func newArtifactShowCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Show artifact details for an exact ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openLocalStore(deps)
			if err != nil {
				return localExitError(deps, "artifact.show", err)
			}
			defer store.Close()
			selection, err := localSelection(cmd.Context(), deps, store)
			if err != nil {
				return localExitError(deps, "artifact.show", err)
			}
			artifact, err := store.GetArtifact(cmd.Context(), selection.Workspace.ID, args[0])
			if err != nil {
				return localExitError(deps, "artifact.show", err)
			}
			view := localArtifactView(artifact)
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("artifact.show", view)
				return nil
			}
			fmt.Fprintf(deps.Stdout, "%s %s\n%s %d\n%s %s\n%s %s\n", label(deps, "ID:"), styled(deps, output.StyleBold, artifact.ID), label(deps, "Version:"), artifact.Version, label(deps, "Status:"), styledStatus(deps, artifact.Status), label(deps, "Feature:"), artifact.FeatureKey)
			return nil
		},
	}
}

// specgate artifact publish --file <path>
