package command_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/deploy"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestCleanupRunsMaintenanceCleanup(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.maintenanceCleanupResult = map[string]any{
		"expired_artifacts_deleted":        float64(2),
		"referenced_skipped":               float64(1),
		"demo_features_deleted":            float64(3),
		"demo_change_requests_deleted":     float64(5),
		"demo_artifacts_deleted":           float64(12),
		"archived_change_requests_deleted": float64(4),
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--yes", "cleanup")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if !fc.maintenanceCleanupCalled {
		t.Fatal("expected MaintenanceCleanup to be called")
	}
	got := out.String()
	for _, want := range []string{"Workspace cleanup complete", "expired artifacts", "archived work items", "demo"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}

func TestCleanupCancelledPrompt(t *testing.T) {
	t.Parallel()
	deps, fc, fp, out := newFakeDeps(t)
	deps.StdinIsTTY = func() bool { return true }
	fp.confirmValue = false
	code := command.ExecuteForCode(command.NewRootCommand(deps), "cleanup")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.maintenanceCleanupCalled {
		t.Fatal("cleanup must not run after cancelled prompt")
	}
}

func TestCleanupWorkDryRunAndSelectedRemoval(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	dir := t.TempDir()
	deps.WorkingDir = dir
	workDir := filepath.Join(dir, ".specgate", "work")
	if err := os.MkdirAll(filepath.Join(workDir, "dogfood"), 0o700); err != nil {
		t.Fatal(err)
	}
	for path := range map[string]string{
		filepath.Join(workDir, "plan.md"):                              "plan",
		filepath.Join(workDir, "dogfood", "state.db"):                  "state",
		filepath.Join(dir, ".specgate", "local-stack", "specgate.env"): "keep",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "cleanup", "--work", "--dry-run")
	if code != output.ExitOK {
		t.Fatalf("dry-run exit = %d, output = %s", code, out.String())
	}
	if _, err := os.Stat(filepath.Join(workDir, "plan.md")); err != nil {
		t.Fatalf("dry-run removed plan: %v", err)
	}
	if fc.maintenanceCleanupCalled {
		t.Fatal("project work cleanup must not call workspace maintenance")
	}

	out.Reset()
	code = command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--yes", "cleanup", "--work", "--item", "plan.md")
	if code != output.ExitOK {
		t.Fatalf("selected cleanup exit = %d, output = %s", code, out.String())
	}
	if _, err := os.Stat(filepath.Join(workDir, "plan.md")); !os.IsNotExist(err) {
		t.Fatalf("plan still exists or stat failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workDir, "dogfood", "state.db")); err != nil {
		t.Fatalf("unselected work was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".specgate", "local-stack", "specgate.env")); err != nil {
		t.Fatalf("Full appliance was removed: %v", err)
	}
}

func TestCleanupWorkIncludesGeneratedDeliveryScaffolds(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	dir := t.TempDir()
	deps.WorkingDir = dir
	specgateDir := filepath.Join(dir, ".specgate")
	if err := os.MkdirAll(specgateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	completion := filepath.Join(specgateDir, "completion-CR-123.json")
	peerReview := filepath.Join(specgateDir, "peer-review-CR-123.json")
	configPath := filepath.Join(specgateDir, "config")
	for _, path := range []string{completion, peerReview, configPath} {
		if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "cleanup", "--work", "--dry-run")
	if code != output.ExitOK {
		t.Fatalf("dry-run exit = %d, output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "completion-CR-123.json") || !strings.Contains(out.String(), "peer-review-CR-123.json") {
		t.Fatalf("generated scaffolds missing from cleanup preview: %s", out.String())
	}
	if strings.Contains(out.String(), `"config"`) {
		t.Fatalf("project config must not be eligible for cleanup: %s", out.String())
	}

	out.Reset()
	code = command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "cleanup", "--work", "--item", "completion-CR-123.json")
	if code != output.ExitOK {
		t.Fatalf("cleanup exit = %d, output = %s", code, out.String())
	}
	if _, err := os.Stat(completion); !os.IsNotExist(err) {
		t.Fatalf("completion scaffold still exists or stat failed: %v", err)
	}
	for _, path := range []string{peerReview, configPath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("unselected or protected file removed: %s: %v", path, err)
		}
	}
	if fc.maintenanceCleanupCalled {
		t.Fatal("project cleanup must not call workspace maintenance")
	}
}

func TestCleanupWorkNeedsYesOutsideInteractiveTerminal(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	dir := t.TempDir()
	deps.WorkingDir = dir
	path := filepath.Join(dir, ".specgate", "work", "plan.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("plan"), 0o600); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "cleanup", "--work")
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want usage; output = %s", code, out.String())
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("work was removed without --yes: %v", err)
	}
}

func TestCleanupWorkNonInteractiveYesRequiresExplicitItems(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	dir := t.TempDir()
	deps.WorkingDir = dir
	path := filepath.Join(dir, ".specgate", "work", "user-spec.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "cleanup", "--work")
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want usage; output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "--item user-spec.md") {
		t.Fatalf("missing explicit recovery command: %s", out.String())
	}
	if body, err := os.ReadFile(path); err != nil || string(body) != "keep" {
		t.Fatalf("unowned work file changed: body=%q err=%v", body, err)
	}
}

func TestCleanupWorkFlagsFailBeforeWorkspaceMaintenance(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "cleanup", "--item", "plan.md")
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want usage; output = %s", code, out.String())
	}
	if fc.maintenanceCleanupCalled {
		t.Fatal("invalid project cleanup flags must not call workspace maintenance")
	}
	if !strings.Contains(out.String(), "require --work") {
		t.Fatalf("missing actionable flag error: %s", out.String())
	}
}

