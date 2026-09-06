package command

import (
	"path/filepath"
	"strings"

	"github.com/specgate/specgate/app/cli/internal/client"
)

// pluginAgentAdapter is the single registration point for IDE-specific plugin
// behavior. Platform file formats stay in focused helpers; command routing,
// prompts, install, health, and removal all derive from this registry.
type pluginAgentAdapter struct {
	name          string
	label         string
	skillsSHDir   string
	available     func() bool
	preload       func(*pluginInstaller, map[string]bool)
	validate      func(*pluginInstaller, string) error
	projectDirs   func(string) []string
	install       func(*pluginInstaller) error
	inspectNative func(string) nativePluginInspection
	nativeRemoval func(string) string
	health        func(string, bool, *client.PluginPackage, ...string) pluginAgentHealth
	installed     func(string) bool
	remove        func(*pluginRemover) error
}

var pluginAgentAdapters = []pluginAgentAdapter{
	{
		name:        "cursor",
		label:       "Cursor",
		skillsSHDir: filepath.Join(".agents", "skills"),
		available:   cursorPluginAgentAvailable,
		preload:     preloadCursorPluginFiles,
		validate:    validateCursorPluginInstall,
		projectDirs: cursorProjectPluginDirs,
		install:     (*pluginInstaller).installCursor,
		health:      checkCursorPluginAgent,
		installed:   cursorPluginInstalled,
		remove:      (*pluginRemover).removeCursor,
	},
	{
		name:          "codex",
		label:         "Codex",
		skillsSHDir:   filepath.Join(".agents", "skills"),
		available:     codexPluginAgentAvailable,
		preload:       preloadCodexPluginFiles,
		validate:      validateCodexPluginInstall,
		projectDirs:   codexProjectPluginDirs,
		install:       (*pluginInstaller).installCodex,
		inspectNative: inspectNativeCodexPlugin,
		nativeRemoval: codexNativePluginRemovalAction,
		health:        checkCodexPluginAgent,
		installed:     codexPluginInstalled,
		remove:        (*pluginRemover).removeCodex,
	},
	{
		name:          "claude",
		label:         "Claude Code",
		skillsSHDir:   filepath.Join(".claude", "skills"),
		available:     claudePluginAgentAvailable,
		preload:       preloadClaudePluginFiles,
		validate:      validateClaudePluginInstall,
		projectDirs:   claudeProjectPluginDirs,
		install:       (*pluginInstaller).installClaude,
		inspectNative: inspectNativeClaudePlugin,
		nativeRemoval: claudeNativePluginRemovalAction,
		health:        checkClaudePluginAgent,
		installed:     claudePluginInstalled,
		remove:        (*pluginRemover).removeClaude,
	},
}

func pluginAgentAdapterFor(name string) (*pluginAgentAdapter, bool) {
	for index := range pluginAgentAdapters {
		if pluginAgentAdapters[index].name == name {
			return &pluginAgentAdapters[index], true
		}
	}
	return nil, false
}

func pluginAgentNames() []string {
	names := make([]string, 0, len(pluginAgentAdapters))
	for _, adapter := range pluginAgentAdapters {
		names = append(names, adapter.name)
	}
	return names
}

func supportedPluginAgentList() string {
	return strings.Join(pluginAgentNames(), ", ") + ", all, or comma-separated subset"
}

func cursorProjectPluginDirs(root string) []string {
	return []string{
		filepath.Join(root, ".cursor", "rules"),
		filepath.Join(root, ".cursor", "skills"),
	}
}

func codexProjectPluginDirs(root string) []string {
	return []string{filepath.Join(root, ".agents", "skills")}
}

func claudeProjectPluginDirs(root string) []string {
	return []string{
		filepath.Join(root, ".claude", "skills"),
		filepath.Join(root, ".claude", specgateHookDirName),
	}
}
