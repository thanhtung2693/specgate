package command

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/interactive"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

// ErrInputRequired is returned when interactive input is needed but --no-input is set.
var ErrInputRequired = errors.New("interactive input required: re-run without --no-input")

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
				if deps.Printer.Mode() == output.ModeJSON {
					deps.Printer.Success("work.list", map[string]any{"items": localWorkViews(items)})
					return nil
				}
				for _, item := range items {
					fmt.Fprintf(deps.Stdout, "%s  [%s]  %s\n", item.Key, item.Phase, item.Title)
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

	if deps.Printer.Mode() == output.ModeJSON {
		deps.Printer.Success("work.list", map[string]any{"items": filtered})
		return nil
	}
	if len(filtered) == 0 {
		fmt.Fprintf(deps.Stdout, "No work items in phase(s) %s.\n", phaseCSV)
		return nil
	}
	for _, it := range filtered {
		fmt.Fprintf(deps.Stdout, "%s  [%s]  %s\n", styled(deps, output.StyleBold, it.Key), styledStatus(deps, it.Phase), it.Title)
	}
	fmt.Fprintln(deps.Stdout, nextStep(deps, "Show details with", "specgate work show <ref>"))
	return nil
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
					return localExitError(deps, "work.show", ErrInputRequired)
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
					return localExitError(deps, "work.context", ErrInputRequired)
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
func newWorkCreateCmd(deps *Deps) *cobra.Command {
	var (
		feature     string
		title       string
		description string
		criteria    []string
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a feature-backed work item from the feature's approved spec",
		Long: `Create work bound to a feature's approved canonical artifact.

The IDE agent must read the canonical specification and pass each confirmed
acceptance criterion explicitly with a repeated --ac flag.`,
		Example: `  specgate work create --feature feature-key --title "Implement export" --ac "Exported data preserves every field" --json
  specgate work create --feature feature-key --title "Fix timeout" --ac "Retries stop after three attempts" --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(feature) == "" || strings.TrimSpace(title) == "" {
				payload := output.ErrorPayload{Code: "usage", Message: "--feature and --title are required"}
				code := deps.Printer.Error("work.create", payload)
				return &output.ExitError{Code: code}
			}
			if len(criteria) == 0 {
				payload := output.ErrorPayload{Code: "usage", Message: "at least one --ac is required"}
				code := deps.Printer.Error("work.create", payload)
				return &output.ExitError{Code: code}
			}
			body := map[string]any{"feature": strings.TrimSpace(feature), "title": strings.TrimSpace(title)}
			if description != "" {
				body["description"] = description
			}
			if len(criteria) > 0 {
				body["acceptance_criteria"] = criteria
			}
			if deps.Topology == config.ModeLocal {
				store, err := openLocalStore(deps)
				if err != nil {
					return localExitError(deps, "work.create", err)
				}
				defer store.Close()
				selection, err := localSelection(cmd.Context(), deps, store)
				if err != nil {
					return localExitError(deps, "work.create", err)
				}
				work, err := store.CreateWork(cmd.Context(), selection.Workspace.ID, local.WorkInput{FeatureRef: feature, Title: title, Description: description, AcceptanceCriteria: criteria})
				if err != nil {
					return localExitError(deps, "work.create", err)
				}
				if deps.Printer.Mode() == output.ModeJSON {
					view := localWorkView(work)
					view["change_request_id"] = work.ID
					view["change_request_key"] = work.Key
					view["feature_key"] = work.FeatureKey
					view["lead_artifact_id"] = work.ArtifactID
					deps.Printer.Success("work.create", view)
					return nil
				}
				fmt.Fprintln(deps.Stdout, notice(deps, output.StyleSuccess, "Created", work.Key))
				fmt.Fprintln(deps.Stdout, nextStep(deps, "Read the implementation contract with", "specgate work context "+work.Key))
				return nil
			}
			if err := annotateBodyWithCurrentSelection(cmd.Context(), deps, body); err != nil {
				return apiExitError(deps, "work.create", err)
			}
			result, err := deps.Client.CreateWorkItem(requestContextForBody(cmd.Context(), body), body)
			if err != nil {
				return apiExitError(deps, "work.create", err)
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("work.create", result)
				return nil
			}
			key, _ := result["change_request_key"].(string)
			featKey, _ := result["feature_key"].(string)
			lead, _ := result["lead_artifact_id"].(string)
			acs, _ := result["acceptance_criteria"].([]any)
			fmt.Fprintf(deps.Stdout, "%s %s — feature %s, lead artifact %s, %d acceptance criteria\n", styled(deps, output.StyleSuccess, "Created"), styled(deps, output.StyleBold, key), featKey, lead, len(acs))
			fmt.Fprintln(deps.Stdout, nextStep(deps, "Read the implementation contract with", "specgate work context "+key))
			return nil
		},
	}
	cmd.Flags().StringVar(&feature, "feature", "", "Feature key or id (required)")
	cmd.Flags().StringVar(&title, "title", "", "Work item title (required)")
	cmd.Flags().StringVar(&description, "description", "", "Work item description")
	cmd.Flags().StringArrayVar(&criteria, "ac", nil, "Acceptance criterion (repeatable; append @check:<name> to bind a human-authored delivery check)")
	return cmd
}

func localWorkView(work local.WorkItem) map[string]any {
	return map[string]any{"id": work.ID, "key": work.Key, "workspace_id": work.WorkspaceID, "feature_id": work.FeatureID, "artifact_id": work.ArtifactID, "title": work.Title, "description": work.Description, "phase": work.Phase, "context_digest": work.ContextDigest, "acceptance_criteria": work.AcceptanceCriteria, "created_at": work.CreatedAt}
}

func localWorkViews(items []local.WorkItem) []map[string]any {
	views := make([]map[string]any, 0, len(items))
	for _, item := range items {
		views = append(views, localWorkView(item))
	}
	return views
}

// specgate work create-quick ["Title"] [--description <text>] [--ac <criterion>]... [--file <path>]
func newWorkCreateQuickCmd(deps *Deps) *cobra.Command {
	var (
		filePath    string
		description string
		criteria    []string
	)

	cmd := &cobra.Command{
		Use:   "create-quick [title]",
		Short: "Create a quick-route change request",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			title := ""
			if len(args) > 0 {
				title = strings.TrimSpace(args[0])
			}
			if title != "" && filePath != "" {
				payload := output.ErrorPayload{Code: "validation", Message: "pass a title argument or --file, not both"}
				code := deps.Printer.Error("work.create-quick", payload)
				return &output.ExitError{Code: code}
			}

			var body map[string]any

			switch {
			case filePath != "":
				var err error
				body, err = readJSONBodyFile(deps, "work.create-quick", filePath)
				if err != nil {
					return err
				}
			case title != "":
				// Title given via args: build the same JSON body without prompting.
				body = map[string]any{"title": title, "description": title}
				if description != "" {
					body["description"] = description
				}
				if len(criteria) > 0 {
					body["acceptance_criteria"] = acceptanceCriteriaBody(criteria)
				}
			case !canPrompt(deps):
				return &output.ExitError{Code: output.ExitUsage, Err: ErrInputRequired}
			default:
				// Interactive: prompt for title, description, and acceptance criteria.
				promptedTitle, err := deps.Prompter.Input("Work item title", "", func(s string) error {
					if s == "" {
						return errors.New("title is required")
					}
					return nil
				})
				if err != nil {
					return &output.ExitError{Code: output.ExitUsage, Err: err}
				}
				desc, err := deps.Prompter.Input("Description (optional)", "", nil)
				if err != nil {
					return &output.ExitError{Code: output.ExitUsage, Err: err}
				}
				acs := append([]string(nil), criteria...)
				for {
					ac, err := deps.Prompter.Input("Add acceptance criterion (empty to finish)", "", nil)
					if err != nil {
						return &output.ExitError{Code: output.ExitUsage, Err: err}
					}
					ac = strings.TrimSpace(ac)
					if ac == "" {
						break
					}
					acs = append(acs, ac)
				}
				body = map[string]any{"title": promptedTitle, "description": desc}
				if len(acs) > 0 {
					body["acceptance_criteria"] = acceptanceCriteriaBody(acs)
				}
			}
			if deps.Topology == config.ModeLocal {
				input, err := localQuickWorkInput(body)
				if err != nil {
					return localExitError(deps, "work.create-quick", err)
				}
				store, err := openLocalStore(deps)
				if err != nil {
					return localExitError(deps, "work.create-quick", err)
				}
				defer store.Close()
				selection, err := localSelection(cmd.Context(), deps, store)
				if err != nil {
					return localExitError(deps, "work.create-quick", err)
				}
				work, err := store.CreateQuickWork(cmd.Context(), selection.Workspace.ID, input)
				if err != nil {
					return localExitError(deps, "work.create-quick", err)
				}
				result := localWorkView(work)
				result["change_request_id"] = work.ID
				result["change_request_key"] = work.Key
				result["lead_artifact_id"] = ""
				result["acceptance_count"] = len(work.AcceptanceCriteria)
				if deps.Printer.Mode() == output.ModeJSON {
					deps.Printer.Success("work.create-quick", result)
					return nil
				}
				fmt.Fprintf(deps.Stdout, "%s %s\n", styled(deps, output.StyleSuccess, "Created"), styled(deps, output.StyleBold, work.Key))
				fmt.Fprintln(deps.Stdout, nextStep(deps, "Read the implementation contract with", "specgate work context "+work.Key))
				return nil
			}
			if err := annotateBodyWithCurrentSelection(cmd.Context(), deps, body); err != nil {
				return apiExitError(deps, "work.create-quick", err)
			}

			result, err := deps.Client.CreateQuickWorkItem(requestContextForBody(cmd.Context(), body), body)
			if err != nil {
				return apiExitError(deps, "work.create-quick", err)
			}

			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("work.create-quick", result)
				return nil
			}

			key, _ := result["change_request_key"].(string)
			id, _ := result["change_request_id"].(string)
			ref := key
			if ref == "" {
				ref = id
			}
			if ref == "" {
				fmt.Fprintln(deps.Stdout, "Created work item")
				return nil
			}
			fmt.Fprintf(deps.Stdout, "%s %s", styled(deps, output.StyleSuccess, "Created"), styled(deps, output.StyleBold, ref))
			if key != "" && id != "" {
				fmt.Fprintf(deps.Stdout, " (%s)", id)
			}
			fmt.Fprintln(deps.Stdout)
			fmt.Fprintln(deps.Stdout, nextStep(deps, "Read the implementation contract with", "specgate work context "+ref))
			return nil
		},
	}

	cmd.Flags().StringVar(&filePath, "file", "", "JSON file to POST as the work item body")
	cmd.Flags().StringVar(&description, "description", "", "Work item description (with a title argument)")
	cmd.Flags().StringArrayVar(&criteria, "ac", nil, "Acceptance criterion (repeatable; append @check:<name> to bind a human-authored delivery check)")
	cmd.MarkFlagsMutuallyExclusive("file", "description")
	cmd.MarkFlagsMutuallyExclusive("file", "ac")
	return cmd
}

func localQuickWorkInput(body map[string]any) (local.QuickWorkInput, error) {
	title, _ := body["title"].(string)
	description, _ := body["description"].(string)
	input := local.QuickWorkInput{
		Title:       strings.TrimSpace(title),
		Description: strings.TrimSpace(description),
	}
	switch rows := body["acceptance_criteria"].(type) {
	case []map[string]string:
		for _, row := range rows {
			input.AcceptanceCriteria = appendLocalQuickCriterion(input.AcceptanceCriteria, row["text"], row["verification_binding"])
		}
	case []any:
		for _, raw := range rows {
			switch row := raw.(type) {
			case string:
				input.AcceptanceCriteria = appendLocalQuickCriterion(input.AcceptanceCriteria, row, "")
			case map[string]any:
				text, _ := row["text"].(string)
				binding, _ := row["verification_binding"].(string)
				input.AcceptanceCriteria = appendLocalQuickCriterion(input.AcceptanceCriteria, text, binding)
			}
		}
	case []string:
		for _, row := range rows {
			input.AcceptanceCriteria = appendLocalQuickCriterion(input.AcceptanceCriteria, row, "")
		}
	}
	if input.Title == "" {
		return local.QuickWorkInput{}, fmt.Errorf("title is required")
	}
	if len(input.AcceptanceCriteria) == 0 {
		return local.QuickWorkInput{}, fmt.Errorf("at least one acceptance criterion is required in Local mode; repeat --ac")
	}
	return input, nil
}

func appendLocalQuickCriterion(criteria []string, text, binding string) []string {
	text = strings.TrimSpace(text)
	binding = strings.TrimSpace(binding)
	if text == "" {
		return criteria
	}
	if binding != "" {
		text += " @check:" + binding
	}
	return append(criteria, text)
}

func acceptanceCriteriaBody(criteria []string) any {
	type criterion struct {
		Text                string
		VerificationBinding string
	}
	parsed := make([]criterion, 0, len(criteria))
	for _, raw := range criteria {
		text, binding := parseAcceptanceCriterionBinding(raw)
		if text == "" {
			continue
		}
		parsed = append(parsed, criterion{Text: text, VerificationBinding: binding})
	}
	out := make([]map[string]string, 0, len(parsed))
	for _, ac := range parsed {
		row := map[string]string{"text": ac.Text}
		if ac.VerificationBinding != "" {
			row["verification_binding"] = ac.VerificationBinding
		}
		out = append(out, row)
	}
	return out
}

func parseAcceptanceCriterionBinding(raw string) (string, string) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", ""
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return "", ""
	}
	last := fields[len(fields)-1]
	const prefix = "@check:"
	if !strings.HasPrefix(last, prefix) {
		return trimmed, ""
	}
	binding := strings.TrimSpace(strings.TrimPrefix(last, prefix))
	if binding == "" {
		return trimmed, ""
	}
	text := strings.TrimSpace(strings.TrimSuffix(trimmed, last))
	if text == "" {
		return trimmed, ""
	}
	return text, binding
}

// specgate work policy <work-ref>
//
// Canonical user-facing policy explanation command.
func newWorkPolicyCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "policy <work-ref>",
		Short: "Explain governance policy for a work item",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			work, err := deps.Client.ResolveWorkRef(cmd.Context(), args[0])
			if err != nil {
				code := deps.Printer.Error("work.policy", mapWorkRefError(args[0], err))
				return &output.ExitError{Code: code, Err: err}
			}
			exp, err := deps.Client.WorkPolicy(cmd.Context(), work.ChangeRequestID)
			if err != nil {
				return apiExitError(deps, "work.policy", err)
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("work.policy", exp)
				return nil
			}
			printPolicyExplanation(deps, exp.Title, exp.Reasons, exp.Summary, exp.Obligations)
			return nil
		},
	}
}

// resolveRef returns the first CLI arg if present; otherwise it prompts the user
// to pick from NeedsAttention items. Returns ErrInputRequired when no-input is set
// and no ref was given.
func resolveRef(cmd *cobra.Command, args []string, deps *Deps) (string, error) {
	if len(args) > 0 && args[0] != "" {
		return args[0], nil
	}

	if !canPrompt(deps) {
		return "", &output.ExitError{Code: output.ExitUsage, Err: ErrInputRequired}
	}

	workspaceID, err := currentWorkspaceID(cmd.Context(), deps)
	if err != nil {
		return "", apiExitError(deps, cmd.Name(), err)
	}
	st, err := deps.Client.Status(cmd.Context(), workspaceID)
	if err != nil {
		return "", apiExitError(deps, cmd.Name(), err)
	}

	opts := make([]interactive.Option, 0, len(st.NeedsAttention))
	for _, item := range st.NeedsAttention {
		opts = append(opts, interactive.Option{
			Label: fmt.Sprintf("%s — %s (%s)", item.Key, item.Title, item.Phase),
			Value: item.Key,
		})
	}

	if len(opts) == 0 {
		payload := output.ErrorPayload{Code: "not_found", Message: "no work items need attention; pass a ref explicitly"}
		code := deps.Printer.Error(cmd.Name(), payload)
		return "", &output.ExitError{Code: code}
	}

	return deps.Prompter.Select("Select a work item", opts)
}
