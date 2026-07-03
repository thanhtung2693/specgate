package command

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/client"
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
	gates.AddCommand(newGatesTasksCmd(deps))
	root.AddCommand(gates)
}

// specgate gates run [work-ref]
func newGatesRunCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "run [ref]",
		Short: "Trigger LLM quality gates for a work item",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := resolveRef(cmd, args, deps)
			if err != nil {
				return err
			}

			work, err := deps.Client.ResolveWorkRef(cmd.Context(), ref)
			if err != nil {
				code := deps.Printer.Error("gates.run", mapWorkRefError("gates.run", ref, err))
				return &output.ExitError{Code: code, Err: err}
			}

			proceed, err := requireConfirm(deps,
				fmt.Sprintf("Run LLM quality gates for %s?", work.ChangeRequestKey))
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			result, err := deps.Client.RunLLMGates(cmd.Context(), work.ChangeRequestID)
			if err != nil {
				code := deps.Printer.Error("gates.run", mapAPIError("gates.run", err))
				return &output.ExitError{Code: code, Err: err}
			}

			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("gates.run", result)
				return nil
			}

			if status, ok := result["status"].(string); ok {
				fmt.Fprintf(deps.Stdout, "Gates %s for %s\n", status, work.ChangeRequestKey)
			} else {
				fmt.Fprintf(deps.Stdout, "Gates triggered for %s\n", work.ChangeRequestKey)
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
				code := deps.Printer.Error("gates.status", mapWorkRefError("gates.status", ref, err))
				return &output.ExitError{Code: code, Err: err}
			}

			gs, err := deps.Client.GatesStatus(cmd.Context(), work.ChangeRequestID)
			if err != nil {
				code := deps.Printer.Error("gates.status", mapAPIError("gates.status", err))
				return &output.ExitError{Code: code, Err: err}
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
				code := deps.Printer.Error("gates.history", mapWorkRefError("gates.history", ref, err))
				return &output.ExitError{Code: code, Err: err}
			}

			gh, err := deps.Client.GateHistory(cmd.Context(), work.ChangeRequestID, gateFilter, limit)
			if err != nil {
				code := deps.Printer.Error("gates.history", mapAPIError("gates.history", err))
				return &output.ExitError{Code: code, Err: err}
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
	return &cobra.Command{
		Use:   "check <artifact-id>",
		Short: "Run artifact-scoped quality/readiness checks",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			commandName := "gates.check"
			result, err := deps.Client.RunArtifactReadiness(cmd.Context(), args[0])
			if err != nil {
				code := deps.Printer.Error(commandName, mapAPIError(commandName, err))
				return &output.ExitError{Code: code, Err: err}
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success(commandName, result)
				return nil
			}
			if agg, ok := result["aggregate"].(string); ok {
				fmt.Fprintf(deps.Stdout, "Spec quality: %s\n", agg)
			} else {
				fmt.Fprintln(deps.Stdout, "Spec quality check submitted")
			}
			return nil
		},
	}
}

