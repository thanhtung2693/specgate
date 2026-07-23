package command

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/specgate/specgate/app/cli/internal/fsutil"
)

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
