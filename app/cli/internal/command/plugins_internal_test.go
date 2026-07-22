package command

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	clientpkg "github.com/specgate/specgate/app/cli/internal/client"
)

func TestCodexMarketplaceHasPluginParsesJSON(t *testing.T) {
	t.Parallel()

	if !codexMarketplaceHasPlugin([]byte(`{"plugins":[{"name":"specgate","source":{"source":"local","path":"./.codex/plugins/specgate"}}]}`), "specgate", false) {
		t.Fatal("compact marketplace entry was not found")
	}
	if codexMarketplaceHasPlugin([]byte(`{"plugins":[{"name":"other","description":"specgate"}]}`), "specgate", false) {
		t.Fatal("plugin name must match structurally")
	}
	if codexMarketplaceHasPlugin([]byte(`{"plugins":[{"name":"specgate","source":{"source":"local","path":"/custom/specgate"}}]}`), "specgate", false) {
		t.Fatal("unowned SpecGate marketplace entry must not pass health validation")
	}
	if codexMarketplaceHasPlugin([]byte(`not json`), "specgate", false) {
		t.Fatal("malformed marketplace must not pass health validation")
	}
	committed := []byte(`{"plugins":[{"name":"specgate","source":{"source":"local","path":"./plugins"}}]}`)
	if codexMarketplaceHasPlugin(committed, "specgate", false) {
		t.Fatal("project-only source was accepted for a global install")
	}
	if !codexMarketplaceHasPlugin(committed, "specgate", true) {
		t.Fatal("project-only source was not accepted for a project-local install")
	}
}

func TestRemoveCodexConfigSectionsReplacesFileAtomically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[plugins.\"specgate@personal\"]\nenabled = true\n\n[tools]\nkeep = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	changed, err := removeCodexConfigSections(path, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected SpecGate config section removal")
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(before, after) {
		t.Fatal("shared Codex config was truncated in place instead of atomically replaced")
	}
	if after.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 600", after.Mode().Perm())
	}
}

func TestRemoveCodexMarketplaceKeepsUnownedSpecGateEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "marketplace.json")
	original := `{"name":"personal","plugins":[{"name":"specgate","source":{"source":"local","path":"/custom/specgate"}},{"name":"keep"}]}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	changed, err := removeCodexMarketplaceEntry(path)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("unowned same-name SpecGate entry was removed")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != original {
		t.Fatalf("unowned marketplace changed:\n%s", body)
	}
}

func TestRemoveCodexMarketplaceKeepsSamePathFromUnownedSourceType(t *testing.T) {
	path := filepath.Join(t.TempDir(), "marketplace.json")
	original := `{"name":"personal","plugins":[{"name":"specgate","source":{"source":"git","path":"./.codex/plugins/specgate"}}]}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	changed, err := removeCodexMarketplaceEntry(path)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("same path with an unowned source type was removed")
	}
}

func TestRemoveCodexMarketplaceKeepsAlreadyEmptySharedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "marketplace.json")
	original := `{"name":"personal","plugins":[],"user_note":"keep"}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	changed, err := removeCodexMarketplaceEntry(path)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("empty shared marketplace was reported as changed")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != original {
		t.Fatalf("empty shared marketplace changed:\n%s", body)
	}
}

func TestRemoveCodexMarketplacePreservesUnknownTopLevelFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "marketplace.json")
	original := `{"name":"personal","user_note":{"keep":true},"plugins":[{"name":"specgate","source":{"source":"local","path":"./.codex/plugins/specgate"}},{"name":"other"}]}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	changed, err := removeCodexMarketplaceEntry(path)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("managed SpecGate entry was not removed")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if note, ok := got["user_note"].(map[string]any); !ok || note["keep"] != true {
		t.Fatalf("unknown top-level field was lost: %s", body)
	}
	plugins, _ := got["plugins"].([]any)
	if len(plugins) != 1 || plugins[0].(map[string]any)["name"] != "other" {
		t.Fatalf("unexpected remaining plugins: %s", body)
	}
}

func TestValidatePluginOwnerMarkerRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	owned := filepath.Join(root, "owned-marker")
	if err := os.WriteFile(owned, []byte(pluginOwnerValue), 0o600); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "foreign", pluginOwnerMarker)
	if err := os.MkdirAll(filepath.Dir(marker), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(owned, marker); err != nil {
		t.Fatal(err)
	}

	if err := validatePluginOwnerMarker(marker, filepath.Dir(marker)); err == nil {
		t.Fatal("symlinked owner marker was accepted")
	}
}

