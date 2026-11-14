package command

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/interactive"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

// specgate init
func newInitCmd(deps *Deps) *cobra.Command {
	var (
		mode            string
		localDir        string
		installPlugins  bool
		pluginAgentList string
		pluginScope     string
		workspaceName   string
		displayName     string
		username        string
		email           string
	)
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize SpecGate Local",
		Example: strings.TrimSpace(`
  # Interactive setup.
  specgate init

  # Non-interactive Local CLI setup: no Docker or server.
  specgate init --mode local --no-input \
    --workspace-name "My workspace" \
    --display-name "Jane Doe" \
    --username jane

`),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if mode != "" && mode != "local" {
				return initUsageError(deps, "--mode must be local")
			}
			if err := validateInitPluginScope(pluginScope); err != nil {
				return initUsageError(deps, err.Error())
			}
			cfg, err := config.LoadFrom(deps.ConfigPath)
			if err != nil {
				return localExitError(deps, "init", err)
			}
			writeInitWelcome(deps)
			if canPrompt(deps) && !installPlugins {
				confirmed, err := deps.Prompter.Confirm("Install IDE plugins now?", false)
				if err != nil {
					return err
				}
				installPlugins = confirmed
			}
			if installPlugins && strings.TrimSpace(pluginAgentList) == "" {
				if canPrompt(deps) {
					if err := resolvePluginAgentPrompt(cmd, deps, &pluginAgentList, "Select IDE plugins", "plugin-agent"); err != nil {
						return err
					}
				} else {
					pluginAgentList = "codex"
				}
			}
			if installPlugins && canPrompt(deps) && !cmd.Flags().Changed("plugin-scope") {
				selected, err := deps.Prompter.Select("Install scope", []interactive.Option{
					{Label: "Global — available in every project; does not modify this repository", Value: "global"},
					{Label: "This project — writes IDE files into this repository", Value: "project"},
				})
				if err != nil {
					return err
				}
				pluginScope = selected
				if err := validateInitPluginScope(pluginScope); err != nil {
					return initUsageError(deps, err.Error())
				}
			}
			return runLocalInit(cmd, deps, cfg, localDir, workspaceName, displayName, username, email, installPlugins, pluginAgentList, pluginScope)
		},
	}
	f := cmd.Flags()
	f.StringVar(&mode, "mode", "", "Setup mode: local")
	f.StringVar(&localDir, "local-dir", "", "Local SQLite state directory (Local mode only)")
	f.BoolVar(&installPlugins, "install-plugins", false, "Install matching IDE plugin files after setup")
	f.StringVar(&pluginAgentList, "plugin-agent", "", "IDE plugin target for --install-plugins: "+supportedPluginAgentList())
	f.StringVar(&pluginScope, "plugin-scope", "global", "IDE plugin install scope: global or project")
	f.StringVar(&workspaceName, "workspace-name", "", "Workspace name to create or reuse during local identity setup")
	f.StringVar(&displayName, "display-name", "", "Display name for the local user")
	f.StringVar(&username, "username", "", "Username for attribution")
	f.StringVar(&email, "email", "", "Optional email address for local identity setup")
	return cmd
}

