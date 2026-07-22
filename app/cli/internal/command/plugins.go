package command

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/fsutil"
	"github.com/specgate/specgate/app/cli/internal/interactive"
	"github.com/specgate/specgate/app/cli/internal/output"
)

const (
	specgatePluginName         = "specgate"
	pluginPathPlaceholder      = "__SPECGATE_PLUGIN_PATH__"
	codexPersonalMarketURL     = ".agents/plugins/personal-marketplace.json"
	installedCodexPluginSource = "./.codex/plugins/specgate"
	// committedCodexPluginSource is the plugin source path used by the checked-in
	// repo-root marketplace pointer (.agents/plugins/marketplace.json). The
	// installer must never overwrite that committed pointer with a machine path.
	committedCodexPluginSource = "./plugins"
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
	Agents       []string `json:"agents"`
	Scope        string   `json:"scope"`
	RestartIDEs  bool     `json:"restart_ides"`
	WrittenCount int      `json:"written_count"`
	DryRun       bool     `json:"dry_run"`
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
	if homeErr != nil && !opts.ProjectLocal {
		return pluginInstallResult{}, homeErr
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
	if opts.useRegistry {
		pc = pluginClientFor(deps, opts.Registry)
	} else if deps.Topology == config.ModeLocal {
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
		Agents:       agents,
		Scope:        pluginScope(opts.ProjectLocal),
		RestartIDEs:  true,
		WrittenCount: installer.written,
		DryRun:       opts.DryRun,
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
			if err != nil && !opts.ProjectLocal {
				payload := output.ErrorPayload{Code: "unavailable", Message: err.Error()}
				code := deps.Printer.Error("plugins.doctor", payload)
				return &output.ExitError{Code: code, Err: err}
			}
			if deps.Topology == config.ModeLocal && strings.TrimSpace(opts.Registry) != "" {
				payload := output.ErrorPayload{Code: "incompatible", Message: "--registry cannot be used in Local mode; the matching plugin is embedded in this CLI"}
				code := deps.Printer.Error("plugins.doctor", payload)
				return &output.ExitError{Code: code}
			}
			pc := pluginPackageClient(pluginClientFor(deps, opts.Registry))
			if deps.Topology == config.ModeLocal {
				pc = embeddedLocalPlugin{}
			}
			pkg, pkgErr := pc.PluginPackage(cmd.Context())
			result := pluginDoctorResult{Scope: pluginScope(opts.ProjectLocal)}
			if pkg != nil {
				result.LatestVersion = pkg.Version
			}
			if pkg != nil {
				if err := validatePluginPackage(pkg); err != nil {
					payload := output.ErrorPayload{Code: "validation_failed", Message: err.Error()}
					code := deps.Printer.Error("plugins.doctor", payload)
					return &output.ExitError{Code: code, Err: err}
				}
			}
			if pkgErr != nil {
				result.Warnings = append(result.Warnings, "Could not fetch the latest plugin package inventory; checked local files only.")
			}
			ok := true
			for _, agent := range agents {
				health := checkPluginAgent(agent, home, opts.ProjectLocal, pkg)
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
					if len(health.Warnings) == 0 {
						fmt.Fprintf(deps.Stdout, "%s   %s\n", styled(deps, output.StyleSuccess, "OK"), styled(deps, output.StyleBold, health.Agent))
						continue
					}
					fmt.Fprintf(deps.Stdout, "%s %s\n", styled(deps, output.StyleWarning, "WARN"), styled(deps, output.StyleBold, health.Agent))
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

func (i *pluginInstaller) install(agents []string) error {
	if err := i.validateInstallTargets(agents); err != nil {
		return pluginInstallError{kind: "validation_failed", err: err}
	}
	if err := i.preloadPluginFiles(agents); err != nil {
		return pluginInstallError{kind: "validation_failed", err: err}
	}
	for _, agent := range agents {
		switch agent {
		case "cursor":
			if err := i.installCursor(); err != nil {
				return err
			}
		case "codex":
			if err := i.installCodex(); err != nil {
				return err
			}
		case "claude":
			if err := i.installClaude(); err != nil {
				return err
			}
		}
	}
	return nil
}

func (i *pluginInstaller) preloadPluginFiles(agents []string) error {
	paths := map[string]bool{}
	addSkills := func() {
		for _, skill := range i.pkg.Skills {
			paths["skills/"+skill+"/SKILL.md"] = true
		}
	}
	for _, agent := range agents {
		switch agent {
		case "cursor":
			paths["rules/using-specgate.mdc"] = true
			addSkills()
		case "codex":
			if i.opts.ProjectLocal {
				addSkills()
				continue
			}
			for _, path := range []string{".codex-plugin/plugin.json", "assets/logo.svg", "hooks/hooks.json", "hooks/run-hook.cmd", "hooks/session-start", codexPersonalMarketURL} {
				paths[path] = true
			}
			addSkills()
		case "claude":
			if i.opts.ProjectLocal {
				addSkills()
				continue
			}
			for _, path := range []string{".claude-plugin/plugin.json", "assets/logo.svg", "hooks/hooks-claude.json", "hooks/run-hook.cmd", "hooks/session-start"} {
				paths[path] = true
			}
			addSkills()
		}
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	files := make(map[string][]byte, len(ordered))
	total := 0
	for _, path := range ordered {
		body, err := i.client.PluginFile(i.ctx, path)
		if err != nil {
			return err
		}
		total += len(body)
		if total > maxPluginPackageBytes {
			return fmt.Errorf("plugin package files exceed the 32 MiB aggregate limit")
		}
		files[path] = body
	}
	i.files = files
	return nil
}

func (i *pluginInstaller) validateInstallTargets(agents []string) error {
	root := i.home
	if i.opts.ProjectLocal {
		root = "."
		if err := validateProjectLocalPluginAncestors(root, agents); err != nil {
			return err
		}
	}
	validateSkills := func(dir string) error {
		for _, skill := range i.pkg.Skills {
			if err := validateOwnedPluginDir(filepath.Join(dir, skill)); err != nil {
				return err
			}
		}
		return nil
	}
	for _, agent := range agents {
		switch agent {
		case "cursor":
			rule := filepath.Join(root, ".cursor", "rules", "using-specgate.mdc")
			if err := validateOwnedPluginFile(rule); err != nil {
				return err
			}
			if err := validateSkills(filepath.Join(root, ".cursor", "skills")); err != nil {
				return err
			}
		case "codex":
			if i.opts.ProjectLocal {
				if err := validateSkills(filepath.Join(root, ".agents", "skills")); err != nil {
					return err
				}
				if err := i.validateLegacyProjectCodexInstall(root); err != nil {
					return err
				}
				continue
			}
			if err := validateOwnedPluginDir(filepath.Join(root, ".codex", "plugins", specgatePluginName)); err != nil {
				return err
			}
			if strings.TrimSpace(i.pkg.Version) != "" {
				if err := validateOwnedPluginDir(filepath.Join(root, ".codex", "plugins", "cache", "personal", specgatePluginName, i.pkg.Version)); err != nil {
					return err
				}
			}
			for _, path := range []string{
				filepath.Join(root, ".codex", "config.toml"),
				filepath.Join(root, ".agents", "plugins", "marketplace.json"),
			} {
				if err := validateRegularFileOrMissing(path); err != nil {
					return err
				}
			}
			if err := validateCodexMarketplaceOwnership(filepath.Join(root, ".agents", "plugins", "marketplace.json"), i.opts.ProjectLocal); err != nil {
				return err
			}
			configPath := filepath.Join(root, ".codex", "config.toml")
			marketplaceRoot, err := filepath.Abs(root)
			if err != nil {
				return err
			}
			var configText string
			if body, err := os.ReadFile(configPath); err == nil {
				configText = string(body)
			} else if !os.IsNotExist(err) {
				return err
			}
			if _, err := updateCodexConfig(configText, marketplaceRoot); err != nil {
				return fmt.Errorf("parse %s: %w", configPath, err)
			}
		case "claude":
			if i.opts.ProjectLocal {
				if err := validateSkills(filepath.Join(root, ".claude", "skills")); err != nil {
					return err
				}
				continue
			}
			if err := validateOwnedPluginDir(filepath.Join(root, ".claude", "skills", specgatePluginName)); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateProjectLocalPluginAncestors(root string, agents []string) error {
	var dirs []string
	for _, agent := range agents {
		switch agent {
		case "cursor":
			dirs = append(dirs,
				filepath.Join(root, ".cursor", "rules"),
				filepath.Join(root, ".cursor", "skills"),
			)
		case "codex":
			dirs = append(dirs, filepath.Join(root, ".agents", "skills"))
		case "claude":
			dirs = append(dirs, filepath.Join(root, ".claude", "skills"))
		}
	}
	for _, dir := range dirs {
		if err := validatePluginDirectoryPath(root, dir); err != nil {
			return err
		}
	}
	return nil
}

func validatePluginDirectoryPath(root, path string) error {
	absRoot, _, rel, err := resolvePluginPath(root, path)
	if err != nil {
		return err
	}
	return validatePluginDirectoryChain(absRoot, rel)
}

func resolvePluginPath(root, path string) (string, string, string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", "", "", err
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", "", "", err
	}
	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", "", fmt.Errorf("plugin path %s is outside install root %s", path, root)
	}
	return absRoot, absPath, rel, nil
}

func validatePluginDirectoryChain(root, rel string) error {
	current := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("%s is not a real directory; refusing plugin changes", current)
		}
	}
	return nil
}

func validateRegularFileOrMissing(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("%s exists but is not a regular file; refusing to overwrite it", path)
	}
	return nil
}

func validateOwnedPluginDir(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%s exists but is not a real directory; refusing to overwrite it", path)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return nil
	}
	if err := validatePluginOwnerMarker(filepath.Join(path, pluginOwnerMarker), path); err != nil {
		return err
	}
	return rejectSymlinksWithin(path)
}

func rejectSymlinksWithin(root string) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s contains a symlink; refusing to install through it", root)
		}
		return nil
	})
}