func TestCleanupWorkPromptsForSelection(t *testing.T) {
	t.Parallel()
	deps, fc, fp, out := newFakeDeps(t)
	dir := t.TempDir()
	deps.WorkingDir = dir
	deps.StdinIsTTY = func() bool { return true }
	fp.multiValues = []string{"plan.md"}
	fp.confirmValue = true
	for path := range map[string]string{
		filepath.Join(dir, ".specgate", "work", "plan.md"):      "plan",
		filepath.Join(dir, ".specgate", "work", "dogfood", "x"): "keep",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "cleanup", "--work")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fp.multiTitle != "Select transient SpecGate work files to remove" || fp.confirmTitle == "" {
		t.Fatalf("selection/confirmation prompts missing: select=%q confirm=%q", fp.multiTitle, fp.confirmTitle)
	}
	if _, err := os.Stat(filepath.Join(dir, ".specgate", "work", "plan.md")); !os.IsNotExist(err) {
		t.Fatalf("selected item still exists or stat failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".specgate", "work", "dogfood", "x")); err != nil {
		t.Fatalf("unselected item was removed: %v", err)
	}
	if fc.maintenanceCleanupCalled {
		t.Fatal("project work cleanup must not call workspace maintenance")
	}
}

func TestCleanupWorkJSONDryRunListsOnlyProjectWork(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	dir := t.TempDir()
	deps.WorkingDir = dir
	path := filepath.Join(dir, ".specgate", "work", "plan.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("plan"), 0o600); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "cleanup", "--work", "--dry-run")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var response struct {
		OK   bool `json:"ok"`
		Data struct {
			WorkDir   string   `json:"work_dir"`
			Available []string `json:"available"`
			Selected  []string `json:"selected"`
			Removed   []string `json:"removed"`
			DryRun    bool     `json:"dry_run"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &response); err != nil {
		t.Fatalf("parse JSON: %v\n%s", err, out.String())
	}
	if !response.OK || !response.Data.DryRun || response.Data.WorkDir == "" || len(response.Data.Available) != 1 || !strings.EqualFold(response.Data.Available[0], "plan.md") || len(response.Data.Removed) != 0 {
		t.Fatalf("unexpected cleanup JSON: %s", out.String())
	}
	if fc.maintenanceCleanupCalled {
		t.Fatal("project work cleanup must not call workspace maintenance")
	}
}

func TestCleanupBackupsDryRunAndSelectedRemoval(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	deployDir := t.TempDir()
	if err := deploy.MarkManagedDirectory(deployDir); err != nil {
		t.Fatal(err)
	}
	backupDir := filepath.Join(deployDir, "backups")
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		t.Fatal(err)
	}
	selected := "specgate-before-0.2.0-20260717T010203Z.tar.gz"
	kept := "specgate-before-0.2.1-20260717T020304Z.tar.gz"
	unrelated := "notes.txt"
	for _, name := range []string{selected, kept, unrelated} {
		if err := os.WriteFile(filepath.Join(backupDir, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "cleanup", "--backups", "--dir", deployDir, "--dry-run")
	if code != output.ExitOK {
		t.Fatalf("dry-run exit = %d, output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), selected) || !strings.Contains(out.String(), kept) || strings.Contains(out.String(), unrelated) {
		t.Fatalf("backup preview includes wrong files: %s", out.String())
	}

	out.Reset()
	code = command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "cleanup", "--backups", "--dir", deployDir, "--item", selected)
	if code != output.ExitOK {
		t.Fatalf("selected cleanup exit = %d, output = %s", code, out.String())
	}
	if _, err := os.Stat(filepath.Join(backupDir, selected)); !os.IsNotExist(err) {
		t.Fatalf("selected backup still exists or stat failed: %v", err)
	}
	for _, name := range []string{kept, unrelated} {
		if _, err := os.Stat(filepath.Join(backupDir, name)); err != nil {
			t.Fatalf("unselected file was removed: %s: %v", name, err)
		}
	}
	if fc.maintenanceCleanupCalled {
		t.Fatal("backup cleanup must not call workspace maintenance")
	}
}

func TestCleanupBackupsRejectsUnmanagedDirectory(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	deployDir := t.TempDir()
	archive := filepath.Join(deployDir, "backups", "specgate-before-0.2.0-20260717T010203Z.tar.gz")
	if err := os.MkdirAll(filepath.Dir(archive), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archive, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "cleanup", "--backups", "--dir", deployDir)
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want usage; output = %s", code, out.String())
	}
	if body, err := os.ReadFile(archive); err != nil || string(body) != "keep" {
		t.Fatalf("unmanaged archive changed: body=%q err=%v", body, err)
	}
}

func TestCleanupWorkRejectsSymlinkedWorkDirectory(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	projectDir := t.TempDir()
	deps.WorkingDir = projectDir
	external := t.TempDir()
	externalFile := filepath.Join(external, "plan.md")
	if err := os.WriteFile(externalFile, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	specgateDir := filepath.Join(projectDir, ".specgate")
	if err := os.MkdirAll(specgateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(specgateDir, "work")); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "cleanup", "--work")
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want usage; output = %s", code, out.String())
	}
	if body, err := os.ReadFile(externalFile); err != nil || string(body) != "keep" {
		t.Fatalf("symlink target changed: body=%q err=%v", body, err)
	}
}
