package command

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const maxNativePluginMetadataBytes = 4 << 20

type nativePluginInstall struct {
	Agent       string `json:"agent"`
	Marketplace string `json:"marketplace"`
}

type nativePluginInspection struct {
	Installations []nativePluginInstall
	Problems      []string
}

func inspectNativePlugin(agent, home string) nativePluginInspection {
	var inspection nativePluginInspection
	switch agent {
	case "codex":
		inspection = inspectNativeCodexPlugin(home)
	case "claude":
		inspection = inspectNativeClaudePlugin(home)
	}
	sort.Slice(inspection.Installations, func(i, j int) bool {
		return inspection.Installations[i].Marketplace < inspection.Installations[j].Marketplace
	})
	if len(inspection.Installations) > 1 {
		inspection.Problems = append(inspection.Problems, fmt.Sprintf("multiple %s SpecGate marketplace installs are configured", agent))
	}
	return inspection
}

func inspectNativeCodexPlugin(home string) nativePluginInspection {
	path := filepath.Join(home, ".codex", "config.toml")
	body, exists, err := readNativePluginMetadata(path)
	if err != nil {
		return nativePluginInspection{Problems: []string{err.Error()}}
	}
	if !exists {
		return nativePluginInspection{}
	}
	config, err := parseTOML(string(body))
	if err != nil {
		return nativePluginInspection{Problems: []string{fmt.Sprintf("parse native Codex plugin configuration %s: %v", path, err)}}
	}
	pluginConfig, exists := config["plugins"]
	if !exists {
		return nativePluginInspection{}
	}
	plugins, ok := pluginConfig.(map[string]any)
	if !ok {
		return nativePluginInspection{Problems: []string{fmt.Sprintf("native Codex plugin configuration %s has an invalid plugins table", path)}}
	}
	inspection := nativePluginInspection{}
	for key := range plugins {
		marketplace, ok := nativePluginMarketplace(key)
		if strings.HasPrefix(key, specgatePluginName+"@") && !ok {
			inspection.Problems = append(inspection.Problems, fmt.Sprintf("invalid Codex marketplace plugin %q", key))
		} else if ok && marketplace != "personal" {
			inspection.Installations = append(inspection.Installations, nativePluginInstall{Agent: "codex", Marketplace: marketplace})
		}
	}
	return inspection
}

func needsNativePluginInspection(agents []string) bool {
	for _, agent := range agents {
		if agent == "codex" || agent == "claude" {
			return true
		}
	}
	return false
}

func rejectNativePluginConflicts(agents []string, home string, projectLocal bool) error {
	var conflicts []nativePluginInstall
	var problems []string
	for _, agent := range agents {
		inspection := inspectNativePlugin(agent, home)
		conflicts = append(conflicts, inspection.Installations...)
		for _, problem := range inspection.Problems {
			problems = append(problems, agent+": "+problem)
		}
	}
	if len(conflicts) == 0 && len(problems) == 0 {
		return nil
	}
	recovery := append([]string(nil), problems...)
	for _, conflict := range conflicts {
		recovery = append(recovery, nativePluginRemovalAction(conflict.Agent, conflict.Marketplace))
	}
	retryCommand := pluginRepairCommand(strings.Join(agents, ","), projectLocal) + " --no-input"
	return pluginInstallError{
		kind: "conflict",
		err:  fmt.Errorf("native plugin manager already owns SpecGate; %s; then retry %s; no plugin files were changed", strings.Join(recovery, "; "), retryCommand),
		details: map[string]any{
			"owner":         "native",
			"conflicts":     conflicts,
			"recovery":      recovery,
			"retry_command": retryCommand,
		},
	}
}

func nativePluginRemovalAction(agent, marketplace string) string {
	if agent == "claude" {
		return fmt.Sprintf("claude plugin uninstall specgate@%s", marketplace)
	}
	return fmt.Sprintf("Open Codex and use /plugins to remove specgate@%s", marketplace)
}

func nativePluginHealth(agent, home string) (pluginAgentHealth, bool) {
	inspection := inspectNativePlugin(agent, home)
	if len(inspection.Problems) > 0 {
		return pluginAgentHealth{
			Agent:    agent,
			OK:       false,
			Owner:    "native",
			Warnings: inspection.Problems,
		}, true
	}
	if len(inspection.Installations) != 1 {
		return pluginAgentHealth{}, false
	}
	return pluginAgentHealth{
		Agent:       agent,
		OK:          true,
		Owner:       "native",
		Marketplace: inspection.Installations[0].Marketplace,
	}, true
}

func inspectNativeClaudePlugin(home string) nativePluginInspection {
	path := filepath.Join(home, ".claude", "plugins", "installed_plugins.json")
	body, exists, err := readNativePluginMetadata(path)
	if err != nil {
		return nativePluginInspection{Problems: []string{err.Error()}}
	}
	if !exists {
		return nativePluginInspection{}
	}
	var installed struct {
		Plugins map[string]json.RawMessage `json:"plugins"`
	}
	if err := json.Unmarshal(body, &installed); err != nil {
		return nativePluginInspection{Problems: []string{fmt.Sprintf("parse native Claude plugin record %s: %v", path, err)}}
	}
	inspection := nativePluginInspection{}
	for key := range installed.Plugins {
		marketplace, ok := nativePluginMarketplace(key)
		if strings.HasPrefix(key, specgatePluginName+"@") && !ok {
			inspection.Problems = append(inspection.Problems, fmt.Sprintf("invalid Claude marketplace plugin %q", key))
		} else if ok {
			inspection.Installations = append(inspection.Installations, nativePluginInstall{Agent: "claude", Marketplace: marketplace})
		}
	}
	return inspection
}

func nativePluginMarketplace(key string) (string, bool) {
	marketplace, ok := strings.CutPrefix(key, specgatePluginName+"@")
	return marketplace, ok && marketplace != "" && validPluginVersion(marketplace)
}

func readNativePluginMetadata(path string) ([]byte, bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !info.Mode().IsRegular() {
		return nil, false, fmt.Errorf("native plugin metadata %s is not a regular file", path)
	}
	if info.Size() > maxNativePluginMetadataBytes {
		return nil, false, fmt.Errorf("native plugin metadata %s exceeds 4 MiB", path)
	}
	body, err := os.ReadFile(path)
	return body, true, err
}
