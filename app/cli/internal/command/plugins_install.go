package command

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/specgate/specgate/app/cli/internal/fsutil"
)

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
			if err := validateCodexMarketplaceOwnership(filepath.Join(root, ".agents", "plugins", "marketplace.json")); err != nil {
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
		return i.installFocusedSkills(filepath.Join(root, ".agents", "skills"))
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
	if err := i.mergeCodexMarketplace(marketplace); err != nil {
		return err
	}
	return i.enableCodexConfig(configPath, marketplaceRoot)
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
