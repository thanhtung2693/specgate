package command

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/interactive"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

const (
	workspaceSaveScopeGlobal  = "global"
	workspaceSaveScopeProject = "project"
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
	cmd.AddCommand(newUserListCmd(deps), newUserCurrentCmd(deps), newUserLoginCmd(deps), newUserLogoutCmd(deps))
	return cmd
}

func newUserLoginCmd(deps *Deps) *cobra.Command {
	var workspaceName, displayName, username, email string

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Create or select the local user and workspace",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var in client.IdentityBootstrapInput
			var err error
			if strings.TrimSpace(workspaceName) == "" ||
				strings.TrimSpace(displayName) == "" ||
				strings.TrimSpace(username) == "" {
				if !canPrompt(deps) {
					payload := output.ErrorPayload{
						Code:    "validation",
						Message: formatRequiredLoginFlags(workspaceName, displayName, username) + " are required",
					}
					code := deps.Printer.Error("user.login", payload)
					return &output.ExitError{Code: code}
				}
				in, err = promptIdentityBootstrap(deps)
				if err != nil {
					return err
				}
			} else {
				in = client.IdentityBootstrapInput{
					WorkspaceName: strings.TrimSpace(workspaceName),
					DisplayName:   strings.TrimSpace(displayName),
					Username:      strings.ToLower(strings.TrimSpace(username)),
					Email:         strings.TrimSpace(email),
				}
				if err := validateUsernamePrompt(in.Username); err != nil {
					payload := output.ErrorPayload{Code: "validation", Message: err.Error()}
					code := deps.Printer.Error("user.login", payload)
					return &output.ExitError{Code: code, Err: err}
				}
			}

			if deps.Topology == config.ModeLocal {
				store, err := openLocalStore(deps)
				if err != nil {
					return localExitError(deps, "user.login", err)
				}
				defer store.Close()
				selection, err := store.Initialize(cmd.Context(), local.InitInput{
					WorkspaceName: in.WorkspaceName,
					DisplayName:   in.DisplayName,
					Username:      in.Username,
					Email:         in.Email,
				})
				if err != nil {
					return localExitError(deps, "user.login", err)
				}
				result := localIdentitySelection(selection)
				if err := saveIdentitySelection(deps, result); err != nil {
					return localExitError(deps, "user.login", err)
				}
				if deps.Printer.Mode() == output.ModeJSON {
					deps.Printer.Success("user.login", result)
					return nil
				}
				fmt.Fprintf(deps.Stdout, "%s %s\n", styled(deps, output.StyleSuccess, "Logged in as"), styled(deps, output.StyleBold, result.User.Username))
				fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "Workspace set to"), styled(deps, output.StyleAction, result.Workspace.Slug))
				return nil
			}

			selection, err := deps.Client.BootstrapIdentity(cmd.Context(), in)
			if err != nil {
				return apiExitError(deps, "user.login", err)
			}
			if err := saveIdentitySelection(deps, *selection); err != nil {
				payload := output.ErrorPayload{Code: "unavailable", Message: err.Error()}
				code := deps.Printer.Error("user.login", payload)
				return &output.ExitError{Code: code, Err: err}
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("user.login", selection)
				return nil
			}
			fmt.Fprintf(deps.Stdout, "%s %s\n", styled(deps, output.StyleSuccess, "Logged in as"), styled(deps, output.StyleBold, selection.User.Username))
			fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "Workspace set to"), styled(deps, output.StyleAction, selection.Workspace.Slug))
			return nil
		},
	}
	cmd.Flags().StringVar(&workspaceName, "workspace", "", "Workspace name to create or reuse")
	cmd.Flags().StringVar(&displayName, "display-name", "", "Display name for the local user")
	cmd.Flags().StringVar(&username, "username", "", "Username for attribution")
	cmd.Flags().StringVar(&email, "email", "", "Optional email address")
	return cmd
}

func newUserLogoutCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Clear the selected local user and workspace",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if deps.Topology == config.ModeLocal {
				store, err := openLocalStore(deps)
				if err != nil {
					return localExitError(deps, "user.logout", err)
				}
				defer store.Close()
				if err := store.ClearSelection(cmd.Context()); err != nil {
					return localExitError(deps, "user.logout", err)
				}
			}
			cfg, _ := config.LoadFrom(deps.ConfigPath)
			cfg.CurrentUser = config.CurrentUser{}
			cfg.Workspace = config.CurrentWorkspace{}
			cfg.Projects = nil
			if err := saveConfig(deps, cfg); err != nil {
				payload := output.ErrorPayload{Code: "unavailable", Message: err.Error()}
				code := deps.Printer.Error("user.logout", payload)
				return &output.ExitError{Code: code, Err: err}
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("user.logout", map[string]any{"logged_out": true})
				return nil
			}
			fmt.Fprintln(deps.Stdout, styled(deps, output.StyleSuccess, "Logged out"))
			return nil
		},
	}
}

func newUserListCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List local users",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if deps.Topology == config.ModeLocal {
				store, err := openLocalStore(deps)
				if err != nil {
					return localExitError(deps, "user.list", err)
				}
				defer store.Close()
				localUsers, err := store.ListUsers(cmd.Context())
				if err != nil {
					return localExitError(deps, "user.list", err)
				}
				users := make([]client.IdentityUser, 0, len(localUsers))
				for _, user := range localUsers {
					users = append(users, localIdentityUser(user))
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
					fmt.Fprintf(deps.Stdout, "%s %-24s %s\n", styled(deps, output.StyleBold, fmt.Sprintf("%-20s", user.Username)), user.DisplayName, email)
				}
				return nil
			}
			users, err := deps.Client.ListUsers(cmd.Context())
			if err != nil {
				return apiExitError(deps, "user.list", err)
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
				fmt.Fprintf(deps.Stdout, "%s %-24s %s\n", styled(deps, output.StyleBold, fmt.Sprintf("%-20s", user.Username)), user.DisplayName, email)
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
			fmt.Fprintf(deps.Stdout, "%s %s (%s)\n", label(deps, "user:"), styled(deps, output.StyleBold, cfg.CurrentUser.Username), cfg.CurrentUser.DisplayName)
			if cfg.CurrentUser.Email != "" {
				fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "email:"), cfg.CurrentUser.Email)
			}
			return nil
		},
	}
}

func newWorkspaceCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workspace",
		Short: "Manage local SpecGate workspaces",
		Example: strings.TrimSpace(`specgate workspace select platform
specgate workspace bind platform
specgate workspace current
specgate workspace members
specgate workspace unbind
specgate --workspace platform status
SPECGATE_WORKSPACE=platform specgate work list`),
	}
	cmd.AddCommand(newWorkspaceCreateCmd(deps), newWorkspaceListCmd(deps), newWorkspaceCurrentCmd(deps), newWorkspaceMembersCmd(deps), newWorkspaceSelectCmd(deps), newWorkspaceBindCmd(deps), newWorkspaceUnbindCmd(deps))
	return cmd
}

func newWorkspaceListCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List local workspaces",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if deps.Topology == config.ModeLocal {
				store, err := openLocalStore(deps)
				if err != nil {
					return localExitError(deps, "workspace.list", err)
				}
				defer store.Close()
				items, err := store.ListWorkspaces(cmd.Context())
				if err != nil {
					return localExitError(deps, "workspace.list", err)
				}
				if deps.Printer.Mode() == output.ModeJSON {
					deps.Printer.Success("workspace.list", map[string]any{"items": items})
					return nil
				}
				for _, workspace := range items {
					fmt.Fprintf(deps.Stdout, "%s %s\n", styled(deps, output.StyleBold, fmt.Sprintf("%-24s", workspace.Slug)), workspace.Name)
				}
				return nil
			}
			workspaces, err := deps.Client.ListWorkspaces(cmd.Context())
			if err != nil {
				return apiExitError(deps, "workspace.list", err)
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("workspace.list", map[string]any{"items": workspaces})
				return nil
			}
			for _, workspace := range workspaces {
				fmt.Fprintf(deps.Stdout, "%s %s\n", styled(deps, output.StyleBold, fmt.Sprintf("%-24s", workspace.Slug)), workspace.Name)
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
		RunE: func(cmd *cobra.Command, _ []string) error {
			if deps.Topology == config.ModeLocal {
				store, err := openLocalStore(deps)
				if err != nil {
					return localExitError(deps, "workspace.current", err)
				}
				defer store.Close()
				selection, err := localSelection(cmd.Context(), deps, store)
				if err != nil {
					return localExitError(deps, "workspace.current", err)
				}
				workspace := localWorkspaceConfig(selection.Workspace)
				resolved := currentWorkspaceSelection(deps)
				if deps.Printer.Mode() == output.ModeJSON {
					deps.Printer.Success("workspace.current", map[string]any{
						"workspace":    workspace,
						"source":       "local store",
						"scope":        resolved.Source,
						"project_root": resolved.ProjectRoot,
					})
					return nil
				}
				fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "workspace:"), styled(deps, output.StyleBold, formatWorkspaceLabel(workspace)))
				fmt.Fprintln(deps.Stdout, label(deps, "source: local store"))
				fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "scope:"), formatWorkspaceSource(resolved))
				printWorkspaceProjectContext(deps, resolved)
				return nil
			}
			selection := currentWorkspaceSelection(deps)
			if selection.Workspace.ID == "" && selection.Workspace.Slug == "" {
				payload := output.ErrorPayload{Code: "not_found", Message: "no workspace selected"}
				code := deps.Printer.Error("workspace.current", payload)
				return &output.ExitError{Code: code}
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("workspace.current", workspaceSelectionView(selection))
				return nil
			}
			fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "workspace:"), styled(deps, output.StyleBold, formatWorkspaceLabel(selection.Workspace)))
			fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "source:"), formatWorkspaceSource(selection))
			printWorkspaceProjectContext(deps, selection)
			return nil
		},
	}
}

func printWorkspaceProjectContext(deps *Deps, selection config.ResolvedWorkspace) {
	if selection.ProjectRoot == "" {
		return
	}
	if selection.Source != config.WorkspaceSourceGlobal {
		fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "project:"), selection.ProjectRoot)
		return
	}
	fmt.Fprintf(deps.Stdout, "%s %s (not bound)\n", label(deps, "project:"), selection.ProjectRoot)
	if selection.Workspace.Slug == "" {
		return
	}
	if humanVisuals(deps) {
		fmt.Fprintln(deps.Stdout, nextStep(deps, "use this workspace automatically in this checkout:", "specgate workspace bind"))
		return
	}
	fmt.Fprintln(deps.Stdout, "next: run `specgate workspace bind` to use this workspace automatically in this checkout")
}

func newWorkspaceMembersCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "members",
		Short: "List members in the selected workspace",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, _ := config.LoadFrom(deps.ConfigPath)
			selection := resolveWorkspaceSelection(deps, cfg)
			if selection.Workspace.ID == "" && selection.Workspace.Slug == "" {
				payload := output.ErrorPayload{Code: "not_found", Message: "no workspace selected"}
				code := deps.Printer.Error("workspace.members", payload)
				return &output.ExitError{Code: code}
			}
			workspaceID, err := workspaceIDForSelection(cmd.Context(), deps, selection)
			if err != nil {
				return apiExitError(deps, "workspace.members", err)
			}
			members, err := deps.Client.ListWorkspaceMembers(cmd.Context(), workspaceID, cfg.CurrentUser.ID, cfg.CurrentUser.Username)
			if err != nil {
				return apiExitError(deps, "workspace.members", err)
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("workspace.members", members)
				return nil
			}
			workspaceLabel := members.Workspace.Slug
			if workspaceLabel == "" {
				workspaceLabel = selection.Workspace.Slug
			}
			if workspaceLabel == "" {
				workspaceLabel = workspaceID
			}
			fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "workspace:"), styled(deps, output.StyleBold, workspaceLabel))
			for _, member := range members.Members {
				marker := ""
				if member.Current {
					marker = " (you)"
				}
				fmt.Fprintf(deps.Stdout, "%s %-24s %s%s\n", styled(deps, output.StyleBold, fmt.Sprintf("%-20s", member.Username)), member.DisplayName, styledStatusPadded(deps, member.Role, 10), marker)
			}
			return nil
		},
	}
}

