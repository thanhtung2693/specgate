package command

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/buildinfo"
	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/deploy"
	"github.com/specgate/specgate/app/cli/internal/interactive"
	"github.com/specgate/specgate/app/cli/internal/output"
)

const apiVersion = "specgate.api/v1"

const (
	defaultPublicRegistryURL = "https://raw.githubusercontent.com/thanhtung2693/specgate/main"
	defaultCLIInstallURL     = defaultPublicRegistryURL + "/scripts/install-cli.sh"
)

type updateStepResult struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

type progressEvent struct {
	Event   string `json:"event"`
	Command string `json:"command"`
	Step    string `json:"step,omitempty"`
	Message string `json:"message,omitempty"`
}

func registerSystemCommands(root *cobra.Command, deps *Deps) {
	root.AddCommand(newVersionCmd(deps))
	root.AddCommand(newStatusCmd(deps))
	root.AddCommand(newDoctorCmd(deps))
	root.AddCommand(newOpenCmd(deps))
	root.AddCommand(newConfigCmd(deps))
	root.AddCommand(newUpdateCmd(deps))
	root.AddCommand(newUninstallCmd(deps))
	root.AddCommand(newInitCmd(deps))
	root.AddCommand(newUpCmd(deps))
	root.AddCommand(newDownCmd(deps))
	root.AddCommand(newLocalStatusCmd(deps))
}

func warnIfCLIUpdateRecommended(ctx context.Context, deps *Deps, cmd *cobra.Command) {
	if deps == nil || deps.Client == nil || deps.Printer == nil {
		return
	}
	if deps.versionWarningShown || deps.Printer.Mode() == output.ModeJSON {
		return
	}
	if !shouldCheckCLIUpdate(cmd) || isDevVersion(buildinfo.Version) {
		return
	}
	meta, err := deps.Client.Meta(ctx)
	if err != nil {
		return
	}
	recommended := strings.TrimSpace(meta.RecommendedCLIVersion)
	if recommended == "" {
		recommended = strings.TrimSpace(meta.ServerVersion)
	}
	if recommended == "" {
		recommended = strings.TrimSpace(meta.Version)
	}
	if isDevVersion(recommended) || versionsMatch(buildinfo.Version, recommended) {
		warnIfGitHubReleaseIsNewer(ctx, deps)
		return
	}
	deps.versionWarningShown = true
	fmt.Fprintf(deps.Stderr, "Warning: specgate CLI %s is not the server-recommended version %s. Run `specgate update` to update this install.\n", buildinfo.Version, recommended)
}

func warnIfGitHubReleaseIsNewer(ctx context.Context, deps *Deps) {
	if deps.CheckLatestRelease == nil || publicUpdateCheckDisabled() {
		return
	}
	latest, err := deps.CheckLatestRelease(ctx, deps.Timeout, deps.UpdateCheckCachePath)
	if err != nil || !isVersionNewer(latest, buildinfo.Version) {
		return
	}
	deps.versionWarningShown = true
	fmt.Fprintf(deps.Stderr, "Warning: specgate CLI %s is older than latest GitHub release %s. Run `curl -fsSL https://raw.githubusercontent.com/thanhtung2693/specgate/main/scripts/install-cli.sh | sh` to update this install.\n", buildinfo.Version, latest)
}

func publicUpdateCheckDisabled() bool {
	return truthyEnv("SPECGATE_NO_UPDATE_CHECK") || truthyEnv("CI")
}

func truthyEnv(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func shouldCheckCLIUpdate(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	root := cmd.Root()
	if root == nil {
		return false
	}
	args := strings.Fields(strings.TrimPrefix(cmd.CommandPath(), root.Name()))
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "config", "completion", "down", "help", "init", "local-status", "uninstall", "update", "up", "version":
		return false
	default:
		return true
	}
}

func isDevVersion(v string) bool {
	v = strings.ToLower(strings.TrimSpace(v))
	return v == "" || v == "dev" || v == "unknown"
}

func versionsMatch(a, b string) bool {
	return normalizeVersion(a) == normalizeVersion(b)
}

func normalizeVersion(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	v = strings.TrimPrefix(v, "version ")
	v = strings.TrimPrefix(v, "specgate ")
	v = strings.TrimPrefix(v, "v")
	return v
}

func newVersionCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the CLI version",
		Run: func(*cobra.Command, []string) {
			fmt.Fprintf(deps.Stdout, "specgate %s\n", buildinfo.Version)
		},
	}
}

