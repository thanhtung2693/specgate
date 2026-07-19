package command

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func registerGatesCommands(root *cobra.Command, deps *Deps) {
	gates := &cobra.Command{
		Use:   "gates",
		Short: "Manage and inspect quality gates",
	}
	gates.AddCommand(newGatesRunCmd(deps))
	gates.AddCommand(newGatesStatusCmd(deps))
	gates.AddCommand(newGatesHistoryCmd(deps))
	gates.AddCommand(newGatesCheckCmd(deps))
	gates.AddCommand(newGatesResultsCmd(deps))
	gates.AddCommand(newGatesTasksCmd(deps))
	root.AddCommand(gates)
}

// specgate gates run [work-ref]
func newGatesRunCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "run [ref]",
		Short: "Trigger quality gates for a work item",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := resolveRef(cmd, args, deps)
			if err != nil {
				return err
			}

			work, err := deps.Client.ResolveWorkRef(cmd.Context(), ref)
			if err != nil {
				code := deps.Printer.Error("gates.run", mapWorkRefError(ref, err))
				return &output.ExitError{Code: code, Err: err}
			}

			proceed, err := requireConfirm(deps,
				fmt.Sprintf("Run quality gates for %s?", work.ChangeRequestKey))
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			result, err := deps.Client.RunLLMGates(cmd.Context(), work.ChangeRequestID)
			if err != nil {
				return apiExitError(deps, "gates.run", err)
			}

			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("gates.run", result)
				return nil
			}

			if status, ok := result["status"].(string); ok {
				fmt.Fprintf(deps.Stdout, "%s %s for %s\n", title(deps, "Gates"), styledStatus(deps, status), styled(deps, output.StyleBold, work.ChangeRequestKey))
			} else {
				fmt.Fprintf(deps.Stdout, "%s triggered for %s\n", title(deps, "Gates"), styled(deps, output.StyleBold, work.ChangeRequestKey))
			}
			return nil
		},
	}
}

// specgate gates status [work-ref]
func newGatesStatusCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "status [ref]",
		Short: "Show current gate state for a work item",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := resolveRef(cmd, args, deps)
			if err != nil {
				return err
			}

			work, err := deps.Client.ResolveWorkRef(cmd.Context(), ref)
			if err != nil {
				code := deps.Printer.Error("gates.status", mapWorkRefError(ref, err))
				return &output.ExitError{Code: code, Err: err}
			}

			gs, err := deps.Client.GatesStatus(cmd.Context(), work.ChangeRequestID)
			if err != nil {
				return apiExitError(deps, "gates.status", err)
			}

			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("gates.status", gs)
				return nil
			}

			if len(gs.Gates) == 0 {
				fmt.Fprintln(deps.Stdout, "No gate runs found.")
				return nil
			}
			if humanVisuals(deps) {
				printGatesStatusDashboard(deps, gs.Gates)
				return nil
			}
			for _, g := range gs.Gates {
				if g.Hint != "" {
					fmt.Fprintf(deps.Stdout, "%-30s  %s  %s\n", g.Gate, styledStatusPadded(deps, g.State, 15), g.Hint)
				} else {
					fmt.Fprintf(deps.Stdout, "%-30s  %s\n", g.Gate, styledStatus(deps, g.State))
				}
			}
			return nil
		},
	}
}

