package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/specgate/doc-registry/internal/agentpackages"
)

func TestAgentPackagesInstallScript(t *testing.T) {
	t.Parallel()
	rt := &Router{Handlers: &Handlers{}, Config: testConfig()}
	srv := httptest.NewServer(DevCORS(rt.Build()))
	defer srv.Close()

	res, err := http.Get(srv.URL + "/plugins/install.sh")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if got := res.StatusCode; got != http.StatusOK {
		t.Fatalf("status=%d", got)
	}
	if got := res.Header.Get("Content-Type"); !strings.Contains(got, "text/x-shellscript") {
		t.Fatalf("content-type=%q", got)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	// Interactive picker, all/flag usage, global default install,
	// project-local opt-in, CLI install via /cli/install.sh, no MCP credential fetch.
	for _, want := range []string{
		"Select [1-4, comma-separated]:",
		"Tip: enter comma-separated values such as 1,2",
		"/dev/tty",
		"--agent cursor|codex|claude|all|cursor,codex|cursor,claude|codex,claude",
		"--project-local",
		"--skip-cli",
		"find_specgate_bin()",
		`plugins install --agent "$AGENT"`,
		`"$specgate_bin" "$@"`,
		"--dry-run",
		// CLI install — the script downloads the binary, not an MCP token.
		"/cli/install.sh",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("install script missing %q: %s", want, text)
		}
	}
	if !strings.Contains(text, `REGISTRY_URL="${REGISTRY_URL:-`+srv.URL+`}"`) {
		t.Fatalf("install script missing rendered registry url: %s", text)
	}
}

func TestAgentPackagesCursorRuleServed(t *testing.T) {
	t.Parallel()
	rt := &Router{Handlers: &Handlers{}, Config: testConfig()}
	srv := httptest.NewServer(DevCORS(rt.Build()))
	defer srv.Close()

	res, err := http.Get(srv.URL + "/plugins/rules/using-specgate.mdc")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if got := res.StatusCode; got != http.StatusOK {
		t.Fatalf("status=%d", got)
	}
}

