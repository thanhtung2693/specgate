package command

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

// ErrInputRequired is returned when interactive input is needed but --no-input is set.
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
					deps.Printer.Success("work.create", localWorkView(work))
					return nil
				}
				fmt.Fprintln(deps.Stdout, notice(deps, output.StyleSuccess, "Created", work.Key))
				if line := criteriaEnforcementNotice(work.AcceptanceCriteria); line != "" {
					fmt.Fprintln(deps.Stdout, line)
				}
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
			total, bound := criteriaCounts(result["acceptance_criteria"])
			if total == 0 {
				total, bound = criteriaCounts(body["acceptance_criteria"])
			}
			fmt.Fprintf(deps.Stdout, "%s %s — feature %s, implementing %s\n", styled(deps, output.StyleSuccess, "Created"), styled(deps, output.StyleBold, key), featKey, lead)
			if line := criteriaEnforcementLine(total, bound); line != "" {
				fmt.Fprintln(deps.Stdout, line)
			}
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

// localWorkView renders one Local work item. The Full-mode aliases belong here
// rather than in individual handlers: they were added only by `work create`, so
// `work show` and `work list` omitted `lead_artifact_id` and an agent following
// the preparation skill could not verify the approved artifact in Local mode.
func localWorkView(work local.WorkItem) map[string]any {
	return map[string]any{
		"id": work.ID, "key": work.Key, "workspace_id": work.WorkspaceID,
		"feature_id": work.FeatureID, "feature_key": work.FeatureKey,
		"artifact_id": work.ArtifactID, "lead_artifact_id": work.ArtifactID,
		"change_request_id": work.ID, "change_request_key": work.Key,
		"title": work.Title, "description": work.Description, "phase": work.Phase,
		"context_digest": work.ContextDigest, "acceptance_criteria": work.AcceptanceCriteria,
		"created_at": work.CreatedAt,
	}
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
				result["acceptance_count"] = len(work.AcceptanceCriteria)
				result["acceptance_criteria_bound"] = boundCriteriaCount(work.AcceptanceCriteria)
				if deps.Printer.Mode() == output.ModeJSON {
					deps.Printer.Success("work.create-quick", result)
					return nil
				}
				fmt.Fprintf(deps.Stdout, "%s %s\n", styled(deps, output.StyleSuccess, "Created"), styled(deps, output.StyleBold, work.Key))
				if notice := criteriaEnforcementNotice(work.AcceptanceCriteria); notice != "" {
					fmt.Fprintln(deps.Stdout, notice)
				}
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

			criteriaTotal, criteriaBound := criteriaCounts(result["acceptance_criteria"])
			if criteriaTotal == 0 {
				criteriaTotal, criteriaBound = criteriaCounts(body["acceptance_criteria"])
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
			if notice := criteriaEnforcementLine(criteriaTotal, criteriaBound); notice != "" {
				fmt.Fprintln(deps.Stdout, notice)
			}
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
	// Refuse a binding that cannot be resolved. Accepting it would store a
	// criterion its author believes is enforced by a deterministic check while
	// delivery review silently judges it on the agent's own claim.
	if err := rejectUnresolvableBindings(input.AcceptanceCriteria); err != nil {
		return local.QuickWorkInput{}, err
	}
	return input, nil
}

func rejectUnresolvableBindings(criteria []string) error {
	for index, criterion := range criteria {
		if problem := local.AcceptanceCriterionBindingProblem(criterion); problem != "" {
			return fmt.Errorf("acceptance criterion %d %s: %q", index+1, problem, strings.TrimSpace(criterion))
		}
	}
	return nil
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

// parseAcceptanceCriterionBinding splits an acceptance criterion into its text
// and any trailing `@check:<name>` binding. The parser lives in the local
// package because Local delivery review resolves bound criteria against it.
func parseAcceptanceCriterionBinding(raw string) (string, string) {
	return local.ParseAcceptanceCriterionBinding(raw)
}

// criteriaEnforcementNotice reports how many acceptance criteria have a check
// standing behind them. This is a derived count, not a judgement about criterion
// prose: a bound criterion takes its verdict from the check result, while an
// unbound one can only ever be graded on the agent's claim. Saying so at the
// moment the list is created is the honest equivalent of the artifact route's
// verifiability gate, which quick work never runs.
//
// ponytail: a count, not a linter — no vague-wording heuristics, no new ceremony.
//
// boundCriteriaCount is the single source for both the human line and the
// machine envelope: the IDE agent runs create-quick in machine mode, so the
// signal has to travel in the envelope or the default path never sees it. An
// unresolvable or ambiguous binding counts as unbound, because review will fall
// back to the agent's claim for it.
// criteriaCounts reports total and bound criteria from a persisted acceptance
// list. Full mode counts the server's response rather than the request body: the
// body is what was asked for, and reporting enforcement from it would state a
// count for criteria the server may have normalized or refused.
//
// Both stored shapes are accepted — the request shape built by
// acceptanceCriteriaBody and the API's returned rows — because each already
// carries the `@check:` binding in its own field, so no criterion text is
// re-parsed here.
func criteriaCounts(raw any) (total, bound int) {
	binding := func(row map[string]any) bool {
		value, _ := row["verification_binding"].(string)
		return strings.TrimSpace(value) != ""
	}
	switch rows := raw.(type) {
	case []map[string]string:
		for _, row := range rows {
			total++
			if strings.TrimSpace(row["verification_binding"]) != "" {
				bound++
			}
		}
	case []any:
		for _, item := range rows {
			total++
			if row, ok := item.(map[string]any); ok && binding(row) {
				bound++
			}
		}
	}
	return total, bound
}

func boundCriteriaCount(criteria []string) int {
	bound := 0
	for _, criterion := range criteria {
		if _, binding := parseAcceptanceCriterionBinding(criterion); binding != "" {
			bound++
		}
	}
	return bound
}

func criteriaEnforcementNotice(criteria []string) string {
	return criteriaEnforcementLine(len(criteria), boundCriteriaCount(criteria))
}

func criteriaEnforcementLine(total, bound int) string {
	switch {
	case total == 0:
		return ""
	case bound == total:
		return fmt.Sprintf("Enforcement: all %d criteria are bound to a check.", total)
	case bound == 0:
		return fmt.Sprintf("Enforcement: none of the %d criteria are bound to a check, so review can only report the agent's claims. Bind one with `@check:<name>`.", total)
	default:
		return fmt.Sprintf("Enforcement: %d of %d criteria are bound to a check; the rest are graded on the agent's claim.", bound, total)
	}
}

// specgate work policy <work-ref>
//
// Canonical user-facing policy explanation command.