// specgate status
func newStatusCmd(deps *Deps) *cobra.Command {
	var allWorkspaces bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show governance board overview",
		RunE: func(cmd *cobra.Command, _ []string) error {
			workspaceID := ""
			if !allWorkspaces {
				workspaceID = currentIdentityConfig(deps).Workspace.ID
			}
			st, err := deps.Client.Status(cmd.Context(), workspaceID)
			if err != nil {
				code := deps.Printer.Error("status", mapAPIError("status", err))
				return &output.ExitError{Code: code, Err: err}
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("status", st)
				return nil
			}
			if humanVisuals(deps) {
				printStatusDashboard(deps, st, allWorkspaces)
			} else {
				printStatusSummary(deps, st, allWorkspaces)
				printAttentionSection(deps, st.NeedsAttention)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&allWorkspaces, "all-workspaces", false, "Show governance board overview across all workspaces")
	return cmd
}

func printStatusSummary(deps *Deps, st *client.GovernanceStatus, allWorkspaces bool) {
	scope := "selected workspace"
	if allWorkspaces {
		scope = "all workspaces"
	} else if ws := currentIdentityConfig(deps).Workspace.Slug; ws != "" {
		scope = fmt.Sprintf("selected workspace %q", ws)
	}

	fmt.Fprintf(deps.Stdout, "%s %s\n", styled(deps, output.StyleMuted, "Scope:"), scope)
	fmt.Fprintf(deps.Stdout, "%s %s total", styled(deps, output.StyleMuted, "Work:"), styled(deps, output.StyleBold, fmt.Sprintf("%d", st.Counts.Total)))
	if st.Counts.Total > 0 {
		fmt.Fprintf(deps.Stdout, " %s", styled(deps, output.StyleMuted, "("+phaseBreakdown(st.Counts)+")"))
	}
	fmt.Fprintln(deps.Stdout)
	fmt.Fprintf(deps.Stdout, "%s %s  %s %s\n",
		styled(deps, output.StyleMuted, "Ready:"),
		styled(deps, output.StyleSuccess, fmt.Sprintf("%d", st.Counts.Ready)),
		styled(deps, output.StyleMuted, "Needs attention:"),
		styled(deps, output.StyleWarning, fmt.Sprintf("%d", len(st.NeedsAttention))))
	if len(st.NeedsAttention) == 0 {
		if st.Counts.Total == 0 {
			fmt.Fprintln(deps.Stdout, "Next: create a quick work item with `specgate work create-quick`.")
		} else if allWorkspaces {
			fmt.Fprintln(deps.Stdout, "Next: no work needs attention right now. Use `specgate work show <ref>` if you know the work item.")
		} else {
			fmt.Fprintln(deps.Stdout, "Next: no work needs attention right now. Use `specgate work list --all-workspaces` to check other workspaces.")
		}
	}
}

func printStatusDashboard(deps *Deps, st *client.GovernanceStatus, allWorkspaces bool) {
	scope := "selected workspace"
	if allWorkspaces {
		scope = "all workspaces"
	} else if ws := currentIdentityConfig(deps).Workspace.Slug; ws != "" {
		scope = fmt.Sprintf("selected workspace %q", ws)
	}

	fmt.Fprintln(deps.Stdout, styled(deps, output.StyleInfo, "SpecGate Board"))
	fmt.Fprintln(deps.Stdout, visualRule(deps))
	fmt.Fprintln(deps.Stdout, styled(deps, output.StyleBold, "Summary:"))
	fmt.Fprintf(deps.Stdout, "  %s %s %s\n",
		coloredBullet(deps, output.StyleInfo),
		styled(deps, output.StyleMuted, "Scope:"),
		scope)
	fmt.Fprintf(deps.Stdout, "  %s %s %s total",
		coloredBullet(deps, output.StyleInfo),
		styled(deps, output.StyleMuted, "Work:"),
		styled(deps, output.StyleBold, fmt.Sprintf("%d", st.Counts.Total)))
	if st.Counts.Total > 0 {
		fmt.Fprintf(deps.Stdout, " %s", styled(deps, output.StyleMuted, "("+phaseBreakdown(st.Counts)+")"))
	}
	fmt.Fprintln(deps.Stdout)
	fmt.Fprintf(deps.Stdout, "  %s %s %s\n",
		coloredBullet(deps, output.StyleSuccess),
		styled(deps, output.StyleMuted, "Ready:"),
		styled(deps, output.StyleSuccess, fmt.Sprintf("%d", st.Counts.Ready)))
	fmt.Fprintf(deps.Stdout, "  %s %s %s\n",
		coloredBullet(deps, output.StyleWarning),
		styled(deps, output.StyleMuted, "Needs attention:"),
		styled(deps, output.StyleWarning, fmt.Sprintf("%d", len(st.NeedsAttention))))
	if st.Counts.Total > 0 {
		fmt.Fprintf(deps.Stdout, "  %s %s %s %d/%d (%d%%)\n",
			coloredBullet(deps, output.StyleSuccess),
			styled(deps, output.StyleMuted, "Ready work:"),
			progressBar(deps, st.Counts.Ready, st.Counts.Total, 18),
			st.Counts.Ready,
			st.Counts.Total,
			percent(st.Counts.Ready, st.Counts.Total))
	}

	if len(st.NeedsAttention) == 0 {
		fmt.Fprintln(deps.Stdout)
		printStatusNextAction(deps, st, allWorkspaces)
		return
	}

	fmt.Fprintln(deps.Stdout)
	printAttentionSection(deps, st.NeedsAttention)
}

// printAttentionSection renders the status board's "Needs Attention" section.
// Shared by `status` and `work list` so both surfaces stay identical.
func printAttentionSection(deps *Deps, items []client.NeedsAttentionItem) {
	if humanVisuals(deps) {
		fmt.Fprintln(deps.Stdout, styled(deps, output.StyleWarning, "Needs Attention"))
		fmt.Fprintln(deps.Stdout, visualRule(deps))
		for _, item := range items {
			fmt.Fprintf(deps.Stdout, "  %s %s — %s\n",
				statusIcon(deps, "warning"),
				styled(deps, output.StyleBold, item.Key),
				item.Title)
			if len(item.Issues) > 0 {
				fmt.Fprintf(deps.Stdout, "    %s %s\n",
					styled(deps, output.StyleMuted, "issues:"),
					styled(deps, output.StyleWarning, strings.Join(item.Issues, "; ")))
			}
		}
		return
	}
	for _, item := range items {
		fmt.Fprintf(deps.Stdout, "  ! %s — %s (%s)\n",
			item.Key,
			item.Title,
			strings.Join(item.Issues, "; "))
	}
}

func phaseBreakdown(counts client.PhaseCounts) string {
	parts := make([]string, 0, 5)
	for _, phase := range []struct {
		name  string
		count int
	}{
		{name: "intake", count: counts.Intake},
		{name: "draft", count: counts.Draft},
		{name: "review", count: counts.Review},
		{name: "ready", count: counts.Ready},
		{name: "handoff", count: counts.Handoff},
	} {
		if phase.count > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", phase.name, phase.count))
		}
	}
	if len(parts) == 0 {
		return "no phase counts"
	}
	return strings.Join(parts, ", ")
}

