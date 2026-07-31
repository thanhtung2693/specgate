package command

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/config"
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
	work.AddCommand(newWorkArchiveCmd(deps))
	work.AddCommand(newWorkCreateQuickCmd(deps))
	work.AddCommand(newWorkCreateCmd(deps))
	work.AddCommand(newWorkPolicyCmd(deps))
	root.AddCommand(work)
}

// specgate work list
func newWorkListCmd(deps *Deps) *cobra.Command {
	var (
		allWorkspaces bool
		phase         string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List work items needing attention, or enumerate a phase with --phase",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if deps.Topology == config.ModeLocal {
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
					filtered := items[:0]
					for _, item := range items {
						if strings.EqualFold(item.Phase, strings.TrimSpace(phase)) {
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
			}
			if allWorkspaces && strings.TrimSpace(phase) != "" {
				code := deps.Printer.Error("work.list", output.ErrorPayload{
					Code:    "validation",
					Message: "--all-workspaces cannot be combined with --phase; phase discovery requires a selected workspace",
				})
				return &output.ExitError{Code: code}
			}
			workspaceID := ""
			unscoped := allWorkspaces
			ctx := cmd.Context()
			if !allWorkspaces {
				selection := currentWorkspaceSelection(deps)
				if selection.Source == config.WorkspaceSourceNone {
					payload := output.ErrorPayload{Code: "validation", Message: "select a workspace first with `specgate workspace select`, or use `work list --all-workspaces` for an explicit cross-workspace view"}
					code := deps.Printer.Error("work.list", payload)
					return &output.ExitError{Code: code}
				}
				var err error
				workspaceID, err = workspaceIDForSelection(cmd.Context(), deps, selection)
				if err != nil {
					return apiExitError(deps, "work.list", err)
				}
			} else {
				ctx = client.WithAllWorkspaces(ctx)
			}

			// --phase enumerates actual work items (with refs) so an agent or
			// human who did not create the work can discover what to pick up.
			// The attention queue (default) never lists pickup-ready items.
			if strings.TrimSpace(phase) != "" {
				cmd.SetContext(ctx)
				return runWorkListByPhase(cmd, deps, workspaceID, phase)
			}

			st, err := deps.Client.Status(ctx, workspaceID)
			if err != nil {
				return apiExitError(deps, "work.list", err)
			}

			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("work.list", map[string]any{
					"counts":          st.Counts,
					"needs_attention": st.NeedsAttention,
				})
				return nil
			}

			if len(st.NeedsAttention) == 0 {
				printNoWorkNeedsAttention(deps, st, unscoped)
				return nil
			}
			printAttentionSection(deps, st.NeedsAttention)
			return nil
		},
	}
	cmd.Flags().BoolVar(&allWorkspaces, "all-workspaces", false, "List work items across all workspaces")
	cmd.Flags().StringVar(&phase, "phase", "", "Enumerate work items in these lifecycle phases (comma-separated, e.g. ready) instead of the attention queue")
	return cmd
}

// runWorkListByPhase enumerates work items whose lifecycle phase matches any of
// the comma-separated phases, printing ref + phase + title for pickup.
func runWorkListByPhase(cmd *cobra.Command, deps *Deps, workspaceID, phaseCSV string) error {
	wanted := map[string]bool{}
	for _, p := range strings.Split(phaseCSV, ",") {
		if p = strings.ToLower(strings.TrimSpace(p)); p != "" {
			wanted[p] = true
		}
	}
	items, err := deps.Client.ListWorkItems(cmd.Context(), workspaceID)
	if err != nil {
		return apiExitError(deps, "work.list", err)
	}
	filtered := make([]client.WorkItemSummary, 0, len(items))
	for _, it := range items {
		if wanted[strings.ToLower(strings.TrimSpace(it.Phase))] {
			filtered = append(filtered, it)
		}
	}
	views, err := fullWorkListViews(cmd.Context(), deps, filtered)
	if err != nil {
		return apiExitError(deps, "work.list", err)
	}

	if deps.Printer.Mode() == output.ModeJSON {
		deps.Printer.Success("work.list", map[string]any{"items": views})
		return nil
	}
	if len(views) == 0 {
		fmt.Fprintf(deps.Stdout, "No work items in phase(s) %s.\n", phaseCSV)
		return nil
	}
	for _, it := range views {
		fmt.Fprintf(deps.Stdout, "%s  [%s / %s -> %s]  %s\n", styled(deps, output.StyleBold, it.Key), styledStatus(deps, it.Phase), it.ChangeState, it.NextActor, it.Title)
	}
	fmt.Fprintln(deps.Stdout, nextStep(deps, "Show details with", "specgate work show <ref>"))
	return nil
}