// specgate gates history [work-ref] [--gate <gate>] [--limit <n>]
func newGatesHistoryCmd(deps *Deps) *cobra.Command {
	var gateFilter string
	var limit int

	cmd := &cobra.Command{
		Use:   "history [ref]",
		Short: "Show gate run history for a work item",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := resolveRef(cmd, args, deps)
			if err != nil {
				return err
			}

			work, err := deps.Client.ResolveWorkRef(cmd.Context(), ref)
			if err != nil {
				code := deps.Printer.Error("gates.history", mapWorkRefError(ref, err))
				return &output.ExitError{Code: code, Err: err}
			}

			gh, err := deps.Client.GateHistory(cmd.Context(), work.ChangeRequestID, gateFilter, limit)
			if err != nil {
				return apiExitError(deps, "gates.history", err)
			}

			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("gates.history", gh)
				return nil
			}

			if len(gh.Runs) == 0 {
				fmt.Fprintln(deps.Stdout, "No gate runs found.")
				return nil
			}
			if humanVisuals(deps) {
				printGateHistoryDashboard(deps, gh.Runs)
				return nil
			}
			for _, r := range gh.Runs {
				id := r.GateRunID
				if len(id) > 8 {
					id = id[:8]
				}
				if r.Hint != "" {
					fmt.Fprintf(deps.Stdout, "%s  %-30s  %s  %s  %s\n", id, r.Gate, styledStatusPadded(deps, r.State, 15), r.CreatedAt, r.Hint)
				} else {
					fmt.Fprintf(deps.Stdout, "%s  %-30s  %s  %s\n", id, r.Gate, styledStatusPadded(deps, r.State, 15), r.CreatedAt)
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&gateFilter, "gate", "", "Filter history to a specific gate name")
	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum number of runs to return (default 20 server-side)")
	return cmd
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
			if deps.Topology == config.ModeLocal {
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
				if len(run.Dispatch.PendingTaskIDs) > 0 {
					fmt.Fprintf(deps.Stdout, "%s %d semantic checks need your IDE agent.\n", styled(deps, output.StyleWarning, "No external model configured —"), len(run.Dispatch.PendingTaskIDs))
					fmt.Fprintln(deps.Stdout, nextStep(deps, "inspect the rubrics with", "specgate gates tasks list "+args[0]))
				}
				return nil
			}
			result, err := deps.Client.RunArtifactReadiness(cmd.Context(), args[0])
			if err != nil {
				return apiExitError(deps, commandName, err)
			}
			if deps.Printer.Mode() == output.ModeJSON {
				if summary {
					result = artifactReadinessSummary(args[0], result)
				}
				deps.Printer.Success(commandName, result)
				return nil
			}
			if agg, ok := result["aggregate"].(string); ok {
				fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "Spec quality:"), styledStatus(deps, agg))
			} else {
				fmt.Fprintln(deps.Stdout, styled(deps, output.StyleSuccess, "Spec quality check submitted"))
			}
			// No platform model: the server dispatched model-judged gates as
			// ide_agent tasks instead of evaluating them. Say so — otherwise
			// "not_run" reads as a dead end.
			if disp, ok := result["dispatched_to_ide_agent"].(map[string]any); ok {
				ids, _ := disp["pending_task_ids"].([]any)
				if len(ids) == 0 {
					ids, _ = disp["created_task_ids"].([]any)
				}
				if len(ids) > 0 {
					fmt.Fprintf(deps.Stdout, "%s %d gate task(s) dispatched to the IDE agent.\n", styled(deps, output.StyleWarning, "No platform model configured —"), len(ids))
					fmt.Fprintln(deps.Stdout, nextStep(deps, "inspect the rubrics with", "specgate gates tasks list "+args[0]))
				}
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
			if deps.Topology == config.ModeLocal {
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
			}
			result, err := deps.Client.ListArtifactReadinessRuns(cmd.Context(), args[0])
			if err != nil {
				return apiExitError(deps, "gates.results", err)
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("gates.results", result)
				return nil
			}
			return printReadinessItems(deps, result)
		},
	}
}