func workspaceSelectionView(selection config.ResolvedWorkspace) map[string]any {
	return map[string]any{
		"workspace":    selection.Workspace,
		"source":       selection.Source,
		"project_root": selection.ProjectRoot,
	}
}

func formatWorkspaceLabel(workspace config.CurrentWorkspace) string {
	if workspace.Name == "" {
		return workspace.Slug
	}
	return fmt.Sprintf("%s (%s)", workspace.Slug, workspace.Name)
}

func formatWorkspaceSource(selection config.ResolvedWorkspace) string {
	switch selection.Source {
	case config.WorkspaceSourceProject:
		return "project"
	case config.WorkspaceSourceRepo:
		return "repo default (.specgate/config)"
	case config.WorkspaceSourceGlobal:
		return "global default"
	case config.WorkspaceSourceOverride:
		return "override (--workspace/SPECGATE_WORKSPACE)"
	case config.WorkspaceSourceNone:
		return "none"
	default:
		return string(selection.Source)
	}
}

func newWorkspaceSelectCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "select [slug]",
		Short: "Select the global or project workspace used by subsequent CLI commands",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if deps.Topology == config.ModeLocal {
				if len(args) != 1 || strings.TrimSpace(args[0]) == "" {
					return localExitError(deps, "workspace.select", fmt.Errorf("workspace slug is required"))
				}
				store, err := openLocalStore(deps)
				if err != nil {
					return localExitError(deps, "workspace.select", err)
				}
				defer store.Close()
				if err := store.SelectWorkspace(cmd.Context(), strings.TrimSpace(args[0])); err != nil {
					return localExitError(deps, "workspace.select", err)
				}
				selection, err := store.Current(cmd.Context())
				if err != nil {
					return localExitError(deps, "workspace.select", err)
				}
				workspace := localWorkspaceIdentity(selection.Workspace)
				if err := saveWorkspaceSelection(deps, workspace, workspaceSaveScopeGlobal, ""); err != nil {
					return localExitError(deps, "workspace.select", err)
				}
				if deps.Printer.Mode() == output.ModeJSON {
					deps.Printer.Success("workspace.select", workspace)
					return nil
				}
				fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "Workspace set to"), styled(deps, output.StyleAction, workspace.Slug))
				return nil
			}
			var workspace *client.IdentityWorkspace
			var err error
			if len(args) == 0 {
				if !canPrompt(deps) {
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
				return apiExitError(deps, "workspace.select", err)
			}
			saveScope, projectRoot, err := selectWorkspaceSaveScope(deps)
			if err != nil {
				return err
			}
			if err := saveWorkspaceSelection(deps, *workspace, saveScope, projectRoot); err != nil {
				payload := output.ErrorPayload{Code: "unavailable", Message: err.Error()}
				code := deps.Printer.Error("workspace.select", payload)
				return &output.ExitError{Code: code, Err: err}
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("workspace.select", workspace)
				return nil
			}
			if saveScope == workspaceSaveScopeProject {
				fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "Project workspace set to"), styled(deps, output.StyleAction, workspace.Slug))
			} else {
				fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "Workspace set to"), styled(deps, output.StyleAction, workspace.Slug))
			}
			return nil
		},
	}
}

func newWorkspaceCreateCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "create <name>",
		Short: "Create a Local-mode workspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if deps.Topology != config.ModeLocal {
				return localExitError(deps, "workspace.create", fmt.Errorf("workspace creation is available in Local mode; use your Full-mode team workspace instead"))
			}
			store, err := openLocalStore(deps)
			if err != nil {
				return localExitError(deps, "workspace.create", err)
			}
			defer store.Close()
			workspace, err := store.CreateWorkspace(cmd.Context(), args[0])
			if err != nil {
				return localExitError(deps, "workspace.create", err)
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("workspace.create", workspace)
				return nil
			}
			fmt.Fprintf(deps.Stdout, "%s %s\n", styled(deps, output.StyleSuccess, "Workspace created:"), styled(deps, output.StyleAction, workspace.Slug))
			return nil
		},
	}
}

