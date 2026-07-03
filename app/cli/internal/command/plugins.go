package command

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/interactive"
	"github.com/specgate/specgate/app/cli/internal/output"
)

const (
	specgatePluginName     = "specgate"
	codexConfigSection     = `[plugins."specgate@personal"]`
	pluginPathPlaceholder  = "__SPECGATE_PLUGIN_PATH__"
	codexPersonalMarketURL = ".agents/plugins/personal-marketplace.json"
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
	RemovedCount int      `json:"removed_count"`
	DryRun       bool     `json:"dry_run"`
}

type pluginDoctorResult struct {
	Scope         string              `json:"scope"`
	LatestVersion string              `json:"latest_version,omitempty"`
	Warnings      []string            `json:"warnings,omitempty"`
	Agents        []pluginAgentHealth `json:"agents"`
}

type pluginAgentHealth struct {
	Agent            string   `json:"agent"`
	OK               bool     `json:"ok"`
	InstalledVersion string   `json:"installed_version,omitempty"`
	LatestVersion    string   `json:"latest_version,omitempty"`
	NeedsUpdate      bool     `json:"needs_update,omitempty"`
	Missing          []string `json:"missing,omitempty"`
	Warnings         []string `json:"warnings,omitempty"`
}

type pluginInstaller struct {
	ctx    context.Context
	deps   *Deps
	client pluginPackageClient
	pkg    *client.PluginPackage
	opts   pluginInstallOptions
	home   string

	written int
	removed int
}