func printStatusNextAction(deps *Deps, st *client.GovernanceStatus, allWorkspaces bool) {
	prefix := "Next:"
	if humanVisuals(deps) {
		prefix = statusIcon(deps, "ready") + " " + styled(deps, output.StyleMuted, "Next:")
	}
	if st.Counts.Total == 0 {
		fmt.Fprintf(deps.Stdout, "%s create a quick work item with `specgate work create-quick`.\n", prefix)
	} else if allWorkspaces {
		fmt.Fprintf(deps.Stdout, "%s no work needs attention right now. Use `specgate work show <ref>` if you know the work item.\n", prefix)
	} else {
		fmt.Fprintf(deps.Stdout, "%s no work needs attention right now. Use `specgate work list --all-workspaces` to check other workspaces.\n", prefix)
	}
}

// specgate doctor
func newDoctorCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check CLI-to-server connectivity and capability health",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			// Probe liveness first (failure is non-fatal — endpoint is optional).
			_ = deps.Client.Healthz(ctx)

			meta, err := deps.Client.Meta(ctx)
			if err != nil {
				code := deps.Printer.Error("doctor", mapAPIError("doctor", err))
				return &output.ExitError{Code: code, Err: err}
			}

			if meta.APIVersion != apiVersion {
				payload := output.ErrorPayload{
					Code:    "incompatible",
					Message: fmt.Sprintf("server api_version %q is not supported (want %q)", meta.APIVersion, apiVersion),
				}
				code := deps.Printer.Error("doctor", payload)
				return &output.ExitError{Code: code}
			}

			if meta.Capabilities != nil {
				if agents, set := meta.Capabilities["agents"]; set && !agents {
					payload := output.ErrorPayload{
						Code:    "unavailable",
						Message: "agents service is not available on this server",
					}
					code := deps.Printer.Error("doctor", payload)
					return &output.ExitError{Code: code}
				}
			}

			// Probe the board endpoint the daily commands depend on: a healthy
			// process with a failing workflow surface is not a healthy setup.
			if _, err := deps.Client.Status(ctx, ""); err != nil {
				payload := output.ErrorPayload{
					Code:    "unavailable",
					Message: fmt.Sprintf("board endpoint failing (status/work list will not work): %v", err),
				}
				code := deps.Printer.Error("doctor", payload)
				return &output.ExitError{Code: code, Err: err}
			}

			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("doctor", meta)
				return nil
			}
			fmt.Fprintf(deps.Stdout, "OK  %s  version=%s\n", deps.ServerURL, meta.Version)
			printDoctorLocalStack(ctx, deps)
			return nil
		},
	}
}

// printDoctorLocalStack renders a "Local stack" section when a CLI-managed
// deployment exists, using the same data as `local-status` (which stays as-is
// for scripts). Skipped silently when no deployment directory is initialized.
func printDoctorLocalStack(ctx context.Context, deps *Deps) {
	dir := resolveDeployDir(deps)
	if _, err := os.Stat(filepath.Join(dir, "compose.yml")); err != nil {
		return // no CLI-managed deployment
	}
	fmt.Fprintf(deps.Stdout, "\nLocal stack (%s):\n", dir)
	statuses, err := makeDeployService(deps, dir).LocalStatus(ctx)
	if err != nil {
		fmt.Fprintf(deps.Stdout, "  unavailable: %v\n", err)
		return
	}
	if len(statuses) == 0 {
		fmt.Fprintln(deps.Stdout, "  no services running")
		return
	}
	for _, s := range statuses {
		fmt.Fprintf(deps.Stdout, "  %-30s  %s\n", s.Name, s.Status)
	}
}

// specgate open
func newOpenCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "open",
		Short: "Open the configured SpecGate URL in the default browser",
		RunE: func(cmd *cobra.Command, _ []string) error {
			meta, err := deps.Client.Meta(cmd.Context())
			webURL := deps.ServerURL
			if err == nil && meta.WebURL != "" {
				webURL = meta.WebURL
			}
			if deps.Opener == nil {
				return fmt.Errorf("no opener configured")
			}
			if err := deps.Opener(webURL); err != nil {
				payload := output.ErrorPayload{Code: "unavailable", Message: err.Error()}
				code := deps.Printer.Error("open", payload)
				return &output.ExitError{Code: code, Err: err}
			}
			if deps.Printer.Mode() != output.ModeJSON {
				fmt.Fprintf(deps.Stdout, "Opening %s\n", webURL)
			}
			return nil
		},
	}
}

