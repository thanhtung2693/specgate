package command

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Uninstall output is what a user reads at the moment they check what SpecGate
// touched. A shared config SpecGate only removed its own sections from is still
// their file; listing it under removed_paths reported a deletion that did not
// happen.
func TestCodexConfigCleanupIsReportedAsModifiedNotRemoved(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	configPath := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "model = \"gpt-5\"\n\n[plugins.\"specgate@personal\"]\nenabled = true\n"
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	remover := &pluginRemover{home: home}
	if err := remover.removeCodex(); err != nil {
		t.Fatalf("removeCodex: %v", err)
	}

	for _, path := range remover.paths {
		if path == configPath {
			t.Fatal("a config SpecGate only edited is reported as removed")
		}
	}
	found := false
	for _, path := range remover.modified {
		if path == configPath {
			found = true
		}
	}
	if !found {
		t.Fatalf("edited config missing from modified: %v", remover.modified)
	}

	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("SpecGate deleted a file it only had entries in: %v", err)
	}
	if !strings.Contains(string(raw), `model = "gpt-5"`) {
		t.Fatalf("the user's own setting did not survive: %q", raw)
	}
	if strings.Contains(string(raw), "specgate@personal") {
		t.Fatalf("SpecGate's own section was left behind: %q", raw)
	}
}

// Purging Local data removes SpecGate's database and nothing else in that
// directory, so an unrelated file a user parked beside it survives.
func TestPurgeLocalStateKeepsUnrelatedFiles(t *testing.T) {
	t.Parallel()
	store := t.TempDir()
	for name, body := range map[string]string{
		"state.db":      "sqlite",
		"state.db-wal":  "wal",
		"my-backup.txt": "keep me",
	} {
		if err := os.WriteFile(filepath.Join(store, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(store, "my-dir"), 0o755); err != nil {
		t.Fatal(err)
	}

	removed, err := removeLocalSQLiteState(store)
	if err != nil {
		t.Fatalf("removeLocalSQLiteState: %v", err)
	}
	if len(removed) == 0 {
		t.Fatal("nothing was reported as removed")
	}

	for _, name := range []string{"my-backup.txt", "my-dir"} {
		if _, err := os.Stat(filepath.Join(store, name)); err != nil {
			t.Fatalf("unrelated %s did not survive the purge: %v", name, err)
		}
	}
	for _, name := range []string{"state.db", "state.db-wal"} {
		if _, err := os.Stat(filepath.Join(store, name)); !os.IsNotExist(err) {
			t.Fatalf("%s survived the purge: %v", name, err)
		}
	}
}
