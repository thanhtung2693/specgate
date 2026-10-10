package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/client"
)

func TestFocusedSkillReferenceLifecycle(t *testing.T) {
	root := t.TempDir()
	const source = "skills/specgate-work-preparation/references/preservation.md"
	pkg := &client.PluginPackage{Version: "0.2.5", Skills: []string{"specgate-work-preparation"}, ServedFiles: []string{source}}
	installer := &pluginInstaller{
		ctx: context.Background(), home: root, pkg: pkg,
		deps: &Deps{Stdout: io.Discard},
		files: map[string][]byte{
			"skills/specgate-work-preparation/SKILL.md": []byte("Read references/preservation.md\n"),
			source: []byte("Preserve existing behavior.\n"),
		},
	}
	dir := filepath.Join(root, "skills")
	if err := installer.installFocusedSkills(dir); err != nil {
		t.Fatal(err)
	}
	reference := filepath.Join(dir, "specgate-work-preparation", "references", "preservation.md")
	if body, err := os.ReadFile(reference); err != nil || string(body) != "Preserve existing behavior.\n" {
		t.Fatalf("installed skill lost its reference: body=%q err=%v", body, err)
	}
	var layout pluginAgentFileLayout
	layout.addSkills(dir, pkg)
	health := checkPluginAgentFiles("cursor", false, pkg, layout)
	if !health.OK || len(health.Missing) != 0 {
		t.Fatalf("complete reference install unhealthy: %+v", health)
	}
	if err := os.Remove(reference); err != nil {
		t.Fatal(err)
	}
	health = checkPluginAgentFiles("cursor", false, pkg, layout)
	if len(health.Missing) == 0 || !strings.Contains(strings.Join(health.Missing, "\n"), "preservation.md") {
		t.Fatalf("doctor ignored a missing required reference: %+v", health)
	}
	if err := installer.installFocusedSkills(dir); err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(filepath.Dir(reference), "user-notes.md")
	if err := os.WriteFile(unrelated, []byte("keep\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if changed, _, err := removeOwnedPluginDir(filepath.Join(dir, "specgate-work-preparation")); err != nil || !changed {
		t.Fatalf("remove owned skill: changed=%v err=%v", changed, err)
	}
	if _, err := os.Stat(reference); !os.IsNotExist(err) {
		t.Fatalf("uninstall left managed reference: %v", err)
	}
	if body, err := os.ReadFile(unrelated); err != nil || string(body) != "keep\n" {
		t.Fatalf("uninstall lost unrelated reference directory content: %q err=%v", body, err)
	}
	if err := installer.validateFocusedSkills(dir); err == nil {
		t.Fatal("reinstall accepted unowned notes left by uninstall")
	}
	if _, err := os.Stat(filepath.Join(dir, "specgate-work-preparation", pluginOwnerMarker)); !os.IsNotExist(err) {
		t.Fatalf("refused preflight restored ownership marker: %v", err)
	}
	// Explicit user recovery relocates the whole leftover, never deletes notes.
	saved := filepath.Join(root, "preserved-user-notes")
	if err := os.Rename(filepath.Join(dir, "specgate-work-preparation"), saved); err != nil {
		t.Fatal(err)
	}
	if err := installer.validateFocusedSkills(dir); err != nil {
		t.Fatal(err)
	}
	if err := installer.installFocusedSkills(dir); err != nil {
		t.Fatal(err)
	}
	if body, err := os.ReadFile(filepath.Join(saved, "references", "user-notes.md")); err != nil || string(body) != "keep\n" {
		t.Fatalf("explicit reinstall recovery lost notes: %q err=%v", body, err)
	}
}

func TestPluginReferenceInventoryRejectsUnsafePaths(t *testing.T) {
	for _, file := range []string{
		"skills/specgate/../../outside.md", "skills/specgate/references/../outside.md",
		"/skills/specgate/ref.md", `skills/specgate/references\outside.md`,
		"skills/specgate/ref.md:stream", "skills/specgate/ref.md?query",
		"skills/foreign/ref.md", "skills/specgate/.specgate-owned",
		"skills/specgate/ref.md.specgate-owned",
		"skills/specgate/ref\n.md", "skills/specgate/SKILL.md/nested.md",
		"skills/specgate/.specgate-owned/note.md",
		"skills/specgate/references/a.specgate-owned/b",
		"skills/specgate/skill.md", "skills/specgate/skill.md/note.md",
		"skills/specgate/.SPECGATE-OWNED/note.md",
		"skills/specgate/references/CON.md", "skills/specgate/references/LPT1",
		"skills/specgate/references/COM¹.md", "skills/specgate/references/name.",
		"skills/specgate/references/name /note.md", "skills/specgate/references/*.md",
		"skills/specgate/references/a|b.md", "skills/specgate/references/a\"b.md",
	} {
		pkg := &client.PluginPackage{Version: "0.2.5", Skills: []string{"specgate"}, ServedFiles: []string{file}}
		if err := validatePluginPackage(pkg); err == nil {
			t.Errorf("unsafe inventory path accepted: %q", file)
		}
	}
}

func TestPluginReferenceInventoryRejectsCaseAliases(t *testing.T) {
	for _, files := range [][]string{
		{"skills/specgate/references/a.md", "skills/specgate/references/A.md"},
		{"skills/specgate/references/a.md", "skills/specgate/References/b.md"},
		{"skills/specgate/references/A", "skills/specgate/references/a/note.md"},
		{"skills/specgate/references/café.md", "skills/specgate/references/cafe\u0301.md"},
	} {
		pkg := &client.PluginPackage{Version: "0.2.5", Skills: []string{"specgate"}, ServedFiles: files}
		if err := validatePluginPackage(pkg); err == nil {
			t.Errorf("case-alias inventory accepted: %v", files)
		}
	}
}

func TestPluginReferenceInventoryPreservesValidNamesAndOlderPackages(t *testing.T) {
	for _, files := range [][]string{
		nil,
		{"skills/specgate/SKILL.md", "skills/specgate/references/COM10.md", "skills/specgate/references/café.md"},
	} {
		pkg := &client.PluginPackage{Version: "0.2.5", Skills: []string{"specgate"}, ServedFiles: files}
		if err := validatePluginPackage(pkg); err != nil {
			t.Fatalf("valid inventory refused: %v: %v", files, err)
		}
		got := pluginSkillFiles(pkg)
		if len(files) == 0 && (len(got) != 1 || got[0] != "skills/specgate/SKILL.md") {
			t.Fatalf("older inventory lost its entrypoint: %v", got)
		}
		if len(files) != 0 && strings.Join(got, "\n") != strings.Join(files, "\n") {
			t.Fatalf("valid file spellings changed: %v", got)
		}
	}
}

func TestPluginReferenceUnicodeFilesystemAlias(t *testing.T) {
	dir := t.TempDir()
	composed := filepath.Join(dir, "café.md")
	decomposed := filepath.Join(dir, "cafe\u0301.md")
	if err := os.WriteFile(composed, []byte("entrypoint\n"), 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(decomposed)
	if os.IsNotExist(err) {
		t.Skip("filesystem distinguishes Unicode normalization spellings")
	}
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.Stat(composed)
	if err != nil || !os.SameFile(original, info) {
		t.Fatalf("unexpected alias stat: %v", err)
	}
	pkg := &client.PluginPackage{Version: "0.2.5", Skills: []string{"specgate"}, ServedFiles: []string{
		"skills/specgate/references/café.md", "skills/specgate/references/cafe\u0301.md",
	}}
	if err := validatePluginPackage(pkg); err == nil {
		t.Fatal("inventory permits two paths to the same filesystem file")
	}
}

func TestPluginReferencePreflightChecksOrphanMarker(t *testing.T) {
	for _, owned := range []bool{false, true} {
		t.Run(fmt.Sprint(owned), func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "skills")
			skillDir := filepath.Join(dir, "specgate")
			reference := filepath.Join(skillDir, "references", "preservation.md")
			if err := os.MkdirAll(filepath.Dir(reference), 0700); err != nil {
				t.Fatal(err)
			}
			body := "user note\n"
			if owned {
				body = pluginOwnerValue
			}
			for file, text := range map[string]string{
				filepath.Join(skillDir, pluginOwnerMarker): pluginOwnerValue,
				reference + pluginOwnerMarker:              body,
			} {
				if err := os.WriteFile(file, []byte(text), 0600); err != nil {
					t.Fatal(err)
				}
			}
			installer := &pluginInstaller{home: root, pkg: &client.PluginPackage{
				Version: "0.2.5", Skills: []string{"specgate"}, ServedFiles: []string{"skills/specgate/references/preservation.md"},
			}}
			err := installer.validateFocusedSkills(dir)
			if (err == nil) != owned {
				t.Errorf("orphan marker ownership=%v: err=%v", owned, err)
			}
			if got, err := os.ReadFile(reference + pluginOwnerMarker); err != nil || string(got) != body {
				t.Fatalf("preflight changed orphan marker: %q err=%v", got, err)
			}
		})
	}
}