func validateOwnedPluginFile(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("%s exists but is not a regular file; refusing to overwrite it", path)
	}
	return validatePluginOwnerMarker(path+pluginOwnerMarker, path)
}

func validatePluginOwnerMarker(marker, target string) error {
	_, err := pluginOwnerMarkerVersion(marker, target)
	return err
}

func pluginOwnerMarkerVersion(marker, target string) (string, error) {
	info, err := os.Lstat(marker)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s exists but is not owned by SpecGate; move or remove it before installing", target)
	}
	body, err := os.ReadFile(marker)
	if err != nil {
		return "", fmt.Errorf("%s exists but is not owned by SpecGate; move or remove it before installing", target)
	}
	text := string(body)
	if text == pluginOwnerValue {
		return "", nil
	}
	version, ok := strings.CutPrefix(text, pluginOwnerValue+"version=")
	if !ok || !strings.HasSuffix(version, "\n") {
		return "", fmt.Errorf("%s exists but is not owned by SpecGate; move or remove it before installing", target)
	}
	version = strings.TrimSuffix(version, "\n")
	if !validPluginVersion(version) {
		return "", fmt.Errorf("%s exists but is not owned by SpecGate; move or remove it before installing", target)
	}
	return version, nil
}

func pluginOwnerMarkerValue(version string) string {
	return pluginOwnerValue + "version=" + strings.TrimSpace(version) + "\n"
}

func (i *pluginInstaller) installCursor() error {
	root := i.home
	if i.opts.ProjectLocal {
		root = "."
	}
	rule := filepath.Join(root, ".cursor", "rules", "using-specgate.mdc")
	skillsDir := filepath.Join(root, ".cursor", "skills")
	if err := i.writePluginFile("rules/using-specgate.mdc", rule, 0o644); err != nil {
		return err
	}
	if err := i.writeFile(rule+pluginOwnerMarker, []byte(pluginOwnerMarkerValue(i.pkg.Version)), 0o600); err != nil {
		return err
	}
	return i.installFocusedSkills(skillsDir)
}

func (i *pluginInstaller) installCodex() error {
	root := i.home
	if i.opts.ProjectLocal {
		root = "."
		if err := i.installFocusedSkills(filepath.Join(root, ".agents", "skills")); err != nil {
			return err
		}
		return i.migrateLegacyProjectCodexInstall(root)
	}
	pluginRoot := filepath.Join(root, ".codex", "plugins", specgatePluginName)
	cacheRoot := filepath.Join(root, ".codex", "plugins", "cache", "personal", specgatePluginName, i.pkg.Version)
	marketplace := filepath.Join(root, ".agents", "plugins", "marketplace.json")
	configPath := filepath.Join(root, ".codex", "config.toml")
	marketplaceRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	if err := i.installPluginBundle(pluginRoot, ".codex-plugin/plugin.json", "hooks/hooks.json"); err != nil {
		return err
	}
	if strings.TrimSpace(i.pkg.Version) != "" {
		if err := i.installPluginBundle(cacheRoot, ".codex-plugin/plugin.json", "hooks/hooks.json"); err != nil {
			return err
		}
		cacheParent := filepath.Dir(cacheRoot)
		if err := i.removeObsoleteOwnedDirs(cacheParent, map[string]bool{cacheRoot: true}, "cache"); err != nil {
			return err
		}
	}
	if err := i.mergeCodexMarketplace(marketplace, installedCodexPluginSource); err != nil {
		return err
	}
	return i.enableCodexConfig(configPath, marketplaceRoot)
}