type workListItemView struct {
	client.WorkItemSummary
	ChangeState string `json:"change_state"`
	NextActor   string `json:"next_actor"`
	NextCommand string `json:"next_command"`
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

func fullWorkListViews(ctx context.Context, deps *Deps, items []client.WorkItemSummary) ([]workListItemView, error) {
	views := make([]workListItemView, 0, len(items))
	for _, item := range items {
		delivery, err := deps.Client.DeliveryStatus(ctx, item.ID, true)
		if err != nil {
			return nil, err
		}
		status := deriveFullChangeStatus(&client.ResolvedWork{
			ChangeRequestID:  item.ID,
			ChangeRequestKey: item.Key,
			Title:            item.Title,
			Phase:            item.Phase,
		}, delivery)
		views = append(views, workListItemView{
			WorkItemSummary: item,
			ChangeState:     status.State,
			NextActor:       status.NextActor,
			NextCommand:     status.NextCommand,
		})
	}
	return views, nil
}

func printNoWorkNeedsAttention(deps *Deps, st *client.GovernanceStatus, unscoped bool) {
	fmt.Fprintln(deps.Stdout, "No work items need attention.")
	if st.Counts.Total > 0 {
		fmt.Fprintf(deps.Stdout, "%d work item(s) are tracked in other phases: %s.\n", st.Counts.Total, phaseBreakdown(st.Counts))
		fmt.Fprintln(deps.Stdout, "Next: run `specgate status` for the board overview or `specgate work show <ref>` if you know the work item.")
		return
	}
	if unscoped {
		fmt.Fprintln(deps.Stdout, "Next: create a quick work item with `specgate work create-quick`.")
		return
	}
	fmt.Fprintln(deps.Stdout, "Next: try `specgate work list --all-workspaces` or create a quick work item with `specgate work create-quick`.")
}

// specgate work show [ref]
func newWorkShowCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "show [ref]",
		Short: "Show details for a work item",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if deps.Topology == config.ModeLocal {
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
			}
			ref, err := resolveRef(cmd, args, deps)
			if err != nil {
				return err
			}

			work, err := deps.Client.ResolveWorkRef(cmd.Context(), ref)
			if err != nil {
				code := deps.Printer.Error("work.show", mapWorkRefError(ref, err))
				return &output.ExitError{Code: code, Err: err}
			}

			// Best-effort readback in BOTH modes: the criteria are what delivery
			// review will judge. Coding agents consume --json, so the envelope
			// must carry them too; a fetch failure is reported, not swallowed.
			criteria, criteriaErr := deps.Client.ListAcceptanceCriteria(cmd.Context(), work.ChangeRequestID)
			if criteriaErr != nil {
				fmt.Fprintln(deps.Stderr, stderrNotice(deps, output.StyleWarning, "Warning", fmt.Sprintf("could not read acceptance criteria: %v", criteriaErr)))
			} else {
				delivery, deliveryErr := deps.Client.DeliveryStatus(cmd.Context(), work.ChangeRequestID, true)
				if deliveryErr != nil {
					fmt.Fprintln(deps.Stderr, stderrNotice(deps, output.StyleWarning, "Warning", fmt.Sprintf("could not read delivery evidence: %v", deliveryErr)))
				} else if delivery.Found {
					overlayAcceptanceCriteriaDone(criteria, delivery.PerCriterion)
				}
			}

			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("work.show", struct {
					*client.ResolvedWork
					AcceptanceCriteria []client.AcceptanceCriterion `json:"acceptance_criteria,omitempty"`
				}{work, criteria})
				return nil
			}

			fmt.Fprintf(deps.Stdout, "%s  %s\n", styled(deps, output.StyleBold, work.ChangeRequestKey), work.Title)
			fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "Phase:"), styledStatus(deps, work.Phase))
			if len(criteria) > 0 {
				fmt.Fprintln(deps.Stdout, title(deps, "Acceptance criteria:"))
				for _, criterion := range criteria {
					fmt.Fprintf(deps.Stdout, "  %s %s\n", criterionMarker(deps, criterion.Done), criterion.Text)
				}
			}
			return nil
		},
	}
}

