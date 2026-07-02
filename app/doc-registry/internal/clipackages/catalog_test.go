package clipackages_test

import (
	"strings"
	"testing"

	"github.com/specgate/doc-registry/internal/clipackages"
)

func TestInstanceInstallerPinsServerAndNeverReadsMCPKey(t *testing.T) {
	body, err := clipackages.RenderInstallScript("https://specgate.example", "v1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`SPECGATE_VERSION="v1.2.3"`,
		`SERVER_URL="https://specgate.example"`,
		`GITHUB_REPO="thanhtung2693/specgate"`,
		`"${DEST}" config set server "${SERVER_URL}"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("installer missing %q\n\nfull output:\n%s", want, body)
		}
	}
	if strings.Contains(body, "install-cli.sh") {
		t.Fatal("instance installer must not fetch an external install-cli.sh wrapper")
	}
	if strings.Contains(body, "/mcp/api-key") || strings.Contains(body, "MCP_API_KEY") {
		t.Fatal("CLI installer must not retrieve MCP credentials")
	}
}

func TestInstanceInstallerDevVersionUsesLatestRelease(t *testing.T) {
	for _, version := range []string{"dev", ""} {
		body, err := clipackages.RenderInstallScript("http://localhost:8080", version)
		if err != nil {
			t.Fatalf("version=%q: %v", version, err)
		}
		if !strings.Contains(body, `SPECGATE_VERSION=""`) {
			t.Fatalf("version=%q: expected dev installer to resolve latest release at runtime, got:\n%s", version, body)
		}
		for _, want := range []string{
			`if [ -z "$SPECGATE_VERSION" ]; then`,
			`app/cli/go.mod`,
			`go build -o "$BINARY" ./cmd/specgate`,
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("version=%q: missing dev local-build flow %q in:\n%s", version, want, body)
			}
		}
	}
}