func (i *pluginInstaller) validateLegacyProjectCodexInstall(root string) error {
	legacyRoot := filepath.Join(root, ".codex", "plugins", specgatePluginName)
	if !hasValidPluginOwnership(legacyRoot, true) {
		return nil
	}
	for _, dir := range []string{
		filepath.Join(root, ".codex", "plugins"),
		filepath.Join(root, ".agents", "plugins"),
	} {
		if err := validatePluginDirectoryPath(root, dir); err != nil {
			return err
		}
	}
	if err := validateOwnedPluginDir(legacyRoot); err != nil {
		return err
	}
	marketplacePath := filepath.Join(root, ".agents", "plugins", "marketplace.json")
	if err := validateRegularFileOrMissing(marketplacePath); err != nil {
		return err
	}
	unowned, err := codexMarketplaceHasUnownedSpecGateEntry(marketplacePath)
	if err != nil {
		return err
	}
	if unowned {
		return nil
	}
	configPath := filepath.Join(root, ".codex", "config.toml")
	if err := validateRegularFileOrMissing(configPath); err != nil {
		return err
	}
	body, err := os.ReadFile(configPath)
	if os.IsNotExist(err) || strings.TrimSpace(string(body)) == "" {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := parseTOML(string(body)); err != nil {
		return fmt.Errorf("parse %s: %w", configPath, err)
	}
	return nil
}

func (i *pluginInstaller) migrateLegacyProjectCodexInstall(root string) error {
	legacyRoot := filepath.Join(root, ".codex", "plugins", specgatePluginName)
	if !hasValidPluginOwnership(legacyRoot, true) {
		return nil
	}
	if i.opts.DryRun {
		i.printf("[dry-run] remove owned legacy Codex plugin files from %s\n", legacyRoot)
		return nil
	}
	marketplacePath := filepath.Join(root, ".agents", "plugins", "marketplace.json")
	unowned, err := codexMarketplaceHasUnownedSpecGateEntry(marketplacePath)
	if err != nil {
		return err
	}
	if !unowned {
		if _, err := removeCodexMarketplaceEntry(marketplacePath); err != nil {
			return err
		}
		removePersonalMarketplace := false
		if _, err := os.Stat(marketplacePath); os.IsNotExist(err) {
			removePersonalMarketplace = true
		} else if err != nil {
			return err
		}
		marketplaceRoot, err := filepath.Abs(root)
		if err != nil {
			return err
		}
		if _, err := removeCodexConfigSections(filepath.Join(root, ".codex", "config.toml"), removePersonalMarketplace, marketplaceRoot); err != nil {
			return err
		}
	}
	changed, fullyRemoved, err := removeOwnedPluginDir(legacyRoot)
	if err != nil {
		return err
	}
	if changed {
		if fullyRemoved {
			i.printf("removed owned legacy Codex plugin files from %s\n", legacyRoot)
		} else {
			i.printf("removed owned legacy Codex plugin files from %s; preserved unrelated files\n", legacyRoot)
		}
	}
	for _, dir := range []string{
		filepath.Join(root, ".codex", "plugins"),
		filepath.Join(root, ".agents", "plugins"),
		filepath.Join(root, ".codex"),
	} {
		if _, err := removeDirIfEmpty(dir); err != nil {
			return err
		}
	}
	return nil
}

func (i *pluginInstaller) installPluginBundle(pluginRoot, manifest, hooks string) error {
	for _, path := range []string{manifest, "assets/logo.svg", hooks, "hooks/run-hook.cmd", "hooks/session-start"} {
		mode := os.FileMode(0o644)
		if path == "hooks/run-hook.cmd" || path == "hooks/session-start" {
			mode = 0o755
		}
		if err := i.writePluginFile(path, filepath.Join(pluginRoot, path), mode); err != nil {
			return err
		}
	}
	if err := i.installFocusedSkills(filepath.Join(pluginRoot, "skills")); err != nil {
		return err
	}
	return i.writeFile(filepath.Join(pluginRoot, pluginOwnerMarker), []byte(pluginOwnerMarkerValue(i.pkg.Version)), 0o600)
}

func (i *pluginInstaller) installClaude() error {
	root := i.home
	if i.opts.ProjectLocal {
		root = "."
		return i.installFocusedSkills(filepath.Join(root, ".claude", "skills"))
	}
	pluginRoot := filepath.Join(root, ".claude", "skills", specgatePluginName)
	return i.installPluginBundle(pluginRoot, ".claude-plugin/plugin.json", "hooks/hooks-claude.json")
}

func (i *pluginInstaller) installFocusedSkills(destDir string) error {
	for _, skill := range i.pkg.Skills {
		skillDir := filepath.Join(destDir, skill)
		if err := i.writePluginFile("skills/"+skill+"/SKILL.md", filepath.Join(skillDir, "SKILL.md"), 0o644); err != nil {
			return err
		}
		if err := i.writeFile(filepath.Join(skillDir, pluginOwnerMarker), []byte(pluginOwnerMarkerValue(i.pkg.Version)), 0o600); err != nil {
			return err
		}
	}
	return i.removeObsoleteFocusedSkills(destDir)
}

func (i *pluginInstaller) removeObsoleteFocusedSkills(destDir string) error {
	current := make(map[string]bool, len(i.pkg.Skills))
	for _, skill := range i.pkg.Skills {
		current[filepath.Join(destDir, skill)] = true
	}
	return i.removeObsoleteOwnedDirs(destDir, current, "skill")
}

func (i *pluginInstaller) removeObsoleteOwnedDirs(root string, current map[string]bool, kind string) error {
	owned, err := ownedPluginDirs(root)
	if err != nil {
		return err
	}
	for _, path := range owned {
		if current[path] {
			continue
		}
		if i.opts.DryRun {
			i.printf("[dry-run] remove obsolete owned %s %s\n", kind, path)
			continue
		}
		changed, _, err := removeOwnedPluginDir(path)
		if err != nil {
			return err
		}
		if changed {
			i.printf("removed obsolete owned %s %s\n", kind, path)
		}
	}
	return nil
}

func ownedPluginDirs(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var owned []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name())
		if validatePluginOwnerMarker(filepath.Join(path, pluginOwnerMarker), path) == nil {
			owned = append(owned, path)
		}
	}
	return owned, nil
}

