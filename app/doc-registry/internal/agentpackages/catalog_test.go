package agentpackages

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestCatalogRendersRegistryAwareFiles(t *testing.T) {
	t.Parallel()

	opts := Options{
		RegistryBaseURL: "https://sdlc.test",
	}

	readme, err := Render("README.md", opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`curl -fsSL "https://sdlc.test/cli/install.sh" | sh`,
		"specgate plugins install",
		"`specgate plugins doctor` follow-up",
		"`.agents/skills/specgate-*`",
		"`.claude/skills/specgate-*`",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("readme missing %q: %s", want, readme)
		}
	}
	for _, stale := range []string{
		"Human mode prints three clear steps",
		"install CLI, then write IDE files",
		"`plugins/specgate/`, `.cursor/skills/`",
	} {
		if strings.Contains(readme, stale) {
			t.Fatalf("readme contains stale guidance %q: %s", stale, readme)
		}
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
		`"Show the delivery status for this SpecGate work item."`,
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
	raw, err := Render("package.json", Options{})
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &manifest); err != nil {
		t.Fatalf("parse package manifest: %v", err)
	}
	if _, found := manifest["retired_skills"]; found {
		t.Fatalf("package manifest must not carry legacy retired_skills metadata")
	}
	if len(pkg.Skills) != 4 {
		t.Fatalf("skills = %v, want 4 focused skills", pkg.Skills)
	}
	for _, want := range []string{
		"specgate-router",
		"specgate-project-setup",
		"specgate-work-preparation",
		"specgate-work-delivery",
	} {
		if !slices.Contains(pkg.Skills, want) {
			t.Fatalf("skills = %v, want %s", pkg.Skills, want)
		}
	}
	for _, retired := range []string{
		"using-specgate",
		"setting-up-specgate-project",
		"preparing-work",
		"delivering-work",
	} {
		if slices.Contains(pkg.Skills, retired) {
			t.Fatalf("skills = %v, retired unprefixed skill %s remains", pkg.Skills, retired)
		}
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

func TestFocusedSkillsKeepOneExplicitSpecGatePhase(t *testing.T) {
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
		frontmatter := strings.SplitN(body, "---", 3)
		if len(frontmatter) != 3 || !strings.Contains(frontmatter[1], "description: Use when") || !strings.Contains(frontmatter[1], "SpecGate") {
			t.Fatalf("%s needs an explicit, trigger-oriented SpecGate description:\n%s", path, body)
		}
		if skill == "specgate-router" {
			if !strings.Contains(body, "## Route one phase") {
				t.Fatalf("%s missing phase routing:\n%s", path, body)
			}
		} else if !strings.Contains(body, "router operating contract") || !strings.Contains(body, "This phase") {
			t.Fatalf("%s does not inherit the router contract as one phase:\n%s", path, body)
		}
	}

	readme, err := Render("README.md", Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"## Skill invocation modes",
		"`specgate-router` selects one narrow phase",
		"`specgate-project-setup` is explicit setup",
		"Lifecycle skills stay focused",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("README missing invocation guidance %q:\n%s", want, readme)
		}
	}
}

func TestSkillsDescribeIDEAgentNoModelBoundaries(t *testing.T) {
	t.Parallel()
	render := func(path string) string {
		t.Helper()
		body, err := Render(path, Options{})
		if err != nil {
			t.Fatal(err)
		}
		return body
	}
	preparing := render("skills/specgate-work-preparation/SKILL.md")
	delivering := render("skills/specgate-work-delivery/SKILL.md")
	router := render("skills/specgate-router/SKILL.md")
	all := preparing + delivering + router
	for _, want := range []string{"data.mode", "gates tasks list", "gates tasks submit-result", "artifact coverage", "Local and Full mode"} {
		if !strings.Contains(all, want) {
			t.Fatalf("skills missing %q", want)
		}
	}
	for _, want := range []string{"readiness pass is not human approval", "human explicitly requests", "different review-only agent"} {
		if !strings.Contains(router+delivering, want) {
			t.Fatalf("skills missing boundary %q", want)
		}
	}
	if strings.Contains(delivering, "In Local mode, the immutable Context Pack is the drift boundary") {
		t.Fatal("Local drift remains unavailable in delivery skill")
	}
}

func TestDeliveringWorkSkillRequiresObservedCheckResults(t *testing.T) {
	t.Parallel()

	body, err := Render("skills/specgate-work-delivery/SKILL.md", Options{})
	if err != nil {
		t.Fatal(err)
	}
	normalized := strings.Join(strings.Fields(body), " ")
	for _, want := range []string{
		"observed result",
		"command name alone is not evidence",
		"pass`, `fail`, or `skipped",
	} {
		if !strings.Contains(normalized, want) {
			t.Fatalf("work-delivery skill missing observed check-result guidance %q:\n%s", want, body)
		}
	}
}