// specgate gates tasks list|show|submit-result|preview|dispatch
func newGatesTasksCmd(deps *Deps) *cobra.Command {
	tasks := &cobra.Command{
		Use:   "tasks",
		Short: "Artifact gate-task operations",
	}

	listCmd := &cobra.Command{
		Use:   "list <artifact-id>",
		Short: "List pending gate tasks for an artifact",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			tasks, err := deps.Client.ListGateTasks(cmd.Context(), args[0])
			if err != nil {
				code := deps.Printer.Error("gates.tasks.list", mapAPIError("gates.tasks.list", err))
				return &output.ExitError{Code: code, Err: err}
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("gates.tasks.list", map[string]any{"tasks": tasks})
				return nil
			}
			if len(tasks) == 0 {
				fmt.Fprintln(deps.Stdout, "No pending gate tasks.")
				return nil
			}
			for _, t := range tasks {
				fmt.Fprintf(deps.Stdout, "%s\t%s\t%s\n", t.TaskID, t.GateKey, t.Executor)
			}
			return nil
		},
	}

	showCmd := &cobra.Command{
		Use:   "show <task-id>",
		Short: "Show a gate task with Skill content",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			task, err := deps.Client.GetGateTask(cmd.Context(), args[0])
			if err != nil {
				code := deps.Printer.Error("gates.tasks.show", mapAPIError("gates.tasks.show", err))
				return &output.ExitError{Code: code, Err: err}
			}
			return printJSON(deps, task)
		},
	}

	submitResultCmd := &cobra.Command{
		Use:   "submit-result <task-id>",
		Short: "Submit a GateResult from a JSON file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			filePath, _ := cmd.Flags().GetString("file")
			raw, err := os.ReadFile(filePath)
			if err != nil {
				return fmt.Errorf("read result file: %w", err)
			}
			var body any
			if err := json.Unmarshal(raw, &body); err != nil {
				return fmt.Errorf("parse result JSON: %w", err)
			}
			result, err := deps.Client.SubmitGateResult(cmd.Context(), args[0], body)
			if err != nil {
				code := deps.Printer.Error("gates.tasks.submit-result", mapAPIError("gates.tasks.submit-result", err))
				return &output.ExitError{Code: code, Err: err}
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("gates.tasks.submit-result", result)
				return nil
			}
			fmt.Fprintf(deps.Stdout, "Result %s submitted (trust: %s, state: %s)\n", result.ResultID, result.Trust, result.State)
			return nil
		},
	}
	submitResultCmd.Flags().String("file", "", "Path to result JSON file (required)")
	_ = submitResultCmd.MarkFlagRequired("file")

	previewCmd := &cobra.Command{
		Use:   "preview <artifact-id>",
		Short: "Show expected gate tasks for an artifact based on its stored profile snapshot",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := deps.Client.GatePreview(cmd.Context(), args[0])
			if err != nil {
				code := deps.Printer.Error("gates.tasks.preview", mapAPIError("gates.tasks.preview", err))
				return &output.ExitError{Code: code, Err: err}
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("gates.tasks.preview", result)
				return nil
			}
			artifactID, _ := result["artifact_id"].(string)
			if artifactID != "" {
				fmt.Fprintf(deps.Stdout, "Gate preview for artifact: %s\n", artifactID)
			}
			tasks, _ := result["preview_tasks"].([]any)
			if len(tasks) == 0 {
				fmt.Fprintln(deps.Stdout, "No expected gate tasks for this artifact.")
				return nil
			}
			for _, t := range tasks {
				row, ok := t.(map[string]any)
				if !ok {
					continue
				}
				fmt.Fprintf(deps.Stdout, "%s\t%s\t%s\n", row["gate_key"], row["executor"], row["note"])
			}
			return nil
		},
	}

	dispatchCmd := &cobra.Command{
		Use:   "dispatch <artifact-id>",
		Short: "Dispatch ide_agent gate tasks for an artifact's enabled gates",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := deps.Client.DispatchGateTasks(cmd.Context(), args[0])
			if err != nil {
				code := deps.Printer.Error("gates.tasks.dispatch", mapAPIError("gates.tasks.dispatch", err))
				return &output.ExitError{Code: code, Err: err}
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("gates.tasks.dispatch", result)
				return nil
			}
			fmt.Fprintf(deps.Stdout, "Dispatched %d gate task(s) for artifact %s (%d already pending).\n",
				len(result.CreatedTaskIDs), result.ArtifactID, len(result.SkippedGateKeys))
			return nil
		},
	}

	tasks.AddCommand(listCmd, showCmd, submitResultCmd, previewCmd, dispatchCmd)
	return tasks
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
	fmt.Fprintln(deps.Stdout, styled(deps, output.StyleInfo, "Quality Gates"))
	fmt.Fprintln(deps.Stdout, visualRule(deps))
	fmt.Fprintf(deps.Stdout, "%s %s %s %d/%d pass (%d%%)\n\n",
		coloredBullet(deps, output.StyleSuccess),
		styled(deps, output.StyleMuted, "Progress:"),
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
	fmt.Fprintln(deps.Stdout, styled(deps, output.StyleInfo, "Quality Gate History"))
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