func (i *pluginInstaller) writePluginFile(src, dest string, mode os.FileMode) error {
	body, err := i.pluginFile(src)
	if err != nil {
		return err
	}
	return i.writeFile(dest, body, mode)
}

func (i *pluginInstaller) pluginFile(src string) ([]byte, error) {
	body, ok := i.files[src]
	if !ok {
		return nil, fmt.Errorf("plugin package omitted preloaded file %q", src)
	}
	return body, nil
}

func (i *pluginInstaller) writeFile(dest string, body []byte, mode os.FileMode) error {
	if i.opts.DryRun {
		i.printf("[dry-run] write %s\n", dest)
		return nil
	}
	root := i.home
	if i.opts.ProjectLocal {
		root = "."
	}
	if err := validatePluginWritePath(root, dest); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	if err := fsutil.AtomicWriteFile(dest, body, mode); err != nil {
		return err
	}
	i.written++
	i.printf("wrote %s\n", dest)
	return nil
}

func validatePluginWritePath(root, dest string) error {
	absRoot, absDest, rel, err := resolvePluginPath(root, dest)
	if err != nil {
		return err
	}
	if err := validatePluginDirectoryChain(absRoot, filepath.Dir(rel)); err != nil {
		return err
	}
	if info, err := os.Lstat(absDest); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("%s exists but is not a regular file; refusing to overwrite it", dest)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

// hasCommittedCodexPointer reports whether the plugin entry named entryName
// targets the checked-in repo-root source ("./plugins"). Only project-local
// installs may preserve this pointer.
func hasCommittedCodexPointer(plugins []map[string]any, entryName string) bool {
	for _, plugin := range plugins {
		if pname, _ := plugin["name"].(string); pname != entryName {
			continue
		}
		if codexPluginHasLocalSource(plugin, committedCodexPluginSource) {
			return true
		}
	}
	return false
}

func codexPluginSourcePath(plugin map[string]any) string {
	source, _ := plugin["source"].(map[string]any)
	path, _ := source["path"].(string)
	return strings.TrimSpace(path)
}

func codexPluginHasLocalSource(plugin map[string]any, path string) bool {
	source, _ := plugin["source"].(map[string]any)
	sourceType, _ := source["source"].(string)
	return strings.TrimSpace(sourceType) == "local" && codexPluginSourcePath(plugin) == path
}

func validateCodexMarketplaceOwnership(path string, projectLocal bool) error {
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var data struct {
		Plugins []map[string]any `json:"plugins"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	for _, plugin := range data.Plugins {
		name, _ := plugin["name"].(string)
		if name != specgatePluginName {
			continue
		}
		source := codexPluginSourcePath(plugin)
		if codexPluginHasLocalSource(plugin, installedCodexPluginSource) ||
			(projectLocal && codexPluginHasLocalSource(plugin, committedCodexPluginSource)) {
			continue
		}
		return fmt.Errorf("marketplace entry %q points to %q and is not managed by SpecGate; move or remove it before installing", specgatePluginName, source)
	}
	return nil
}

func codexMarketplaceHasUnownedSpecGateEntry(path string) (bool, error) {
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var data struct {
		Plugins []map[string]any `json:"plugins"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return false, fmt.Errorf("parse %s: %w", path, err)
	}
	for _, plugin := range data.Plugins {
		name, _ := plugin["name"].(string)
		if name == specgatePluginName && !codexPluginHasLocalSource(plugin, installedCodexPluginSource) {
			return true, nil
		}
	}
	return false, nil
}

func (i *pluginInstaller) mergeCodexMarketplace(path, pluginPath string) error {
	templateBytes, err := i.pluginFile(codexPersonalMarketURL)
	if err != nil {
		return err
	}
	templateBytes = bytes.ReplaceAll(templateBytes, []byte(pluginPathPlaceholder), []byte(pluginPath))
	if i.opts.DryRun {
		i.printf("[dry-run] add or update specgate entry in %s\n", path)
		return nil
	}
	var tmpl struct {
		Name      string           `json:"name"`
		Interface map[string]any   `json:"interface"`
		Plugins   []map[string]any `json:"plugins"`
	}
	if err := json.Unmarshal(templateBytes, &tmpl); err != nil {
		return fmt.Errorf("parse %s: %w", codexPersonalMarketURL, err)
	}
	if len(tmpl.Plugins) == 0 {
		return fmt.Errorf("%s has no plugins entry", codexPersonalMarketURL)
	}
	entry := tmpl.Plugins[0]
	entryName, _ := entry["name"].(string)
	if entryName == "" {
		return fmt.Errorf("%s plugin entry missing name", codexPersonalMarketURL)
	}
	var data struct {
		Name      string           `json:"name"`
		Interface map[string]any   `json:"interface"`
		Plugins   []map[string]any `json:"plugins"`
	}
	if existing, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(existing))) > 0 {
		if err := json.Unmarshal(existing, &data); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
	}
	// A project-local install must not clobber the checked-in repo-root pointer.
	// It already resolves to the local package; rewriting it would corrupt the
	// committed marketplace.
	if i.opts.ProjectLocal && hasCommittedCodexPointer(data.Plugins, entryName) {
		i.printf("keeping committed repo-root marketplace pointer in %s (source %s)\n", path, committedCodexPluginSource)
		return nil
	}
	if data.Name == "" {
		data.Name = tmpl.Name
	}
	if data.Name == "" {
		data.Name = "personal"
	}
	if data.Interface == nil {
		data.Interface = tmpl.Interface
	}
	if data.Interface == nil {
		data.Interface = map[string]any{"displayName": "Personal"}
	}
	filtered := data.Plugins[:0]
	for _, plugin := range data.Plugins {
		if name, _ := plugin["name"].(string); name != entryName {
			filtered = append(filtered, plugin)
		}
	}
	data.Plugins = append(filtered, entry)
	out, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return i.writeFile(path, append(out, '\n'), 0o644)
}

func (i *pluginInstaller) enableCodexConfig(path string, marketplaceRoot string) error {
	if i.opts.DryRun {
		i.printf("[dry-run] enable specgate@personal and personal marketplace in %s\n", path)
		return nil
	}
	if err := validateRegularFileOrMissing(path); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	var text string
	if info, err := os.Lstat(path); err == nil {
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return err
	}
	if body, err := os.ReadFile(path); err == nil {
		text = string(body)
	} else if !os.IsNotExist(err) {
		return err
	}
	body, err := updateCodexConfig(text, marketplaceRoot)
	if err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return i.writeFile(path, body, mode)
}

