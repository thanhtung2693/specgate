package command

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

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

// printReadinessGates names every gate, its state, and its trust origin. An
// aggregate word alone cannot be judged: "warn" reads the same whether one gate
// raised a soft hint or four are still waiting on a result.
