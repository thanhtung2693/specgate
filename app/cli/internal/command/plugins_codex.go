package command

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/output"
)

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

func validateCodexMarketplaceOwnership(path string) error {
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
		if codexPluginHasLocalSource(plugin, installedCodexPluginSource) {
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

func (i *pluginInstaller) mergeCodexMarketplace(path string) error {
	templateBytes, err := i.pluginFile(codexPersonalMarketURL)
	if err != nil {
		return err
	}
	templateBytes = bytes.ReplaceAll(templateBytes, []byte(pluginPathPlaceholder), []byte(installedCodexPluginSource))
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
	message := fmt.Sprintf(format, args...)
	if i.opts.DryRun {
		if operation, ok := strings.CutPrefix(strings.TrimSpace(message), "[dry-run] "); ok {
			i.planned = append(i.planned, operation)
		}
	}
	if i.deps.Printer != nil && i.deps.Printer.Mode() == output.ModeJSON {
		return
	}
	fmt.Fprint(i.deps.Stdout, message)
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
			if !codexMarketplaceHasPlugin(body) {
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

func codexMarketplaceHasPlugin(body []byte) bool {
	var data struct {
		Plugins []map[string]any `json:"plugins"`
	}
	if json.Unmarshal(body, &data) != nil {
		return false
	}
	for _, plugin := range data.Plugins {
		pluginName, _ := plugin["name"].(string)
		if pluginName == specgatePluginName && codexPluginHasLocalSource(plugin, installedCodexPluginSource) {
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