func runLocalInit(cmd *cobra.Command, deps *Deps, cfg config.Config, stateDir, workspaceName, displayName, username, email string, installPlugins bool, pluginAgentList, pluginScope string) error {
	var err error
	projectLocal := pluginScope == "project"
	if installPlugins {
		if projectLocal {
			root, ok := config.FindProjectRoot(deps.WorkingDir)
			if !ok {
				return initUsageError(deps, "project plugin scope requires a Git repository; cd to its root and retry")
			}
			working, _ := filepath.Abs(deps.WorkingDir)
			if real, evalErr := filepath.EvalSymlinks(working); evalErr == nil {
				working = real
			}
			if filepath.Clean(working) != filepath.Clean(root) {
				return initUsageError(deps, fmt.Sprintf("project plugin scope must run from the repository root; run `cd %s` and retry; no Local state or plugin files were changed", root))
			}
		}
		// The installer dry run performs the same package, ownership, and native
		// manager checks as installation without writing plugin or Local state.
		if _, err := runPluginInstall(cmd.Context(), deps, pluginInstallOptions{Agent: pluginAgentList, ProjectLocal: projectLocal, DryRun: true}); err != nil {
			payload := pluginInstallErrorPayload(err)
			code := deps.Printer.Error("init", payload)
			return &output.ExitError{Code: code, Err: err}
		}
	}
	if strings.TrimSpace(workspaceName) == "" || strings.TrimSpace(displayName) == "" || strings.TrimSpace(username) == "" {
		if canPrompt(deps) {
			input, promptErr := promptIdentityBootstrap(deps)
			if promptErr != nil {
				return promptErr
			}
			workspaceName, displayName, username, email = input.WorkspaceName, input.DisplayName, input.Username, input.Email
		} else {
			payload := output.ErrorPayload{Code: "validation", Message: "workspace-name, display-name, and username are required for Local mode with --no-input"}
			code := deps.Printer.Error("init", payload)
			return &output.ExitError{Code: code, Err: fmt.Errorf("local identity is required")}
		}
	}
	if err := validateUsernamePrompt(strings.ToLower(strings.TrimSpace(username))); err != nil {
		code := deps.Printer.Error("init", output.ErrorPayload{Code: "validation", Message: err.Error()})
		return &output.ExitError{Code: code, Err: err}
	}
	projectRoot, _ := config.FindProjectRoot(deps.WorkingDir)
	project := cfg.Projects[projectRoot]
	stateDir = config.ResolveLocalDir(stateDir, os.Getenv("SPECGATE_LOCAL_DIR"), project, cfg, "")
	if strings.TrimSpace(stateDir) == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			code := deps.Printer.Error("init", output.ErrorPayload{Code: "unavailable", Message: err.Error()})
			return &output.ExitError{Code: code, Err: err}
		}
		stateDir = filepath.Join(base, "specgate", "local")
	}
	stateDir, err = filepath.Abs(stateDir)
	if err != nil {
		code := deps.Printer.Error("init", output.ErrorPayload{Code: "usage", Message: err.Error()})
		return &output.ExitError{Code: code, Err: err}
	}
	if err := validateRealDirectoryIfExists(stateDir); err != nil {
		code := deps.Printer.Error("init", output.ErrorPayload{Code: "validation", Message: err.Error()})
		return &output.ExitError{Code: code, Err: err}
	}
	if projectRoot != "" {
		specgateDir := filepath.Join(projectRoot, ".specgate")
		if relative, err := filepath.Rel(specgateDir, stateDir); err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			if err := config.EnsureSpecgateDirGitignore(specgateDir); err != nil {
				code := deps.Printer.Error("init", output.ErrorPayload{Code: "unavailable", Message: err.Error()})
				return &output.ExitError{Code: code, Err: err}
			}
		}
	}
	store, err := local.Open(filepath.Join(stateDir, "state.db"))
	if err != nil {
		code := deps.Printer.Error("init", output.ErrorPayload{Code: "unavailable", Message: err.Error()})
		return &output.ExitError{Code: code, Err: err}
	}
	defer store.Close()
	selection, err := store.Initialize(cmd.Context(), local.InitInput{WorkspaceName: workspaceName, DisplayName: displayName, Username: strings.ToLower(strings.TrimSpace(username)), Email: strings.TrimSpace(email)})
	if err != nil {
		code := deps.Printer.Error("init", output.ErrorPayload{Code: "validation", Message: err.Error()})
		return &output.ExitError{Code: code, Err: err}
	}
	cfg.Mode = config.ModeLocal
	selectedStore := config.LocalStore{Path: stateDir, ID: selection.StoreID}
	selectedUser := config.CurrentUser{ID: selection.User.ID, Username: selection.User.Username, DisplayName: selection.User.DisplayName, Email: selection.User.Email}
	selectedWorkspace := localWorkspaceConfig(selection.Workspace)
	if project.Local.Path == "" || selectedStore.Path == cfg.Local.Path {
		cfg.Local = selectedStore
		cfg.CurrentUser = selectedUser
		cfg.Workspace = selectedWorkspace
	}
	if projectRoot != "" {
		if err := config.EnsureSpecgateDirGitignore(filepath.Join(projectRoot, ".specgate")); err != nil {
			return localExitError(deps, "init", err)
		}
		cfg.SetProjectWorkspace(projectRoot, selectedWorkspace)
		project = cfg.Projects[projectRoot]
		project.Local = selectedStore
		cfg.Projects[projectRoot] = project
	}
	if err := saveConfig(deps, cfg); err != nil {
		code := deps.Printer.Error("init", output.ErrorPayload{Code: "unavailable", Message: err.Error()})
		return &output.ExitError{Code: code, Err: err}
	}
	var pluginResult *pluginInstallResult
	if installPlugins {
		installed, err := runPluginInstall(cmd.Context(), deps, pluginInstallOptions{Agent: pluginAgentList, ProjectLocal: projectLocal})
		if err != nil {
			payload := pluginInstallErrorPayload(err)
			if payload.Details == nil {
				payload.Details = map[string]any{}
			}
			payload.Details["local_setup"] = "complete"
			payload.Details["next"] = pluginRepairCommand(pluginAgentList, projectLocal)
			code := deps.Printer.Error("init", payload)
			return &output.ExitError{Code: code, Err: err}
		}
		pluginResult = &installed
	}
	next := "specgate plugins install"
	if pluginResult != nil {
		next = "specgate plugins doctor --agent " + strings.Join(pluginResult.Agents, ",")
		if projectLocal {
			next += " --project-local"
		}
	}
	result := map[string]any{"mode": config.ModeLocal, "state_dir": stateDir, "user": selectedUser, "workspace": selectedWorkspace, "next": next}
	if projectRoot != "" {
		result["project_root"] = projectRoot
	}
	if pluginResult != nil {
		result["plugins"] = pluginResult
	}
	if deps.Printer.Mode() == output.ModeJSON {
		deps.Printer.Success("init", result)
		return nil
	}
	fmt.Fprintln(deps.Stdout, title(deps, "SpecGate Local is ready"))
	fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "Workspace:"), selectedWorkspace.Slug)
	fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "User:"), selectedUser.Username)
	fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "State:"), stateDir)
	if projectRoot != "" {
		fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "Project:"), projectRoot)
	}
	if pluginResult != nil {
		fmt.Fprintln(deps.Stdout, nextStep(deps, "Verify installed IDE plugins with", next))
	} else {
		fmt.Fprintln(deps.Stdout, nextStep(deps, "Install IDE integration files with", "specgate plugins install"))
	}
	return nil
}