// specgate gates tasks list|show|submit-result|dispatch
func newGatesTasksCmd(deps *Deps) *cobra.Command {
	tasks := &cobra.Command{
		Use:   "tasks",
		Short: "Artifact gate-task operations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	listCmd := &cobra.Command{
		Use:   "list <artifact-id>",
		Short: "List pending gate tasks for an artifact",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if deps.Topology == config.ModeLocal {
				store, err := openLocalStore(deps)
				if err != nil {
					return localExitError(deps, "gates.tasks.list", err)
				}
				defer store.Close()
				selection, err := localSelection(cmd.Context(), deps, store)
				if err != nil {
					return localExitError(deps, "gates.tasks.list", err)
				}
				localTasks, err := store.ListGateTasks(cmd.Context(), selection.Workspace.ID, args[0])
				if err != nil {
					return localExitError(deps, "gates.tasks.list", err)
				}
				tasks := localGateTasks(localTasks)
				if deps.Printer.Mode() == output.ModeJSON {
					deps.Printer.Success("gates.tasks.list", map[string]any{"tasks": tasks})
					return nil
				}
				if len(tasks) == 0 {
					fmt.Fprintln(deps.Stdout, notice(deps, output.StyleInfo, "Notice", "No pending gate tasks."))
					return nil
				}
				for _, task := range tasks {
					fmt.Fprintf(deps.Stdout, "%s\t%s\t%s\n", styled(deps, output.StyleBold, task.TaskID), task.GateKey, task.Executor)
				}
				return nil
			}
			workspaceID, err := currentWorkspaceID(cmd.Context(), deps)
			if err != nil {
				return apiExitError(deps, "gates.tasks.list", err)
			}
			if workspaceID == "" {
				return apiExitError(deps, "gates.tasks.list", fmt.Errorf("select a workspace first"))
			}
			tasks, err := deps.Client.ListGateTasks(client.WithWorkspace(cmd.Context(), workspaceID), args[0])
			if err != nil {
				return apiExitError(deps, "gates.tasks.list", err)
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("gates.tasks.list", map[string]any{"tasks": tasks})
				return nil
			}
			if len(tasks) == 0 {
				fmt.Fprintln(deps.Stdout, notice(deps, output.StyleInfo, "Notice", "No pending gate tasks."))
				return nil
			}
			for _, t := range tasks {
				fmt.Fprintf(deps.Stdout, "%s\t%s\t%s\n", styled(deps, output.StyleBold, t.TaskID), t.GateKey, t.Executor)
			}
			return nil
		},
	}

	showCmd := &cobra.Command{
		Use:   "show <task-id>",
		Short: "Show a gate task with Skill content",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if deps.Topology == config.ModeLocal {
				store, err := openLocalStore(deps)
				if err != nil {
					return localExitError(deps, "gates.tasks.show", err)
				}
				defer store.Close()
				selection, err := localSelection(cmd.Context(), deps, store)
				if err != nil {
					return localExitError(deps, "gates.tasks.show", err)
				}
				task, err := store.GetGateTask(cmd.Context(), selection.Workspace.ID, args[0])
				if err != nil {
					return localExitError(deps, "gates.tasks.show", err)
				}
				result := localGateTask(task)
				if deps.Printer.Mode() == output.ModeJSON {
					deps.Printer.Success("gates.tasks.show", result)
					return nil
				}
				return printJSON(deps, result)
			}
			workspaceID, err := currentWorkspaceID(cmd.Context(), deps)
			if err != nil {
				return apiExitError(deps, "gates.tasks.show", err)
			}
			if workspaceID == "" {
				return apiExitError(deps, "gates.tasks.show", fmt.Errorf("select a workspace first"))
			}
			task, err := deps.Client.GetGateTask(client.WithWorkspace(cmd.Context(), workspaceID), args[0])
			if err != nil {
				return apiExitError(deps, "gates.tasks.show", err)
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("gates.tasks.show", task)
				return nil
			}
			return printJSON(deps, task)
		},
	}

	submitResultCmd := &cobra.Command{
		Use:   "submit-result <task-id>",
		Short: "Submit a GateResult from a JSON file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if deps.Topology == config.ModeLocal {
				store, err := openLocalStore(deps)
				if err != nil {
					return localExitError(deps, "gates.tasks.submit-result", err)
				}
				defer store.Close()
				selection, err := localSelection(cmd.Context(), deps, store)
				if err != nil {
					return localExitError(deps, "gates.tasks.submit-result", err)
				}
				filePath, _ := cmd.Flags().GetString("file")
				body, err := readJSONBodyFile(deps, "gates.tasks.submit-result", filePath)
				if err != nil {
					return err
				}
				raw, err := json.Marshal(body)
				if err != nil {
					return localExitError(deps, "gates.tasks.submit-result", err)
				}
				var input local.GateResultInput
				if err := json.Unmarshal(raw, &input); err != nil {
					return localExitError(deps, "gates.tasks.submit-result", local.ErrGateTaskInvalid)
				}
				result, err := store.SubmitGateResult(cmd.Context(), selection.Workspace.ID, args[0], input)
				if err != nil {
					return localExitError(deps, "gates.tasks.submit-result", err)
				}
				if deps.Printer.Mode() == output.ModeJSON {
					deps.Printer.Success("gates.tasks.submit-result", result)
					return nil
				}
				fmt.Fprintf(deps.Stdout, "%s %s (%s %s, %s %s)\n", styled(deps, output.StyleSuccess, "Result submitted:"), styled(deps, output.StyleBold, result.ResultID), label(deps, "trust:"), result.Trust, label(deps, "state:"), styledStatus(deps, result.State))
				return nil
			}
			workspaceID, err := currentWorkspaceID(cmd.Context(), deps)
			if err != nil {
				return apiExitError(deps, "gates.tasks.submit-result", err)
			}
			if workspaceID == "" {
				return apiExitError(deps, "gates.tasks.submit-result", fmt.Errorf("select a workspace first"))
			}
			filePath, _ := cmd.Flags().GetString("file")
			body, err := readJSONBodyFile(deps, "gates.tasks.submit-result", filePath)
			if err != nil {
				return err
			}
			result, err := deps.Client.SubmitGateResult(client.WithWorkspace(cmd.Context(), workspaceID), args[0], body)
			if err != nil {
				return apiExitError(deps, "gates.tasks.submit-result", err)
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("gates.tasks.submit-result", result)
				return nil
			}
			fmt.Fprintf(deps.Stdout, "%s %s (%s %s, %s %s)\n", styled(deps, output.StyleSuccess, "Result submitted:"), styled(deps, output.StyleBold, result.ResultID), label(deps, "trust:"), result.Trust, label(deps, "state:"), styledStatus(deps, result.State))
			return nil
		},
	}
	submitResultCmd.Flags().String("file", "", "Path to result JSON file (required)")
	_ = submitResultCmd.MarkFlagRequired("file")

	dispatchCmd := &cobra.Command{
		Use:   "dispatch <artifact-id>",
		Short: "Dispatch ide_agent gate tasks for an artifact's enabled gates",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if deps.Topology == config.ModeLocal {
				store, err := openLocalStore(deps)
				if err != nil {
					return localExitError(deps, "gates.tasks.dispatch", err)
				}
				defer store.Close()
				selection, err := localSelection(cmd.Context(), deps, store)
				if err != nil {
					return localExitError(deps, "gates.tasks.dispatch", err)
				}
				localResult, err := store.DispatchGateTasks(cmd.Context(), selection.Workspace.ID, args[0])
				if err != nil {
					return localExitError(deps, "gates.tasks.dispatch", err)
				}
				result := localDispatchResult(localResult)
				if deps.Printer.Mode() == output.ModeJSON {
					deps.Printer.Success("gates.tasks.dispatch", result)
					return nil
				}
				fmt.Fprintf(deps.Stdout, "%s %d gate task(s) for artifact %s (%d pending).\n", styled(deps, output.StyleSuccess, "Dispatched"), len(result.CreatedTaskIDs), styled(deps, output.StyleBold, result.ArtifactID), len(result.PendingTaskIDs))
				return nil
			}
			workspaceID, err := currentWorkspaceID(cmd.Context(), deps)
			if err != nil {
				return apiExitError(deps, "gates.tasks.dispatch", err)
			}
			if workspaceID == "" {
				return apiExitError(deps, "gates.tasks.dispatch", fmt.Errorf("select a workspace first"))
			}
			result, err := deps.Client.DispatchGateTasks(client.WithWorkspace(cmd.Context(), workspaceID), args[0])
			if err != nil {
				return apiExitError(deps, "gates.tasks.dispatch", err)
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("gates.tasks.dispatch", result)
				return nil
			}
			fmt.Fprintf(deps.Stdout, "%s %d gate task(s) for artifact %s (%d pending).\n",
				styled(deps, output.StyleSuccess, "Dispatched"), len(result.CreatedTaskIDs), styled(deps, output.StyleBold, result.ArtifactID), len(result.PendingTaskIDs))
			return nil
		},
	}

	tasks.AddCommand(listCmd, showCmd, submitResultCmd, dispatchCmd)
	return tasks
}