func (i *pluginInstaller) printf(format string, args ...any) {
	if i.deps.Printer != nil && i.deps.Printer.Mode() == output.ModeJSON {
		return
	}
	fmt.Fprintf(i.deps.Stdout, format, args...)
}

func updateCodexConfig(text string, marketplaceRoot string) ([]byte, error) {
	config, err := parseTOML(text)
	if err != nil {
		return nil, err
	}
	cleanRoot := filepath.Clean(strings.TrimSpace(marketplaceRoot))
	targets := map[string]bool{}
	if plugins, ok := config["plugins"].(map[string]any); ok {
		if _, ok := plugins["specgate@personal"]; ok {
			targets[`plugins.specgate@personal`] = true
		}
	}
	if marketplaces, ok := config["marketplaces"].(map[string]any); ok {
		if personal, ok := marketplaces["personal"].(map[string]any); ok {
			sourceType, _ := personal["source_type"].(string)
			source, _ := personal["source"].(string)
			if (sourceType != "" && sourceType != "local") || (source != "" && filepath.Clean(source) != cleanRoot) {
				return nil, fmt.Errorf("[marketplaces.personal] is already configured for another source")
			}
			targets["marketplaces.personal"] = true
		}
	}
	base := text
	for _, target := range []string{`plugins.specgate@personal`, "marketplaces.personal"} {
		if !targets[target] {
			continue
		}
		var removed bool
		base, removed = removeTOMLSections(base, map[string]bool{target: true})
		if !removed {
			return nil, fmt.Errorf("refusing to rewrite non-section TOML for [%s]", target)
		}
	}
	if base != "" && !strings.HasSuffix(base, "\n") {
		base += "\n"
	}
	if strings.TrimSpace(base) != "" && !strings.HasSuffix(base, "\n\n") {
		base += "\n"
	}
	base += "[plugins.\"specgate@personal\"]\nenabled = true\n\n"
	base += "[marketplaces.personal]\nsource_type = \"local\"\nsource = " + fmt.Sprintf("%q", cleanRoot) + "\n"
	return []byte(base), nil
}

func parseTOML(text string) (map[string]any, error) {
	config := map[string]any{}
	if strings.TrimSpace(text) == "" {
		return config, nil
	}
	if err := toml.Unmarshal([]byte(text), &config); err != nil {
		return nil, err
	}
	return config, nil
}

func removeTOMLSections(text string, targets map[string]bool) (string, bool) {
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	skip := false
	changed := false
	for _, line := range lines {
		if section, ok := tomlSectionName(line); ok {
			skip = targets[section]
			if skip {
				changed = true
				continue
			}
		}
		if !skip {
			out = append(out, line)
		}
	}
	result := strings.Join(out, "\n")
	result = strings.TrimRight(result, "\n")
	if result != "" {
		result += "\n"
	}
	return result, changed
}

func tomlSectionName(line string) (string, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "[") {
		return "", false
	}
	closeAt := strings.Index(line, "]")
	if closeAt < 2 {
		return "", false
	}
	if trailing := strings.TrimSpace(line[closeAt+1:]); trailing != "" && !strings.HasPrefix(trailing, "#") {
		return "", false
	}
	name := strings.TrimSpace(line[1:closeAt])
	name = strings.ReplaceAll(name, `"`, "")
	name = strings.ReplaceAll(name, `'`, "")
	return name, name != ""
}

func codexConfigRegistersPersonalMarketplace(text, marketplaceRoot string) bool {
	config, err := parseTOML(text)
	if err != nil {
		return false
	}
	marketplaces, ok := config["marketplaces"].(map[string]any)
	if !ok {
		return false
	}
	personal, ok := marketplaces["personal"].(map[string]any)
	if !ok {
		return false
	}
	sourceType, _ := personal["source_type"].(string)
	source, _ := personal["source"].(string)
	return sourceType == "local" &&
		filepath.Clean(strings.TrimSpace(source)) == filepath.Clean(strings.TrimSpace(marketplaceRoot))
}