func overlayAcceptanceCriteriaDone(criteria []client.AcceptanceCriterion, reviews []client.CriterionReview) {
	verdicts := make(map[string]string, len(reviews))
	for _, review := range reviews {
		verdicts[review.CriterionID] = strings.TrimSpace(review.Verdict)
	}
	for index := range criteria {
		if verdict, ok := verdicts[criteria[index].ID]; ok {
			criteria[index].Done = verdict == "met"
		}
	}
}

// specgate work context [ref]
func newWorkContextCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "context [ref]",
		Short: "Fetch the context pack for a work item",
		Long: `Fetch the full derived Context Pack used as the implementation contract.

Artifact-backed work contains the approved artifact context. Quick work is
derived from its persisted intent and acceptance criteria. Unlike
summary-oriented commands, this intentionally returns complete governed content.
IDE agents should read it before editing implementation files.`,
		Example: `  specgate work context CR-123 --json`,
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if deps.Topology == config.ModeLocal {
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
			}
			ref, err := resolveRef(cmd, args, deps)
			if err != nil {
				return err
			}

			// Resolve ref → change_request_id
			work, err := deps.Client.ResolveWorkRef(cmd.Context(), ref)
			if err != nil {
				code := deps.Printer.Error("work.context", mapWorkRefError(ref, err))
				return &output.ExitError{Code: code, Err: err}
			}

			cp, err := deps.Client.ContextPack(cmd.Context(), work.ChangeRequestID)
			if err != nil {
				return apiExitError(deps, "work.context", err)
			}

			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("work.context", cp)
				return nil
			}

			fmt.Fprint(deps.Stdout, cp.Markdown)
			if cp.Markdown != "" && cp.Markdown[len(cp.Markdown)-1] != '\n' {
				fmt.Fprintln(deps.Stdout)
			}
			return nil
		},
	}

	return cmd
}

// specgate work archive [ref...]
func newWorkArchiveCmd(deps *Deps) *cobra.Command {
	var reason string

	cmd := &cobra.Command{
		Use:   "archive [ref...]",
		Short: "Archive one or more work items",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			proceed, err := requireConfirm(deps, fmt.Sprintf("Archive %d work item(s)?", len(args)))
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			archived := make([]map[string]any, 0, len(args))
			for _, ref := range args {
				work, err := deps.Client.ResolveWorkRef(cmd.Context(), ref)
				if err != nil {
					code := deps.Printer.Error("work.archive", mapWorkRefError(ref, err))
					return &output.ExitError{Code: code, Err: err}
				}
				result, err := deps.Client.ArchiveWorkItem(cmd.Context(), work.ChangeRequestID, reason, currentActor(deps))
				if err != nil {
					return apiExitError(deps, "work.archive", err)
				}
				result["ref"] = ref
				archived = append(archived, result)
			}

			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("work.archive", map[string]any{"items": archived})
				return nil
			}

			for _, item := range archived {
				key, _ := item["change_request_key"].(string)
				if key == "" {
					key, _ = item["change_request_id"].(string)
				}
				if key == "" {
					key, _ = item["ref"].(string)
				}
				fmt.Fprintf(deps.Stdout, "Archived %s\n", key)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&reason, "reason", "", "Optional archive reason")
	return cmd
}

// specgate work create --feature <key-or-id> --title <t> [--description <d>] --ac <c> [--ac <c>]...
//
// Full-route sibling of create-quick: creates a feature-backed work item bound
// to the feature's approved canonical spec (POST /api/v1/work-items/create).
