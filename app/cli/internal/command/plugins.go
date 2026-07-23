package command

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/interactive"
	"github.com/specgate/specgate/app/cli/internal/output"
)

const (
	specgatePluginName         = "specgate"
	pluginPathPlaceholder      = "__SPECGATE_PLUGIN_PATH__"
	codexPersonalMarketURL     = ".agents/plugins/personal-marketplace.json"
	installedCodexPluginSource = "./.codex/plugins/specgate"
	pluginOwnerMarker          = ".specgate-owned"
	pluginOwnerValue           = "specgate-plugin-v1\n"
	maxPluginSkills            = 16
	maxPluginPackageBytes      = 32 << 20
	maxSkillsSHLockBytes       = 1 << 20
	skillsSHSpecGateSource     = "thanhtung2693/specgate"
	skillsSHSpecGateSkillPath  = "plugins/skills/specgate/SKILL.md"
)

type pluginPackageClient interface {
	PluginPackage(ctx context.Context) (*client.PluginPackage, error)
	PluginFile(ctx context.Context, path string) ([]byte, error)
}

type pluginInstallOptions struct {
	Agent        string
	Registry     string
	ProjectLocal bool
	DryRun       bool
	useRegistry  bool
}

type pluginDoctorOptions struct {
	Agent        string
	Registry     string
	ProjectLocal bool
}

type pluginInstallResult struct {
	Agents            []string `json:"agents"`
	Scope             string   `json:"scope"`
	RestartIDEs       bool     `json:"restart_ides"`
	WrittenCount      int      `json:"written_count"`
	DryRun            bool     `json:"dry_run"`
	PlannedOperations []string `json:"planned_operations,omitempty"`
}

type pluginDoctorResult struct {
	Scope         string              `json:"scope"`
	LatestVersion string              `json:"latest_version,omitempty"`
	Warnings      []string            `json:"warnings,omitempty"`
	Agents        []pluginAgentHealth `json:"agents"`
}

type skillsSHConflict struct {
	Scope         string `json:"scope"`
	Path          string `json:"path"`
	RemoveCommand string `json:"remove_command"`
}

type skillsSHLock struct {
	Skills map[string]struct {
		Source     string `json:"source"`
		SourceType string `json:"sourceType"`
		SkillPath  string `json:"skillPath"`
	} `json:"skills"`
}

type pluginAgentHealth struct {
	Agent            string   `json:"agent"`
	OK               bool     `json:"ok"`
	Owner            string   `json:"owner,omitempty"`
	Marketplace      string   `json:"marketplace,omitempty"`
	InstalledVersion string   `json:"installed_version,omitempty"`
	LatestVersion    string   `json:"latest_version,omitempty"`
	NeedsUpdate      bool     `json:"needs_update,omitempty"`
	Missing          []string `json:"missing,omitempty"`
	Warnings         []string `json:"warnings,omitempty"`
	RepairCommand    string   `json:"repair_command,omitempty"`
}

type pluginOwnershipTarget struct {
	path      string
	directory bool
}

type pluginInstaller struct {
	ctx    context.Context
	deps   *Deps
	client pluginPackageClient
	pkg    *client.PluginPackage
	opts   pluginInstallOptions
	home   string
	files  map[string][]byte

	written int
	planned []string
}

func registerPluginCommands(root *cobra.Command, deps *Deps) {
	cmd := &cobra.Command{
		Use:   "plugins",
		Short: "Install and inspect SpecGate IDE plugins",
	}
	cmd.AddCommand(newPluginsInstallCmd(deps))
	cmd.AddCommand(newPluginsDoctorCmd(deps))
	root.AddCommand(cmd)
}