func TestAgentPackagesCodexManifestServed(t *testing.T) {
	t.Parallel()
	rt := &Router{Handlers: &Handlers{}, Config: testConfig()}
	srv := httptest.NewServer(DevCORS(rt.Build()))
	defer srv.Close()

	res, err := http.Get(srv.URL + "/plugins/.codex-plugin/plugin.json")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if got := res.StatusCode; got != http.StatusOK {
		t.Fatalf("status=%d", got)
	}
	if got := res.Header.Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("content-type=%q", got)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{
		`"name": "specgate"`,
		`"skills": "./skills/"`,
		`"composerIcon": "./assets/logo.svg"`,
		`"logo": "./assets/logo.svg"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("codex manifest missing %q: %s", want, text)
		}
	}
	if strings.Contains(text, `"hooks"`) {
		t.Fatalf("codex manifest must not contain unsupported hooks field: %s", text)
	}
}

func TestAgentPackagesNativePluginManifestsServed(t *testing.T) {
	t.Parallel()
	rt := &Router{Handlers: &Handlers{}, Config: testConfig()}
	srv := httptest.NewServer(DevCORS(rt.Build()))
	defer srv.Close()

	for _, path := range []string{
		"/plugins/.agents/plugins/marketplace.json",
		"/plugins/.codex-plugin/plugin.json",
		"/plugins/.claude-plugin/plugin.json",
		"/plugins/.claude-plugin/marketplace.json",
		"/plugins/.cursor-plugin/plugin.json",
		"/plugins/.cursor-plugin/marketplace.json",
		"/plugins/hooks/hooks.json",
		"/plugins/hooks/hooks-claude.json",
		"/plugins/hooks/hooks-cursor.json",
	} {
		res, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if got := res.StatusCode; got != http.StatusOK {
			t.Fatalf("%s status=%d, want 200", path, got)
		}
		if got := res.Header.Get("Content-Type"); !strings.Contains(got, "application/json") {
			t.Fatalf("%s content-type=%q", path, got)
		}
	}
}

func TestAgentPackagesServesInventoryAllowlist(t *testing.T) {
	t.Parallel()
	rt := &Router{Handlers: &Handlers{}, Config: testConfig()}
	srv := httptest.NewServer(DevCORS(rt.Build()))
	defer srv.Close()

	files, err := agentpackages.ServedFiles()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		res, err := http.Get(srv.URL + "/plugins/" + path)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if got := res.StatusCode; got != http.StatusOK {
			t.Fatalf("%s status=%d, want 200", path, got)
		}
	}

	res, err := http.Get(srv.URL + "/plugins/not-in-package.txt")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if got := res.StatusCode; got != http.StatusNotFound {
		t.Fatalf("unlisted path status=%d, want 404", got)
	}
}

func TestAgentPackagesPluginLogoServed(t *testing.T) {
	t.Parallel()
	rt := &Router{Handlers: &Handlers{}, Config: testConfig()}
	srv := httptest.NewServer(DevCORS(rt.Build()))
	defer srv.Close()

	res, err := http.Get(srv.URL + "/plugins/assets/logo.svg")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if got := res.StatusCode; got != http.StatusOK {
		t.Fatalf("status=%d", got)
	}
	if got := res.Header.Get("Content-Type"); !strings.Contains(got, "image/svg+xml") {
		t.Fatalf("content-type=%q", got)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "<svg") || !strings.Contains(string(body), "linearGradient") {
		t.Fatalf("logo does not look like landing SVG: %s", string(body))
	}
}

func TestAgentPackagesFallbackRuleFilesRemoved(t *testing.T) {
	t.Parallel()
	rt := &Router{Handlers: &Handlers{}, Config: testConfig()}
	srv := httptest.NewServer(DevCORS(rt.Build()))
	defer srv.Close()

	for _, path := range []string{
		"/plugins/codex/AGENTS.md",
		"/plugins/claude/CLAUDE.md",
	} {
		res, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if got := res.StatusCode; got != http.StatusNotFound {
			t.Fatalf("%s status=%d, want 404", path, got)
		}
	}
}

func TestAgentPackagesServeFocusedSkills(t *testing.T) {
	t.Parallel()
	rt := &Router{Handlers: &Handlers{}, Config: testConfig()}
	srv := httptest.NewServer(DevCORS(rt.Build()))
	defer srv.Close()

	for _, name := range []string{
		"using-specgate",
		"setting-up-specgate-project",
		"preparing-work",
		"delivering-work",
	} {
		res, err := http.Get(srv.URL + "/plugins/skills/" + name + "/SKILL.md")
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if got := res.StatusCode; got != http.StatusOK {
			t.Fatalf("%s status=%d, want 200", name, got)
		}
	}

	res, err := http.Get(srv.URL + "/plugins/skills/specgate-handoff/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if got := res.StatusCode; got != http.StatusNotFound {
		t.Fatalf("removed specgate-handoff status=%d, want 404", got)
	}

	for _, name := range []string{
		"scoping-work",
		"drafting-acceptance-criteria",
		"submitting-evidence",
		"evaluating-gate-tasks",
		"authoring-gate-package",
	} {
		res, err := http.Get(srv.URL + "/plugins/skills/" + name + "/SKILL.md")
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if got := res.StatusCode; got != http.StatusNotFound {
			t.Fatalf("retired skill %s status=%d, want 404", name, got)
		}
	}
}

func TestRequestBaseURL_SanitizesHostHeader(t *testing.T) {
	t.Parallel()
	// A valid host[:port] is preserved; an attacker-controlled Host carrying
	// shell metacharacters/newlines must NOT be interpolated into served
	// install scripts — it yields an empty base (installer then needs --registry).
	cases := map[string]string{
		"sdlc.example":      "http://sdlc.example",
		"sdlc.example:8080": "http://sdlc.example:8080",
		`evil"; rm -rf / #`: "",
		"evil$(whoami)":     "",
		"evil`id`":          "",
		"evil\nX-Inject: 1": "",
		"evil host":         "",
	}
	for host, want := range cases {
		req := httptest.NewRequest(http.MethodGet, "/plugins/install.sh", nil)
		req.Host = host
		if got := requestBaseURL(req); got != want {
			t.Errorf("requestBaseURL(host=%q) = %q, want %q", host, got, want)
		}
	}
}
