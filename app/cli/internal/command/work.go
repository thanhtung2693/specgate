package command

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/interactive"
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
	work.AddCommand(newWorkPolicyCmd(deps))
	root.AddCommand(work)
}

// specgate work list
func newWorkListCmd(deps *Deps) *cobra.Command {
	var allWorkspaces bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List work items needing attention",
		RunE: func(cmd *cobra.Command, _ []string) error {
			workspaceID := ""
			if !allWorkspaces {
				workspaceID = currentIdentityConfig(deps).Workspace.ID
			}
			st, err := deps.Client.Status(cmd.Context(), workspaceID)
			if err != nil {
				code := deps.Printer.Error("work.list", mapAPIError("work.list", err))
				return &output.ExitError{Code: code, Err: err}
			}

			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("work.list", map[string]any{
					"counts":          st.Counts,
					"needs_attention": st.NeedsAttention,
				})
				return nil
			}

			if len(st.NeedsAttention) == 0 {
				printNoWorkNeedsAttention(deps, st, allWorkspaces)
				return nil
			}
			printAttentionSection(deps, st.NeedsAttention)
			return nil
		},
	}
	cmd.Flags().BoolVar(&allWorkspaces, "all-workspaces", false, "List work items across all workspaces")
	return cmd
}

func printNoWorkNeedsAttention(deps *Deps, st *client.GovernanceStatus, allWorkspaces bool) {
	fmt.Fprintln(deps.Stdout, "No work items need attention.")
	if st.Counts.Total > 0 {
		fmt.Fprintf(deps.Stdout, "%d work item(s) are tracked in other phases: %s.\n", st.Counts.Total, phaseBreakdown(st.Counts))
		fmt.Fprintln(deps.Stdout, "Next: run `specgate status` for the board overview or `specgate work show <ref>` if you know the work item.")
		return
	}
	if allWorkspaces {
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
			ref, err := resolveRef(cmd, args, deps)
			if err != nil {
				return err
			}

			work, err := deps.Client.ResolveWorkRef(cmd.Context(), ref)
			if err != nil {
				code := deps.Printer.Error("work.show", mapAPIError("work.show", err))
				return &output.ExitError{Code: code, Err: err}
			}

			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("work.show", work)
				return nil
			}

			fmt.Fprintf(deps.Stdout, "%s  %s\n", work.ChangeRequestKey, work.Title)
			fmt.Fprintf(deps.Stdout, "Phase: %s\n", work.Phase)
			if work.ContextPackURI != "" {
				fmt.Fprintf(deps.Stdout, "Context pack: %s\n", work.ContextPackURI)
			}
			// Best-effort readback: the criteria are what delivery review will
			// judge, so a human reading the item should see them here.
			if criteria, err := deps.Client.ListAcceptanceCriteria(cmd.Context(), work.ChangeRequestID); err == nil && len(criteria) > 0 {
				fmt.Fprintln(deps.Stdout, "Acceptance criteria:")
				for _, criterion := range criteria {
					marker := "☐"
					if criterion.Done {
						marker = "☑"
					}
					fmt.Fprintf(deps.Stdout, "  %s %s\n", marker, criterion.Text)
				}
			}
			return nil
		},
	}
}