func checkPluginAgent(agent, home string, projectLocal bool, pkg *client.PluginPackage) pluginAgentHealth {
	health := pluginAgentHealth{Agent: agent, OK: true}
	root := home
	if projectLocal {
		root = "."
	}
	skills := pluginSkillsFromPackage(pkg)
	latestVersion := ""
	if pkg != nil {
		latestVersion = strings.TrimSpace(pkg.Version)
		health.LatestVersion = latestVersion
	}
	var required []string
	var pluginManifest string
	var ownershipTargets []pluginOwnershipTarget
	addSkills := func(dir string) {
		for _, skill := range skills {
			skillDir := filepath.Join(dir, skill)
			required = append(required, filepath.Join(skillDir, "SKILL.md"))
			ownershipTargets = append(ownershipTargets, pluginOwnershipTarget{path: skillDir, directory: true})
		}
	}
	switch agent {
	case "cursor":
		pluginRoot := filepath.Join(root, ".cursor")
		rule := filepath.Join(pluginRoot, "rules", "using-specgate.mdc")
		required = append(required, rule)
		ownershipTargets = append(ownershipTargets, pluginOwnershipTarget{path: rule})
		addSkills(filepath.Join(pluginRoot, "skills"))
	case "codex":
		if projectLocal {
			addSkills(filepath.Join(root, ".agents", "skills"))
			break
		}
		pluginRoot := filepath.Join(root, ".codex", "plugins", specgatePluginName)
		configPath := filepath.Join(root, ".codex", "config.toml")
		marketplace := filepath.Join(root, ".agents", "plugins", "marketplace.json")
		pluginManifest = filepath.Join(pluginRoot, ".codex-plugin", "plugin.json")
		required = append(required,
			pluginManifest,
			filepath.Join(pluginRoot, "assets", "logo.svg"),
			filepath.Join(pluginRoot, "hooks", "hooks.json"),
			filepath.Join(pluginRoot, "hooks", "run-hook.cmd"),
			filepath.Join(pluginRoot, "hooks", "session-start"),
			marketplace,
			configPath,
		)
		ownershipTargets = append(ownershipTargets, pluginOwnershipTarget{path: pluginRoot, directory: true})
		addSkills(filepath.Join(pluginRoot, "skills"))
	case "claude":
		if projectLocal {
			addSkills(filepath.Join(root, ".claude", "skills"))
			break
		}
		pluginRoot := filepath.Join(root, ".claude", "skills", specgatePluginName)
		pluginManifest = filepath.Join(pluginRoot, ".claude-plugin", "plugin.json")
		required = append(required,
			pluginManifest,
			filepath.Join(pluginRoot, "assets", "logo.svg"),
			filepath.Join(pluginRoot, "hooks", "hooks-claude.json"),
			filepath.Join(pluginRoot, "hooks", "run-hook.cmd"),
			filepath.Join(pluginRoot, "hooks", "session-start"),
		)
		ownershipTargets = append(ownershipTargets, pluginOwnershipTarget{path: pluginRoot, directory: true})
		addSkills(filepath.Join(pluginRoot, "skills"))
	}
	for _, path := range required {
		if !isRegularPluginFile(path) {
			health.Missing = append(health.Missing, path)
		}
	}
	for _, target := range ownershipTargets {
		if !hasValidPluginOwnership(target.path, target.directory) {
			health.Missing = append(health.Missing, target.path+" SpecGate ownership")
		}
	}
	if agent == "codex" && !projectLocal {
		configPath := filepath.Join(root, ".codex", "config.toml")
		marketplace := filepath.Join(root, ".agents", "plugins", "marketplace.json")
		pluginRoot := filepath.Join(root, ".codex", "plugins", specgatePluginName)
		marketplaceRoot, rootErr := filepath.Abs(root)
		if rootErr != nil {
			health.Missing = append(health.Missing, configPath+" registered personal marketplace")
		}
		if isRegularPluginFile(configPath) {
			body, _ := os.ReadFile(configPath)
			configText := string(body)
			if !codexConfigEnablesSpecGate(configText) {
				health.Missing = append(health.Missing, configPath+" enabled specgate@personal")
			}
			if rootErr != nil || !codexConfigRegistersPersonalMarketplace(configText, marketplaceRoot) {
				health.Missing = append(health.Missing, configPath+" registered personal marketplace")
			}
		}
		if isRegularPluginFile(marketplace) {
			body, _ := os.ReadFile(marketplace)
			if !codexMarketplaceHasPlugin(body, specgatePluginName, projectLocal) {
				health.Missing = append(health.Missing, marketplace+" SpecGate-managed entry")
			}
		}
		if latestVersion != "" {
			cacheWarnings := codexCacheWarnings(home, pluginRoot, latestVersion, skills)
			health.Warnings = append(health.Warnings, cacheWarnings...)
			health.NeedsUpdate = len(cacheWarnings) > 0
		}
	}
	if pluginManifest != "" && latestVersion != "" && isRegularPluginFile(pluginManifest) {
		installedVersion := readPluginManifestVersion(pluginManifest)
		health.InstalledVersion = installedVersion
		if installedVersion != "" && installedVersion != latestVersion {
			health.NeedsUpdate = true
			health.Warnings = append(health.Warnings, pluginVersionMismatchWarning(installedVersion, latestVersion, pluginRepairCommand(agent, projectLocal)))
		}
	}
	if pluginManifest == "" && latestVersion != "" && len(ownershipTargets) > 0 && len(health.Missing) == 0 {
		installedVersion, consistent := directPluginInstallVersion(ownershipTargets)
		if consistent {
			health.InstalledVersion = installedVersion
		}
		if !consistent || installedVersion != latestVersion {
			health.NeedsUpdate = true
			repair := pluginRepairCommand(agent, projectLocal)
			if installedVersion == "" {
				health.Warnings = append(health.Warnings, fmt.Sprintf("Installed plugin file versions are missing or mixed; latest is %s; run '%s'.", latestVersion, repair))
			} else {
				health.Warnings = append(health.Warnings, pluginVersionMismatchWarning(installedVersion, latestVersion, repair))
			}
		}
	}
	sort.Strings(health.Missing)
	sort.Strings(health.Warnings)
	health.OK = len(health.Missing) == 0
	if !health.OK {
		health.RepairCommand = pluginRepairCommand(agent, projectLocal)
	}
	return health
}

func pluginVersionMismatchWarning(installed, latest, repair string) string {
	return fmt.Sprintf("Installed plugin version %s does not match latest %s; run '%s'.", installed, latest, repair)
}

func directPluginInstallVersion(targets []pluginOwnershipTarget) (string, bool) {
	version := ""
	for _, target := range targets {
		marker := target.path + pluginOwnerMarker
		if target.directory {
			marker = filepath.Join(target.path, pluginOwnerMarker)
		}
		current, err := pluginOwnerMarkerVersion(marker, target.path)
		if err != nil || current == "" {
			return "", false
		}
		if version == "" {
			version = current
			continue
		}
		if current != version {
			return "", false
		}
	}
	return version, version != ""
}

func isRegularPluginFile(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&os.ModeSymlink == 0 && info.Mode().IsRegular()
}

func hasValidPluginOwnership(path string, directory bool) bool {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	if directory != info.IsDir() || (!directory && !info.Mode().IsRegular()) {
		return false
	}
	marker := path + pluginOwnerMarker
	if directory {
		marker = filepath.Join(path, pluginOwnerMarker)
	}
	return validatePluginOwnerMarker(marker, path) == nil
}

func pluginAgentAvailable(agent string) bool {
	if _, err := exec.LookPath(agent); err == nil {
		return true
	}
	if agent == "cursor" && runtime.GOOS == "darwin" {
		if _, err := os.Stat("/Applications/Cursor.app"); err == nil {
			return true
		}
	}
	return false
}

func codexMarketplaceHasPlugin(body []byte, name string, projectLocal bool) bool {
	var data struct {
		Plugins []map[string]any `json:"plugins"`
	}
	if json.Unmarshal(body, &data) != nil {
		return false
	}
	for _, plugin := range data.Plugins {
		pluginName, _ := plugin["name"].(string)
		if pluginName == name &&
			(codexPluginHasLocalSource(plugin, installedCodexPluginSource) ||
				(projectLocal && codexPluginHasLocalSource(plugin, committedCodexPluginSource))) {
			return true
		}
	}
	return false
}

func pluginRepairCommand(agent string, projectLocal bool) string {
	cmd := "specgate plugins install --agent " + agent
	if projectLocal {
		cmd += " --project-local"
	}
	return cmd
}

func pluginSkillsFromPackage(pkg *client.PluginPackage) []string {
	if pkg != nil && len(pkg.Skills) > 0 {
		skills := append([]string(nil), pkg.Skills...)
		sort.Strings(skills)
		return skills
	}
	return focusedPluginSkills()
}

func readPluginManifestVersion(path string) string {
	body, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var manifest struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(body, &manifest); err != nil {
		return ""
	}
	return strings.TrimSpace(manifest.Version)
}

