package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/specgate/doc-registry/internal/agentpackages"
)

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

	pkg, err := agentpackages.PackageInventory()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range pkg.ServedFiles {
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
		"/plugins/install.sh",
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
		"specgate-router",
		"specgate-project-setup",
		"specgate-work-preparation",
		"specgate-work-delivery",
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

	res, err := http.Get(srv.URL + "/plugins/skills/not-a-specgate-skill/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if got := res.StatusCode; got != http.StatusNotFound {
		t.Fatalf("unknown skill status=%d, want 404", got)
	}
}

func TestRequestBaseURL_SanitizesHostHeader(t *testing.T) {
	t.Parallel()
	// A valid host[:port] is preserved; an attacker-controlled Host carrying
	// shell metacharacters/newlines must NOT be interpolated into served package
	// templates.
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
		req := httptest.NewRequest(http.MethodGet, "/plugins/README.md", nil)
		req.Host = host
		if got := requestBaseURL(req); got != want {
			t.Errorf("requestBaseURL(host=%q) = %q, want %q", host, got, want)
		}
	}
}