// specgate work context [ref] [--lane fe|be]
func newWorkContextCmd(deps *Deps) *cobra.Command {
	var lane string

	cmd := &cobra.Command{
		Use:   "context [ref]",
		Short: "Fetch the context pack for a work item",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := resolveRef(cmd, args, deps)
			if err != nil {
				return err
			}

			// Resolve ref → change_request_id
			work, err := deps.Client.ResolveWorkRef(cmd.Context(), ref)
			if err != nil {
				code := deps.Printer.Error("work.context", mapAPIError("work.context", err))
				return &output.ExitError{Code: code, Err: err}
			}

			cp, err := deps.Client.ContextPack(cmd.Context(), work.ChangeRequestID, lane)
			if err != nil {
				code := deps.Printer.Error("work.context", mapAPIError("work.context", err))
				return &output.ExitError{Code: code, Err: err}
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

	cmd.Flags().StringVar(&lane, "lane", "", "Lane filter: fe or be (empty = full pack)")
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
			if !deps.Yes {
				label := fmt.Sprintf("Archive %d work item(s)?", len(args))
				confirmed, err := deps.Prompter.Confirm(label, false)
				if err != nil {
					return &output.ExitError{Code: output.ExitUsage, Err: err}
				}
				if !confirmed {
					return nil
				}
			}

			archived := make([]map[string]any, 0, len(args))
			for _, ref := range args {
				work, err := deps.Client.ResolveWorkRef(cmd.Context(), ref)
				if err != nil {
					code := deps.Printer.Error("work.archive", mapAPIError("work.archive", err))
					return &output.ExitError{Code: code, Err: err}
				}
				result, err := deps.Client.ArchiveWorkItem(cmd.Context(), work.ChangeRequestID, reason, currentActor(deps))
				if err != nil {
					code := deps.Printer.Error("work.archive", mapAPIError("work.archive", err))
					return &output.ExitError{Code: code, Err: err}
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
				data, err := os.ReadFile(filePath)
				if err != nil {
					payload := output.ErrorPayload{Code: "usage", Message: fmt.Sprintf("read file %s: %v", filePath, err)}
					code := deps.Printer.Error("work.create-quick", payload)
					return &output.ExitError{Code: code, Err: err}
				}
				if err := json.Unmarshal(data, &body); err != nil {
					payload := output.ErrorPayload{Code: "usage", Message: fmt.Sprintf("parse JSON from %s: %v", filePath, err)}
					code := deps.Printer.Error("work.create-quick", payload)
					return &output.ExitError{Code: code, Err: err}
				}
			case title != "":
				// Title given via args: build the same JSON body without prompting.
				body = map[string]any{"title": title}
				if description != "" {
					body["description"] = description
				}
				if len(criteria) > 0 {
					body["acceptance_criteria"] = criteria
				}
			case deps.NoInput:
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
					body["acceptance_criteria"] = acs
				}
			}
			annotateBodyWithCurrentSelection(deps, body)

			result, err := deps.Client.CreateQuickWorkItem(cmd.Context(), body)
			if err != nil {
				code := deps.Printer.Error("work.create-quick", mapAPIError("work.create-quick", err))
				return &output.ExitError{Code: code, Err: err}
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
			fmt.Fprintf(deps.Stdout, "Created %s", ref)
			if key != "" && id != "" {
				fmt.Fprintf(deps.Stdout, " (%s)", id)
			}
			fmt.Fprintln(deps.Stdout)
			fmt.Fprintf(deps.Stdout, "Next: specgate work context %s\n", ref)
			return nil
		},
	}

	cmd.Flags().StringVar(&filePath, "file", "", "JSON file to POST as the work item body")
	cmd.Flags().StringVar(&description, "description", "", "Work item description (with a title argument)")
	cmd.Flags().StringArrayVar(&criteria, "ac", nil, "Acceptance criterion (repeatable)")
	cmd.MarkFlagsMutuallyExclusive("file", "description")
	cmd.MarkFlagsMutuallyExclusive("file", "ac")
	return cmd
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
				code := deps.Printer.Error("work.policy", mapAPIError("work.policy", err))
				return &output.ExitError{Code: code, Err: err}
			}
			exp, err := deps.Client.WorkPolicy(cmd.Context(), work.ChangeRequestID)
			if err != nil {
				code := deps.Printer.Error("work.policy", mapAPIError("work.policy", err))
				return &output.ExitError{Code: code, Err: err}
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

	if deps.NoInput {
		return "", &output.ExitError{Code: output.ExitUsage, Err: ErrInputRequired}
	}

	st, err := deps.Client.Status(cmd.Context(), currentIdentityConfig(deps).Workspace.ID)
	if err != nil {
		code := deps.Printer.Error(cmd.Name(), mapAPIError(cmd.Name(), err))
		return "", &output.ExitError{Code: code, Err: err}
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