func codexCacheWarnings(home, pluginRoot, latestVersion string, skills []string) []string {
	cacheRoot := filepath.Join(home, ".codex", "plugins", "cache", "personal", specgatePluginName, latestVersion)
	if _, err := os.Stat(cacheRoot); err != nil {
		return nil
	}
	var warnings []string
	for _, skill := range skills {
		cacheSkill := filepath.Join(cacheRoot, "skills", skill, "SKILL.md")
		sourceSkill := filepath.Join(pluginRoot, "skills", skill, "SKILL.md")
		if _, err := os.Stat(cacheSkill); err == nil {
			continue
		}
		if _, err := os.Stat(sourceSkill); err != nil {
			continue
		}
		warnings = append(warnings, fmt.Sprintf("Codex plugin cache is stale and is missing %s; restart Codex so the refreshed plugin loads.", skill))
	}
	if installed := readPluginManifestVersion(filepath.Join(pluginRoot, ".codex-plugin", "plugin.json")); installed == latestVersion {
		cached := readPluginManifestVersion(filepath.Join(cacheRoot, ".codex-plugin", "plugin.json"))
		if cached != "" && cached != installed {
			warnings = append(warnings, fmt.Sprintf("Codex plugin cache still has version %s while installed source is %s; restart Codex.", cached, installed))
		}
	}
	return warnings
}

func codexConfigEnablesSpecGate(text string) bool {
	config, err := parseTOML(text)
	if err != nil {
		return false
	}
	plugins, ok := config["plugins"].(map[string]any)
	if !ok {
		return false
	}
	specgate, ok := plugins["specgate@personal"].(map[string]any)
	if !ok {
		return false
	}
	enabled, _ := specgate["enabled"].(bool)
	return enabled
}

func removeSpecGatePluginFiles(deps *Deps) (int, []string, []string, error) {
	home, err := userHomeDir(deps)
	if err != nil {
		return 0, nil, nil, err
	}
	var removed int
	var paths []string
	var preserved []string
	removeDir := func(path string) error {
		changed, fullyRemoved, err := removeOwnedPluginDir(path)
		if err != nil {
			return err
		}
		if changed {
			removed++
			if fullyRemoved {
				paths = append(paths, path)
			} else {
				preserved = append(preserved, path)
			}
		}
		return nil
	}
	removeFile := func(path string) error {
		ok, err := removeOwnedPluginFile(path)
		if err != nil {
			return err
		}
		if ok {
			removed++
			paths = append(paths, path)
		}
		return nil
	}
	cursorRule := filepath.Join(home, ".cursor", "rules", "using-specgate.mdc")
	if err := removeFile(cursorRule); err != nil {
		return removed, paths, preserved, err
	}
	for _, path := range []string{
		filepath.Join(home, ".codex", "plugins", specgatePluginName),
		filepath.Join(home, ".claude", "skills", specgatePluginName),
	} {
		if err := removeDir(path); err != nil {
			return removed, paths, preserved, err
		}
	}
	cacheRoot := filepath.Join(home, ".codex", "plugins", "cache", "personal", specgatePluginName)
	if entries, err := os.ReadDir(cacheRoot); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				if err := removeDir(filepath.Join(cacheRoot, entry.Name())); err != nil {
					return removed, paths, preserved, err
				}
			}
		}
		_, _ = removeDirIfEmpty(cacheRoot)
	} else if !os.IsNotExist(err) {
		return removed, paths, preserved, err
	}
	cursorSkills := filepath.Join(home, ".cursor", "skills")
	ownedSkills, err := ownedPluginDirs(cursorSkills)
	if err != nil {
		return removed, paths, preserved, err
	}
	for _, path := range ownedSkills {
		if err := removeDir(path); err != nil {
			return removed, paths, preserved, err
		}
	}
	marketplacePath := filepath.Join(home, ".agents", "plugins", "marketplace.json")
	unownedMarketplaceEntry, err := codexMarketplaceHasUnownedSpecGateEntry(marketplacePath)
	if err != nil {
		return removed, paths, preserved, err
	}
	if unownedMarketplaceEntry {
		return removed, paths, preserved, nil
	}
	if changed, err := removeCodexMarketplaceEntry(marketplacePath); err != nil {
		return removed, paths, preserved, err
	} else if changed {
		removed++
		paths = append(paths, marketplacePath)
	}
	removePersonalMarketplace := false
	if _, err := os.Stat(marketplacePath); os.IsNotExist(err) {
		removePersonalMarketplace = true
	} else if err != nil {
		return removed, paths, preserved, err
	}
	marketplaceRoot, err := filepath.Abs(home)
	if err != nil {
		return removed, paths, preserved, err
	}
	configPath := filepath.Join(home, ".codex", "config.toml")
	if changed, err := removeCodexConfigSections(configPath, removePersonalMarketplace, marketplaceRoot); err != nil {
		return removed, paths, preserved, err
	} else if changed {
		removed++
		paths = append(paths, configPath)
	}
	for _, dir := range []string{
		filepath.Join(home, ".codex", "plugins"),
		filepath.Join(home, ".agents", "plugins"),
		filepath.Join(home, ".cursor", "skills"),
	} {
		if ok, err := removeDirIfEmpty(dir); err != nil {
			return removed, paths, preserved, err
		} else if ok {
			removed++
			paths = append(paths, dir)
		}
	}
	return removed, paths, preserved, nil
}