func localWorkspaceConfig(workspace local.Workspace) config.CurrentWorkspace {
	return config.CurrentWorkspace{ID: workspace.ID, Slug: workspace.Slug, Name: workspace.Name}
}

func localWorkspaceIdentity(workspace local.Workspace) client.IdentityWorkspace {
	return client.IdentityWorkspace{ID: workspace.ID, Slug: workspace.Slug, Name: workspace.Name}
}

func localIdentityUser(user local.User) client.IdentityUser {
	return client.IdentityUser{ID: user.ID, Username: user.Username, DisplayName: user.DisplayName, Email: user.Email}
}

func localIdentitySelection(selection local.Selection) client.IdentitySelection {
	return client.IdentitySelection{User: localIdentityUser(selection.User), Workspace: localWorkspaceIdentity(selection.Workspace)}
}

func localExitError(deps *Deps, command string, err error) error {
	payload := output.ErrorPayload{Code: "unavailable", Message: err.Error()}
	if errors.Is(err, sql.ErrNoRows) {
		payload = output.ErrorPayload{Code: "not_found", Message: "not found in the selected Local workspace; run `specgate workspace current` or `specgate workspace select <workspace>`"}
	} else if errors.Is(err, local.ErrGateTaskNotFound) {
		payload = output.ErrorPayload{Code: "not_found", Message: "gate task not found in the selected Local workspace"}
	} else if errors.Is(err, local.ErrGateTaskExpired) || errors.Is(err, local.ErrGateTaskStale) || errors.Is(err, local.ErrGateTaskInvalid) {
		payload = output.ErrorPayload{Code: "validation", Message: err.Error()}
	} else if errors.Is(err, local.ErrDeliveryApproved) {
		payload = output.ErrorPayload{Code: "conflict", Message: err.Error()}
	}
	code := deps.Printer.Error(command, payload)
	return &output.ExitError{Code: code, Err: err}
}

func newWorkspaceBindCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "bind [slug]",
		Short: "Bind the current Git project to a workspace",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			projectRoot, ok := config.FindProjectRoot(deps.WorkingDir)
			if !ok {
				payload := output.ErrorPayload{Code: "not_found", Message: "no Git project found from the current directory"}
				code := deps.Printer.Error("workspace.bind", payload)
				return &output.ExitError{Code: code}
			}
			if deps.Topology == config.ModeLocal {
				store, err := openLocalStore(deps)
				if err != nil {
					return localExitError(deps, "workspace.bind", err)
				}
				defer store.Close()
				if len(args) == 1 {
					if err := store.SelectWorkspace(cmd.Context(), strings.TrimSpace(args[0])); err != nil {
						return localExitError(deps, "workspace.bind", err)
					}
				}
				selection, err := store.Current(cmd.Context())
				if len(args) == 0 {
					selection, err = localSelection(cmd.Context(), deps, store)
				}
				if err != nil {
					return localExitError(deps, "workspace.bind", err)
				}
				if err := config.EnsureSpecgateDirGitignore(filepath.Join(projectRoot, ".specgate")); err != nil {
					return localExitError(deps, "workspace.bind", err)
				}
				cfg, _ := config.LoadFrom(deps.ConfigPath)
				cfg.SetProjectWorkspace(projectRoot, localWorkspaceConfig(selection.Workspace))
				if err := saveConfig(deps, cfg); err != nil {
					return localExitError(deps, "workspace.bind", err)
				}
				if deps.Printer.Mode() == output.ModeJSON {
					deps.Printer.Success("workspace.bind", map[string]any{"workspace": localWorkspaceIdentity(selection.Workspace), "source": config.WorkspaceSourceProject, "project_root": projectRoot})
					return nil
				}
				fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "Project workspace set to"), styled(deps, output.StyleAction, selection.Workspace.Slug))
				fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "Project:"), projectRoot)
				return nil
			}

			var workspace *client.IdentityWorkspace
			var err error
			if len(args) == 0 {
				workspace, err = workspaceForProjectBind(cmd, deps)
			} else {
				slug := strings.TrimSpace(args[0])
				if err := validateWorkspaceSlug(slug); err != nil {
					payload := output.ErrorPayload{Code: "validation", Message: err.Error()}
					code := deps.Printer.Error("workspace.bind", payload)
					return &output.ExitError{Code: code, Err: err}
				}
				workspace, err = deps.Client.GetWorkspace(cmd.Context(), slug)
			}
			if err != nil {
				if exitErr, ok := err.(*output.ExitError); ok {
					return exitErr
				}
				return apiExitError(deps, "workspace.bind", err)
			}
			if err := config.EnsureSpecgateDirGitignore(filepath.Join(projectRoot, ".specgate")); err != nil {
				payload := output.ErrorPayload{Code: "unavailable", Message: err.Error()}
				code := deps.Printer.Error("workspace.bind", payload)
				return &output.ExitError{Code: code, Err: err}
			}
			if err := saveWorkspaceSelection(deps, *workspace, workspaceSaveScopeProject, projectRoot); err != nil {
				payload := output.ErrorPayload{Code: "unavailable", Message: err.Error()}
				code := deps.Printer.Error("workspace.bind", payload)
				return &output.ExitError{Code: code, Err: err}
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("workspace.bind", map[string]any{
					"workspace":    workspace,
					"source":       config.WorkspaceSourceProject,
					"project_root": projectRoot,
				})
				return nil
			}
			fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "Project workspace set to"), styled(deps, output.StyleAction, workspace.Slug))
			fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "Project:"), projectRoot)
			return nil
		},
	}
}