// specgate config
func newConfigCmd(deps *Deps) *cobra.Command {
	cfg := &cobra.Command{
		Use:   "config",
		Short: "Manage CLI configuration",
	}
	cfg.AddCommand(newConfigSetCmd(deps))
	return cfg
}

func newConfigSetCmd(deps *Deps) *cobra.Command {
	set := &cobra.Command{
		Use:   "set",
		Short: "Set a configuration value",
	}
	set.AddCommand(newConfigSetServerCmd(deps))
	return set
}

func newConfigSetServerCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "server <url>",
		Short: "Set the SpecGate server URL",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			serverURL := args[0]
			var cfg config.Config
			if deps.ConfigPath != "" {
				loaded, _ := config.LoadFrom(deps.ConfigPath)
				cfg = loaded
			} else {
				loaded, _ := config.Load()
				cfg = loaded
			}
			cfg.Server = serverURL
			var err error
			if deps.ConfigPath != "" {
				err = cfg.SaveTo(deps.ConfigPath)
			} else {
				err = cfg.Save()
			}
			if err != nil {
				payload := output.ErrorPayload{Code: "unavailable", Message: err.Error()}
				code := deps.Printer.Error("config.set.server", payload)
				return &output.ExitError{Code: code, Err: err}
			}
			if deps.Printer.Mode() != output.ModeJSON {
				fmt.Fprintf(deps.Stdout, "Server set to %s\n", serverURL)
				return nil
			}
			deps.Printer.Success("config.set.server", map[string]string{"server": serverURL})
			return nil
		},
	}
}

// defaultDeployDir returns the default deployment directory (~/.specgate).
func defaultDeployDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".specgate"
	}
	return filepath.Join(home, ".specgate")
}

// resolveDeployDir returns the deployment dir from config or the default.
func resolveDeployDir(deps *Deps) string {
	cfg, _ := config.LoadFrom(deps.ConfigPath)
	if cfg.DeploymentDir != "" {
		return cfg.DeploymentDir
	}
	return defaultDeployDir()
}

// makeDeployService creates a deploy.Service, using the injected DeployRunner
// for tests and the production ExecRunner otherwise.
func makeDeployService(deps *Deps, dir string) *deploy.Service {
	runner := deps.DeployRunner
	if runner == nil {
		runner = deploy.ExecRunner{}
	}
	return deploy.New(dir, runner)
}

