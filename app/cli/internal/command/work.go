package command

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

// ErrInputRequired is returned when interactive input is needed but --no-input is set.
var ErrInputRequired = errors.New("interactive input required: re-run without --no-input")

// ErrWorkRefRequired is returned when a command needs a work reference and none
// was passed. Machine callers imply --no-input with --json, so telling them to
// drop --no-input names a flag they never set and an action they cannot take;
// the missing argument is the thing to report.
var ErrWorkRefRequired = errors.New("work reference required: pass it as the first argument, for example `specgate work list --json` to find one")

func registerWorkCommands(root *cobra.Command, deps *Deps) {
	work := &cobra.Command{
		Use:   "work",
		Short: "Manage and inspect work items",
	}
	work.AddCommand(newWorkListCmd(deps))
	work.AddCommand(newWorkShowCmd(deps))
	work.AddCommand(newWorkContextCmd(deps))
	work.AddCommand(newWorkVerificationCmd(deps))
	work.AddCommand(newWorkCheckpointCmd(deps))
	work.AddCommand(newWorkResumeCmd(deps))
	work.AddCommand(newWorkCreateQuickCmd(deps))
	work.AddCommand(newWorkCreateCmd(deps))
	root.AddCommand(work)
}

// specgate work list
func newWorkListCmd(deps *Deps) *cobra.Command {
	var (
		phase string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List work items needing attention, or enumerate a phase with --phase",
		RunE: func(cmd *cobra.Command, _ []string) error {

			store, err := openLocalStore(deps)
			if err != nil {
				return localExitError(deps, "work.list", err)
			}
			defer store.Close()
			selection, err := localSelection(cmd.Context(), deps, store)
			if err != nil {
				return localExitError(deps, "work.list", err)
			}
			items, err := store.ListWork(cmd.Context(), selection.Workspace.ID)
			if err != nil {
				return localExitError(deps, "work.list", err)
			}
			if strings.TrimSpace(phase) != "" {
				wanted := workListPhases(phase)
				filtered := items[:0]
				for _, item := range items {
					if wanted[strings.ToLower(strings.TrimSpace(item.Phase))] {
						filtered = append(filtered, item)
					}
				}
				items = filtered
			}
			views, err := localWorkListViews(cmd.Context(), store, selection.Workspace.ID, items)
			if err != nil {
				return localExitError(deps, "work.list", err)
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("work.list", map[string]any{"items": views})
				return nil
			}
			for _, item := range views {
				fmt.Fprintf(deps.Stdout, "%s  [%s / %s -> %s]  %s\n", item["key"], item["phase"], item["change_state"], item["next_actor"], item["title"])
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&phase, "phase", "", "Enumerate work items in these lifecycle phases (comma-separated, e.g. ready) instead of the attention queue")
	return cmd
}

func workListPhases(phaseCSV string) map[string]bool {
	wanted := map[string]bool{}
	for _, p := range strings.Split(phaseCSV, ",") {
		if p = strings.ToLower(strings.TrimSpace(p)); p != "" {
			wanted[p] = true
		}
	}
	return wanted
}

func localWorkListViews(ctx context.Context, store *local.Store, workspaceID string, items []local.WorkItem) ([]map[string]any, error) {
	views := make([]map[string]any, 0, len(items))
	for _, item := range items {
		status, _, err := deriveLocalChangeStatusFromStore(ctx, store, workspaceID, item)
		if err != nil {
			return nil, err
		}
		view := localWorkView(item)
		view["change_state"] = status.State
		view["next_actor"] = status.NextActor
		view["next_command"] = status.NextCommand
		views = append(views, view)
	}
	return views, nil
}

// specgate work show [ref]
func newWorkShowCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "show [ref]",
		Short: "Show details for a work item",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return localExitError(deps, "work.show", ErrWorkRefRequired)
			}
			store, err := openLocalStore(deps)
			if err != nil {
				return localExitError(deps, "work.show", err)
			}
			defer store.Close()
			selection, err := localSelection(cmd.Context(), deps, store)
			if err != nil {
				return localExitError(deps, "work.show", err)
			}
			work, err := store.GetWork(cmd.Context(), selection.Workspace.ID, args[0])
			if err != nil {
				return localExitError(deps, "work.show", err)
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("work.show", localWorkView(work))
				return nil
			}
			fmt.Fprintf(deps.Stdout, "%s  %s\n%s %s\n", styled(deps, output.StyleBold, work.Key), work.Title, label(deps, "Phase:"), styledStatus(deps, work.Phase))
			for _, criterion := range work.AcceptanceCriteria {
				fmt.Fprintf(deps.Stdout, "  %s %s\n", criterionMarker(deps, false), criterion)
			}
			return nil
		},
	}
}

// specgate work context [ref]
func newWorkContextCmd(deps *Deps) *cobra.Command {
	var summary bool
	var document string
	var role string
	cmd := &cobra.Command{
		Use:   "context [ref]",
		Short: "Fetch the context pack for a work item",
		Long: `Fetch the full derived Context Pack used as the implementation contract.

Artifact-backed work contains the approved artifact context. Quick work is
derived from its persisted intent and acceptance criteria. Unlike
summary-oriented commands, this intentionally returns complete governed content.
IDE agents should read it before editing implementation files.`,
		Example: `  specgate work context LOCAL-123 --json`,
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if summary || cmd.Flags().Changed("document") || cmd.Flags().Changed("role") {

				if len(args) == 0 {
					return localExitError(deps, "work.context", ErrWorkRefRequired)
				}
				if cmd.Flags().Changed("document") && document == "" {
					return completionValidationError(deps, "work.context", "--document requires an exact indexed path")
				}
				if cmd.Flags().Changed("role") && !cmd.Flags().Changed("document") {
					return completionValidationError(deps, "work.context", "--role requires --document")
				}
				return runLocalContextProjection(cmd, deps, args[0], document, role)
			}
			if len(args) == 0 {
				return localExitError(deps, "work.context", ErrWorkRefRequired)
			}
			store, err := openLocalStore(deps)
			if err != nil {
				return localExitError(deps, "work.context", err)
			}
			defer store.Close()
			selection, err := localSelection(cmd.Context(), deps, store)
			if err != nil {
				return localExitError(deps, "work.context", err)
			}
			pack, err := store.ContextPack(cmd.Context(), selection.Workspace.ID, args[0])
			if err != nil {
				return localExitError(deps, "work.context", err)
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("work.context", map[string]any{"work_id": pack.WorkID, "work_key": pack.WorkKey, "context_digest": pack.Digest, "markdown": pack.Markdown})
				return nil
			}
			fmt.Fprint(deps.Stdout, pack.Markdown)
			return nil
		},
	}

	cmd.Flags().BoolVar(&summary, "summary", false, "Return scope, criteria and pinned document index without document bodies (Local)")
	cmd.Flags().StringVar(&document, "document", "", "Read one exact path from the pinned document index (Local)")
	cmd.Flags().StringVar(&role, "role", "", "Select the artifact role when --document path occurs in multiple roles (Local)")
	cmd.MarkFlagsMutuallyExclusive("summary", "document")
	return cmd
}

// specgate work create --feature <key-or-id> --title <t> [--description <d>] --ac <c> [--ac <c>]...
//
// Artifact-backed sibling of create-quick: creates a feature-backed work item bound
// to the feature's approved canonical spec (POST /api/v1/work-items/create).