func workspaceForProjectBind(cmd *cobra.Command, deps *Deps) (*client.IdentityWorkspace, error) {
	selection := currentWorkspaceSelection(deps)
	if selection.Workspace != (config.CurrentWorkspace{}) {
		return &client.IdentityWorkspace{
			ID:   selection.Workspace.ID,
			Slug: selection.Workspace.Slug,
			Name: selection.Workspace.Name,
		}, nil
	}
	if !canPrompt(deps) {
		payload := output.ErrorPayload{Code: "validation", Message: "workspace slug is required when no workspace is selected and input is disabled"}
		code := deps.Printer.Error("workspace.bind", payload)
		return nil, &output.ExitError{Code: code}
	}
	return promptWorkspaceSelection(cmd, deps)
}

func newWorkspaceUnbindCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "unbind",
		Short: "Remove the workspace binding for the current Git project",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			projectRoot, ok := config.FindProjectRoot(deps.WorkingDir)
			if !ok {
				payload := output.ErrorPayload{Code: "not_found", Message: "no Git project found from the current directory"}
				code := deps.Printer.Error("workspace.unbind", payload)
				return &output.ExitError{Code: code}
			}
			cfg, _ := config.LoadFrom(deps.ConfigPath)
			if !cfg.RemoveProjectWorkspace(projectRoot) {
				payload := output.ErrorPayload{Code: "not_found", Message: "no workspace binding exists for this project"}
				code := deps.Printer.Error("workspace.unbind", payload)
				return &output.ExitError{Code: code}
			}
			if err := saveConfig(deps, cfg); err != nil {
				payload := output.ErrorPayload{Code: "unavailable", Message: err.Error()}
				code := deps.Printer.Error("workspace.unbind", payload)
				return &output.ExitError{Code: code, Err: err}
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("workspace.unbind", map[string]any{"project_root": projectRoot, "unbound": true})
				return nil
			}
			fmt.Fprintf(deps.Stdout, "%s %s\n", styled(deps, output.StyleSuccess, "Project workspace binding removed for"), projectRoot)
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

func formatRequiredLoginFlags(workspaceName, displayName, username string) string {
	missing := make([]string, 0, 3)
	if strings.TrimSpace(workspaceName) == "" {
		missing = append(missing, "workspace")
	}
	if strings.TrimSpace(displayName) == "" {
		missing = append(missing, "display-name")
	}
	if strings.TrimSpace(username) == "" {
		missing = append(missing, "username")
	}
	switch len(missing) {
	case 0:
		return ""
	case 1:
		return missing[0]
	case 2:
		return missing[0] + " and " + missing[1]
	default:
		return strings.Join(missing[:len(missing)-1], ", ") + ", and " + missing[len(missing)-1]
	}
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
	cfg.Projects = nil
	return saveConfig(deps, cfg)
}

