package command

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/buildinfo"
	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/deploy"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

// specgate init
func newInitCmd(deps *Deps) *cobra.Command {
	var (
		deployDir       string
		mode            string
		localDir        string
		seedFlag        bool
		noSeed          bool
		bundleVersion   string
		installPlugins  bool
		pluginAgentList string
		workspaceName   string
		displayName     string
		username        string
		email           string
	)
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize Local CLI or Full appliance SpecGate",
		Example: strings.TrimSpace(`
  # Interactive setup: choose Local or Full.
  specgate init

  # Non-interactive Local CLI setup: no Docker or server.
  specgate init --mode local --no-input \
    --workspace-name "My workspace" \
    --display-name "Jane Doe" \
    --username jane

  # Full appliance setup: Docker, browser UI, chat, and team integrations.
  specgate init --mode full
`),
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if mode == "" && !canPrompt(deps) {
				payload := output.ErrorPayload{Code: "usage", Message: "--mode local|full is required with --no-input"}
				code := deps.Printer.Error("init", payload)
				return &output.ExitError{Code: code, Err: fmt.Errorf("mode is required")}
			}
			if mode != "" && mode != "local" && mode != "full" {
				payload := output.ErrorPayload{Code: "usage", Message: "--mode must be local or full"}
				code := deps.Printer.Error("init", payload)
				return &output.ExitError{Code: code, Err: fmt.Errorf("invalid mode %q", mode)}
			}
			if err := validateInitModeFlags(cmd, mode); err != nil {
				code := deps.Printer.Error("init", output.ErrorPayload{Code: "validation_failed", Message: err.Error()})
				return &output.ExitError{Code: code, Err: err}
			}
			cfg, err := config.LoadFrom(deps.ConfigPath)
			if err != nil {
				code := deps.Printer.Error("init", output.ErrorPayload{Code: "unavailable", Message: err.Error()})
				return &output.ExitError{Code: code, Err: err}
			}
			if mode == "local" || mode == "" {
				if installPlugins && strings.TrimSpace(pluginAgentList) == "" {
					pluginAgentList = "codex"
				}
				return runLocalInit(cmd, deps, cfg, localDir, workspaceName, displayName, username, email, installPlugins, pluginAgentList)
			}
			dir := deployDir
			if dir == "" {
				dir = resolveDeployDir(deps)
			}

			seed := deploy.SeedAsk
			switch {
			case seedFlag:
				seed = deploy.SeedYes
			case noSeed || !canPrompt(deps):
				seed = deploy.SeedNo
			}

			if canPrompt(deps) && seed == deploy.SeedAsk {
				confirmed, err := deps.Prompter.Confirm("Seed demo data?", false)
				if err != nil {
					return err
				}
				if confirmed {
					seed = deploy.SeedYes
				} else {
					seed = deploy.SeedNo
				}
			}

			version := bundleVersion
			if version == "" {
				version = buildinfo.Version
			}

			bootstrapInput, err := resolveInitBootstrapInput(deps, seed, workspaceName, displayName, username, email)
			if err != nil {
				payload := output.ErrorPayload{Code: "validation", Message: err.Error()}
				code := deps.Printer.Error("init", payload)
				return &output.ExitError{Code: code, Err: err}
			}

			initSeed := seed
			if seed == deploy.SeedYes {
				initSeed = deploy.SeedNo
			}
			svc := makeDeployService(deps, dir)
			if err := svc.Init(ctx, deploy.InitOptions{Seed: initSeed, BundleVersion: version}); err != nil {
				code := deps.Printer.Error("init", output.ErrorPayload{Code: "unavailable", Message: err.Error()})
				return &output.ExitError{Code: code, Err: err}
			}

			// Persist the deployment dir for future up/down/status calls.
			if cfg.Mode == config.ModeLocal {
				// Local IDs and project bindings are private SQLite state, not valid
				// Full-appliance identities or workspace references.
				cfg.CurrentUser = config.CurrentUser{}
				cfg.Workspace = config.CurrentWorkspace{}
				cfg.Projects = nil
			}
			cfg.Mode = config.ModeFull
			cfg.Local = config.LocalStore{}
			cfg.DeploymentDir = dir
			if (cfg.Server == "" || cfg.Server == config.DefaultServerURL) && os.Getenv("SPECGATE_SERVER") == "" && deps.ServerURL == config.DefaultServerURL {
				if inferred := inferLocalServerURL(dir); inferred != "" {
					cfg.Server = inferred
					deps.ServerURL = inferred
					deps.PluginRegistryURL = inferred
					if _, ok := deps.Client.(*client.Client); ok {
						deps.Client = client.New(inferred, deps.Timeout)
					}
				}
			}
			if err := saveConfig(deps, cfg); err != nil {
				code := deps.Printer.Error("init", output.ErrorPayload{Code: "unavailable", Message: err.Error()})
				return &output.ExitError{Code: code, Err: err}
			}

			var selection *client.IdentitySelection
			if bootstrapInput != nil {
				selection, err = deps.Client.BootstrapIdentity(ctx, *bootstrapInput)
				if err != nil {
					return apiExitError(deps, "init", err)
				}
				if err := saveIdentitySelection(deps, *selection); err != nil {
					code := deps.Printer.Error("init", output.ErrorPayload{Code: "unavailable", Message: err.Error()})
					return &output.ExitError{Code: code, Err: err}
				}
			} else if canPrompt(deps) {
				bootstrap, err := promptIdentityBootstrap(deps)
				if err != nil {
					return err
				}
				selection, err = deps.Client.BootstrapIdentity(ctx, bootstrap)
				if err != nil {
					return apiExitError(deps, "init", err)
				}
				if err := saveIdentitySelection(deps, *selection); err != nil {
					code := deps.Printer.Error("init", output.ErrorPayload{Code: "unavailable", Message: err.Error()})
					return &output.ExitError{Code: code, Err: err}
				}
			}

			if seed == deploy.SeedYes && initSeed == deploy.SeedNo {
				seedOpts := deploy.SeedDemoOptions{}
				if selection != nil {
					seedOpts.WorkspaceID = selection.Workspace.ID
					seedOpts.CreatedBy = selection.User.Username
				}
				if err := svc.SeedDemo(ctx, seedOpts); err != nil {
					code := deps.Printer.Error("init", output.ErrorPayload{Code: "unavailable", Message: err.Error()})
					return &output.ExitError{Code: code, Err: err}
				}
			}

			if canPrompt(deps) && !installPlugins {
				confirmed, err := deps.Prompter.Confirm("Install IDE plugins now?", true)
				if err != nil {
					return err
				}
				installPlugins = confirmed
			}
			if installPlugins {
				if err := resolvePluginAgentPrompt(cmd, deps, &pluginAgentList, "Select IDE plugins", "plugin-agent"); err != nil {
					code := deps.Printer.Error("init", output.ErrorPayload{Code: "validation_failed", Message: err.Error()})
					return &output.ExitError{Code: code, Err: err}
				}
			}

			var pluginResult *pluginInstallResult
			if installPlugins {
				if deps.Printer.Mode() != output.ModeJSON {
					fmt.Fprintln(deps.Stderr, "Installing IDE plugins...")
				}
				result, err := runPluginInstall(ctx, deps, pluginInstallOptions{
					Agent: pluginAgentList,
				})
				if err != nil {
					payload := pluginInstallErrorPayload(err)
					code := deps.Printer.Error("init", payload)
					return &output.ExitError{Code: code, Err: err}
				}
				pluginResult = &result
			}

			if deps.Printer.Mode() == output.ModeJSON {
				result := map[string]any{"dir": dir}
				if selection != nil {
					result["user"] = selection.User
					result["workspace"] = selection.Workspace
				}
				if pluginResult != nil {
					result["plugins"] = pluginResult
				}
				deps.Printer.Success("init", result)
				return nil
			}
			webURL := strings.TrimSuffix(inferLocalServerURL(dir), "/api/doc-registry")
			fmt.Fprintln(deps.Stdout, title(deps, "SpecGate Full is ready"))
			fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "Web UI:"), webURL)
			fmt.Fprintf(deps.Stdout, "%s %s\n\n", label(deps, "Local files:"), dir)
			if selection != nil {
				fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "Current user:"), selection.User.Username)
				fmt.Fprintf(deps.Stdout, "%s %s\n\n", label(deps, "Workspace:"), selection.Workspace.Slug)
			}
			fmt.Fprintln(deps.Stdout, title(deps, "Next steps:"))
			fmt.Fprintf(deps.Stdout, "  %s  verify services are healthy\n", styled(deps, output.StyleAction, "specgate doctor"))
			if pluginResult != nil {
				fmt.Fprintf(deps.Stdout, "  %s  verify IDE plugin files\n", styled(deps, output.StyleAction, "specgate plugins doctor --agent "+pluginAgentList))
			} else {
				fmt.Fprintf(deps.Stdout, "  %s  install IDE plugins\n", styled(deps, output.StyleAction, "specgate plugins install --agent all"))
			}
			fmt.Fprintf(deps.Stdout, "  %s  configure a model (optional; reads OPENAI_API_KEY)\n", styled(deps, output.StyleAction, "specgate model set --provider openai"))
			fmt.Fprintf(deps.Stdout, "  %s  view the governance board\n", styled(deps, output.StyleAction, "specgate status"))
			fmt.Fprintln(deps.Stdout)
			fmt.Fprintln(deps.Stdout, "A server-side model is optional — without one, readiness gates run on your IDE agent.")
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&mode, "mode", "", "Setup mode: local (CLI only) or full (Docker appliance)")
	f.StringVar(&localDir, "local-dir", "", "Local SQLite state directory (Local mode only)")
	f.StringVar(&deployDir, "dir", "", "Deployment directory (Full mode only; default ~/.specgate)")
	f.BoolVar(&seedFlag, "seed", false, "Seed demo data after startup (Full mode only)")
	f.BoolVar(&noSeed, "no-seed", false, "Skip seeding demo data (Full mode only)")
	f.StringVar(&bundleVersion, "bundle-version", "", "Compose bundle release to download (Full mode only; default: this CLI's version)")
	f.BoolVar(&installPlugins, "install-plugins", false, "Install matching IDE plugin files after setup")
	f.StringVar(&pluginAgentList, "plugin-agent", "", "IDE plugin target for --install-plugins: cursor, codex, claude, all, or a comma-separated subset")
	f.StringVar(&workspaceName, "workspace-name", "", "Workspace name to create or reuse during local identity setup")
	f.StringVar(&displayName, "display-name", "", "Display name for the local user")
	f.StringVar(&username, "username", "", "Username for attribution")
	f.StringVar(&email, "email", "", "Optional email address for local identity setup")
	cmd.MarkFlagsMutuallyExclusive("seed", "no-seed")
	return cmd
}

