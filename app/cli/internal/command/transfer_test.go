package command_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func runTransfer(t *testing.T, deps *command.Deps, out *bytes.Buffer, exit int, args ...string) map[string]any {
	t.Helper()
	out.Reset()
	code := command.ExecuteForCode(command.NewRootCommand(deps), append([]string{"--json", "transfer"}, args...)...)
	var envelope struct {
		OK   bool
		Data map[string]any
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); code != exit || err != nil || envelope.OK != (exit == output.ExitOK) {
		t.Fatalf("%v: exit=%d want=%d json=%v body=%s", args, code, exit, err, out)
	}
	return envelope.Data
}

func TestTransferWholeStoreWALAndExplicitActivation(t *testing.T) {
	deps, out := newTestDeps(t)
	source, store, selected, work := newLocalChangeWork(t, deps)
	defer store.Close()
	repo := filepath.Join(t.TempDir(), "repo with ' quote")
	if err := os.Mkdir(repo, 0700); err != nil {
		t.Fatal(err)
	}
	gitInit(t, repo)
	transferTestGit(t, repo, "commit", "--allow-empty", "-m", "Transfer")
	transferTestGit(t, repo, "remote", "add", "origin", "https://name:origin-secret@example.test/team/repo?token=secret")
	root, _ := config.FindProjectRoot(repo)
	deps.WorkingDir = root

	ctx := t.Context()
	other, err := store.Initialize(ctx, local.InitInput{WorkspaceName: "Other workspace", DisplayName: "Second user", Username: "second", Email: "exclude@example.test"})
	if err != nil {
		t.Fatal(err)
	}
	walWork, err := store.CreateQuickWork(ctx, other.Workspace.ID, local.QuickWorkInput{Title: "Committed in WAL", AcceptanceCriteria: []string{"Preserve other work"}})
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(filepath.Join(source, "state.db-wal")); err != nil || info.Size() == 0 {
		t.Fatalf("fixture must hold WAL: %v", err)
	}
	checkpoint, err := store.CreateCheckpointWithFiles(ctx, selected.Workspace.ID, work.Key, "checkout", nil, "historic")
	if err != nil {
		t.Fatal(err)
	}
	submitLocalChangeDelivery(t, store, selected.Workspace.ID, work, "fixture", "source")
	history, err := store.Audit(ctx, selected.Workspace.ID, work.Key)
	if err != nil || len(history) == 0 {
		t.Fatalf("fixture history: %v %v", history, err)
	}
	cfg := config.Config{Mode: config.ModeLocal, Local: config.LocalStore{Path: source, ID: selected.StoreID}}
	cfg.SetProjectWorkspace(root, config.CurrentWorkspace{ID: selected.Workspace.ID, Slug: selected.Workspace.Slug})
	if err := cfg.SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

	packagePath := filepath.Join(t.TempDir(), "package.zip")
	args := []string{"prepare", "--work", work.Key, "--checkpoint", checkpoint.ID, "--file", packagePath}
	runTransfer(t, deps, out, output.ExitUsage, args...)
	prepared := runTransfer(t, deps, out, output.ExitOK, append(args, "--yes")...)
	digest, _ := prepared["snapshot_sha256"].(string)
	if len(digest) != 64 || !strings.Contains(out.String(), "second") || strings.Contains(out.String(), "origin-secret") || strings.Contains(out.String(), "exclude@example.test") {
		t.Fatalf("unsafe or incomplete prepare: %s", out)
	}
	runTransfer(t, deps, out, output.ExitConflict, append(args, "--yes")...)

	receiver, receivedOut := newTestDeps(t)
	writeTestFile(t, receiver.ConfigPath, "{bad unrelated config")
	runTransfer(t, receiver, receivedOut, output.ExitOK, "inspect", "--file", packagePath)
	dst := filepath.Join(t.TempDir(), "receive")
	receiveArgs := []string{"receive", "--file", packagePath, "--into", dst}
	runTransfer(t, receiver, receivedOut, output.ExitUsage, receiveArgs...)
	runTransfer(t, receiver, receivedOut, output.ExitConflict, append(receiveArgs, "--expect-snapshot", strings.Repeat("0", 64))...)
	first := runTransfer(t, receiver, receivedOut, output.ExitOK, append(receiveArgs, "--expect-snapshot", digest)...)
	generation, _ := first["receiving_generation"].(string)
	if len(generation) != 32 {
		t.Fatalf("invalid generation: %s", receivedOut)
	}
	runTransfer(t, receiver, receivedOut, output.ExitConflict, append(receiveArgs, "--expect-snapshot", digest)...)
	if _, err := os.Stat(filepath.Join(dst, "state.db-wal")); !os.IsNotExist(err) {
		t.Fatalf("physical receiving WAL: %v", err)
	}
	copied, err := local.Open(filepath.Join(dst, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer copied.Close()
	if got, err := copied.GetWork(ctx, other.Workspace.ID, walWork.Key); err != nil || got.ID != walWork.ID {
		t.Fatalf("WAL work was lost: %#v %v", got, err)
	}
	if got, err := copied.GetWork(ctx, selected.Workspace.ID, work.Key); err != nil || got.ID != work.ID || got.ContextDigest != work.ContextDigest {
		t.Fatalf("bookmarked work changed: %#v %v", got, err)
	}
	if got, err := copied.GetCheckpoint(ctx, selected.Workspace.ID, work.Key, checkpoint.ID); err != nil || got.Note != checkpoint.Note {
		t.Fatalf("checkpoint changed: %#v %v", got, err)
	}
	if got, err := copied.Audit(ctx, selected.Workspace.ID, work.Key); err != nil || len(got) != len(history) {
		t.Fatalf("audit changed: %#v %v", got, err)
	}

	runTransfer(t, receiver, receivedOut, output.ExitUnavailable, "activate", "--from", dst, "--repo", root, "--yes")
	if err := os.Remove(receiver.ConfigPath); err != nil {
		t.Fatal(err)
	}
	runTransfer(t, receiver, receivedOut, output.ExitUsage, "activate", "--from", dst, "--repo", root)
	result := runTransfer(t, receiver, receivedOut, output.ExitOK, "activate", "--from", dst, "--repo", root, "--yes")
	next, _ := result["resume_command"].(string)
	quotedApostrophe := "'\"'\"'"
	if runtime.GOOS == "windows" {
		quotedApostrophe = "''"
	}
	if result["activation_committed"] != true || result["verified"] != true || !strings.Contains(next, "--since '"+checkpoint.ID+"'") || !strings.Contains(next, quotedApostrophe) || strings.Contains(next, "accept") {
		t.Fatalf("unexpected activation: %s", receivedOut)
	}
	resolved, err := config.LoadFrom(receiver.ConfigPath)
	if err != nil || resolved.Local != (config.LocalStore{}) || resolved.Projects[root].Local.Path != dst || resolved.Projects[root].Workspace.ID != selected.Workspace.ID {
		t.Fatalf("config binding not isolated: %#v %v", resolved, err)
	}
}

func TestTransferPrepareConfirmsTheExactStagedSnapshot(t *testing.T) {
	deps, prompt, out := newWorkflowTestDeps(t)
	stateDir, store, selected, work := newLocalChangeWork(t, deps)
	defer store.Close()
	cfg := config.Config{Mode: config.ModeLocal, Local: config.LocalStore{Path: stateDir}}
	if err := cfg.SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	deps.Stderr = out
	prompt.confirmValue = true
	prompt.confirmObserver = func() {
		if !strings.Contains(out.String(), "ENTIRE selected Local store") || !strings.Contains(out.String(), selected.Workspace.Slug) {
			t.Fatalf("scope must be printed before consent: %s", out)
		}
		if _, err := store.CreateWorkspace(t.Context(), "After consent preview"); err != nil {
			t.Fatal(err)
		}
	}
	archive := filepath.Join(t.TempDir(), "stage-once.zip")
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "transfer", "prepare", "--work", work.Key, "--file", archive); code != output.ExitOK {
		t.Fatalf("prepare exit=%d body=%s", code, out)
	}
	runTransfer(t, deps, out, output.ExitOK, "inspect", "--file", archive)
	if strings.Contains(out.String(), "after-consent-preview") {
		t.Fatalf("published a different snapshot: %s", out)
	}
}

func transferTestGit(t *testing.T, repo string, args ...string) {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "git", append([]string{"-C", repo}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
}
