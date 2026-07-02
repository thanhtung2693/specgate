package agentpackages

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestCatalogRendersRegistryAwareFiles(t *testing.T) {
	t.Parallel()

	opts := Options{
		RegistryBaseURL: "https://sdlc.test",
		SkillRepoURL:    "https://github.com/thanhtung2693/specgate/plugins",
	}

	readme, err := Render("README.md", opts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readme, "npx skills add https://github.com/thanhtung2693/specgate/plugins") {
		t.Fatalf("readme missing skill install command: %s", readme)
	}
	if !strings.Contains(readme, "https://sdlc.test") {
		t.Fatalf("readme missing registry URL: %s", readme)
	}

	manifest, err := Render(".codex-plugin/plugin.json", opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"name": "specgate"`,
		`"skills": "./skills/"`,
		`"composerIcon": "./assets/logo.svg"`,
		`"logo": "./assets/logo.svg"`,
	} {
		if !strings.Contains(manifest, want) {
			t.Fatalf("codex plugin manifest missing %q: %s", want, manifest)
		}
	}
	if strings.Contains(manifest, `"hooks"`) {
		t.Fatalf("codex plugin manifest must not contain unsupported hooks field: %s", manifest)
	}

	for _, path := range []string{
		".agents/plugins/marketplace.json",
		".agents/plugins/personal-marketplace.json",
		"assets/logo.svg",
		"hooks/hooks.json",
		"hooks/hooks-claude.json",
		".claude-plugin/plugin.json",
		".claude-plugin/marketplace.json",
		".cursor-plugin/plugin.json",
		".cursor-plugin/marketplace.json",
		"rules/using-specgate.mdc",
	} {
		if _, err := Render(path, opts); err != nil {
			t.Fatalf("render %s: %v", path, err)
		}
	}
}

func TestPackageInventoryServedFilesRender(t *testing.T) {
	t.Parallel()

	pkg, err := PackageInventory()
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Schema != "specgate.plugin-package/v1" {
		t.Fatalf("schema = %q", pkg.Schema)
	}
	if len(pkg.Skills) != 4 {
		t.Fatalf("skills = %v, want 4 focused skills", pkg.Skills)
	}
	if !slices.Contains(pkg.Skills, "setting-up-specgate-project") {
		t.Fatalf("skills = %v, want setting-up-specgate-project", pkg.Skills)
	}
	if !slices.Contains(pkg.Skills, "preparing-work") {
		t.Fatalf("skills = %v, want preparing-work", pkg.Skills)
	}
	if !slices.Contains(pkg.Skills, "delivering-work") {
		t.Fatalf("skills = %v, want delivering-work", pkg.Skills)
	}
	seen := map[string]bool{}
	for _, path := range pkg.ServedFiles {
		if seen[path] {
			t.Fatalf("duplicate served file %q", path)
		}
		seen[path] = true
		if _, err := Render(path, Options{RegistryBaseURL: "https://sdlc.test"}); err != nil {
			t.Fatalf("render served file %s: %v", path, err)
		}
	}
	for _, skill := range pkg.Skills {
		path := "skills/" + skill + "/SKILL.md"
		if !seen[path] {
			t.Fatalf("skill %s missing served file %s", skill, path)
		}
	}
	for _, path := range []string{
		"install.sh",
		"package.json",
		".agents/plugins/personal-marketplace.json",
		".codex-plugin/plugin.json",
		".claude-plugin/plugin.json",
		".cursor-plugin/plugin.json",
	} {
		if !seen[path] {
			t.Fatalf("required package file %s missing from inventory", path)
		}
	}
}

func TestFocusedSkillsDocumentInvocationModes(t *testing.T) {
	t.Parallel()

	pkg, err := PackageInventory()
	if err != nil {
		t.Fatal(err)
	}
	for _, skill := range pkg.Skills {
		path := "skills/" + skill + "/SKILL.md"
		body, err := Render(path, Options{})
		if err != nil {
			t.Fatalf("render %s: %v", path, err)
		}
		if !strings.Contains(body, "\n## Invocation\n") {
			t.Fatalf("%s missing Invocation section:\n%s", path, body)
		}
		if !strings.Contains(body, "Invocation mode:") {
			t.Fatalf("%s missing invocation mode label:\n%s", path, body)
		}
	}

	readme, err := Render("README.md", Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"## Skill invocation modes",
		"`using-specgate` is the router",
		"`setting-up-specgate-project` is explicit setup",
		"Lifecycle skills stay focused",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("README missing invocation guidance %q:\n%s", want, readme)
		}
	}
}

func TestAirConfigWatchesPluginSourceMount(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("../../.air.toml")
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if !strings.Contains(body, `include_dir = ["watch/plugins"]`) {
		t.Fatalf(".air.toml must watch watch/plugins so plugin-only changes rebuild embedded package assets:\n%s", body)
	}
	if !strings.Contains(body, "scripts/sync-embedded-plugins.sh && /usr/local/go/bin/go build") {
		t.Fatalf(".air.toml build command must sync embedded plugins before go build using the absolute Go path:\n%s", body)
	}
	if !strings.Contains(body, "poll = true") || !strings.Contains(body, "poll_interval = 1000") {
		t.Fatalf(".air.toml must use polling so Docker Desktop bind mounts trigger plugin rebuilds:\n%s", body)
	}
}

func TestPluginManifestsUsePackageMetadata(t *testing.T) {
	t.Parallel()

	pkg, err := PackageInventory()
	if err != nil {
		t.Fatal(err)
	}
	type manifest struct {
		Name        string   `json:"name"`
		Version     string   `json:"version"`
		Description string   `json:"description"`
		License     string   `json:"license"`
		Keywords    []string `json:"keywords"`
	}
	for _, path := range []string{
		".codex-plugin/plugin.json",
		".claude-plugin/plugin.json",
		".cursor-plugin/plugin.json",
	} {
		body, err := Render(path, Options{})
		if err != nil {
			t.Fatalf("render %s: %v", path, err)
		}
		var got manifest
		if err := json.Unmarshal([]byte(body), &got); err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		if got.Name != pkg.Name {
			t.Fatalf("%s name = %q, want %q", path, got.Name, pkg.Name)
		}
		if got.Version != pkg.Version {
			t.Fatalf("%s version = %q, want %q", path, got.Version, pkg.Version)
		}
		if got.Description != pkg.Description {
			t.Fatalf("%s description = %q, want %q", path, got.Description, pkg.Description)
		}
		if got.License != pkg.License {
			t.Fatalf("%s license = %q, want %q", path, got.License, pkg.License)
		}
		if strings.Join(got.Keywords, ",") != strings.Join(pkg.Keywords, ",") {
			t.Fatalf("%s keywords = %v, want %v", path, got.Keywords, pkg.Keywords)
		}
	}
}

func TestInstallScriptUsesCliAndIsTokenSafe(t *testing.T) {
	t.Parallel()

	script, err := Render("install.sh", Options{RegistryBaseURL: "https://sdlc.test"})
	if err != nil {
		t.Fatal(err)
	}
	// Interactive IDE picker, --agent/--dry-run flags, global default install,
	// project-local opt-in, subset support, CLI install via /cli/install.sh.
	for _, want := range []string{
		`REGISTRY_URL="${REGISTRY_URL:-https://sdlc.test}"`,
		"Select [1-4, comma-separated]:",
		"Tip: enter comma-separated values such as 1,2",
		"read -r choice < /dev/tty",
		`case "$choice" in`,
		`AGENT="all"`,
		`cursor,codex`,
		`--project-local`,
		`PROJECT_LOCAL=1`,
		`--skip-cli`,
		`SKIP_CLI=1`,
		`Skipped (--skip-cli)`,
		`find_specgate_bin()`,
		`plugins install --agent "$AGENT"`,
		`"$specgate_bin" "$@"`,
		"DRY_RUN=1",
		// CLI install — no MCP token fetch.
		"/cli/install.sh",
		`curl -fsSL "$cli_url" -o "$tmpfile"`,
		`sh "$tmpfile"`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("install.sh missing %q:\n%s", want, script)
		}
	}
	for _, forbidden := range []string{
		`write_file codex/AGENTS.md AGENTS.specgate.md`,
		`write_file claude/CLAUDE.md CLAUDE.specgate.md`,
		`python3 -`,
		`SPECGATE_RETIRED_SKILLS=`,
	} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("install.sh must not write removed fallback rule files (%q present):\n%s", forbidden, script)
		}
	}
	if strings.Contains(script, `curl -fsSL "$REGISTRY_URL/cli/install.sh" | sh`) {
		t.Fatalf("install.sh must not pipe the instance installer directly into sh:\n%s", script)
	}
	// The installer must never fetch MCP credentials.
	for _, forbidden := range []string{"mcp/api-key", "MCP_TOKEN", "MCP_API_KEY"} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("install.sh must not reference MCP credentials (%q present):\n%s", forbidden, script)
		}
	}
}

func TestInstallScriptValidatesTargetsBeforeWriting(t *testing.T) {
	t.Parallel()
	requireShellAndCurl(t)

	scriptPath := writeRenderedInstallScript(t)
	workDir := t.TempDir()
	homeDir := t.TempDir()
	registry := newFakePluginRegistry(t, nil)
	defer registry.Close()

	out, err := runInstallScript(t, scriptPath, workDir, homeDir, registry.URL,
		"--agent", "cursor, nope", "--project-local", "--skip-cli")
	if err == nil {
		t.Fatalf("install succeeded unexpectedly:\n%s", out)
	}
	for _, path := range []string{
		".cursor/rules/using-specgate.mdc",
		".cursor/skills/using-specgate/SKILL.md",
		"plugins/specgate/.codex-plugin/plugin.json",
		".claude/skills/specgate/.claude-plugin/plugin.json",
	} {
		if _, statErr := os.Stat(filepath.Join(workDir, path)); !os.IsNotExist(statErr) {
			t.Fatalf("%s was written before validation completed; stat err=%v output=\n%s", path, statErr, out)
		}
	}
}

func TestInstallScriptAllowsSpacedAgentLists(t *testing.T) {
	requireShellAndCurl(t)

	scriptPath := writeRenderedInstallScript(t)
	workDir := t.TempDir()
	homeDir := t.TempDir()
	argsFile := filepath.Join(workDir, "specgate-args.txt")
	t.Setenv("SPECGATE_ARGS_FILE", argsFile)
	writeFakeSpecgate(t, homeDir)
	registry := newFakePluginRegistry(t, nil)
	defer registry.Close()

	out, err := runInstallScript(t, scriptPath, workDir, homeDir, registry.URL,
		"--agent", "cursor, codex", "--project-local", "--skip-cli")
	if err != nil {
		t.Fatalf("install failed: %v\n%s", err, out)
	}
	got, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("fake specgate args missing: %v\n%s", err, out)
	}
	for _, want := range []string{
		"--server\n" + registry.URL,
		"plugins\ninstall\n--agent\ncursor,codex",
		"--project-local",
	} {
		if !strings.Contains(string(got), want) {
			t.Fatalf("delegated args missing %q:\n%s\noutput:\n%s", want, got, out)
		}
	}
}

func requireShellAndCurl(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skipf("sh not available: %v", err)
	}
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skipf("curl not available: %v", err)
	}
}

func writeRenderedInstallScript(t *testing.T) string {
	t.Helper()
	script, err := Render("install.sh", Options{RegistryBaseURL: "http://registry.invalid"})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "install.sh")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func runInstallScript(t *testing.T, scriptPath, workDir, homeDir, registryURL string, args ...string) (string, error) {
	t.Helper()
	allArgs := append([]string{scriptPath, "--registry", registryURL}, args...)
	cmd := exec.Command("sh", allArgs...)
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(), "HOME="+homeDir)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func writeFakeSpecgate(t *testing.T, homeDir string) {
	t.Helper()
	path := filepath.Join(homeDir, ".local", "bin", "specgate")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "#!/usr/bin/env sh\nprintf '%s\\n' \"$@\" > \"$SPECGATE_ARGS_FILE\"\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func newFakePluginRegistry(t *testing.T, missing map[string]bool) *httptest.Server {
	t.Helper()
	pkg, err := PackageInventory()
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, path := range pkg.ServedFiles {
		body, err := Render(path, Options{RegistryBaseURL: "https://sdlc.test"})
		if err != nil {
			t.Fatalf("render fake registry file %s: %v", path, err)
		}
		files[path] = body
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/plugins/")
		if missing[path] {
			http.NotFound(w, r)
			return
		}
		body, ok := files[path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
}
