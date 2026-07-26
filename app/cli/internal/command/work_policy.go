package command

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/interactive"
	"github.com/specgate/specgate/app/cli/internal/output"
)

// ErrInputRequired is returned when interactive input is needed but --no-input is set.
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
		return "", &output.ExitError{Code: output.ExitUsage, Err: ErrWorkRefRequired}
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