func TestValidatePluginPackageRejectsUnsafeVersionAndExcessiveSkills(t *testing.T) {
	if err := validatePluginPackage(&clientpkg.PluginPackage{
		Version: "1.0.0",
		Skills:  []string{"specgate", "specgate-project-setup"},
	}); err != nil {
		t.Fatalf("product-named root skill was rejected: %v", err)
	}
	for _, version := range []string{"", "../escape", "nested/version", `nested\version`} {
		pkg := &clientpkg.PluginPackage{Version: version, Skills: []string{"specgate"}}
		if err := validatePluginPackage(pkg); err == nil {
			t.Fatalf("unsafe version %q was accepted", version)
		}
	}
	skills := make([]string, maxPluginSkills+1)
	for index := range skills {
		skills[index] = "skill-" + strings.Repeat("x", index+1)
	}
	if err := validatePluginPackage(&clientpkg.PluginPackage{Version: "0.1.0", Skills: skills}); err == nil {
		t.Fatal("excessive plugin skill inventory was accepted")
	}
	if err := validatePluginPackage(&clientpkg.PluginPackage{Version: "1.0.0"}); err == nil {
		t.Fatal("empty skill list was accepted")
	}
	if err := validatePluginPackage(&clientpkg.PluginPackage{Version: "1.0.0", Skills: []string{"delivery"}}); err == nil {
		t.Fatal("unnamespaced skill was accepted")
	}
	if err := validatePluginPackage(&clientpkg.PluginPackage{Version: "1.0.0", Skills: []string{"specgate", "specgate"}}); err == nil {
		t.Fatal("duplicate skill was accepted")
	}
}

func TestPluginInstallerPreloadsWithinAggregateLimitBeforeWriting(t *testing.T) {
	client := &fakePluginPackageClient{
		files: map[string][]byte{
			"rules/using-specgate.mdc":        []byte("rule"),
			"skills/specgate/SKILL.md": make([]byte, maxPluginPackageBytes),
		},
	}
	installer := &pluginInstaller{
		ctx: context.Background(), client: client,
		pkg:  &clientpkg.PluginPackage{Version: "0.1.0", Skills: []string{"specgate"}},
		opts: pluginInstallOptions{ProjectLocal: true},
	}

	if err := installer.preloadPluginFiles([]string{"cursor"}); err == nil {
		t.Fatal("oversized aggregate plugin package was accepted")
	}
	if installer.written != 0 || len(installer.files) != 0 {
		t.Fatalf("plugin files were staged before aggregate validation: written=%d files=%d", installer.written, len(installer.files))
	}
}