// specgate init
func newInitCmd(deps *Deps) *cobra.Command {
	var (
		deployDir       string
		seedFlag        bool
		noSeed          bool
		bundleVersion   string
		installPlugins  bool
		pluginAgentList string
	)
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize and start a local SpecGate stack",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			dir := deployDir
			if dir == "" {
				dir = resolveDeployDir(deps)
			}

			seed := deploy.SeedAsk
			switch {
			case seedFlag:
				seed = deploy.SeedYes
			case noSeed || deps.NoInput:
				seed = deploy.SeedNo
			}

			if !deps.NoInput && seed == deploy.SeedAsk {
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

			initSeed := seed
			if !deps.NoInput && seed == deploy.SeedYes {
				initSeed = deploy.SeedNo
			}
			svc := makeDeployService(deps, dir)
			if err := svc.Init(ctx, deploy.InitOptions{Seed: initSeed, BundleVersion: version}); err != nil {
				code := deps.Printer.Error("init", output.ErrorPayload{Code: "unavailable", Message: err.Error()})
				return &output.ExitError{Code: code, Err: err}
			}

			// Persist the deployment dir for future up/down/status calls.
			cfg, _ := config.LoadFrom(deps.ConfigPath)
			cfg.DeploymentDir = dir
			if cfg.Server == "" && os.Getenv("SPECGATE_SERVER") == "" && deps.ServerURL == "http://localhost:8080" {
				if inferred := inferLocalServerURL(dir); inferred != "" {
					cfg.Server = inferred
					deps.ServerURL = inferred
					if _, ok := deps.Client.(*client.Client); ok {
						deps.Client = client.New(inferred, deps.Timeout)
					}
				}
			}
			_ = saveConfig(deps, cfg)

			var selection *client.IdentitySelection
			if !deps.NoInput {
				bootstrap, err := promptIdentityBootstrap(deps)
				if err != nil {
					return err
				}
				selection, err = deps.Client.BootstrapIdentity(ctx, bootstrap)
				if err != nil {
					code := deps.Printer.Error("init", mapAPIError("init", err))
					return &output.ExitError{Code: code, Err: err}
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

			if !deps.NoInput && !installPlugins {
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
					fmt.Fprintln(deps.Stdout, "Installing IDE plugins...")
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
			fmt.Fprintf(deps.Stdout, "SpecGate started at %s\n\n", dir)
			if selection != nil {
				fmt.Fprintf(deps.Stdout, "Current user: %s\n", selection.User.Username)
				fmt.Fprintf(deps.Stdout, "Workspace:    %s\n\n", selection.Workspace.Slug)
			}
			fmt.Fprintln(deps.Stdout, "Next steps:")
			fmt.Fprintln(deps.Stdout, "  specgate doctor                                       verify services are healthy")
			if pluginResult != nil {
				fmt.Fprintf(deps.Stdout, "  specgate plugins doctor --agent %s                 verify IDE plugin files\n", pluginAgentList)
			} else {
				fmt.Fprintln(deps.Stdout, "  specgate plugins install --agent all                  install IDE plugins")
			}
			fmt.Fprintln(deps.Stdout, "  specgate model set --provider openai --api-key <key>  configure a model (optional)")
			fmt.Fprintln(deps.Stdout, "  specgate status                                       view the governance board")
			fmt.Fprintln(deps.Stdout)
			fmt.Fprintln(deps.Stdout, "A local model is optional — without one, readiness gates run on your IDE agent.")
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&deployDir, "dir", "", "Deployment directory (default ~/.specgate)")
	f.BoolVar(&seedFlag, "seed", false, "Seed demo data after startup")
	f.BoolVar(&noSeed, "no-seed", false, "Skip seeding demo data")
	f.StringVar(&bundleVersion, "bundle-version", "", "Compose bundle release to download (default: this CLI's version)")
	f.BoolVar(&installPlugins, "install-plugins", false, "Install Codex, Claude Code, and Cursor plugin files after startup")
	f.StringVar(&pluginAgentList, "plugin-agent", "", "IDE plugin target for --install-plugins: cursor, codex, claude, all, or comma-separated subset (prompts interactively if omitted)")
	cmd.MarkFlagsMutuallyExclusive("seed", "no-seed")
	return cmd
}

func inferLocalServerURL(dir string) string {
	port := "8080"
	data, err := os.ReadFile(filepath.Join(dir, ".env"))
	if err != nil {
		return "http://localhost:" + port
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != "DOC_REGISTRY_PORT" {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if value != "" {
			port = value
		}
		break
	}
	return "http://localhost:" + port
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

func validateUsernamePrompt(value string) error {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) < 3 || len(value) > 40 {
		return fmt.Errorf("username must be 3-40 characters")
	}
	for i, r := range value {
		valid := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-'
		if !valid {
			return fmt.Errorf("username can only use lowercase letters, numbers, underscores, and hyphens")
		}
		if i == 0 && !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
			return fmt.Errorf("username must start with a letter or number")
		}
	}
	return nil
}

// specgate up
func newUpCmd(deps *Deps) *cobra.Command {
	var deployDir string
	cmd := &cobra.Command{
		Use:   "up",
		Short: "Start the local SpecGate stack",
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir := deployDir
			if dir == "" {
				dir = resolveDeployDir(deps)
			}
			svc := makeDeployService(deps, dir)
			if err := svc.Up(cmd.Context()); err != nil {
				code := deps.Printer.Error("up", output.ErrorPayload{Code: "unavailable", Message: err.Error()})
				return &output.ExitError{Code: code, Err: err}
			}
			if deps.Printer.Mode() != output.ModeJSON {
				fmt.Fprintf(deps.Stdout, "Stack is up at %s\n", dir)
				return nil
			}
			deps.Printer.Success("up", map[string]string{"dir": dir})
			return nil
		},
	}
	cmd.Flags().StringVar(&deployDir, "dir", "", "Deployment directory (overrides config)")
	return cmd
}

// specgate down
func newDownCmd(deps *Deps) *cobra.Command {
	var deployDir string
	cmd := &cobra.Command{
		Use:   "down",
		Short: "Stop the local SpecGate stack",
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir := deployDir
			if dir == "" {
				dir = resolveDeployDir(deps)
			}
			svc := makeDeployService(deps, dir)
			if err := svc.Down(cmd.Context()); err != nil {
				code := deps.Printer.Error("down", output.ErrorPayload{Code: "unavailable", Message: err.Error()})
				return &output.ExitError{Code: code, Err: err}
			}
			if deps.Printer.Mode() != output.ModeJSON {
				fmt.Fprintf(deps.Stdout, "Stack stopped\n")
				return nil
			}
			deps.Printer.Success("down", map[string]string{"dir": dir})
			return nil
		},
	}
	cmd.Flags().StringVar(&deployDir, "dir", "", "Deployment directory (overrides config)")
	return cmd
}

type uninstallResult struct {
	Dir            string   `json:"dir"`
	StoppedStack   bool     `json:"stopped_stack"`
	PurgedData     bool     `json:"purged_data"`
	PurgedImages   bool     `json:"purged_images"`
	RemovedImages  []string `json:"removed_images"`
	RemovedConfig  bool     `json:"removed_config"`
	RemovedPlugins int      `json:"removed_plugins"`
	RemovedPaths   []string `json:"removed_paths"`
	KeptData       bool     `json:"kept_data"`
}

// specgate uninstall
func newUninstallCmd(deps *Deps) *cobra.Command {
	var (
		deployDir     string
		purgeData     bool
		purgeImages   bool
		removePlugins = true
	)
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove local SpecGate setup from this user account",
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir := deployDir
			if dir == "" {
				dir = resolveDeployDir(deps)
			}
			if !deps.NoInput && !cmd.Flags().Changed("purge-data") {
				choices, err := deps.Prompter.MultiSelect("Remove SpecGate setup", []interactive.Option{
					{Label: "IDE plugin files", Value: "plugins"},
					{Label: "Local data (Docker volumes and deployment directory)", Value: "data"},
					{Label: "Docker images for SpecGate services", Value: "images"},
				}, []string{"plugins"})
				if err != nil {
					return err
				}
				removePlugins = containsChoice(choices, "plugins")
				purgeData = containsChoice(choices, "data")
				purgeImages = containsChoice(choices, "images")
			}
			if purgeData && cmd.Flags().Changed("purge-data") {
				purgeImages = true
			}
			if purgeData && !deps.Yes {
				if deps.NoInput {
					err := fmt.Errorf("--purge-data permanently removes the deployment directory; pass --yes to confirm")
					code := deps.Printer.Error("uninstall", output.ErrorPayload{Code: "validation_failed", Message: err.Error()})
					return &output.ExitError{Code: code, Err: err}
				}
			}

			result := uninstallResult{Dir: dir, PurgedData: purgeData, PurgedImages: purgeImages, KeptData: !purgeData, RemovedImages: []string{}, RemovedPaths: []string{}}
			svc := makeDeployService(deps, dir)
			if _, err := os.Stat(filepath.Join(dir, "compose.yml")); err == nil {
				var removableImages []string
				if purgeImages {
					images, err := svc.Images(cmd.Context())
					if err != nil {
						code := deps.Printer.Error("uninstall", output.ErrorPayload{Code: "unavailable", Message: err.Error()})
						return &output.ExitError{Code: code, Err: err}
					}
					removableImages = specGateServiceImages(images)
				}
				var downErr error
				if purgeData {
					downErr = svc.DownWithVolumes(cmd.Context())
				} else {
					downErr = svc.Down(cmd.Context())
				}
				if downErr != nil {
					code := deps.Printer.Error("uninstall", output.ErrorPayload{Code: "unavailable", Message: downErr.Error()})
					return &output.ExitError{Code: code, Err: downErr}
				}
				result.StoppedStack = true
				if purgeImages {
					if err := svc.RemoveImages(cmd.Context(), removableImages); err != nil {
						code := deps.Printer.Error("uninstall", output.ErrorPayload{Code: "unavailable", Message: err.Error()})
						return &output.ExitError{Code: code, Err: err}
					}
					result.RemovedImages = removableImages
				}
			}
			if purgeData {
				if err := svc.RemoveLabeledResources(cmd.Context()); err != nil {
					code := deps.Printer.Error("uninstall", output.ErrorPayload{Code: "unavailable", Message: err.Error()})
					return &output.ExitError{Code: code, Err: err}
				}
			}
			if purgeData {
				if removed, err := removePathIfExists(dir); err != nil {
					code := deps.Printer.Error("uninstall", output.ErrorPayload{Code: "unavailable", Message: err.Error()})
					return &output.ExitError{Code: code, Err: err}
				} else if removed {
					result.RemovedPaths = append(result.RemovedPaths, dir)
				}
			}
			if removePlugins {
				removed, paths, err := removeSpecGatePluginFiles()
				if err != nil {
					code := deps.Printer.Error("uninstall", output.ErrorPayload{Code: "unavailable", Message: err.Error()})
					return &output.ExitError{Code: code, Err: err}
				}
				result.RemovedPlugins = removed
				result.RemovedPaths = append(result.RemovedPaths, paths...)
			}
			configRemoved, err := removeConfigFile(deps)
			if err != nil {
				code := deps.Printer.Error("uninstall", output.ErrorPayload{Code: "unavailable", Message: err.Error()})
				return &output.ExitError{Code: code, Err: err}
			}
			result.RemovedConfig = configRemoved
			if configRemoved {
				if path, err := configPathForDeps(deps); err == nil {
					result.RemovedPaths = append(result.RemovedPaths, path)
				}
			}

			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("uninstall", result)
				return nil
			}
			fmt.Fprintln(deps.Stdout, "SpecGate user setup removed.")
			if result.StoppedStack {
				fmt.Fprintln(deps.Stdout, "Stack stopped.")
			}
			if result.KeptData {
				fmt.Fprintf(deps.Stdout, "Local data kept in %s. Re-run with --purge-data --yes to remove it.\n", dir)
			} else {
				fmt.Fprintf(deps.Stdout, "Local data removed from %s.\n", dir)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&deployDir, "dir", "", "Deployment directory (default: saved deployment dir or ~/.specgate)")
	cmd.Flags().BoolVar(&purgeData, "purge-data", false, "Remove Docker volumes and the deployment directory")
	return cmd
}

