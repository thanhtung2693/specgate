package clipackages

import (
	"bytes"
	_ "embed"
	"fmt"
	"strings"
	"text/template"
)

//go:embed install.sh.tmpl
var installShTmpl string

// templateData is the context passed to the install.sh template.
type templateData struct {
	ServerURL string // base URL of the SpecGate instance (no trailing slash)
	Version   string // default CLI version, e.g. "v1.2.3" or "" (empty = resolve latest)
}

// RenderInstallScript renders the instance-aware installer wrapper for the
// given serverURL and CLI version. The generated script downloads the public
// installer and configures it to point at serverURL.
//
// When version is "dev" or empty the script resolves the latest published
// release from GitHub rather than trying to download a non-existent tag.
func RenderInstallScript(serverURL, version string) (string, error) {
	serverURL = strings.TrimRight(strings.TrimSpace(serverURL), "/")
	version = strings.TrimSpace(version)

	if version == "dev" {
		version = ""
	}

	tmpl, err := template.New("install.sh").Parse(installShTmpl)
	if err != nil {
		return "", fmt.Errorf("template parse: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, templateData{
		ServerURL: serverURL,
		Version:   version,
	}); err != nil {
		return "", fmt.Errorf("template render: %w", err)
	}
	return buf.String(), nil
}