func TestOwnedReferenceUninstallKeepsUserNotes(t *testing.T) {
	root := t.TempDir()
	reference := filepath.Join(root, "references", "preservation.md")
	unrelated := filepath.Join(root, "references", "notes.md")
	if err := os.MkdirAll(filepath.Dir(reference), 0700); err != nil {
		t.Fatal(err)
	}
	for file, body := range map[string]string{
		filepath.Join(root, pluginOwnerMarker): pluginOwnerValue,
		filepath.Join(root, "SKILL.md"):        "managed skill\n",
		reference:                              "managed reference\n", reference + pluginOwnerMarker: pluginOwnerValue,
		unrelated: "keep\n",
	} {
		if err := os.WriteFile(file, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := removeOwnedPluginDir(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(reference); !os.IsNotExist(err) {
		t.Errorf("uninstall left managed reference: %v", err)
	}
	if body, err := os.ReadFile(unrelated); err != nil || string(body) != "keep\n" {
		t.Fatalf("unrelated note changed: %q err=%v", body, err)
	}
}

func TestPluginReferencePreflightRefusesUnownedCollision(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "skills")
	skillDir := filepath.Join(dir, "specgate")
	reference := filepath.Join(skillDir, "references", "preservation.md")
	if err := os.MkdirAll(filepath.Dir(reference), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, pluginOwnerMarker), []byte(pluginOwnerValue), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reference, []byte("user note\n"), 0600); err != nil {
		t.Fatal(err)
	}
	installer := &pluginInstaller{home: root, pkg: &client.PluginPackage{
		Version: "0.2.5", Skills: []string{"specgate"}, ServedFiles: []string{"skills/specgate/references/preservation.md"},
	}}
	if err := installer.validateFocusedSkills(dir); err == nil {
		t.Fatal("unmarked user reference collision accepted")
	}
	if body, err := os.ReadFile(reference); err != nil || string(body) != "user note\n" {
		t.Fatalf("preflight changed user note: %q err=%v", body, err)
	}
}

type unavailableReferencePlugin struct{ embeddedLocalPlugin }

func (p unavailableReferencePlugin) PluginFile(ctx context.Context, file string) ([]byte, error) {
	if strings.Contains(file, "/references/") {
		return nil, errors.New("reference unavailable")
	}
	return p.embeddedLocalPlugin.PluginFile(ctx, file)
}

func TestPluginPreloadReferenceFailureWritesNothing(t *testing.T) {
	pkg, err := (embeddedLocalPlugin{}).PluginPackage(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	installer := &pluginInstaller{
		ctx: t.Context(), home: root, pkg: pkg, client: unavailableReferencePlugin{},
		deps: &Deps{Stdout: io.Discard},
	}
	if err := installer.install([]string{"cursor"}); err == nil || !strings.Contains(err.Error(), "reference unavailable") {
		t.Fatalf("missing reference did not stop preload: %v", err)
	}
	if installer.written != 0 {
		t.Fatalf("reference failure wrote %d files", installer.written)
	}
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
		t.Fatalf("reference failure changed install tree: entries=%v err=%v", entries, err)
	}
}