func registerPluginCommands(root *cobra.Command, deps *Deps) {
	cmd := &cobra.Command{
		Use:     "plugins",
		Aliases: []string{"plugin"},
		Short:   "Install and inspect SpecGate IDE plugins",
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
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := resolvePluginAgentPrompt(cmd, deps, &opts.Agent, "Select IDE plugins", "agent"); err != nil {
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
			fmt.Fprintf(deps.Stdout, "\nSpecGate agent setup installed for: %s\n", strings.Join(result.Agents, ", "))
			if opts.ProjectLocal {
				fmt.Fprintln(deps.Stdout, "Scope: project-local files in the current repository.")
			} else {
				fmt.Fprintln(deps.Stdout, "Scope: global user files under your home directory.")
			}
			fmt.Fprintln(deps.Stdout, "Restart the selected IDE(s) so the new SpecGate plugin, Skills, hooks, and rules are loaded.")
			fmt.Fprintln(deps.Stdout, "Run 'specgate plugins doctor' to verify IDE files.")
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&opts.Agent, "agent", "", "IDE target: cursor, codex, claude, all, or comma-separated subset (prompts interactively if omitted)")
	f.StringVar(&opts.Registry, "registry", "", "Plugin registry base URL (default: configured SpecGate server)")
	f.BoolVar(&opts.ProjectLocal, "project-local", false, "Install IDE files into the current repository instead of user-global locations")
	f.BoolVar(&opts.DryRun, "dry-run", false, "Print planned file operations without writing")
	return cmd
}

func runPluginInstall(ctx context.Context, deps *Deps, opts pluginInstallOptions) (pluginInstallResult, error) {
	agents, err := normalizePluginAgents(opts.Agent)
	if err != nil {
		return pluginInstallResult{}, pluginInstallError{kind: "validation_failed", err: err}
	}
	pc := pluginClientFor(deps, opts.Registry)
	pkg, err := pc.PluginPackage(ctx)
	if err != nil {
		return pluginInstallResult{}, err
	}
	home, err := os.UserHomeDir()
	if err != nil && !opts.ProjectLocal {
		return pluginInstallResult{}, err
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
		RemovedCount: installer.removed,
		DryRun:       opts.DryRun,
	}, nil
}

type pluginInstallError struct {
	kind string
	err  error
}

func (e pluginInstallError) Error() string { return e.err.Error() }
func (e pluginInstallError) Unwrap() error { return e.err }

func pluginInstallErrorPayload(err error) output.ErrorPayload {
	var installErr pluginInstallError
	if errors.As(err, &installErr) {
		return output.ErrorPayload{Code: installErr.kind, Message: installErr.Error()}
	}
	return mapAPIError("plugins.install", err)
}

func newPluginsDoctorCmd(deps *Deps) *cobra.Command {
	var opts pluginDoctorOptions
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check installed SpecGate IDE plugin files",
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
			home, err := os.UserHomeDir()
			if err != nil && !opts.ProjectLocal {
				payload := output.ErrorPayload{Code: "unavailable", Message: err.Error()}
				code := deps.Printer.Error("plugins.doctor", payload)
				return &output.ExitError{Code: code, Err: err}
			}
			pkg, pkgErr := pluginClientFor(deps, opts.Registry).PluginPackage(cmd.Context())
			result := pluginDoctorResult{Scope: pluginScope(opts.ProjectLocal)}
			if pkg != nil {
				result.LatestVersion = pkg.Version
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
				fmt.Fprintf(deps.Stdout, "WARN %s\n", warning)
			}
			for _, health := range result.Agents {
				if health.OK {
					if len(health.Warnings) == 0 {
						fmt.Fprintf(deps.Stdout, "OK   %s\n", health.Agent)
						continue
					}
					fmt.Fprintf(deps.Stdout, "WARN %s\n", health.Agent)
				} else {
					fmt.Fprintf(deps.Stdout, "MISS %s\n", health.Agent)
					for _, path := range health.Missing {
						fmt.Fprintf(deps.Stdout, "  - %s\n", path)
					}
				}
				for _, warning := range health.Warnings {
					fmt.Fprintf(deps.Stdout, "  ! %s\n", warning)
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
	f.StringVar(&opts.Registry, "registry", "", "Plugin registry base URL (default: configured SpecGate server)")
	f.BoolVar(&opts.ProjectLocal, "project-local", false, "Inspect project-local IDE files in the current repository")
	return cmd
}

func resolvePluginAgentPrompt(cmd *cobra.Command, deps *Deps, agent *string, title, flagName string) error {
	if deps.NoInput || cmd.Flags().Changed(flagName) {
		return nil
	}
	values, err := deps.Prompter.MultiSelect(title, pluginAgentPromptOptions(), []string{"cursor", "codex", "claude"})
	if err != nil {
		return err
	}
	if len(values) == 0 {
		return fmt.Errorf("select at least one IDE plugin")
	}
	*agent = strings.Join(values, ",")
	return nil
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

func pluginScope(projectLocal bool) string {
	if projectLocal {
		return "project"
	}
	return "global"
}

func (i *pluginInstaller) install(agents []string) error {
	if i.opts.ProjectLocal {
		i.removeLegacyProjectLocal()
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

func (i *pluginInstaller) installCursor() error {
	rule := filepath.Join(i.home, ".cursor", "rules", "using-specgate.mdc")
	skillsDir := filepath.Join(i.home, ".cursor", "skills")
	if i.opts.ProjectLocal {
		rule = filepath.Join(".cursor", "rules", "using-specgate.mdc")
		skillsDir = filepath.Join(".cursor", "skills")
	}
	if err := i.writePluginFile("rules/using-specgate.mdc", rule, 0o644); err != nil {
		return err
	}
	i.removeSkillDirs(skillsDir, i.pkg.RetiredSkills)
	return i.installFocusedSkills(skillsDir)
}

func (i *pluginInstaller) installCodex() error {
	codexSkills := filepath.Join(i.home, ".codex", "skills")
	pluginRoot := filepath.Join(i.home, ".codex", "plugins", specgatePluginName)
	marketplace := filepath.Join(i.home, ".agents", "plugins", "marketplace.json")
	marketplacePluginPath := "./.codex/plugins/specgate"
	configPath := filepath.Join(i.home, ".codex", "config.toml")
	if i.opts.ProjectLocal {
		codexSkills = filepath.Join(".codex", "skills")
		pluginRoot = filepath.Join(".codex", "plugins", specgatePluginName)
		marketplace = filepath.Join(".agents", "plugins", "marketplace.json")
		marketplacePluginPath = "./.codex/plugins/specgate"
		configPath = filepath.Join(".codex", "config.toml")
	}
	i.removeSkillDirs(codexSkills, append([]string{specgatePluginName}, i.pkg.RetiredSkills...))
	i.removeSkillDirs(filepath.Join(pluginRoot, "skills"), i.pkg.RetiredSkills)
	for _, item := range []struct {
		src  string
		dest string
		mode os.FileMode
	}{
		{".codex-plugin/plugin.json", filepath.Join(pluginRoot, ".codex-plugin", "plugin.json"), 0o644},
		{"assets/logo.svg", filepath.Join(pluginRoot, "assets", "logo.svg"), 0o644},
		{"hooks/hooks.json", filepath.Join(pluginRoot, "hooks", "hooks.json"), 0o644},
		{"hooks/run-hook.cmd", filepath.Join(pluginRoot, "hooks", "run-hook.cmd"), 0o755},
		{"hooks/session-start", filepath.Join(pluginRoot, "hooks", "session-start"), 0o755},
	} {
		if err := i.writePluginFile(item.src, item.dest, item.mode); err != nil {
			return err
		}
	}
	if err := i.installFocusedSkills(filepath.Join(pluginRoot, "skills")); err != nil {
		return err
	}
	if err := i.mergeCodexMarketplace(marketplace, marketplacePluginPath); err != nil {
		return err
	}
	return i.enableCodexConfig(configPath)
}

func (i *pluginInstaller) installClaude() error {
	pluginRoot := filepath.Join(i.home, ".claude", "skills", specgatePluginName)
	claudeSkills := filepath.Join(i.home, ".claude", "skills")
	if i.opts.ProjectLocal {
		pluginRoot = filepath.Join(".claude", "skills", specgatePluginName)
		claudeSkills = filepath.Join(".claude", "skills")
	}
	i.removeSkillDirs(claudeSkills, append([]string{specgatePluginName}, i.pkg.RetiredSkills...))
	i.removeSkillDirs(filepath.Join(pluginRoot, "skills"), i.pkg.RetiredSkills)
	for _, item := range []struct {
		src  string
		dest string
		mode os.FileMode
	}{
		{".claude-plugin/plugin.json", filepath.Join(pluginRoot, ".claude-plugin", "plugin.json"), 0o644},
		{"assets/logo.svg", filepath.Join(pluginRoot, "assets", "logo.svg"), 0o644},
		{"hooks/hooks-claude.json", filepath.Join(pluginRoot, "hooks", "hooks-claude.json"), 0o644},
		{"hooks/run-hook.cmd", filepath.Join(pluginRoot, "hooks", "run-hook.cmd"), 0o755},
		{"hooks/session-start", filepath.Join(pluginRoot, "hooks", "session-start"), 0o755},
	} {
		if err := i.writePluginFile(item.src, item.dest, item.mode); err != nil {
			return err
		}
	}
	return i.installFocusedSkills(filepath.Join(pluginRoot, "skills"))
}

func (i *pluginInstaller) installFocusedSkills(destDir string) error {
	for _, skill := range i.pkg.Skills {
		if err := i.writePluginFile("skills/"+skill+"/SKILL.md", filepath.Join(destDir, skill, "SKILL.md"), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func (i *pluginInstaller) removeLegacyProjectLocal() {
	for _, path := range []string{"AGENTS.specgate.md", "CLAUDE.specgate.md", filepath.Join(".claude", "specgate")} {
		i.removePath(path)
	}
	i.removeSkillDirs(filepath.Join(".codex", "skills"), append([]string{specgatePluginName}, i.pkg.RetiredSkills...))
	i.removeSkillDirs(filepath.Join(".claude", "skills"), append([]string{specgatePluginName}, i.pkg.RetiredSkills...))
}

func (i *pluginInstaller) removeSkillDirs(base string, skills []string) {
	for _, skill := range skills {
		i.removePath(filepath.Join(base, skill))
	}
}

func (i *pluginInstaller) removePath(path string) {
	if i.opts.DryRun {
		i.printf("[dry-run] remove %s if present\n", path)
		return
	}
	if _, err := os.Stat(path); err == nil {
		_ = os.RemoveAll(path)
		i.removed++
		i.printf("removed %s\n", path)
	}
}

func (i *pluginInstaller) writePluginFile(src, dest string, mode os.FileMode) error {
	body, err := i.client.PluginFile(i.ctx, src)
	if err != nil {
		return err
	}
	return i.writeFile(dest, body, mode)
}

func (i *pluginInstaller) writeFile(dest string, body []byte, mode os.FileMode) error {
	if i.opts.DryRun {
		i.printf("[dry-run] write %s\n", dest)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".specgate-write-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		return err
	}
	if err := os.Rename(tmpName, dest); err != nil {
		return err
	}
	i.written++
	i.printf("wrote %s\n", dest)
	return nil
}

func (i *pluginInstaller) mergeCodexMarketplace(path, pluginPath string) error {
	templateBytes, err := i.client.PluginFile(i.ctx, codexPersonalMarketURL)
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

func (i *pluginInstaller) enableCodexConfig(path string) error {
	if i.opts.DryRun {
		i.printf("[dry-run] enable specgate@personal in %s\n", path)
		return nil
	}
	var text string
	if body, err := os.ReadFile(path); err == nil {
		text = string(body)
	}
	text = ensureCodexPluginEnabled(text)
	return i.writeFile(path, []byte(text), 0o644)
}

func (i *pluginInstaller) printf(format string, args ...any) {
	if i.deps.Printer != nil && i.deps.Printer.Mode() == output.ModeJSON {
		return
	}
	fmt.Fprintf(i.deps.Stdout, format, args...)
}

func ensureCodexPluginEnabled(text string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	var out []string
	inSection := false
	sawSection := false
	sawEnabled := false
	changed := strings.TrimSpace(text) == ""
	for _, line := range lines {
		stripped := strings.TrimSpace(line)
		if inSection && strings.HasPrefix(stripped, "[") && strings.HasSuffix(stripped, "]") {
			if !sawEnabled {
				out = append(out, "enabled = true")
				changed = true
			}
			inSection = false
		}
		if stripped == codexConfigSection {
			sawSection = true
			inSection = true
			sawEnabled = false
			out = append(out, line)
			continue
		}
		if inSection && strings.HasPrefix(stripped, "enabled") {
			if stripped != "enabled = true" {
				changed = true
			}
			out = append(out, "enabled = true")
			sawEnabled = true
			continue
		}
		out = append(out, line)
	}
	if inSection && !sawEnabled {
		out = append(out, "enabled = true")
		changed = true
	}
	if !sawSection {
		if len(out) > 0 && strings.TrimSpace(strings.Join(out, "\n")) != "" {
			out = append(out, "")
		}
		out = append(out, codexConfigSection, "enabled = true")
		changed = true
	}
	result := strings.TrimRight(strings.Join(out, "\n"), "\n") + "\n"
	if !changed && result == "" {
		return codexConfigSection + "\nenabled = true\n"
	}
	return result
}

func checkPluginAgent(agent, home string, projectLocal bool, pkg *client.PluginPackage) pluginAgentHealth {
	health := pluginAgentHealth{Agent: agent, OK: true}
	skills := pluginSkillsFromPackage(pkg)
	latestVersion := ""
	if pkg != nil {
		latestVersion = strings.TrimSpace(pkg.Version)
		health.LatestVersion = latestVersion
	}
	var required []string
	var pluginManifest string
	switch agent {
	case "cursor":
		root := filepath.Join(home, ".cursor")
		if projectLocal {
			root = ".cursor"
		}
		required = append(required, filepath.Join(root, "rules", "using-specgate.mdc"))
		for _, skill := range skills {
			required = append(required, filepath.Join(root, "skills", skill, "SKILL.md"))
		}
	case "codex":
		pluginRoot := filepath.Join(home, ".codex", "plugins", specgatePluginName)
		configPath := filepath.Join(home, ".codex", "config.toml")
		marketplace := filepath.Join(home, ".agents", "plugins", "marketplace.json")
		if projectLocal {
			pluginRoot = filepath.Join(".codex", "plugins", specgatePluginName)
			configPath = filepath.Join(".codex", "config.toml")
			marketplace = filepath.Join(".agents", "plugins", "marketplace.json")
		}
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
		for _, skill := range skills {
			required = append(required, filepath.Join(pluginRoot, "skills", skill, "SKILL.md"))
		}
	case "claude":
		pluginRoot := filepath.Join(home, ".claude", "skills", specgatePluginName)
		if projectLocal {
			pluginRoot = filepath.Join(".claude", "skills", specgatePluginName)
		}
		pluginManifest = filepath.Join(pluginRoot, ".claude-plugin", "plugin.json")
		required = append(required,
			pluginManifest,
			filepath.Join(pluginRoot, "assets", "logo.svg"),
			filepath.Join(pluginRoot, "hooks", "hooks-claude.json"),
			filepath.Join(pluginRoot, "hooks", "run-hook.cmd"),
			filepath.Join(pluginRoot, "hooks", "session-start"),
		)
		for _, skill := range skills {
			required = append(required, filepath.Join(pluginRoot, "skills", skill, "SKILL.md"))
		}
	}
	for _, path := range required {
		if _, err := os.Stat(path); err != nil {
			health.Missing = append(health.Missing, path)
		}
	}
	if agent == "codex" {
		configPath := filepath.Join(home, ".codex", "config.toml")
		marketplace := filepath.Join(home, ".agents", "plugins", "marketplace.json")
		pluginRoot := filepath.Join(home, ".codex", "plugins", specgatePluginName)
		if projectLocal {
			configPath = filepath.Join(".codex", "config.toml")
			marketplace = filepath.Join(".agents", "plugins", "marketplace.json")
			pluginRoot = filepath.Join(".codex", "plugins", specgatePluginName)
		}
		if body, err := os.ReadFile(configPath); err == nil && !codexConfigEnablesSpecGate(string(body)) {
			health.Missing = append(health.Missing, configPath+" enabled specgate@personal")
		}
		if body, err := os.ReadFile(marketplace); err == nil && !bytes.Contains(body, []byte(`"name": "specgate"`)) {
			health.Missing = append(health.Missing, marketplace+" specgate entry")
		}
		if !projectLocal && latestVersion != "" {
			health.Warnings = append(health.Warnings, codexCacheWarnings(home, pluginRoot, latestVersion, skills)...)
		}
	}
	if pluginManifest != "" && latestVersion != "" {
		installedVersion := readPluginManifestVersion(pluginManifest)
		health.InstalledVersion = installedVersion
		if installedVersion != "" && installedVersion != latestVersion {
			health.NeedsUpdate = true
			health.Warnings = append(health.Warnings, fmt.Sprintf("Installed plugin version %s is older than latest %s; run 'specgate plugins install --agent %s'.", installedVersion, latestVersion, agent))
		}
	}
	if len(health.Warnings) > 0 {
		health.NeedsUpdate = true
	}
	sort.Strings(health.Missing)
	sort.Strings(health.Warnings)
	health.OK = len(health.Missing) == 0
	return health
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
	inSection := false
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		stripped := strings.TrimSpace(line)
		if stripped == codexConfigSection {
			inSection = true
			continue
		}
		if inSection && strings.HasPrefix(stripped, "[") && strings.HasSuffix(stripped, "]") {
			return false
		}
		if inSection && stripped == "enabled = true" {
			return true
		}
	}
	return false
}

func removeSpecGatePluginFiles() (int, []string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return 0, nil, err
	}
	var removed int
	var paths []string
	remove := func(path string) error {
		ok, err := removePathIfExists(path)
		if err != nil {
			return err
		}
		if ok {
			removed++
			paths = append(paths, path)
		}
		return nil
	}
	for _, path := range []string{
		filepath.Join(home, ".cursor", "rules", "using-specgate.mdc"),
		filepath.Join(home, ".codex", "skills", specgatePluginName),
		filepath.Join(home, ".codex", "plugins", specgatePluginName),
		filepath.Join(home, ".codex", "plugins", "cache", "personal", specgatePluginName),
		filepath.Join(home, ".claude", "skills", specgatePluginName),
	} {
		if err := remove(path); err != nil {
			return removed, paths, err
		}
	}
	for _, skill := range focusedPluginSkills() {
		if err := remove(filepath.Join(home, ".cursor", "skills", skill)); err != nil {
			return removed, paths, err
		}
		if err := remove(filepath.Join(home, ".codex", "skills", skill)); err != nil {
			return removed, paths, err
		}
	}
	if changed, err := removeCodexConfigSection(filepath.Join(home, ".codex", "config.toml")); err != nil {
		return removed, paths, err
	} else if changed {
		removed++
		paths = append(paths, filepath.Join(home, ".codex", "config.toml"))
	}
	if changed, err := removeCodexMarketplaceEntry(filepath.Join(home, ".agents", "plugins", "marketplace.json")); err != nil {
		return removed, paths, err
	} else if changed {
		removed++
		paths = append(paths, filepath.Join(home, ".agents", "plugins", "marketplace.json"))
	}
	return removed, paths, nil
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

func removeCodexConfigSection(path string) (bool, error) {
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	lines := strings.Split(strings.ReplaceAll(string(body), "\r\n", "\n"), "\n")
	var out []string
	inSection := false
	changed := false
	for _, line := range lines {
		stripped := strings.TrimSpace(line)
		if stripped == codexConfigSection {
			inSection = true
			changed = true
			continue
		}
		if inSection && strings.HasPrefix(stripped, "[") && strings.HasSuffix(stripped, "]") {
			inSection = false
		}
		if inSection {
			continue
		}
		out = append(out, line)
	}
	if !changed {
		return false, nil
	}
	text := strings.TrimRight(strings.Join(out, "\n"), "\n")
	if text != "" {
		text += "\n"
	}
	return true, os.WriteFile(path, []byte(text), 0o644)
}

func removeCodexMarketplaceEntry(path string) (bool, error) {
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var data struct {
		Name      string           `json:"name"`
		Interface map[string]any   `json:"interface,omitempty"`
		Plugins   []map[string]any `json:"plugins"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return false, fmt.Errorf("parse %s: %w", path, err)
	}
	filtered := data.Plugins[:0]
	changed := false
	for _, plugin := range data.Plugins {
		if name, _ := plugin["name"].(string); name == specgatePluginName {
			changed = true
			continue
		}
		filtered = append(filtered, plugin)
	}
	if !changed {
		return false, nil
	}
	data.Plugins = filtered
	out, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return false, err
	}
	return true, os.WriteFile(path, append(out, '\n'), 0o644)
}

func focusedPluginSkills() []string {
	return []string{
		"using-specgate",
		"setting-up-specgate-project",
		"checking-spec-readiness",
		"shaping-work",
		"picking-up-work",
		"implementing-work",
		"completing-delivery",
	}
}