func saveWorkspaceSelection(deps *Deps, workspace client.IdentityWorkspace, scope string, projectRoot string) error {
	cfg, _ := config.LoadFrom(deps.ConfigPath)
	selected := config.CurrentWorkspace{
		ID:   workspace.ID,
		Slug: workspace.Slug,
		Name: workspace.Name,
	}
	if scope == workspaceSaveScopeProject {
		cfg.SetProjectWorkspace(projectRoot, selected)
	} else {
		cfg.Workspace = selected
		if projectRoot != "" {
			cfg.RemoveProjectWorkspace(projectRoot)
		}
	}
	return saveConfig(deps, cfg)
}

func selectWorkspaceSaveScope(deps *Deps) (string, string, error) {
	projectRoot, ok := config.FindProjectRoot(deps.WorkingDir)
	if !ok {
		return workspaceSaveScopeGlobal, "", nil
	}
	if !canPrompt(deps) {
		return workspaceSaveScopeGlobal, projectRoot, nil
	}
	scope, err := deps.Prompter.Select("Save workspace selection", []interactive.Option{
		{Label: "This project", Value: workspaceSaveScopeProject},
		{Label: "Global default", Value: workspaceSaveScopeGlobal},
	})
	if err != nil {
		return "", "", err
	}
	if scope == workspaceSaveScopeProject {
		return workspaceSaveScopeProject, projectRoot, nil
	}
	return workspaceSaveScopeGlobal, projectRoot, nil
}

func saveConfig(deps *Deps, cfg config.Config) error {
	if deps.ConfigPath != "" {
		return cfg.SaveTo(deps.ConfigPath)
	}
	return cfg.Save()
}

func currentWorkspaceSelection(deps *Deps) config.ResolvedWorkspace {
	cfg, _ := config.LoadFrom(deps.ConfigPath)
	return resolveWorkspaceSelection(deps, cfg)
}

func resolveWorkspaceSelection(deps *Deps, cfg config.Config) config.ResolvedWorkspace {
	repoCfg := config.LoadRepoConfig(deps.WorkingDir)
	return config.ResolveWorkspaceSource(cfg, repoCfg, deps.WorkingDir, deps.WorkspaceOverride)
}

func currentWorkspaceID(ctx context.Context, deps *Deps) (string, error) {
	return workspaceIDForSelection(ctx, deps, currentWorkspaceSelection(deps))
}

func workspaceIDForSelection(ctx context.Context, deps *Deps, selection config.ResolvedWorkspace) (string, error) {
	if selection.Workspace.ID != "" {
		return selection.Workspace.ID, nil
	}
	if selection.Workspace.Slug == "" {
		return "", nil
	}
	workspace, err := deps.Client.GetWorkspace(ctx, selection.Workspace.Slug)
	if err != nil {
		return "", err
	}
	return workspace.ID, nil
}

func currentActor(deps *Deps) string {
	cfg, _ := config.LoadFrom(deps.ConfigPath)
	if cfg.CurrentUser.Username != "" {
		return cfg.CurrentUser.Username
	}
	return cfg.CurrentUser.DisplayName
}

func annotateBodyWithCurrentSelection(ctx context.Context, deps *Deps, body map[string]any) error {
	if body == nil {
		return nil
	}
	cfg, _ := config.LoadFrom(deps.ConfigPath)
	if cfg.CurrentUser.Username != "" {
		if _, exists := body["created_by"]; !exists {
			body["created_by"] = cfg.CurrentUser.Username
		}
	}
	if _, exists := body["workspace_id"]; !exists {
		selection := resolveWorkspaceSelection(deps, cfg)
		workspaceID, err := workspaceIDForSelection(ctx, deps, selection)
		if err != nil {
			return err
		}
		if workspaceID != "" {
			body["workspace_id"] = workspaceID
		}
	}
	return nil
}

func requestContextForBody(ctx context.Context, body map[string]any) context.Context {
	workspaceID, _ := body["workspace_id"].(string)
	if workspaceID == "" {
		return ctx
	}
	return client.WithWorkspace(ctx, workspaceID)
}
