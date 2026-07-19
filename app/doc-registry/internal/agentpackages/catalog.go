package agentpackages

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"
)

//go:embed plugins plugins/.agents/plugins/marketplace.json plugins/.agents/plugins/personal-marketplace.json plugins/.codex-plugin/plugin.json plugins/.claude-plugin/plugin.json plugins/.claude-plugin/marketplace.json plugins/.cursor-plugin/plugin.json plugins/.cursor-plugin/marketplace.json
var pluginsFS embed.FS

// PluginPackage is the manifest-backed inventory of files this server may
// publish under /plugins.
type PluginPackage struct {
	Schema      string   `json:"schema"`
	Name        string   `json:"name"`
	DisplayName string   `json:"display_name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	License     string   `json:"license"`
	Keywords    []string `json:"keywords"`
	Skills      []string `json:"skills"`
	ServedFiles []string `json:"served_files"`
}

// Options controls dynamic values substituted into plugin template files.
type Options struct {
	RegistryBaseURL string
}

// templateData is the context passed to text/template when rendering .tmpl files.
type templateData struct {
	RegistryBaseURL string // base URL of the doc-registry (no trailing slash)
}

// PackageInventory returns the canonical plugin package inventory.
func PackageInventory() (*PluginPackage, error) {
	raw, err := pluginsFS.ReadFile("plugins/package.json")
	if err != nil {
		return nil, fmt.Errorf("read plugin package inventory: %w", err)
	}
	var pkg PluginPackage
	if err := json.Unmarshal(raw, &pkg); err != nil {
		return nil, fmt.Errorf("parse plugin package inventory: %w", err)
	}
	return &pkg, nil
}

// IsServedFile reports whether path is declared in the package inventory.
func IsServedFile(path string) (bool, error) {
	path = strings.TrimPrefix(strings.TrimSpace(path), "/")
	pkg, err := PackageInventory()
	if err != nil {
		return false, err
	}
	for _, served := range pkg.ServedFiles {
		if path == served {
			return true, nil
		}
	}
	return false, nil
}

// Render returns the content of the plugin file at path, with template
// substitution applied when a .tmpl variant exists in the embedded plugins tree.
// Returns an error for paths with no matching file.
func Render(path string, opts Options) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(opts.RegistryBaseURL), "/")

	// Try <path>.tmpl first (needs dynamic substitution), fall back to static <path>.
	raw, err := pluginsFS.ReadFile("plugins/" + path + ".tmpl")
	if err != nil {
		raw, err = pluginsFS.ReadFile("plugins/" + path)
		if err != nil {
			return "", fmt.Errorf("unknown agent package path %q", path)
		}
		return string(raw), nil
	}

	data := templateData{
		RegistryBaseURL: base,
	}
	tmpl, err := template.New(path).Parse(string(raw))
	if err != nil {
		return "", fmt.Errorf("template parse %q: %w", path, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("template render %q: %w", path, err)
	}
	return buf.String(), nil
}
