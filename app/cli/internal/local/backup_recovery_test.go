package local

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPreEnhancedBackupRestoresLegacyWritableSnapshot(t *testing.T) {
	binary := os.Getenv("SPECGATE_LEGACY_CLI")
	if binary == "" {
		t.Skip("set SPECGATE_LEGACY_CLI to a pre-enhancement CLI binary")
	}
	liveDir := t.TempDir()
	live, err := Open(filepath.Join(liveDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer live.Close()
	selection, err := live.Initialize(t.Context(), InitInput{WorkspaceName: "Recovery baseline", Username: "human", DisplayName: "Human"})
	if err != nil {
		t.Fatal(err)
	}
	before, err := live.CreateQuickWork(t.Context(), selection.Workspace.ID, QuickWorkInput{Title: "Before upgrade", AcceptanceCriteria: []string{"Preserve baseline"}})
	if err != nil {
		t.Fatal(err)
	}
	contextBefore, err := live.ContextPack(t.Context(), selection.Workspace.ID, before.Key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := live.CreateCheckpointWithFiles(t.Context(), selection.Workspace.ID, before.Key, "checkout", nil, "first enhanced write"); err != nil {
		t.Fatal(err)
	}
	if _, err := live.CreateQuickWork(t.Context(), selection.Workspace.ID, QuickWorkInput{Title: "After upgrade", AcceptanceCriteria: []string{"Not in backup"}}); err != nil {
		t.Fatal(err)
	}
	backupPath := live.enhancedStoreBackupPath()
	backup, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	// Recover into a separate empty fixture directory; never overwrite the live DB.
	restoredDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(restoredDir, "state.db"), backup, 0600); err != nil {
		t.Fatal(err)
	}
	config, err := json.Marshal(map[string]any{"mode": "local", "local": map[string]string{"path": restoredDir}})
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(restoredDir, "config.json")
	if err := os.WriteFile(configPath, config, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), binary, "--json", "--yes", "workspace", "create", "Recovered legacy writer")
	cmd.Dir = restoredDir
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + restoredDir, "SPECGATE_CONFIG_PATH=" + configPath, "SPECGATE_NO_UPDATE_CHECK=1", "CI=1"}
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("legacy CLI could not write restored backup: %v %s", err, output)
	}
	var envelope struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(output, &envelope); err != nil || !envelope.OK {
		t.Fatalf("legacy write envelope: %v %s", err, output)
	}
	restored, err := Open(filepath.Join(restoredDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	workAfter, err := restored.GetWork(t.Context(), selection.Workspace.ID, before.Key)
	if err != nil || !reflect.DeepEqual(workAfter, before) {
		t.Fatalf("restored work differs: got=%#v want=%#v err=%v", workAfter, before, err)
	}
	contextAfter, err := restored.ContextPack(t.Context(), selection.Workspace.ID, before.Key)
	if err != nil || contextAfter != contextBefore {
		t.Fatalf("restored Context Pack differs: err=%v", err)
	}
	for _, check := range []struct {
		query string
		want  int
	}{
		{`SELECT COUNT(*) FROM work_items WHERE title='Before upgrade'`, 1},
		{`SELECT COUNT(*) FROM work_items WHERE title='After upgrade'`, 0},
		{`SELECT COUNT(*) FROM workspaces WHERE name='Recovered legacy writer'`, 1},
		{`SELECT COUNT(*) FROM metadata WHERE key='enhanced_store_version'`, 0},
		{`SELECT COUNT(*) FROM work_checkpoints`, 0},
		{`SELECT COUNT(*) FROM sqlite_master WHERE type='trigger' AND name LIKE 'specgate_enhanced_%'`, 0},
	} {
		var got int
		if err := restored.db.QueryRowContext(t.Context(), check.query).Scan(&got); err != nil || got != check.want {
			t.Fatalf("restored snapshot query %s: got=%d want=%d err=%v", check.query, got, check.want, err)
		}
	}
	var later int
	if err := live.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM work_items WHERE title='After upgrade'`).Scan(&later); err != nil || later != 1 {
		t.Fatalf("recovery changed live data: count=%d err=%v", later, err)
	}
	unchanged, err := os.ReadFile(backupPath)
	if err != nil || !bytes.Equal(backup, unchanged) {
		t.Fatalf("recovery changed original backup: %v", err)
	}
}