func newPluginsInstallCmd(deps *Deps) *cobra.Command {
	var opts pluginInstallOptions
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install SpecGate IDE plugin files",
		Example: strings.TrimSpace(`specgate plugins install
specgate plugins install --agent codex
specgate plugins install --agent codex --project-local`),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := resolvePluginAgentPrompt(cmd, deps, &opts.Agent, "Select IDE plugins", "agent"); err != nil {
				code := deps.Printer.Error("plugins.install", output.ErrorPayload{Code: "validation_failed", Message: err.Error()})
				return &output.ExitError{Code: code, Err: err}
			}
			if err := resolvePluginScopePrompt(cmd, deps, &opts.ProjectLocal); err != nil {
				code := deps.Printer.Error("plugins.install", output.ErrorPayload{Code: "validation_failed", Message: err.Error()})
				return &output.ExitError{Code: code, Err: err}
			}
			result, err := runPluginInstall(cmd.Context(), deps, opts)
			if err != nil {
				code := deps.Printer.Error("plugins.install", pluginInstallErrorPayload(err))
				return &output.ExitError{Code: code, Err: err}
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("plugins.install", result)
				return nil
			}
			if opts.DryRun {
				fmt.Fprintf(deps.Stdout, "\n%s %s\n", title(deps, "Plugin setup plan for:"), strings.Join(result.Agents, ", "))
				fmt.Fprintln(deps.Stdout, "No files were written.")
				return nil
			}
			fmt.Fprintf(deps.Stdout, "\n%s %s\n", styled(deps, output.StyleSuccess, "SpecGate agent setup installed for:"), styled(deps, output.StyleBold, strings.Join(result.Agents, ", ")))
			if opts.ProjectLocal {
				fmt.Fprintln(deps.Stdout, label(deps, "Scope:")+" project-local files in the current repository.")
			} else {
				fmt.Fprintln(deps.Stdout, label(deps, "Scope:")+" global user files under your home directory.")
			}
			fmt.Fprintln(deps.Stdout, "Restart the selected IDE(s) so the new SpecGate files are loaded.")
			if humanVisuals(deps) {
				fmt.Fprintln(deps.Stdout, nextStep(deps, "verify IDE files:", "specgate plugins doctor"))
			} else {
				fmt.Fprintln(deps.Stdout, "Run 'specgate plugins doctor' to verify IDE files.")
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&opts.Agent, "agent", "", "IDE target: cursor, codex, claude, all, or comma-separated subset (prompts interactively if omitted)")
	f.StringVar(&opts.Registry, "registry", "", "SpecGate agent-package registry base URL (default: configured SpecGate server)")
	f.BoolVar(&opts.ProjectLocal, "project-local", false, "Install IDE files into the current repository instead of user-global locations")
	f.BoolVar(&opts.DryRun, "dry-run", false, "Print planned file operations without writing")
	return cmd
}

func runPluginInstall(ctx context.Context, deps *Deps, opts pluginInstallOptions) (pluginInstallResult, error) {
	agents, err := normalizePluginAgents(opts.Agent)
	if err != nil {
		return pluginInstallResult{}, pluginInstallError{kind: "validation_failed", err: err}
	}
	if deps.Topology == config.ModeLocal && strings.TrimSpace(opts.Registry) != "" && !opts.useRegistry {
		return pluginInstallResult{}, pluginInstallError{kind: "incompatible", err: fmt.Errorf("--registry cannot be used in Local mode; the matching plugin is embedded in this CLI")}
	}
	home, homeErr := userHomeDir(deps)
	if homeErr != nil && (!opts.ProjectLocal || needsNativePluginInspection(agents)) {
		return pluginInstallResult{}, homeErr
	}
	if err := rejectNativePluginConflicts(agents, home, opts.ProjectLocal); err != nil {
		return pluginInstallResult{}, err
	}
	conflicts := findSkillsSHConflicts(agents, home)
	if len(conflicts) > 0 {
		retryCommand := pluginRepairCommand(strings.Join(agents, ","), opts.ProjectLocal) + " --no-input"
		return pluginInstallResult{}, pluginInstallError{
			kind: "conflict",
			err:  fmt.Errorf("skills.sh manages the SpecGate bootstrap at %s; run %s, then retry %s; no plugin files were changed", skillsSHConflictPaths(conflicts), skillsSHRemovalCommands(conflicts), retryCommand),
			details: map[string]any{
				"conflicts":     conflicts,
				"retry_command": retryCommand,
			},
		}
	}
	var pc pluginPackageClient
	if useEmbeddedPluginPackage(deps, opts.Registry, opts.useRegistry) {
		pc = embeddedLocalPlugin{}
	} else {
		pc = pluginClientFor(deps, opts.Registry)
	}
	pkg, err := pc.PluginPackage(ctx)
	if err != nil {
		return pluginInstallResult{}, err
	}
	if err := validatePluginPackage(pkg); err != nil {
		return pluginInstallResult{}, pluginInstallError{kind: "validation_failed", err: err}
	}
	installer := &pluginInstaller{
		ctx:    ctx,
		deps:   deps,
		client: pc,
		pkg:    pkg,
		opts:   opts,
		home:   home,
	}
	if err := installer.install(agents); err != nil {
		return pluginInstallResult{}, err
	}
	return pluginInstallResult{
		Agents:            agents,
		Scope:             pluginScope(opts.ProjectLocal),
		RestartIDEs:       true,
		WrittenCount:      installer.written,
		DryRun:            opts.DryRun,
		PlannedOperations: installer.planned,
	}, nil
}