func removeOwnedPluginDir(path string) (bool, bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, false, nil
	} else if err != nil {
		return false, false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return false, false, nil
	}
	if err := validatePluginOwnerMarker(filepath.Join(path, pluginOwnerMarker), path); err != nil {
		return false, false, nil
	}
	if err := rejectSymlinksWithin(path); err != nil {
		return false, false, err
	}
	managedFiles := []string{
		filepath.Join(path, "SKILL.md"),
		filepath.Join(path, ".codex-plugin", "plugin.json"),
		filepath.Join(path, ".claude-plugin", "plugin.json"),
		filepath.Join(path, "assets", "logo.svg"),
		filepath.Join(path, "hooks", "hooks.json"),
		filepath.Join(path, "hooks", "hooks-claude.json"),
		filepath.Join(path, "hooks", "run-hook.cmd"),
		filepath.Join(path, "hooks", "session-start"),
	}
	managedDirs := []string{
		filepath.Join(path, ".codex-plugin"),
		filepath.Join(path, ".claude-plugin"),
		filepath.Join(path, "assets"),
		filepath.Join(path, "hooks"),
		filepath.Join(path, "skills"),
	}
	skillsDir := filepath.Join(path, "skills")
	if entries, err := os.ReadDir(skillsDir); err == nil {
		for _, entry := range entries {
			skillDir := filepath.Join(skillsDir, entry.Name())
			if !entry.IsDir() {
				continue
			}
			if err := validatePluginOwnerMarker(filepath.Join(skillDir, pluginOwnerMarker), skillDir); err != nil {
				continue
			}
			managedFiles = append(managedFiles,
				filepath.Join(skillDir, pluginOwnerMarker),
				filepath.Join(skillDir, "SKILL.md"),
			)
			managedDirs = append(managedDirs, skillDir)
		}
	} else if !os.IsNotExist(err) {
		return false, false, err
	}
	managedFiles = append(managedFiles, filepath.Join(path, pluginOwnerMarker))
	for _, file := range managedFiles {
		info, err := os.Lstat(file)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return false, false, err
		}
		if !info.Mode().IsRegular() {
			return false, false, fmt.Errorf("refusing to remove managed plugin path %s because it is not a regular file", file)
		}
	}
	changed := false
	for _, file := range managedFiles {
		if err := os.Remove(file); err == nil {
			changed = true
		} else if !os.IsNotExist(err) {
			return false, false, err
		}
	}
	for index := len(managedDirs) - 1; index >= 0; index-- {
		if _, err := removeDirIfEmpty(managedDirs[index]); err != nil {
			return false, false, err
		}
	}
	fullyRemoved, err := removeDirIfEmpty(path)
	if err != nil {
		return false, false, err
	}
	return changed, fullyRemoved, nil
}

func removeOwnedPluginFile(path string) (bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return false, nil
	}
	marker := path + pluginOwnerMarker
	if err := validatePluginOwnerMarker(marker, path); err != nil {
		return false, nil
	}
	if err := os.Remove(path); err != nil {
		return false, err
	}
	if err := os.Remove(marker); err != nil && !os.IsNotExist(err) {
		return false, err
	}
	return true, nil
}

func removePathIfExists(path string) (bool, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if err := os.RemoveAll(path); err != nil {
		return false, err
	}
	return true, nil
}

func removeCodexConfigSections(path string, removePersonalMarketplace bool, marketplaceRoot string) (bool, error) {
	if err := validateRegularFileOrMissing(path); err != nil {
		return false, err
	}
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(string(body)) == "" {
		return true, os.Remove(path)
	}
	config, err := parseTOML(string(body))
	if err != nil {
		return false, fmt.Errorf("parse %s: %w", path, err)
	}
	changed := false
	targets := map[string]bool{}
	if plugins, ok := config["plugins"].(map[string]any); ok {
		if _, ok := plugins["specgate@personal"]; ok {
			targets[`plugins.specgate@personal`] = true
			changed = true
		}
	}
	cleanRoot := filepath.Clean(strings.TrimSpace(marketplaceRoot))
	if removePersonalMarketplace && cleanRoot != "" && cleanRoot != "." {
		if marketplaces, ok := config["marketplaces"].(map[string]any); ok {
			if personal, ok := marketplaces["personal"].(map[string]any); ok {
				sourceType, _ := personal["source_type"].(string)
				source, _ := personal["source"].(string)
				if sourceType == "local" && filepath.Clean(source) == cleanRoot {
					targets["marketplaces.personal"] = true
					changed = true
				}
			}
		}
	}
	if !changed {
		return false, nil
	}
	out := string(body)
	for _, target := range []string{`plugins.specgate@personal`, "marketplaces.personal"} {
		if !targets[target] {
			continue
		}
		var removed bool
		out, removed = removeTOMLSections(out, map[string]bool{target: true})
		if !removed {
			return false, fmt.Errorf("refusing to rewrite non-section TOML for [%s]", target)
		}
	}
	if strings.TrimSpace(out) == "" {
		return true, os.Remove(path)
	}
	return true, atomicReplaceFile(path, []byte(out), 0o644)
}

func detectInstalledPluginAgents(root string) []string {
	checks := []struct {
		agent string
		path  string
	}{
		{"cursor", filepath.Join(root, ".cursor", "rules", "using-specgate.mdc")},
		{"codex", filepath.Join(root, ".codex", "plugins", specgatePluginName, ".codex-plugin", "plugin.json")},
		{"claude", filepath.Join(root, ".claude", "skills", specgatePluginName, ".claude-plugin", "plugin.json")},
	}
	var agents []string
	for _, check := range checks {
		if info, err := os.Stat(check.path); err == nil && info.Mode().IsRegular() {
			agents = append(agents, check.agent)
		}
	}
	return agents
}

func removeCodexMarketplaceEntry(path string) (bool, error) {
	if err := validateRegularFileOrMissing(path); err != nil {
		return false, err
	}
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var data map[string]json.RawMessage
	if err := json.Unmarshal(body, &data); err != nil {
		return false, fmt.Errorf("parse %s: %w", path, err)
	}
	pluginsRaw, ok := data["plugins"]
	if !ok {
		return false, nil
	}
	var plugins []map[string]any
	if err := json.Unmarshal(pluginsRaw, &plugins); err != nil {
		return false, fmt.Errorf("parse %s plugins: %w", path, err)
	}
	if len(plugins) == 0 {
		return false, nil
	}
	filtered := plugins[:0]
	changed := false
	for _, plugin := range plugins {
		if name, _ := plugin["name"].(string); name == specgatePluginName && codexPluginHasLocalSource(plugin, installedCodexPluginSource) {
			changed = true
			continue
		}
		filtered = append(filtered, plugin)
	}
	if !changed {
		return false, nil
	}
	if len(filtered) == 0 && codexMarketplaceHasOnlyManagedFields(data) {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return false, err
		}
		return true, nil
	}
	pluginsBody, err := json.Marshal(filtered)
	if err != nil {
		return false, err
	}
	data["plugins"] = pluginsBody
	out, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return false, err
	}
	return true, atomicReplaceFile(path, append(out, '\n'), 0o644)
}

func codexMarketplaceHasOnlyManagedFields(data map[string]json.RawMessage) bool {
	for key := range data {
		if key != "name" && key != "interface" && key != "plugins" {
			return false
		}
	}
	return true
}

func atomicReplaceFile(path string, body []byte, fallbackMode os.FileMode) error {
	mode := fallbackMode
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return err
	}
	return fsutil.AtomicWriteFile(path, body, mode)
}

func removeDirIfEmpty(path string) (bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return false, nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return false, err
	}
	if len(entries) > 0 {
		return false, nil
	}
	if err := os.Remove(path); err != nil {
		return false, err
	}
	return true, nil
}

func focusedPluginSkills() []string {
	return []string{
		"specgate",
		"specgate-project-setup",
		"specgate-work-preparation",
		"specgate-work-delivery",
	}
}
