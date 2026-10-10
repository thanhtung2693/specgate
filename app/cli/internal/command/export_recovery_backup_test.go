package command_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestExportsPreservePreEnhancedRecoveryBackup(t *testing.T) {
	deps, _, _, out := newFakeDeps(t)
	dir := t.TempDir()
	store, err := local.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	sel, err := store.Initialize(t.Context(), local.InitInput{WorkspaceName: "Upgraded", Username: "fixture", DisplayName: "Fixture"})
	if err != nil {
		t.Fatal(err)
	}
	work, err := store.CreateQuickWork(t.Context(), sel.Workspace.ID, local.QuickWorkInput{Title: "Upgrade", AcceptanceCriteria: []string{"Keep recovery data"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateCheckpointWithFiles(t.Context(), sel.Workspace.ID, work.Key, "checkout", nil, "upgrade"); err != nil {
		t.Fatal(err)
	}
	other, err := store.CreateWorkspace(t.Context(), "Unenhanced")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SelectWorkspace(t.Context(), other.ID); err != nil {
		t.Fatal(err)
	}
	work, err = store.CreateQuickWork(t.Context(), other.ID, local.QuickWorkInput{Title: "Portable work", AcceptanceCriteria: []string{"Export without losing recovery"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := (config.Config{Mode: config.ModeLocal, Local: config.LocalStore{Path: dir}}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(dir, "state.db.pre-enhanced.bak")
	original, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "backup-alias")
	if err := os.Symlink(backup, alias); err != nil {
		t.Fatal(err)
	}
	hardlink := filepath.Join(t.TempDir(), "backup-hardlink")
	if err := os.Link(backup, hardlink); err != nil {
		t.Fatal(err)
	}
	for _, destination := range []string{backup, alias, hardlink} {
		for _, args := range [][]string{
			{"portable", "export"},
			{"delivery", "handoff", "export", work.Key},
		} {
			out.Reset()
			args = append(args, "--file", destination, "--json")
			if code := command.ExecuteForCode(command.NewRootCommand(deps), args...); code != output.ExitUsage {
				t.Errorf("%v: exit=%d, output=%s", args, code, out.String())
			}
			got, err := os.ReadFile(backup)
			if err != nil || !bytes.Equal(got, original) {
				t.Fatalf("recovery backup changed: %v", err)
			}
		}
	}
}