type pluginInstallError struct {
	kind    string
	err     error
	details map[string]any
}

func (e pluginInstallError) Error() string { return e.err.Error() }
func (e pluginInstallError) Unwrap() error { return e.err }

func pluginInstallErrorPayload(err error) output.ErrorPayload {
	var installErr pluginInstallError
	if errors.As(err, &installErr) {
		return output.ErrorPayload{Code: installErr.kind, Message: installErr.Error(), Details: installErr.details}
	}
	return mapAPIError(err)
}

func newPluginsDoctorCmd(deps *Deps) *cobra.Command {
	var opts pluginDoctorOptions
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check installed SpecGate IDE plugin files",
		Example: strings.TrimSpace(`specgate plugins doctor
specgate plugins doctor --agent codex
specgate plugins doctor --agent codex --project-local`),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := resolvePluginAgentPrompt(cmd, deps, &opts.Agent, "Select IDE plugins to check", "agent"); err != nil {
				payload := output.ErrorPayload{Code: "validation_failed", Message: err.Error()}
				code := deps.Printer.Error("plugins.doctor", payload)
				return &output.ExitError{Code: code, Err: err}
			}
			agents, err := normalizePluginAgents(opts.Agent)
			if err != nil {
				payload := output.ErrorPayload{Code: "validation_failed", Message: err.Error()}
				code := deps.Printer.Error("plugins.doctor", payload)
				return &output.ExitError{Code: code, Err: err}
			}
			home, err := userHomeDir(deps)
			if err != nil && (!opts.ProjectLocal || needsNativePluginInspection(agents)) {
				payload := output.ErrorPayload{Code: "unavailable", Message: err.Error()}
				code := deps.Printer.Error("plugins.doctor", payload)
				return &output.ExitError{Code: code, Err: err}
			}
			if deps.Topology == config.ModeLocal && strings.TrimSpace(opts.Registry) != "" {
				payload := output.ErrorPayload{Code: "incompatible", Message: "--registry cannot be used in Local mode; the matching plugin is embedded in this CLI"}
				code := deps.Printer.Error("plugins.doctor", payload)
				return &output.ExitError{Code: code}
			}
			result := pluginDoctorResult{Scope: pluginScope(opts.ProjectLocal)}
			nativeHealth := make(map[string]pluginAgentHealth, len(agents))
			needsPackage := false
			for _, agent := range agents {
				if health, native := nativePluginHealth(agent, home); native {
					nativeHealth[agent] = health
				} else {
					needsPackage = true
				}
			}
			var pkg *client.PluginPackage
			if needsPackage {
				pc := pluginPackageClient(pluginClientFor(deps, opts.Registry))
				if useEmbeddedPluginPackage(deps, opts.Registry, false) {
					pc = embeddedLocalPlugin{}
				}
				pkg, err = pc.PluginPackage(cmd.Context())
				if pkg != nil {
					result.LatestVersion = pkg.Version
					if err := validatePluginPackage(pkg); err != nil {
						payload := output.ErrorPayload{Code: "validation_failed", Message: err.Error()}
						code := deps.Printer.Error("plugins.doctor", payload)
						return &output.ExitError{Code: code, Err: err}
					}
				}
				if err != nil {
					result.Warnings = append(result.Warnings, "Could not fetch the latest plugin package inventory; checked local files only.")
				}
			}
			ok := true
			for _, agent := range agents {
				health, native := nativeHealth[agent]
				if !native {
					health = checkPluginAgent(agent, home, opts.ProjectLocal, pkg)
				}
				if !health.OK {
					ok = false
				}
				result.Agents = append(result.Agents, health)
			}
			if deps.Printer.Mode() == output.ModeJSON {
				if ok {
					deps.Printer.Success("plugins.doctor", result)
					return nil
				}
				payload := output.ErrorPayload{
					Code:    "unavailable",
					Message: "one or more IDE plugin installs are incomplete",
					Details: map[string]any{
						"scope":  result.Scope,
						"agents": result.Agents,
					},
				}
				code := deps.Printer.Error("plugins.doctor", payload)
				return &output.ExitError{Code: code}
			}
			for _, warning := range result.Warnings {
				fmt.Fprintln(deps.Stdout, styled(deps, output.StyleWarning, "WARN")+" "+warning)
			}
			for _, health := range result.Agents {
				if health.OK {
					suffix := ""
					if health.Owner == "native" {
						suffix = fmt.Sprintf(" (native marketplace %s)", health.Marketplace)
					}
					if len(health.Warnings) == 0 {
						fmt.Fprintf(deps.Stdout, "%s   %s%s\n", styled(deps, output.StyleSuccess, "OK"), styled(deps, output.StyleBold, health.Agent), suffix)
						continue
					}
					fmt.Fprintf(deps.Stdout, "%s %s%s\n", styled(deps, output.StyleWarning, "WARN"), styled(deps, output.StyleBold, health.Agent), suffix)
				} else {
					fmt.Fprintf(deps.Stdout, "%s %s\n", styled(deps, output.StyleDanger, "MISS"), styled(deps, output.StyleBold, health.Agent))
					for _, path := range health.Missing {
						fmt.Fprintf(deps.Stdout, "  %s %s\n", label(deps, "-"), path)
					}
					if health.RepairCommand != "" {
						fmt.Fprintf(deps.Stdout, "  %s %s\n", label(deps, "repair:"), styled(deps, output.StyleAction, health.RepairCommand))
					}
				}
				for _, warning := range health.Warnings {
					fmt.Fprintf(deps.Stdout, "  %s %s\n", styled(deps, output.StyleWarning, "!"), warning)
				}
			}
			if !ok {
				return &output.ExitError{Code: output.ExitUnavailable, Err: fmt.Errorf("one or more IDE plugin installs are incomplete")}
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&opts.Agent, "agent", "", "IDE target to inspect: cursor, codex, claude, all, or comma-separated subset (prompts interactively if omitted)")
	f.StringVar(&opts.Registry, "registry", "", "SpecGate agent-package registry base URL (default: configured SpecGate server)")
	f.BoolVar(&opts.ProjectLocal, "project-local", false, "Inspect project-local IDE files in the current repository")
	return cmd
}

