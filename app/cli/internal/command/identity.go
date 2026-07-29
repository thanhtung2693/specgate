package command

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/config"
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
						Message: formatRequiredLoginFlags(workspaceName, displayName, username),
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