func validateInitModeFlags(cmd *cobra.Command, mode string) error {
	var incompatible []string
	switch mode {
	case "local", "":
		for _, name := range []string{"dir", "seed", "no-seed", "bundle-version"} {
			if cmd.Flags().Changed(name) {
				incompatible = append(incompatible, "--"+name)
			}
		}
		if len(incompatible) > 0 {
			return fmt.Errorf("%s can be used only with --mode full; remove %s or choose Full mode", strings.Join(incompatible, ", "), strings.Join(incompatible, ", "))
		}
	case "full":
		if cmd.Flags().Changed("local-dir") {
			return fmt.Errorf("--local-dir can be used only with --mode local; remove --local-dir or choose Local mode")
		}
	}
	return nil
}

func runLocalInit(cmd *cobra.Command, deps *Deps, cfg config.Config, stateDir, workspaceName, displayName, username, email string, installPlugins bool, pluginAgentList string) error {
	var err error
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
	if strings.TrimSpace(stateDir) == "" {
		stateDir = strings.TrimSpace(os.Getenv("SPECGATE_LOCAL_DIR"))
	}
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
	if root, ok := config.FindProjectRoot(deps.WorkingDir); ok {
		specgateDir := filepath.Join(root, ".specgate")
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
	if cfg.Mode == config.ModeFull {
		// A Local run must not retain a server, appliance, or Full workspace
		// selection that could make an offline workflow appear connected.
		cfg.Server = ""
		cfg.DeploymentDir = ""
		cfg.Projects = nil
	}
	cfg.Mode = config.ModeLocal
	cfg.Local = config.LocalStore{Path: stateDir, ID: selection.StoreID}
	cfg.CurrentUser = config.CurrentUser{ID: selection.User.ID, Username: selection.User.Username, DisplayName: selection.User.DisplayName, Email: selection.User.Email}
	cfg.Workspace = config.CurrentWorkspace{ID: selection.Workspace.ID, Slug: selection.Workspace.Slug, Name: selection.Workspace.Name}
	if err := saveConfig(deps, cfg); err != nil {
		code := deps.Printer.Error("init", output.ErrorPayload{Code: "unavailable", Message: err.Error()})
		return &output.ExitError{Code: code, Err: err}
	}
	var pluginResult *pluginInstallResult
	if installPlugins {
		installed, err := runPluginInstall(cmd.Context(), deps, pluginInstallOptions{Agent: pluginAgentList})
		if err != nil {
			payload := pluginInstallErrorPayload(err)
			code := deps.Printer.Error("init", payload)
			return &output.ExitError{Code: code, Err: err}
		}
		pluginResult = &installed
	}
	selectedPluginAgents := strings.TrimSpace(pluginAgentList)
	if selectedPluginAgents == "" {
		selectedPluginAgents = "codex"
	}
	next := "specgate plugins install --agent " + selectedPluginAgents
	if pluginResult != nil {
		next = "specgate plugins doctor --agent " + selectedPluginAgents
	}
	result := map[string]any{"mode": config.ModeLocal, "state_dir": stateDir, "user": cfg.CurrentUser, "workspace": cfg.Workspace, "next": next}
	if pluginResult != nil {
		result["plugins"] = pluginResult
	}
	if deps.Printer.Mode() == output.ModeJSON {
		deps.Printer.Success("init", result)
		return nil
	}
	fmt.Fprintln(deps.Stdout, title(deps, "SpecGate Local is ready"))
	fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "Workspace:"), cfg.Workspace.Slug)
	fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "User:"), cfg.CurrentUser.Username)
	fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "State:"), stateDir)
	if pluginResult != nil {
		fmt.Fprintln(deps.Stdout, nextStep(deps, "Verify installed IDE plugins with", next))
	} else {
		fmt.Fprintln(deps.Stdout, nextStep(deps, "Install the Codex plugin with", "specgate plugins install --agent codex"))
	}
	return nil
}

