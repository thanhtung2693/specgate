package command

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/interactive"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func registerIdentityCommands(root *cobra.Command, deps *Deps) {
	root.AddCommand(newUserCmd(deps))
	root.AddCommand(newWorkspaceCmd(deps))
}

func newUserCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "user",
		Short: "Manage local SpecGate users",
	}
	cmd.AddCommand(newUserListCmd(deps), newUserCurrentCmd(deps))
	return cmd
}

func newUserListCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List local users",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			users, err := deps.Client.ListUsers(cmd.Context())
			if err != nil {
				code := deps.Printer.Error("user.list", mapAPIError("user.list", err))
				return &output.ExitError{Code: code, Err: err}
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("user.list", map[string]any{"items": users})
				return nil
			}
			for _, user := range users {
				email := user.Email
				if email == "" {
					email = "-"
				}
				fmt.Fprintf(deps.Stdout, "%-20s %-24s %s\n", user.Username, user.DisplayName, email)
			}
			return nil
		},
	}
}

func newUserCurrentCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "current",
		Short: "Show the selected local user",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, _ := config.LoadFrom(deps.ConfigPath)
			if cfg.CurrentUser.ID == "" && cfg.CurrentUser.Username == "" {
				payload := output.ErrorPayload{Code: "not_found", Message: "no current user selected"}
				code := deps.Printer.Error("user.current", payload)
				return &output.ExitError{Code: code}
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("user.current", cfg.CurrentUser)
				return nil
			}
			fmt.Fprintf(deps.Stdout, "user: %s (%s)\n", cfg.CurrentUser.Username, cfg.CurrentUser.DisplayName)
			if cfg.CurrentUser.Email != "" {
				fmt.Fprintf(deps.Stdout, "email: %s\n", cfg.CurrentUser.Email)
			}
			return nil
		},
	}
}

func newWorkspaceCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workspace",
		Short: "Manage local SpecGate workspaces",
	}
	cmd.AddCommand(newWorkspaceListCmd(deps), newWorkspaceCurrentCmd(deps), newWorkspaceSelectCmd(deps))
	return cmd
}

func newWorkspaceListCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List local workspaces",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			workspaces, err := deps.Client.ListWorkspaces(cmd.Context())
			if err != nil {
				code := deps.Printer.Error("workspace.list", mapAPIError("workspace.list", err))
				return &output.ExitError{Code: code, Err: err}
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("workspace.list", map[string]any{"items": workspaces})
				return nil
			}
			for _, workspace := range workspaces {
				fmt.Fprintf(deps.Stdout, "%-24s %s\n", workspace.Slug, workspace.Name)
			}
			return nil
		},
	}
}

func newWorkspaceCurrentCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "current",
		Short: "Show the selected workspace",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, _ := config.LoadFrom(deps.ConfigPath)
			if cfg.Workspace.ID == "" && cfg.Workspace.Slug == "" {
				payload := output.ErrorPayload{Code: "not_found", Message: "no workspace selected"}
				code := deps.Printer.Error("workspace.current", payload)
				return &output.ExitError{Code: code}
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("workspace.current", cfg.Workspace)
				return nil
			}
			fmt.Fprintf(deps.Stdout, "workspace: %s (%s)\n", cfg.Workspace.Slug, cfg.Workspace.Name)
			return nil
		},
	}
}

func newWorkspaceSelectCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "select [slug]",
		Short: "Select the workspace used by subsequent CLI commands",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var workspace *client.IdentityWorkspace
			var err error
			if len(args) == 0 {
				if deps.NoInput || deps.Printer.Mode() == output.ModeJSON {
					payload := output.ErrorPayload{Code: "validation", Message: "workspace slug is required when input is disabled"}
					code := deps.Printer.Error("workspace.select", payload)
					return &output.ExitError{Code: code}
				}
				workspace, err = promptWorkspaceSelection(cmd, deps)
			} else {
				slug := strings.TrimSpace(args[0])
				if err := validateWorkspaceSlug(slug); err != nil {
					payload := output.ErrorPayload{Code: "validation", Message: err.Error()}
					code := deps.Printer.Error("workspace.select", payload)
					return &output.ExitError{Code: code, Err: err}
				}
				workspace, err = deps.Client.GetWorkspace(cmd.Context(), slug)
			}
			if err != nil {
				code := deps.Printer.Error("workspace.select", mapAPIError("workspace.select", err))
				return &output.ExitError{Code: code, Err: err}
			}
			if err := saveWorkspaceSelection(deps, *workspace); err != nil {
				payload := output.ErrorPayload{Code: "unavailable", Message: err.Error()}
				code := deps.Printer.Error("workspace.select", payload)
				return &output.ExitError{Code: code, Err: err}
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("workspace.select", workspace)
				return nil
			}
			fmt.Fprintf(deps.Stdout, "Workspace set to %s\n", workspace.Slug)
			return nil
		},
	}
}