func containsChoice(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func specGateServiceImages(images []string) []string {
	var filtered []string
	for _, image := range images {
		if strings.Contains(image, "thanhtung2693/") || strings.HasPrefix(image, "specgate-") {
			filtered = append(filtered, image)
		}
	}
	return filtered
}

func removeConfigFile(deps *Deps) (bool, error) {
	path, err := configPathForDeps(deps)
	if err != nil {
		return false, err
	}
	return removePathIfExists(path)
}

func configPathForDeps(deps *Deps) (string, error) {
	if deps.ConfigPath != "" {
		return deps.ConfigPath, nil
	}
	return config.DefaultPath()
}

// specgate local-status
func newLocalStatusCmd(deps *Deps) *cobra.Command {
	var deployDir string
	cmd := &cobra.Command{
		Use:   "local-status",
		Short: "Show the status of the local SpecGate stack",
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir := deployDir
			if dir == "" {
				dir = resolveDeployDir(deps)
			}
			svc := makeDeployService(deps, dir)
			statuses, err := svc.LocalStatus(cmd.Context())
			if err != nil {
				code := deps.Printer.Error("local-status", output.ErrorPayload{Code: "unavailable", Message: err.Error()})
				return &output.ExitError{Code: code, Err: err}
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("local-status", statuses)
				return nil
			}
			if len(statuses) == 0 {
				fmt.Fprintf(deps.Stdout, "No services running in %s\n", dir)
				return nil
			}
			for _, s := range statuses {
				fmt.Fprintf(deps.Stdout, "%-30s  %s\n", s.Name, s.Status)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&deployDir, "dir", "", "Deployment directory (overrides config)")
	return cmd
}

// specgate update
func newUpdateCmd(deps *Deps) *cobra.Command {
	var (
		deployDir string
		version   string
	)
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update the specgate CLI, IDE setup, and local stack",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			resolvedVersion, versionErr := resolveUpdateVersion(ctx, deps, version)
			installArgs := updateInstallerArgs()
			steps := []updateStepResult{
				{ID: "detect_target", Label: "Detect install target", Status: "pending"},
				{ID: "install_cli", Label: "Install CLI", Status: "pending"},
				{ID: "update_plugins", Label: "Update IDE setup", Status: "pending"},
				{ID: "update_stack", Label: "Update local stack", Status: "pending"},
			}

			steps[0].Status = "pass"
			if len(installArgs) == 2 {
				steps[0].Message = installArgs[1]
			} else {
				steps[0].Message = "auto"
			}
			emitProgress(deps, "step_succeeded", steps[0].ID, steps[0].Message)

			if deps.Printer.Mode() != output.ModeJSON {
				fmt.Fprintf(deps.Stdout, "Step 1/4 Detect install target\n")
				fmt.Fprintf(deps.Stdout, "  Using %s\n\n", steps[0].Message)
				fmt.Fprintf(deps.Stdout, "Step 2/4 Install CLI\n")
			}
			registryURL := publicRegistryURLForVersion(deps, resolvedVersion)
			cliInstallURL := strings.TrimSpace(deps.CLIInstallURL)
			if cliInstallURL == "" {
				cliInstallURL = registryURL + "/scripts/install-cli.sh"
			}
			cliInstallArgs := append([]string(nil), installArgs...)
			if resolvedVersion != "" {
				cliInstallArgs = append(cliInstallArgs, "--version", resolvedVersion)
			}
			emitProgress(deps, "step_started", steps[1].ID, steps[1].Label)
			if err := fetchAndRunScript(ctx, deps, cliInstallURL, cliInstallArgs...); err != nil {
				steps[1].Status = "fail"
				steps[1].Message = err.Error()
				steps[2].Status = "skipped"
				steps[2].Message = "previous step failed"
				steps[3].Status = "skipped"
				steps[3].Message = "previous step failed"
				emitProgress(deps, "step_failed", steps[1].ID, err.Error())
				payload := output.ErrorPayload{Code: "unavailable", Message: err.Error()}
				if deps.Printer.Mode() == output.ModeJSON {
					payload.Details = map[string]any{"steps": steps}
				}
				code := deps.Printer.Error("update", payload)
				return &output.ExitError{Code: code, Err: err}
			}
			steps[1].Status = "pass"
			emitProgress(deps, "step_succeeded", steps[1].ID, "")

			if deps.Printer.Mode() != output.ModeJSON {
				fmt.Fprintf(deps.Stdout, "\nStep 3/4 Update IDE setup\n")
			}
			emitProgress(deps, "step_started", steps[2].ID, steps[2].Label)
			pluginDeps := *deps
			pluginDeps.PluginRegistryURL = pluginRegistryURLForUpdate(deps, resolvedVersion)
			if _, err := runPluginInstall(ctx, &pluginDeps, pluginInstallOptions{Agent: "all"}); err != nil {
				steps[2].Status = "fail"
				steps[2].Message = err.Error()
				steps[3].Status = "skipped"
				steps[3].Message = "previous step failed"
				emitProgress(deps, "step_failed", steps[2].ID, err.Error())
				payload := pluginInstallErrorPayload(err)
				if deps.Printer.Mode() == output.ModeJSON {
					payload.Details = map[string]any{"steps": steps}
				}
				code := deps.Printer.Error("update", payload)
				return &output.ExitError{Code: code, Err: err}
			}
			if deps.Printer.Mode() != output.ModeJSON {
				fmt.Fprintf(deps.Stdout, "  Installed SpecGate IDE setup from %s\n", pluginDeps.PluginRegistryURL)
			}
			steps[2].Status = "pass"
			emitProgress(deps, "step_succeeded", steps[2].ID, "")

			if deps.Printer.Mode() != output.ModeJSON {
				fmt.Fprintf(deps.Stdout, "\nStep 4/4 Update local stack\n")
			}
			emitProgress(deps, "step_started", steps[3].ID, steps[3].Label)
			dir := deployDir
			if dir == "" {
				dir = resolveDeployDir(deps)
			}
			if _, err := os.Stat(filepath.Join(dir, "compose.yml")); err != nil {
				steps[3].Status = "skipped"
				steps[3].Message = "no CLI-managed deployment found in " + dir
				emitProgress(deps, "step_skipped", steps[3].ID, steps[3].Message)
				if deps.Printer.Mode() != output.ModeJSON {
					fmt.Fprintf(deps.Stdout, "  Skipped: %s\n", steps[3].Message)
				}
			} else if versionErr != nil {
				steps[3].Status = "skipped"
				steps[3].Message = "could not resolve latest release: " + versionErr.Error()
				emitProgress(deps, "step_skipped", steps[3].ID, steps[3].Message)
				if deps.Printer.Mode() != output.ModeJSON {
					fmt.Fprintf(deps.Stdout, "  Skipped: %s\n", steps[3].Message)
				}
			} else if err := makeDeployService(deps, dir).Update(ctx, deploy.UpdateOptions{BundleVersion: resolvedVersion, BundleBaseURL: deps.BundleBaseURL}); err != nil {
				steps[3].Status = "fail"
				steps[3].Message = err.Error()
				emitProgress(deps, "step_failed", steps[3].ID, err.Error())
				payload := output.ErrorPayload{Code: "unavailable", Message: err.Error()}
				if deps.Printer.Mode() == output.ModeJSON {
					payload.Details = map[string]any{"steps": steps}
				}
				code := deps.Printer.Error("update", payload)
				return &output.ExitError{Code: code, Err: err}
			} else {
				steps[3].Status = "pass"
				steps[3].Message = resolvedVersion
				emitProgress(deps, "step_succeeded", steps[3].ID, resolvedVersion)
				if deps.Printer.Mode() != output.ModeJSON {
					fmt.Fprintf(deps.Stdout, "  Updated local stack in %s to %s\n", dir, resolvedVersion)
				}
			}

			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("update", map[string]any{"steps": steps})
				return nil
			}

			return nil
		},
	}
	cmd.Flags().StringVar(&version, "version", "", "Release version to install (default: latest GitHub release)")
	cmd.Flags().StringVar(&deployDir, "dir", "", "Deployment directory to update (default: saved deployment dir or ~/.specgate)")
	return cmd
}

