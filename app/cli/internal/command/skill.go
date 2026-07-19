package command

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func registerSkillCommands(root *cobra.Command, deps *Deps) {
	sk := &cobra.Command{
		Use:   "skill",
		Short: "Manage user-defined skills",
	}
	sk.AddCommand(newSkillListCmd(deps))
	sk.AddCommand(newSkillShowCmd(deps))
	root.AddCommand(sk)
}

// specgate skill list [--name <prefix>]
func newSkillListCmd(deps *Deps) *cobra.Command {
	var nameFilter string
	var includePrompt bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List user-defined skills",
		RunE: func(cmd *cobra.Command, _ []string) error {
			workspaceID, err := selectedWorkspaceIDOrExit(cmd, deps, "skill.list")
			if err != nil {
				return err
			}
			skills, err := deps.Client.ListSkills(client.WithWorkspace(cmd.Context(), workspaceID), nameFilter)
			if err != nil {
				return apiExitError(deps, "skill.list", err)
			}
			if deps.Printer.Mode() == output.ModeJSON {
				if includePrompt {
					deps.Printer.Success("skill.list", map[string]any{"items": skills})
					return nil
				}
				deps.Printer.Success("skill.list", map[string]any{"items": skillSummaries(skills)})
				return nil
			}
			if len(skills) == 0 {
				fmt.Fprintln(deps.Stdout, notice(deps, output.StyleInfo, "Notice", "No skills found."))
				return nil
			}
			for _, s := range skills {
				id := s.ID
				if len(id) > 8 {
					id = id[:8]
				}
				fmt.Fprintf(deps.Stdout, "%s  %s\n", styled(deps, output.StyleBold, id), s.Name)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&nameFilter, "name", "", "Prefix filter for skill name (case-insensitive)")
	cmd.Flags().BoolVar(&includePrompt, "include-prompt", false, "Include full skill prompts in JSON output")
	return cmd
}

type skillSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

func skillSummaries(skills []client.Skill) []skillSummary {
	out := make([]skillSummary, 0, len(skills))
	for _, s := range skills {
		out = append(out, skillSummary{
			ID:          s.ID,
			Name:        s.Name,
			Description: s.Description,
			CreatedAt:   s.CreatedAt,
			UpdatedAt:   s.UpdatedAt,
		})
	}
	return out
}

// specgate skill show <id-or-name>
func newSkillShowCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "show <id-or-name>",
		Short: "Show skill details by ID or name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := args[0]
			workspaceID, err := selectedWorkspaceIDOrExit(cmd, deps, "skill.show")
			if err != nil {
				return err
			}
			ctx := client.WithWorkspace(cmd.Context(), workspaceID)

			// Try direct ID lookup first.
			s, err := deps.Client.GetSkill(ctx, query)
			if err != nil {
				// Fall back to name search.
				skills, listErr := deps.Client.ListSkills(ctx, query)
				if listErr != nil {
					return apiExitError(deps, "skill.show", err)
				}
				for _, sk := range skills {
					if strings.EqualFold(sk.Name, query) {
						s = &sk
						break
					}
				}
				if s == nil {
					payload := output.ErrorPayload{Code: "not_found", Message: fmt.Sprintf("skill %q not found", query)}
					code := deps.Printer.Error("skill.show", payload)
					return &output.ExitError{Code: code}
				}
			}

			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("skill.show", s)
				return nil
			}
			fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "ID:"), styled(deps, output.StyleBold, s.ID))
			fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "Name:"), s.Name)
			if s.Description != "" {
				fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "Desc:"), s.Description)
			}
			if s.Prompt != "" {
				fmt.Fprintf(deps.Stdout, "\n%s\n", s.Prompt)
			}
			return nil
		},
	}
}