func promptWorkspaceSelection(cmd *cobra.Command, deps *Deps) (*client.IdentityWorkspace, error) {
	workspaces, err := deps.Client.ListWorkspaces(cmd.Context())
	if err != nil {
		return nil, err
	}
	if len(workspaces) == 0 {
		return nil, fmt.Errorf("no workspaces found")
	}
	opts := make([]interactive.Option, 0, len(workspaces))
	bySlug := make(map[string]client.IdentityWorkspace, len(workspaces))
	for _, workspace := range workspaces {
		label := workspace.Slug
		if workspace.Name != "" && workspace.Name != workspace.Slug {
			label = fmt.Sprintf("%s - %s", workspace.Slug, workspace.Name)
		}
		opts = append(opts, interactive.Option{Label: label, Value: workspace.Slug})
		bySlug[workspace.Slug] = workspace
	}
	slug, err := deps.Prompter.Select("Select workspace", opts)
	if err != nil {
		return nil, err
	}
	workspace, ok := bySlug[slug]
	if !ok {
		return nil, fmt.Errorf("workspace %q not found", slug)
	}
	return &workspace, nil
}

func validateWorkspaceSlug(slug string) error {
	if slug == "" {
		return fmt.Errorf("workspace slug is required")
	}
	if isUUIDLike(slug) {
		return fmt.Errorf("workspace slug must use lowercase letters, numbers, and hyphens; internal workspace IDs are not accepted")
	}
	for _, r := range slug {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return fmt.Errorf("workspace slug must use lowercase letters, numbers, and hyphens")
	}
	return nil
}

func isUUIDLike(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, r := range value {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !((r >= 'a' && r <= 'f') || (r >= '0' && r <= '9')) {
				return false
			}
		}
	}
	return true
}

func saveIdentitySelection(deps *Deps, selection client.IdentitySelection) error {
	cfg, _ := config.LoadFrom(deps.ConfigPath)
	cfg.CurrentUser = config.CurrentUser{
		ID:          selection.User.ID,
		Username:    selection.User.Username,
		DisplayName: selection.User.DisplayName,
		Email:       selection.User.Email,
	}
	cfg.Workspace = config.CurrentWorkspace{
		ID:   selection.Workspace.ID,
		Slug: selection.Workspace.Slug,
		Name: selection.Workspace.Name,
	}
	return saveConfig(deps, cfg)
}

func saveWorkspaceSelection(deps *Deps, workspace client.IdentityWorkspace) error {
	cfg, _ := config.LoadFrom(deps.ConfigPath)
	cfg.Workspace = config.CurrentWorkspace{
		ID:   workspace.ID,
		Slug: workspace.Slug,
		Name: workspace.Name,
	}
	return saveConfig(deps, cfg)
}

func saveConfig(deps *Deps, cfg config.Config) error {
	if deps.ConfigPath != "" {
		return cfg.SaveTo(deps.ConfigPath)
	}
	return cfg.Save()
}

func currentIdentityConfig(deps *Deps) config.Config {
	cfg, _ := config.LoadFrom(deps.ConfigPath)
	return cfg
}

func currentActor(deps *Deps) string {
	cfg := currentIdentityConfig(deps)
	if cfg.CurrentUser.Username != "" {
		return cfg.CurrentUser.Username
	}
	return cfg.CurrentUser.DisplayName
}

func annotateBodyWithCurrentSelection(deps *Deps, body map[string]any) {
	if body == nil {
		return
	}
	cfg := currentIdentityConfig(deps)
	if cfg.CurrentUser.Username != "" {
		if _, exists := body["created_by"]; !exists {
			body["created_by"] = cfg.CurrentUser.Username
		}
	}
	if cfg.Workspace.ID != "" {
		if _, exists := body["workspace_id"]; !exists {
			body["workspace_id"] = cfg.Workspace.ID
		}
	}
}