func TestInstallFocusedSkillsRemovesOnlyObsoleteOwnedSkills(t *testing.T) {
	home := t.TempDir()
	skillsDir := filepath.Join(home, ".cursor", "skills")
	retired := filepath.Join(skillsDir, "specgate-router")
	for path, body := range map[string]string{
		filepath.Join(retired, pluginOwnerMarker):       pluginOwnerValue,
		filepath.Join(retired, "SKILL.md"):              "old managed skill",
		filepath.Join(retired, "notes.txt"):             "keep me",
		filepath.Join(skillsDir, "foreign", "SKILL.md"): "user skill",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	const current = "specgate-work-delivery"
	installer := &pluginInstaller{
		home:  home,
		deps:  &Deps{Stdout: io.Discard},
		pkg:   &clientpkg.PluginPackage{Skills: []string{current}},
		files: map[string][]byte{"skills/" + current + "/SKILL.md": []byte("current skill")},
	}
	if err := installer.installFocusedSkills(skillsDir); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(retired, "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("obsolete managed skill remains: %v", err)
	}
	if body, err := os.ReadFile(filepath.Join(retired, "notes.txt")); err != nil || string(body) != "keep me" {
		t.Fatalf("user file in obsolete skill changed: body=%q err=%v", body, err)
	}
	if body, err := os.ReadFile(filepath.Join(skillsDir, "foreign", "SKILL.md")); err != nil || string(body) != "user skill" {
		t.Fatalf("unowned skill changed: body=%q err=%v", body, err)
	}
	if body, err := os.ReadFile(filepath.Join(skillsDir, current, "SKILL.md")); err != nil || string(body) != "current skill" {
		t.Fatalf("current skill missing: body=%q err=%v", body, err)
	}
}

func TestInstallFocusedSkillsKeepsObsoleteOwnedSkillsWhenReplacementFails(t *testing.T) {
	home := t.TempDir()
	skillsDir := filepath.Join(home, ".cursor", "skills")
	retired := filepath.Join(skillsDir, "delivering-work")
	for path, body := range map[string]string{
		filepath.Join(retired, pluginOwnerMarker): pluginOwnerValue,
		filepath.Join(retired, "SKILL.md"):        "old managed skill",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	installer := &pluginInstaller{
		home: home,
		deps: &Deps{Stdout: io.Discard},
		pkg:  &clientpkg.PluginPackage{Skills: []string{"specgate-work-delivery"}},
	}
	if err := installer.installFocusedSkills(skillsDir); err == nil {
		t.Fatal("replacement install unexpectedly succeeded")
	}
	if body, err := os.ReadFile(filepath.Join(retired, "SKILL.md")); err != nil || string(body) != "old managed skill" {
		t.Fatalf("obsolete skill was removed before replacement succeeded: body=%q err=%v", body, err)
	}
}

func TestInstallCodexKeepsObsoleteCacheWhenReplacementFails(t *testing.T) {
	home := t.TempDir()
	obsolete := filepath.Join(home, ".codex", "plugins", "cache", "personal", specgatePluginName, "0.1.0")
	if err := os.MkdirAll(obsolete, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(obsolete, pluginOwnerMarker), []byte(pluginOwnerValue), 0o600); err != nil {
		t.Fatal(err)
	}

	installer := &pluginInstaller{
		home: home,
		deps: &Deps{Stdout: io.Discard},
		pkg:  &clientpkg.PluginPackage{Version: "0.2.0"},
	}
	if err := installer.installCodex(); err == nil {
		t.Fatal("replacement install unexpectedly succeeded")
	}
	if _, err := os.Stat(filepath.Join(obsolete, pluginOwnerMarker)); err != nil {
		t.Fatalf("obsolete cache was removed before replacement succeeded: %v", err)
	}
}

func TestRemoveOwnedPluginDirPreservesUnknownUserFiles(t *testing.T) {
	root := filepath.Join(t.TempDir(), "specgate")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		pluginOwnerMarker: pluginOwnerValue,
		"SKILL.md":        "managed",
		"notes.txt":       "user-owned",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	changed, fullyRemoved, err := removeOwnedPluginDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("managed plugin files were not removed")
	}
	if fullyRemoved {
		t.Fatal("plugin directory containing an unknown user file was reported as removed")
	}
	if body, err := os.ReadFile(filepath.Join(root, "notes.txt")); err != nil || string(body) != "user-owned" {
		t.Fatalf("unknown user file changed: body=%q err=%v", body, err)
	}
	if _, err := os.Stat(filepath.Join(root, "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("managed skill file remains: %v", err)
	}
}

func TestRemoveOwnedPluginDirKeepsRootMarkerWhenNestedRemovalFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX directory permissions are required")
	}
	root := filepath.Join(t.TempDir(), "specgate")
	skillDir := filepath.Join(root, "skills", "specgate")
	for path, body := range map[string]string{
		filepath.Join(root, pluginOwnerMarker):     pluginOwnerValue,
		filepath.Join(skillDir, pluginOwnerMarker): pluginOwnerValue,
		filepath.Join(skillDir, "SKILL.md"):        "managed",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(skillDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(skillDir, 0o700) })

	if _, _, err := removeOwnedPluginDir(root); err == nil {
		t.Fatal("nested removal unexpectedly succeeded")
	}
	if _, err := os.Stat(filepath.Join(root, pluginOwnerMarker)); err != nil {
		t.Fatalf("root ownership marker was removed before cleanup completed: %v", err)
	}
}

type fakePluginPackageClient struct {
	files map[string][]byte
}

func (f *fakePluginPackageClient) PluginPackage(context.Context) (*clientpkg.PluginPackage, error) {
	return nil, nil
}

func (f *fakePluginPackageClient) PluginFile(_ context.Context, path string) ([]byte, error) {
	return f.files[path], nil
}