func resolvePluginAgentPrompt(cmd *cobra.Command, deps *Deps, agent *string, title, flagName string) error {
	if !canPrompt(deps) || cmd.Flags().Changed(flagName) {
		return nil
	}
	values, err := deps.Prompter.MultiSelect(title, pluginAgentPromptOptions(), defaultPluginAgents(deps))
	if err != nil {
		return err
	}
	if len(values) == 0 {
		return fmt.Errorf("select at least one IDE plugin")
	}
	*agent = strings.Join(values, ",")
	return nil
}

func resolvePluginScopePrompt(cmd *cobra.Command, deps *Deps, projectLocal *bool) error {
	if !canPrompt(deps) || cmd.Flags().Changed("project-local") {
		return nil
	}
	scope, err := deps.Prompter.Select("Install scope", []interactive.Option{
		{Label: "Global user files", Value: "global"},
		{Label: "This project", Value: "project"},
	})
	if err != nil {
		return err
	}
	*projectLocal = scope == "project"
	return nil
}

func defaultPluginAgents(deps *Deps) []string {
	if deps.PluginAgentDefaults != nil {
		return normalizePluginAgentDefaults(deps.PluginAgentDefaults())
	}
	return normalizePluginAgentDefaults(detectPluginAgentDefaults())
}

