package command

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/output"
)

func registerGatesCommands(root *cobra.Command, deps *Deps) {
	gates := &cobra.Command{
		Use:   "gates",
		Short: "Manage and inspect quality gates",
	}
	gates.AddCommand(newGatesCheckCmd(deps))
	gates.AddCommand(newGatesResultsCmd(deps))
	gates.AddCommand(newGatesTasksCmd(deps))
	root.AddCommand(gates)
}

// specgate gates check <artifact-id>
func newGatesCheckCmd(deps *Deps) *cobra.Command {
	var summary bool
	cmd := &cobra.Command{
		Use:   "check <artifact-id>",
		Short: "Run artifact-scoped quality/readiness checks",
		Long: `Run artifact-scoped quality/readiness checks and persist their results.

This command runs checks again. With --json --summary it returns only the
aggregate, gate states, hints, executor origins, and IDE-agent task IDs. Use
gates results for read-only access to stored detailed evidence.`,
		Example: `  specgate gates check <artifact-id> --json --summary
  specgate gates results <artifact-id> --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			commandName := "gates.check"
			store, err := openLocalStore(deps)
			if err != nil {
				return localExitError(deps, commandName, err)
			}
			defer store.Close()
			selection, err := localSelection(cmd.Context(), deps, store)
			if err != nil {
				return localExitError(deps, commandName, err)
			}
			run, err := store.RunReadiness(cmd.Context(), selection.Workspace.ID, args[0])
			if err != nil {
				return localExitError(deps, commandName, err)
			}
			evaluations := localReadinessEvaluations(run.Checks)
			readinessRuns := make([]any, len(evaluations))
			for i := range evaluations {
				readinessRuns[i] = evaluations[i]
			}
			result := map[string]any{"artifact_id": run.ArtifactID, "aggregate": run.Aggregate, "readiness_runs": readinessRuns, "dispatched_to_ide_agent": localDispatchPayload(run.Dispatch)}
			if deps.Printer.Mode() == output.ModeJSON {
				if summary {
					result = artifactReadinessSummary(args[0], result)
				}
				deps.Printer.Success(commandName, result)
				return nil
			}
			fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "Spec quality:"), styledStatus(deps, run.Aggregate))
			printReadinessGates(deps, evaluations)
			if len(run.Dispatch.PendingTaskIDs) > 0 {
				fmt.Fprintf(deps.Stdout, "%s %d semantic checks need your IDE agent.\n", styled(deps, output.StyleWarning, "No external model configured —"), len(run.Dispatch.PendingTaskIDs))
				fmt.Fprintln(deps.Stdout, nextStep(deps, "inspect the rubrics with", "specgate gates tasks list "+args[0]))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&summary, "summary", false, "With --json, omit detailed evidence and return compact gate states")
	return cmd
}

func artifactReadinessSummary(artifactID string, result map[string]any) map[string]any {
	summary := map[string]any{"artifact_id": artifactID}
	for _, key := range []string{"aggregate", "governance_level", "evaluations_posted"} {
		if value, ok := result[key]; ok {
			summary[key] = value
		}
	}
	if value, ok := result["artifact_id"]; ok {
		summary["artifact_id"] = value
	}

	gates := make([]map[string]any, 0)
	if runs, ok := result["readiness_runs"].([]any); ok {
		for _, raw := range runs {
			run, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			gate := map[string]any{}
			for _, key := range []string{"gate", "state", "hint", "executor"} {
				if value, ok := run[key]; ok {
					gate[key] = value
				}
			}
			gates = append(gates, gate)
		}
	}
	summary["gates"] = gates

	if dispatch, ok := result["dispatched_to_ide_agent"].(map[string]any); ok {
		compact := map[string]any{}
		for _, key := range []string{"created_task_ids", "pending_task_ids"} {
			if taskIDs, ok := dispatch[key]; ok {
				compact[key] = taskIDs
			}
		}
		if len(compact) > 0 {
			summary["dispatched_to_ide_agent"] = compact
		}
	}
	return summary
}

func newGatesResultsCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "results <artifact-id>",
		Short: "Read stored artifact readiness results",
		Long: `Read stored artifact readiness results without running checks again.

JSON output includes the persisted evidence needed for deeper agent inspection.`,
		Example: `  specgate gates results <artifact-id> --json`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openLocalStore(deps)
			if err != nil {
				return localExitError(deps, "gates.results", err)
			}
			defer store.Close()
			selection, err := localSelection(cmd.Context(), deps, store)
			if err != nil {
				return localExitError(deps, "gates.results", err)
			}
			runs, err := store.ListReadinessRuns(cmd.Context(), selection.Workspace.ID, args[0])
			if err != nil {
				return localExitError(deps, "gates.results", err)
			}
			items := []map[string]any{}
			if len(runs) > 0 {
				items = localReadinessEvaluations(runs[0].Checks)
			}
			result := map[string]any{"items": items}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("gates.results", result)
				return nil
			}
			return printReadinessItems(deps, result)
		},
	}
}

func printReadinessGates(deps *Deps, evaluations []map[string]any) {
	if len(evaluations) == 0 {
		return
	}
	for _, evaluation := range evaluations {
		gate := strings.TrimSpace(fmt.Sprint(evaluation["gate"]))
		state := strings.TrimSpace(fmt.Sprint(evaluation["state"]))
		line := fmt.Sprintf("  [%s] %s", state, gate)
		// A dispatched-but-unanswered task includes the result trust field.
		// Printing it before an answer would claim an attestation nobody has made.
		if trust := strings.TrimSpace(fmt.Sprint(evaluation["trust"])); trust != "" && trust != "<nil>" && state != "not_run" {
			line += styled(deps, output.StyleMuted, " ("+trust+")")
		}
		if hint := strings.TrimSpace(fmt.Sprint(evaluation["hint"])); hint != "" && hint != "<nil>" {
			line += " — " + hint
		}
		fmt.Fprintln(deps.Stdout, line)
	}
}

func localReadinessEvaluations(checks map[string]any) []map[string]any {
	keys := make([]string, 0, len(checks))
	for key := range checks {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	evaluations := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		check, _ := checks[key].(map[string]any)
		evaluation := map[string]any{"gate": key}
		for _, field := range []string{"gate", "state", "hint", "judge_model", "trust", "evidence"} {
			if value, ok := check[field]; ok {
				evaluation[field] = value
			}
		}
		evaluations = append(evaluations, evaluation)
	}
	return evaluations
}

func printReadinessItems(deps *Deps, result map[string]any) error {
	items, _ := result["items"].([]any)
	if len(items) == 0 {
		if localItems, ok := result["items"].([]map[string]any); ok {
			items = make([]any, len(localItems))
			for i := range localItems {
				items[i] = localItems[i]
			}
		}
	}
	if len(items) == 0 {
		fmt.Fprintln(deps.Stdout, notice(deps, output.StyleInfo, "Notice", "No stored readiness results."))
		return nil
	}
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		gate, _ := item["gate"].(string)
		state, _ := item["state"].(string)
		hint, _ := item["hint"].(string)
		if hint == "" {
			fmt.Fprintf(deps.Stdout, "%s\t%s\n", styled(deps, output.StyleBold, gate), styledStatus(deps, state))
		} else {
			fmt.Fprintf(deps.Stdout, "%s\t%s\t%s\n", styled(deps, output.StyleBold, gate), styledStatus(deps, state), hint)
		}
	}
	return nil
}

// printJSON writes v as indented JSON to deps.Stdout.
func printJSON(deps *Deps, v any) error {
	enc := json.NewEncoder(deps.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