func publicRegistryURL(deps *Deps) string {
	if deps != nil && strings.TrimSpace(deps.PublicRegistryURL) != "" {
		return strings.TrimRight(strings.TrimSpace(deps.PublicRegistryURL), "/")
	}
	return defaultPublicRegistryURL
}

func publicRegistryURLForVersion(deps *Deps, version string) string {
	if deps != nil && strings.TrimSpace(deps.PublicRegistryURL) != "" {
		return publicRegistryURL(deps)
	}
	if strings.TrimSpace(version) == "" {
		return defaultPublicRegistryURL
	}
	return "https://raw.githubusercontent.com/thanhtung2693/specgate/" + strings.TrimSpace(version)
}

func pluginRegistryURLForUpdate(deps *Deps, version string) string {
	if deps != nil && strings.TrimSpace(deps.PluginRegistryURL) != "" {
		return strings.TrimRight(strings.TrimSpace(deps.PluginRegistryURL), "/")
	}
	return publicRegistryURLForVersion(deps, version)
}

func resolveUpdateVersion(ctx context.Context, deps *Deps, explicit string) (string, error) {
	if strings.TrimSpace(explicit) != "" {
		return strings.TrimSpace(explicit), nil
	}
	if deps != nil && deps.CheckLatestRelease != nil {
		return deps.CheckLatestRelease(ctx, deps.Timeout, deps.UpdateCheckCachePath)
	}
	if !isDevVersion(buildinfo.Version) {
		return buildinfo.Version, nil
	}
	return "", fmt.Errorf("no published version available from this CLI build")
}