func TestDeliveringWorkSkillRequiresExplicitSpecGateContext(t *testing.T) {
	t.Parallel()

	body, err := Render("skills/specgate-work-delivery/SKILL.md", Options{})
	if err != nil {
		t.Fatal(err)
	}
	frontmatter := strings.SplitN(body, "---", 3)
	if len(frontmatter) != 3 || !strings.Contains(frontmatter[1], "approved SpecGate work item") {
		t.Fatalf("work-delivery skill must require explicit SpecGate context:\n%s", body)
	}
	for _, legacy := range []string{"ordinary developer phrasing", "A work reference (`CR-123`"} {
		if strings.Contains(body, legacy) {
			t.Fatalf("work-delivery skill still auto-governs generic prompts with %q:\n%s", legacy, body)
		}
	}
}

func TestDeliveringWorkSkillRequiresCriterionSpecificEvidence(t *testing.T) {
	t.Parallel()

	body, err := Render("skills/specgate-work-delivery/SKILL.md", Options{})
	if err != nil {
		t.Fatal(err)
	}
	normalized := strings.Join(strings.Fields(body), " ")
	for _, want := range []string{
		"exactly one `criteria[]` entry per canonical criterion",
		"independently reviewable claim",
		"evidence path, line, heading, file key, or URL",
		"command name alone is not evidence",
	} {
		if !strings.Contains(normalized, want) {
			t.Fatalf("work-delivery skill missing criterion-specific evidence guidance %q:\n%s", want, body)
		}
	}
}

func TestDeliveringWorkSkillReportsTruthfulAwaitingAcceptanceReceipt(t *testing.T) {
	t.Parallel()

	body, err := Render("skills/specgate-work-delivery/SKILL.md", Options{})
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.SplitN(body, "## 7. Show the delivery handoff", 2)
	if len(parts) != 2 {
		t.Fatalf("work-delivery skill missing delivery handoff section:\n%s", body)
	}
	receipt := strings.Join(strings.Fields(parts[1]), " ")
	for _, want := range []string{
		`specgate change status "$WORK_REF" --json`,
		"For `awaiting_acceptance`",
		"SpecGate delivery receipt",
		"Evidence: <evidence>",
		"Assurance: <assurance>",
		"Decision: <decision>",
		"Receipt: <receipt>",
		"Freshness: <freshness>",
		"Stale: <stale_reason>",
		"`next_actor`",
		"`next_command`",
		`specgate open "$WORK_REF" --print --json`,
		"In Local mode, never call `open`",
		"SpecGate delivery handoff",
		"Stale is a warning, not a state override",
		"Do not read the completion file again",
		"call stats or audit",
		"Never claim cleanup eligibility, bugs prevented, time saved, accepted, or delivered",
	} {
		if !strings.Contains(receipt, want) {
			t.Fatalf("work-delivery receipt contract missing %q:\n%s", want, parts[1])
		}
	}
	if !strings.Contains(receipt, "For any other state") || !strings.Contains(receipt, "do not use success wording") {
		t.Fatalf("work-delivery handoff contract does not cover non-success states:\n%s", parts[1])
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
		Homepage    string   `json:"homepage"`
		Repository  string   `json:"repository"`
		License     string   `json:"license"`
		Keywords    []string `json:"keywords"`
		Interface   struct {
			WebsiteURL string `json:"websiteURL"`
		} `json:"interface"`
	}
	packageBody, err := Render("package.json", Options{})
	if err != nil {
		t.Fatal(err)
	}
	var metadata struct {
		Homepage   string `json:"homepage"`
		Repository string `json:"repository"`
	}
	if err := json.Unmarshal([]byte(packageBody), &metadata); err != nil {
		t.Fatalf("parse package metadata: %v", err)
	}
	if metadata.Homepage == "" || metadata.Repository == "" {
		t.Fatalf("package website metadata incomplete: %+v", metadata)
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
		if got.Homepage != metadata.Homepage {
			t.Fatalf("%s homepage = %q, want %q", path, got.Homepage, metadata.Homepage)
		}
		if got.Repository != metadata.Repository {
			t.Fatalf("%s repository = %q, want %q", path, got.Repository, metadata.Repository)
		}
		if path == ".codex-plugin/plugin.json" && got.Interface.WebsiteURL != metadata.Homepage {
			t.Fatalf("%s interface website = %q, want %q", path, got.Interface.WebsiteURL, metadata.Homepage)
		}
		if got.License != pkg.License {
			t.Fatalf("%s license = %q, want %q", path, got.License, pkg.License)
		}
		if strings.Join(got.Keywords, ",") != strings.Join(pkg.Keywords, ",") {
			t.Fatalf("%s keywords = %v, want %v", path, got.Keywords, pkg.Keywords)
		}
	}
}