func detectPluginAgentDefaults() []string {
	var agents []string
	if pluginAgentAvailable("cursor") {
		agents = append(agents, "cursor")
	}
	if pluginAgentAvailable("codex") {
		agents = append(agents, "codex")
	}
	if pluginAgentAvailable("claude") {
		agents = append(agents, "claude")
	}
	return agents
}

func normalizePluginAgentDefaults(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			agent := strings.ToLower(strings.TrimSpace(part))
			switch agent {
			case "cursor", "codex", "claude":
			default:
				continue
			}
			if !seen[agent] {
				seen[agent] = true
				out = append(out, agent)
			}
		}
	}
	if len(out) == 0 {
		return []string{"cursor", "codex", "claude"}
	}
	return out
}

func pluginAgentPromptOptions() []interactive.Option {
	return []interactive.Option{
		{Label: "Cursor", Value: "cursor"},
		{Label: "Codex", Value: "codex"},
		{Label: "Claude Code", Value: "claude"},
	}
}

func pluginClientFor(deps *Deps, registry string) pluginPackageClient {
	base := strings.TrimSpace(registry)
	if base == "" {
		base = strings.TrimSpace(deps.PluginRegistryURL)
	}
	if base == "" {
		// Tests and embedded callers can invoke plugin commands without the root
		// pre-run; production commands always populate PluginRegistryURL there.
		base = deps.ServerURL
	}
	return client.New(base, deps.Timeout)
}

func useEmbeddedPluginPackage(deps *Deps, registry string, forceRegistry bool) bool {
	if forceRegistry || strings.TrimSpace(registry) != "" {
		return false
	}
	return deps.Topology == config.ModeLocal || deps.useEmbeddedPlugins
}

func normalizePluginAgents(input string) ([]string, error) {
	input = strings.TrimSpace(input)
	if input == "" || input == "all" {
		return []string{"cursor", "codex", "claude"}, nil
	}
	seen := map[string]bool{}
	var agents []string
	for _, part := range strings.Split(input, ",") {
		agent := strings.ToLower(strings.TrimSpace(part))
		if agent == "" {
			return nil, fmt.Errorf("empty --agent entry in %q", input)
		}
		if agent == "all" {
			return []string{"cursor", "codex", "claude"}, nil
		}
		switch agent {
		case "cursor", "codex", "claude":
		default:
			return nil, fmt.Errorf("unsupported agent %q", agent)
		}
		if !seen[agent] {
			seen[agent] = true
			agents = append(agents, agent)
		}
	}
	return agents, nil
}

func validatePluginPackage(pkg *client.PluginPackage) error {
	if pkg == nil {
		return fmt.Errorf("plugin package is empty")
	}
	if !validPluginVersion(pkg.Version) {
		return fmt.Errorf("plugin package has unsafe version %q", pkg.Version)
	}
	if len(pkg.Skills) == 0 {
		return fmt.Errorf("plugin package has no skills")
	}
	if len(pkg.Skills) > maxPluginSkills {
		return fmt.Errorf("plugin package has %d skills; maximum is %d", len(pkg.Skills), maxPluginSkills)
	}
	seenSkills := make(map[string]bool, len(pkg.Skills))
	for _, skill := range pkg.Skills {
		if !validPluginSkillName(skill) {
			return fmt.Errorf("plugin package has unsafe skill name %q", skill)
		}
		if skill != "specgate" && !strings.HasPrefix(skill, "specgate-") {
			return fmt.Errorf("plugin package skill %q is outside the specgate namespace", skill)
		}
		if seenSkills[skill] {
			return fmt.Errorf("plugin package repeats skill %q", skill)
		}
		seenSkills[skill] = true
	}
	return nil
}