// fetchAndRunScript fetches a shell script from url and pipes it to sh.
// extraArgs, if non-empty, are appended after "sh -s --" so the script sees them as $@.
func fetchAndRunScript(ctx context.Context, deps *Deps, url string, extraArgs ...string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	httpClient := &http.Client{Timeout: deps.Timeout}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %d for %s", resp.StatusCode, url)
	}
	script, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	args := []string{"-s", "--"}
	args = append(args, extraArgs...)
	sh := exec.CommandContext(ctx, "sh", args...)
	sh.Stdin = strings.NewReader(string(script))
	sh.Stdout = deps.Stdout
	sh.Stderr = deps.Stderr
	return sh.Run()
}

func updateInstallerArgs() []string {
	exe, err := os.Executable()
	if err != nil || strings.TrimSpace(exe) == "" {
		return nil
	}
	if resolved, resolveErr := filepath.EvalSymlinks(exe); resolveErr == nil && resolved != "" {
		exe = resolved
	}
	dir := strings.TrimSpace(filepath.Dir(exe))
	if dir == "" || dir == "." || dir == string(filepath.Separator) {
		return nil
	}
	return []string{"--install-dir", dir}
}

func emitProgress(deps *Deps, event, step, message string) {
	if deps.Printer == nil || deps.Printer.Mode() != output.ModeJSON || !deps.JSONProgress {
		return
	}
	data, err := json.Marshal(progressEvent{
		Event:   event,
		Command: "update",
		Step:    step,
		Message: message,
	})
	if err != nil {
		return
	}
	fmt.Fprintf(deps.Stdout, "%s\n", data)
}

// mapAPIError converts a *client.APIError to an output.ErrorPayload.
func mapAPIError(command string, err error) output.ErrorPayload {
	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		details := apiErr.Details
		switch apiErr.Kind {
		case client.ErrorNotFound:
			return output.ErrorPayload{Code: "not_found", Message: apiErr.Error(), Details: details}
		case client.ErrorConflict:
			return output.ErrorPayload{Code: "conflict", Message: apiErr.Error(), Details: details}
		case client.ErrorUnavailable:
			return output.ErrorPayload{Code: "unavailable", Message: apiErr.Error(), Details: details}
		case client.ErrorIncompatible:
			return output.ErrorPayload{Code: "incompatible", Message: apiErr.Error(), Details: details}
		case client.ErrorUsage:
			return output.ErrorPayload{Code: "validation_failed", Message: apiErr.Error(), Details: details}
		}
	}
	return output.ErrorPayload{Code: "unavailable", Message: err.Error()}
}