func localGateTask(task local.GateTask) client.GateTask {
	return client.GateTask{TaskID: task.TaskID, WorkspaceID: task.WorkspaceID, GateKey: task.GateKey, GateVersion: task.GateVersion, GateDigest: task.GateDigest, ArtifactID: task.ArtifactID, ArtifactDigest: task.ArtifactDigest, PolicyDigest: task.PolicyDigest, Executor: task.Executor, SkillContent: task.SkillContent, ExpiresAt: task.ExpiresAt}
}

func localGateTasks(tasks []local.GateTask) []client.GateTask {
	result := make([]client.GateTask, 0, len(tasks))
	for _, task := range tasks {
		result = append(result, localGateTask(task))
	}
	return result
}

func localDispatchResult(result local.DispatchGateTasksResult) client.DispatchGateTasksResult {
	return client.DispatchGateTasksResult{ArtifactID: result.ArtifactID, CreatedTaskIDs: result.CreatedTaskIDs, SkippedGateKeys: result.SkippedGateKeys, PendingTaskIDs: result.PendingTaskIDs}
}

func localDispatchPayload(result local.DispatchGateTasksResult) map[string]any {
	return map[string]any{"artifact_id": result.ArtifactID, "created_task_ids": result.CreatedTaskIDs, "skipped_gate_keys": result.SkippedGateKeys, "pending_task_ids": result.PendingTaskIDs}
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

func printGatesStatusDashboard(deps *Deps, gates []client.GateSummary) {
	passed := 0
	for _, g := range gates {
		if passingStatus(g.State) {
			passed++
		}
	}
	fmt.Fprintln(deps.Stdout, title(deps, "Quality Gates"))
	fmt.Fprintln(deps.Stdout, visualRule(deps))
	fmt.Fprintf(deps.Stdout, "%s %s %s %d/%d pass (%d%%)\n\n",
		coloredBullet(deps, output.StyleSuccess),
		label(deps, "Progress:"),
		progressBar(deps, passed, len(gates), 18),
		passed,
		len(gates),
		percent(passed, len(gates)))
	for _, g := range gates {
		fmt.Fprintf(deps.Stdout, "  %s %-30s %s\n",
			statusIcon(deps, g.State),
			g.Gate,
			styledStatus(deps, g.State))
		if g.Hint != "" {
			fmt.Fprintf(deps.Stdout, "    %s\n", styled(deps, output.StyleMuted, g.Hint))
		}
	}
}

func printGateHistoryDashboard(deps *Deps, runs []client.GateRun) {
	fmt.Fprintln(deps.Stdout, title(deps, "Quality Gate History"))
	fmt.Fprintln(deps.Stdout, visualRule(deps))
	for _, r := range runs {
		id := r.GateRunID
		if len(id) > 8 {
			id = id[:8]
		}
		fmt.Fprintf(deps.Stdout, "  %s %s  %-30s %s  %s\n",
			statusIcon(deps, r.State),
			styled(deps, output.StyleMuted, id),
			r.Gate,
			styledStatus(deps, r.State),
			styled(deps, output.StyleMuted, r.CreatedAt))
		if r.Hint != "" {
			fmt.Fprintf(deps.Stdout, "    %s\n", styled(deps, output.StyleMuted, r.Hint))
		}
	}
}