func validPluginVersion(version string) bool {
	version = strings.TrimSpace(version)
	if version == "" || len(version) > 80 || version == "." || version == ".." {
		return false
	}
	for _, r := range version {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_' || r == '+' {
			continue
		}
		return false
	}
	return true
}

func validPluginSkillName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 80 {
		return false
	}
	if strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") {
		return false
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return false
	}
	return true
}

func pluginScope(projectLocal bool) string {
	if projectLocal {
		return "project"
	}
	return "global"
}

func findSkillsSHConflicts(agents []string, home string) []skillsSHConflict {
	type candidate struct {
		scope         string
		lockPath      string
		skillPath     string
		removeCommand string
	}

	var candidates []candidate
	for _, agent := range agents {
		projectSkill := filepath.Join(skillsSHSkillDir(agent), specgatePluginName, "SKILL.md")
		candidates = append(candidates, candidate{
			scope:         "project",
			lockPath:      "skills-lock.json",
			skillPath:     projectSkill,
			removeCommand: "npx skills remove specgate -y",
		})
		if home != "" {
			candidates = append(candidates, candidate{
				scope:         "global",
				lockPath:      filepath.Join(home, ".agents", ".skill-lock.json"),
				skillPath:     filepath.Join(home, skillsSHSkillDir(agent), specgatePluginName, "SKILL.md"),
				removeCommand: "npx skills remove specgate -g -y",
			})
		}
	}

	seen := map[string]bool{}
	var conflicts []skillsSHConflict
	for _, candidate := range candidates {
		key := candidate.scope + "\x00" + candidate.skillPath
		if seen[key] || !isOfficialSkillsSHBootstrap(candidate.lockPath, candidate.skillPath) {
			continue
		}
		seen[key] = true
		conflicts = append(conflicts, skillsSHConflict{
			Scope:         candidate.scope,
			Path:          candidate.skillPath,
			RemoveCommand: candidate.removeCommand,
		})
	}
	sort.Slice(conflicts, func(a, b int) bool {
		if conflicts[a].Scope == conflicts[b].Scope {
			return conflicts[a].Path < conflicts[b].Path
		}
		return conflicts[a].Scope < conflicts[b].Scope
	})
	return conflicts
}

func skillsSHSkillDir(agent string) string {
	if agent == "claude" {
		return filepath.Join(".claude", "skills")
	}
	return filepath.Join(".agents", "skills")
}

func isOfficialSkillsSHBootstrap(lockPath, skillPath string) bool {
	for _, path := range []string{lockPath, skillPath} {
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return false
		}
	}

	file, err := os.Open(lockPath)
	if err != nil {
		return false
	}
	defer file.Close() //nolint:errcheck
	body, err := io.ReadAll(io.LimitReader(file, maxSkillsSHLockBytes+1))
	if err != nil || len(body) > maxSkillsSHLockBytes {
		return false
	}
	var lock skillsSHLock
	if json.Unmarshal(body, &lock) != nil {
		return false
	}
	entry, ok := lock.Skills[specgatePluginName]
	return ok &&
		entry.Source == skillsSHSpecGateSource &&
		entry.SourceType == "github" &&
		entry.SkillPath == skillsSHSpecGateSkillPath
}

func skillsSHConflictPaths(conflicts []skillsSHConflict) string {
	paths := make([]string, 0, len(conflicts))
	for _, conflict := range conflicts {
		paths = append(paths, conflict.Path)
	}
	return strings.Join(paths, ", ")
}

func skillsSHRemovalCommands(conflicts []skillsSHConflict) string {
	seen := map[string]bool{}
	commands := make([]string, 0, len(conflicts))
	for _, conflict := range conflicts {
		if seen[conflict.RemoveCommand] {
			continue
		}
		seen[conflict.RemoveCommand] = true
		commands = append(commands, conflict.RemoveCommand)
	}
	return strings.Join(commands, " and ")
}