func validateInitPluginScope(scope string) error {
	if scope != "global" && scope != "project" {
		return fmt.Errorf("--plugin-scope must be global or project")
	}
	return nil
}

func initUsageError(deps *Deps, message string) error {
	err := errors.New(message)
	code := deps.Printer.Error("init", output.ErrorPayload{Code: "usage", Message: message})
	return &output.ExitError{Code: code, Err: err}
}

func promptIdentityBootstrap(deps *Deps) (local.InitInput, error) {
	workspaceName, err := deps.Prompter.Input("Workspace name", "My workspace", requiredPrompt("workspace name"))
	if err != nil {
		return local.InitInput{}, err
	}
	displayName, err := deps.Prompter.Input("Your display name", "Jane Doe", requiredPrompt("display name"))
	if err != nil {
		return local.InitInput{}, err
	}
	username, err := deps.Prompter.Input("Username", "jane", validateUsernamePrompt)
	if err != nil {
		return local.InitInput{}, err
	}
	email, err := deps.Prompter.Input("Email (optional)", "jane@example.com", nil)
	if err != nil {
		return local.InitInput{}, err
	}
	return local.InitInput{
		WorkspaceName: strings.TrimSpace(workspaceName),
		DisplayName:   strings.TrimSpace(displayName),
		Username:      strings.ToLower(strings.TrimSpace(username)),
		Email:         strings.TrimSpace(email),
	}, nil
}

func requiredPrompt(name string) func(string) error {
	return func(value string) error {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", name)
		}
		return nil
	}
}