func inferLocalServerURL(dir string) string {
	port := strings.TrimSpace(os.Getenv("SPECGATE_PORT"))
	if port == "" {
		port = "3000"
		data, err := os.ReadFile(filepath.Join(dir, ".env"))
		if err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				key, value, ok := strings.Cut(line, "=")
				if !ok || strings.TrimSpace(key) != "SPECGATE_PORT" {
					continue
				}
				value = strings.Trim(strings.TrimSpace(value), `"'`)
				if value != "" {
					port = value
				}
				break
			}
		}
	}
	return "http://localhost:" + port + "/api/doc-registry"
}

func promptIdentityBootstrap(deps *Deps) (client.IdentityBootstrapInput, error) {
	workspaceName, err := deps.Prompter.Input("Workspace name", "My workspace", requiredPrompt("workspace name"))
	if err != nil {
		return client.IdentityBootstrapInput{}, err
	}
	displayName, err := deps.Prompter.Input("Your display name", "Jane Doe", requiredPrompt("display name"))
	if err != nil {
		return client.IdentityBootstrapInput{}, err
	}
	username, err := deps.Prompter.Input("Username", "jane", validateUsernamePrompt)
	if err != nil {
		return client.IdentityBootstrapInput{}, err
	}
	email, err := deps.Prompter.Input("Email (optional)", "jane@example.com", nil)
	if err != nil {
		return client.IdentityBootstrapInput{}, err
	}
	return client.IdentityBootstrapInput{
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

func resolveInitBootstrapInput(deps *Deps, seed deploy.SeedChoice, workspaceName, displayName, username, email string) (*client.IdentityBootstrapInput, error) {
	trimmedWorkspace := strings.TrimSpace(workspaceName)
	trimmedDisplayName := strings.TrimSpace(displayName)
	trimmedUsername := strings.ToLower(strings.TrimSpace(username))
	trimmedEmail := strings.TrimSpace(email)

	provided := 0
	if trimmedWorkspace != "" {
		provided++
	}
	if trimmedDisplayName != "" {
		provided++
	}
	if trimmedUsername != "" {
		provided++
	}
	if trimmedEmail != "" {
		provided++
	}

	if provided == 0 {
		if seed == deploy.SeedYes && !canPrompt(deps) {
			return nil, fmt.Errorf("workspace-name, display-name, and username are required when using --seed with --no-input")
		}
		return nil, nil
	}

	if trimmedWorkspace == "" || trimmedDisplayName == "" || trimmedUsername == "" {
		return nil, fmt.Errorf("workspace-name, display-name, and username must be provided together when setting init identity flags")
	}

	if err := validateUsernamePrompt(trimmedUsername); err != nil {
		return nil, err
	}

	return &client.IdentityBootstrapInput{
		WorkspaceName: trimmedWorkspace,
		DisplayName:   trimmedDisplayName,
		Username:      trimmedUsername,
		Email:         trimmedEmail,
	}, nil
}
