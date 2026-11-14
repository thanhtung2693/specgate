package command_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/executil"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestTransferRefusesMalformedAndMismatchedPackages(t *testing.T) {
	deps, out, source, work, archive := newTransferSafetyFixture(t)
	data := runTransfer(t, deps, out, output.ExitOK, "prepare", "--file", archive, "--work", work.Key, "--yes")
	sha := data["snapshot_sha256"].(string)
	original := transferReadFile(t, archive)
	payload, manifest := transferEntries(t, archive)
	receiver, receiverOut := newTestDeps(t)
	into := filepath.Join(t.TempDir(), "incoming")
	pkg := filepath.Join(t.TempDir(), "modified.zip")

	testCases := []struct {
		name  string
		exit  int
		write func()
	}{
		{"wrong digest", output.ExitConflict, func() { writeTestFile(t, pkg, string(original)+"x") }},
		{"truncated ZIP", output.ExitUsage, func() { writeTestFile(t, pkg, string(original[:len(original)-10])) }},
		{"null manifest", output.ExitUsage, func() { writeTransferVariant(t, pkg, []byte("null"), payload, "state.db", 0) }},
		{"future manifest", output.ExitIncompatible, func() {
			editTransferManifest(t, pkg, manifest, payload, func(m map[string]any) { m["format"] = "specgate.local-transfer/v99" })
		}},
		{"future store", output.ExitIncompatible, func() {
			editTransferManifest(t, pkg, manifest, payload, func(m map[string]any) { m["inventory"].(map[string]any)["store_version"] = "99" })
		}},
		{"missing unrelated schema column", output.ExitIncompatible, func() {
			dbPath := filepath.Join(t.TempDir(), "tampered.db")
			writeTestFile(t, dbPath, string(payload))
			db, err := sql.Open("sqlite", dbPath)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec("ALTER TABLE delivery_peer_reviews RENAME COLUMN agent_name TO archived_agent"); err != nil {
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			editTransferManifest(t, pkg, manifest, transferReadFile(t, dbPath), nil)
		}},
		{"future SQLite schema", output.ExitIncompatible, func() {
			db := bytes.Clone(payload)
			db[63] = 1 // SQLite file header PRAGMA user_version, big endian.
			editTransferManifest(t, pkg, manifest, db, nil)
		}},
		{"oversize manifest", output.ExitUsage, func() { writeTransferVariant(t, pkg, make([]byte, (1<<20)+1), payload, "state.db", 0) }},
		{"oversize payload declared by manifest", output.ExitUsage, func() {
			editTransferManifest(t, pkg, manifest, payload, func(m map[string]any) { m["payload_size"] = 513 << 20 })
		}},
		{"bad payload checksum", output.ExitUsage, func() {
			editTransferManifest(t, pkg, manifest, payload, func(m map[string]any) { m["payload_sha256"] = strings.Repeat("0", 64) })
		}},
		{"corrupt SQLite with valid checksum", output.ExitUsage, func() {
			db := bytes.Clone(payload)
			db[0] = '!'
			editTransferManifest(t, pkg, manifest, db, nil)
		}},
		{"mismatched Context Pack digest", output.ExitUsage, func() {
			editTransferManifest(t, pkg, manifest, payload, func(m map[string]any) {
				m["inventory"].(map[string]any)["bookmark"].(map[string]any)["context_digest"] = strings.Repeat("1", 64)
			})
		}},
		{"foreign checkpoint", output.ExitNotFound, func() {
			editTransferManifest(t, pkg, manifest, payload, func(m map[string]any) {
				m["inventory"].(map[string]any)["bookmark"].(map[string]any)["checkpoint_id"] = strings.Repeat("1", 32)
			})
		}},
		{"duplicate", output.ExitUsage, func() { writeTransferVariant(t, pkg, manifest, payload, "manifest.json", 0) }},
		{"traversal", output.ExitUsage, func() { writeTransferVariant(t, pkg, manifest, payload, "../state.db", 0) }},
		{"symlink", output.ExitUsage, func() { writeTransferVariant(t, pkg, manifest, payload, "state.db", os.ModeSymlink|0o777) }},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tc.write()
			modified := transferReadFile(t, pkg)
			digest := sha
			if tc.name != "wrong digest" {
				checksum := sha256.Sum256(modified)
				digest = hex.EncodeToString(checksum[:])
				runTransfer(t, receiver, receiverOut, tc.exit, "inspect", "--file", pkg)
			}
			runTransfer(t, receiver, receiverOut, tc.exit, "receive", "--file", pkg, "--into", into, "--expect-snapshot", digest)
			if !bytes.Equal(transferReadFile(t, pkg), modified) {
				t.Fatal("receive modified the input package")
			}
			if _, err := os.Lstat(into); !os.IsNotExist(err) {
				t.Fatalf("invalid receive created target: %v", err)
			}
		})
	}
	if !bytes.Equal(transferReadFile(t, archive), original) {
		t.Fatal("source package changed")
	}
	runTransfer(t, deps, out, output.ExitConflict, "prepare", "--file", filepath.Join(source, "state.db-shm"), "--work", work.Key, "--yes")
	if err := os.Link(archive, filepath.Join(filepath.Dir(archive), "alias.zip")); err == nil {
		runTransfer(t, deps, out, output.ExitConflict, "prepare", "--file", filepath.Join(filepath.Dir(archive), "alias.zip"), "--work", work.Key, "--yes")
	}
	otherStore := t.TempDir()
	other, err := local.Open(filepath.Join(otherStore, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := other.Close(); err != nil {
		t.Fatal(err)
	}
	for _, reserved := range []string{"state.db-wal", "state.db-shm", "state.db-journal", "state.db.pre-enhanced.bak", "STATE.DB-WAL", "received.json"} {
		target := filepath.Join(otherStore, reserved)
		runTransfer(t, deps, out, output.ExitConflict, "prepare", "--file", target, "--work", work.Key, "--yes")
		runTransfer(t, receiver, receiverOut, output.ExitConflict, "receive", "--file", archive, "--into", target, "--expect-snapshot", sha)
		if _, err := os.Lstat(target); !os.IsNotExist(err) {
			t.Fatalf("protected Local path created: %v", err)
		}
	}
	other, err = local.Open(filepath.Join(otherStore, "state.db"))
	if err != nil {
		t.Fatalf("attempts damaged unrelated Local store: %v", err)
	}
	other.Close()
}

func TestTransferActivationConflictsAndRetainsOtherConfig(t *testing.T) {
	deps, out, source, work, archive := newTransferSafetyFixture(t)
	repo := t.TempDir()
	gitInit(t, repo)
	transferTestGit(t, repo, "commit", "--allow-empty", "-m", "Original")
	t.Run("commit tuple", func(t *testing.T) {
		deps.WorkingDir, _ = config.FindProjectRoot(repo)
		prepared := runTransfer(t, deps, out, output.ExitOK, "prepare", "--work", work.Key, "--file", archive, "--yes")
		receiver, receivedOut := newTestDeps(t)
		from := filepath.Join(t.TempDir(), "from")
		runTransfer(t, receiver, receivedOut, output.ExitOK, "receive", "--file", archive, "--into", from, "--expect-snapshot", prepared["snapshot_sha256"].(string))
		activation := []string{"activate", "--from", from, "--repo", repo, "--yes"}
		foreign := t.TempDir()
		gitInit(t, foreign)
		transferTestGit(t, foreign, "commit", "--allow-empty", "-m", "Unrelated")
		runTransfer(t, receiver, receivedOut, output.ExitConflict, "activate", "--from", from, "--repo", foreign, "--yes")
		writeTestFile(t, receiver.ConfigPath, `{"mode":"full","server":"https://legacy.example.invalid"}`)
		runTransfer(t, receiver, receivedOut, output.ExitIncompatible, activation...)
		legacy := `{"mode":"local","server":"https://legacy.example.invalid","credentials":{"private":{"secret":"SYNTHETIC-SECRET"}},"Projects":{"/other/repo":{"local":{"path":"/other/store"},"new_client_option":42}},"client_extension":{"keep":[1,2]}}`
		writeTestFile(t, receiver.ConfigPath, legacy)
		t.Setenv("SPECGATE_LOCAL_DIR", source)
		runTransfer(t, receiver, receivedOut, output.ExitConflict, activation...)
		if got := string(transferReadFile(t, receiver.ConfigPath)); got != legacy {
			t.Fatalf("conflict changed config: %s", got)
		}
		t.Chdir(filepath.Dir(from))
		t.Setenv("SPECGATE_LOCAL_DIR", filepath.Base(from))
		runTransfer(t, receiver, receivedOut, output.ExitConflict, activation...)
		link := filepath.Join(t.TempDir(), "state-alias")
		if err := os.Symlink(from, link); err == nil {
			t.Setenv("SPECGATE_LOCAL_DIR", link)
			runTransfer(t, receiver, receivedOut, output.ExitConflict, activation...)
		}
		caseAlias := filepath.Join(filepath.Dir(from), "FROM")
		if err := os.Mkdir(caseAlias, 0700); err == nil {
			t.Setenv("SPECGATE_LOCAL_DIR", caseAlias)
			runTransfer(t, receiver, receivedOut, output.ExitConflict, activation...)
		}
		if got := string(transferReadFile(t, receiver.ConfigPath)); got != legacy {
			t.Fatal("unsafe override changed config")
		}
		t.Setenv("SPECGATE_LOCAL_DIR", "")
		t.Setenv("SPECGATE_WORKSPACE", "elsewhere")
		runTransfer(t, receiver, receivedOut, output.ExitConflict, activation...)
		t.Setenv("SPECGATE_WORKSPACE", "")
		runTransfer(t, receiver, receivedOut, output.ExitOK, activation...)
		updated := transferReadFile(t, receiver.ConfigPath)
		var fields map[string]any
		if err := json.Unmarshal(updated, &fields); err != nil {
			t.Fatal(err)
		}
		effective, err := config.LoadFrom(receiver.ConfigPath)
		if err != nil || effective.Projects["/other/repo"].Local.Path != "/other/store" {
			t.Fatal("mixed-case projects field was shadowed")
		}
		if fields["projects"] != nil {
			t.Fatal("activation wrote a second projects field")
		}
		if !strings.Contains(string(updated), "SYNTHETIC-SECRET") || strings.Contains(receivedOut.String(), "SYNTHETIC-SECRET") ||
			fields["client_extension"] == nil || fields["Projects"].(map[string]any)["/other/repo"].(map[string]any)["new_client_option"] != float64(42) {
			t.Fatal("unrelated or legacy configuration lost or leaked")
		}
		store, err := local.Open(filepath.Join(from, "state.db"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.CreateWorkspace(t.Context(), "Workspace after initial receipt"); err != nil {
			t.Fatal(err)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		runTransfer(t, receiver, receivedOut, output.ExitOK, activation...)
	})
}

func TestTransferActivationDetectsConfigEditAndCancellation(t *testing.T) {
	deps, out, _, work, archive := newTransferSafetyFixture(t)
	repo := t.TempDir()
	gitInit(t, repo)
	transferTestGit(t, repo, "commit", "--allow-empty", "-m", "Original")
	deps.WorkingDir, _ = config.FindProjectRoot(repo)
	prepared := runTransfer(t, deps, out, output.ExitOK, "prepare", "--work", work.Key, "--file", archive, "--yes")
	receiver, prompt, result := newWorkflowTestDeps(t)
	from := filepath.Join(t.TempDir(), "unbound")
	runTransfer(t, receiver, result, output.ExitOK, "receive", "--file", archive, "--into", from, "--expect-snapshot", prepared["snapshot_sha256"].(string))
	receiver.StdinIsTTY = func() bool { return true }
	receiver.Stderr = result
	args := []string{"transfer", "activate", "--from", from, "--repo", repo}
	if code := command.ExecuteForCode(command.NewRootCommand(receiver), args...); code != output.ExitOK {
		t.Fatalf("cancel exit=%d body=%s", code, result)
	}
	if _, err := os.Stat(receiver.ConfigPath); !os.IsNotExist(err) {
		t.Fatalf("cancel created config: %v", err)
	}
	prompt.confirmValue = true
	const concurrentlyEdited = `{"mode":"local","concurrent":"keep"}`
	prompt.confirmObserver = func() { writeTestFile(t, receiver.ConfigPath, concurrentlyEdited) }
	result.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(receiver), args...); code != output.ExitConflict {
		t.Fatalf("race exit=%d body=%s", code, result)
	}
	if got := string(transferReadFile(t, receiver.ConfigPath)); got != concurrentlyEdited {
		t.Fatalf("config edit overwritten: %s", got)
	}
	runTransfer(t, receiver, result, output.ExitOK, "activate", "--from", from, "--repo", repo, "--yes")
}

func newTransferSafetyFixture(t *testing.T) (*command.Deps, *bytes.Buffer, string, local.WorkItem, string) {
	t.Helper()
	deps, out := newTestDeps(t)
	stateDir, store, _, work := newLocalChangeWork(t, deps)
	closeLocalChangeStore(t, deps, stateDir, store)
	return deps, out, stateDir, work, filepath.Join(t.TempDir(), "original.zip")
}

func transferReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func transferEntries(t *testing.T, archive string) (payload, manifest []byte) {
	t.Helper()
	reader, err := zip.OpenReader(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	for _, f := range reader.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		switch f.Name {
		case "state.db":
			payload = body
		case "manifest.json":
			manifest = body
		}
	}
	return payload, manifest
}

func editTransferManifest(t *testing.T, archive string, manifest, payload []byte, edit func(map[string]any)) {
	t.Helper()
	var fields map[string]any
	if err := json.Unmarshal(manifest, &fields); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(payload)
	fields["payload_sha256"], fields["payload_size"] = hex.EncodeToString(digest[:]), len(payload)
	if edit != nil {
		edit(fields)
	}
	data, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	writeTransferVariant(t, archive, data, payload, "state.db", 0)
}

func writeTransferVariant(t *testing.T, archive string, manifest, payload []byte, entry string, mode os.FileMode) {
	t.Helper()
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	for _, item := range []struct {
		name string
		body []byte
		mode os.FileMode
	}{{"manifest.json", manifest, 0}, {entry, payload, mode}} {
		h := &zip.FileHeader{Name: item.name, Method: zip.Deflate}
		if item.mode != 0 {
			h.SetMode(item.mode)
		}
		w, err := z.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(item.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestTransferActivationReportsCommittedButUnverified(t *testing.T) {
	deps, out, _, work, archive := newTransferSafetyFixture(t)
	repo := t.TempDir()
	gitInit(t, repo)
	transferTestGit(t, repo, "commit", "--allow-empty", "-m", "Source")
	deps.WorkingDir, _ = config.FindProjectRoot(repo)
	prepared := runTransfer(t, deps, out, output.ExitOK, "prepare", "--work", work.Key, "--file", archive, "--yes")
	receiver, result := newTestDeps(t)
	from := filepath.Join(t.TempDir(), "received")
	runTransfer(t, receiver, result, output.ExitOK, "receive", "--file", archive, "--into", from, "--expect-snapshot", prepared["snapshot_sha256"].(string))
	receiver.CommandRunner = transferPostCommitRunner{configPath: receiver.ConfigPath}
	args := []string{"activate", "--from", from, "--repo", repo, "--yes"}
	runTransfer(t, receiver, result, output.ExitUnavailable, args...)
	var envelope struct {
		Error struct {
			Details map[string]any
		}
	}
	if err := json.Unmarshal(result.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	state := envelope.Error.Details
	recovery, _ := state["recovery_command"].(string)
	if state["activation_committed"] != true || state["verified"] != false || state["new_local_dir"] != from ||
		recovery == "" || strings.Contains(recovery, from) || strings.Contains(recovery, "--workspace") {
		t.Fatalf("post-commit error ambiguously reported: %s", result)
	}
	cfg, err := config.LoadFrom(receiver.ConfigPath)
	if err != nil || cfg.Projects[deps.WorkingDir].Local.Path != from {
		t.Fatalf("committed binding was lost: %v", err)
	}
	receiver.CommandRunner = nil
	runTransfer(t, receiver, result, output.ExitOK, args...)
}

type transferPostCommitRunner struct{ configPath string }

func (r transferPostCommitRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	if _, err := os.Stat(r.configPath); err == nil {
		return nil, errors.New("disposable fixture: Git became unavailable after the config commit")
	}
	return (executil.ExecRunner{}).Output(ctx, name, args...)
}

func TestTransferRejectsSymlinkAndLosesNoRacingExistingTarget(t *testing.T) {
	deps, out, _, work, archive := newTransferSafetyFixture(t)
	prepared := runTransfer(t, deps, out, output.ExitOK, "prepare", "--work", work.Key, "--file", archive, "--yes")
	sha := prepared["snapshot_sha256"].(string)
	receiver, prompt, result := newWorkflowTestDeps(t)
	target := filepath.Join(t.TempDir(), "competing")
	archiveLink := filepath.Join(t.TempDir(), "archive-link")
	if err := os.Symlink(archive, archiveLink); err == nil {
		runTransfer(t, receiver, result, output.ExitUsage, "inspect", "--file", archiveLink)
		runTransfer(t, receiver, result, output.ExitUsage, "receive", "--file", archiveLink, "--into", target, "--expect-snapshot", sha)
	}
	receiver.StdinIsTTY = func() bool { return true }
	prompt.confirmValue = true
	prompt.confirmObserver = func() {
		if err := os.Mkdir(target, 0700); err != nil {
			t.Fatal(err)
		}
		writeTestFile(t, filepath.Join(target, "unrelated"), "preserve")
	}
	args := []string{"transfer", "receive", "--file", archive, "--into", target}
	if code := command.ExecuteForCode(command.NewRootCommand(receiver), args...); code != output.ExitConflict {
		t.Fatalf("racing target exit=%d body=%s", code, result)
	}
	if got := string(transferReadFile(t, filepath.Join(target, "unrelated"))); got != "preserve" {
		t.Fatalf("racing user data changed: %s", got)
	}
}

func TestTransferPrepareIgnoresItsOwnTemporaryStageInGitStatus(t *testing.T) {
	deps, out, _, work, _ := newTransferSafetyFixture(t)
	repo := t.TempDir()
	gitInit(t, repo)
	transferTestGit(t, repo, "commit", "--allow-empty", "-m", "Clean fixture")
	deps.WorkingDir, _ = config.FindProjectRoot(repo)
	prepared := runTransfer(t, deps, out, output.ExitOK, "prepare", "--file", filepath.Join(repo, "snapshot.zip"), "--work", work.Key, "--yes")
	got := prepared["manifest"].(map[string]any)["git"].(map[string]any)["dirty"]
	if got != false {
		t.Fatalf("own staging appeared as project changes: %v", got)
	}
}

func TestTransferRejectsPathTrimmedByConfigResolution(t *testing.T) {
	deps, out, _, work, archive := newTransferSafetyFixture(t)
	repo := t.TempDir()
	gitInit(t, repo)
	transferTestGit(t, repo, "commit", "--allow-empty", "-m", "Source")
	deps.WorkingDir, _ = config.FindProjectRoot(repo)
	prepared := runTransfer(t, deps, out, output.ExitOK, "prepare", "--file", archive, "--work", work.Key, "--yes")
	digest := prepared["snapshot_sha256"].(string)
	base := filepath.Join(t.TempDir(), "received")
	t.Chdir(filepath.Dir(base))
	runTransfer(t, deps, out, output.ExitUsage, "receive", "--file", archive, "--into", base+" ", "--expect-snapshot", digest)
	leading := " " + filepath.Base(base)
	runTransfer(t, deps, out, output.ExitUsage, "receive", "--file", archive, "--into", leading, "--expect-snapshot", digest)
	for _, path := range []string{base, base + " ", leading} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("invalid path was populated: %v", err)
		}
	}
	runTransfer(t, deps, out, output.ExitOK, "receive", "--file", archive, "--into", filepath.Base(base), "--expect-snapshot", digest)
	receipt := filepath.Join(base, "received.json")
	before := transferReadFile(t, receipt)
	receiver, receivedOut := newTestDeps(t)
	runTransfer(t, receiver, receivedOut, output.ExitUsage, "activate", "--repo", repo, "--from", base+" ", "--yes")
	if _, err := os.Lstat(receiver.ConfigPath); !os.IsNotExist(err) {
		t.Fatalf("invalid path wrote config: %v", err)
	}
	if got := transferReadFile(t, receipt); !bytes.Equal(got, before) {
		t.Fatal("invalid activation changed receipt")
	}
	runTransfer(t, receiver, receivedOut, output.ExitOK, "activate", "--repo", repo, "--from", filepath.Base(base), "--yes")
}
