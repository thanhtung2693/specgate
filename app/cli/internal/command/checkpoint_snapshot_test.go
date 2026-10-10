package command

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestObservedRunDoesNotDetectWatchedChangeRevertedDuringCommand(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	git("init", "-q")
	git("config", "user.email", "fixture@example.test")
	git("config", "user.name", "Fixture")
	path := filepath.Join(root, "test-script.sh")
	const original = "original test script"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-qm", "baseline")
	deps := &Deps{WorkingDir: root, Printer: output.NewWithColor(io.Discard, io.Discard, output.ModeJSON, false), Stderr: io.Discard}
	deps.RunCheckCommand = func(ctx context.Context, command string) (int, string) {
		out, err := exec.CommandContext(ctx, "sh", "-c", command).CombinedOutput()
		if err != nil {
			return 1, string(out)
		}
		return 0, string(out)
	}
	digest := sha256.Sum256([]byte(original))
	contract := local.VerificationContract{
		Version: 2, WorkID: "work", ContextDigest: "context", Digest: "pin",
		WatchedPaths: []local.WatchedPath{{Path: "test-script.sh", Digest: hex.EncodeToString(digest[:])}},
		Checks:       []local.VerificationCheck{{Name: "unit", Command: "printf changed > test-script.sh; printf 'original test script' > test-script.sh; printf '%s' '<testsuite><testcase classname=\"p\" name=\"test\"/></testsuite>' > \"$SPECGATE_TEST_REPORT\"", Cwd: ".", TestReport: &local.JUnitTestReport{Format: "junit", Selectors: map[string][]local.SelectedTestCase{"local-1": {{ClassName: "p", Name: "test"}}}}}},
	}
	check := map[string]any{"name": "unit", "command": contract.Checks[0].Command, "cwd": ".", "status": "pass"}
	body := map[string]any{"checks": []any{check}}
	executeCompletionChecks(t.Context(), deps, body, &contract, root)
	run, ok := check["test_run"].(local.ObservedRun)
	if !ok || run.Freshness != "matching_endpoints" || run.Before.Fingerprint != run.After.Fingerprint {
		t.Fatalf("reverted edit escaped endpoint limitation: %#v", check["test_run"])
	}
	if len(run.WatchedBefore) != 1 || len(run.WatchedAfter) != 1 || run.WatchedBefore[0].State != "unchanged" || run.WatchedAfter[0].State != "unchanged" {
		t.Fatalf("reverted watched file not visibly limited to endpoints: %#v %#v", run.WatchedBefore, run.WatchedAfter)
	}
	if check["status"] != "pass" {
		t.Fatalf("valid selected observation failed unexpectedly: %#v", check)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestObservedRunFailsChangedEndpointsAndPreservesUnrelatedOutput(t *testing.T) {
	root := t.TempDir()
	if b, e := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); e != nil {
		t.Fatalf("git init: %v %s", e, b)
	}
	for _, args := range [][]string{{"config", "user.email", "fixture@example.test"}, {"config", "user.name", "Fixture"}, {"commit", "--allow-empty", "-qm", "baseline"}} {
		if b, e := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); e != nil {
			t.Fatalf("git: %v %s", e, b)
		}
	}
	deps := &Deps{WorkingDir: root, Printer: output.NewWithColor(io.Discard, io.Discard, output.ModeJSON, false), Stderr: io.Discard}
	command := `printf '%s' '<testsuite><testcase classname="p" name="test"/></testsuite>' > "$SPECGATE_TEST_REPORT"; printf keep > "$(dirname "$SPECGATE_TEST_REPORT")/keep.txt"; printf changed > source.txt`
	contract := local.VerificationContract{Version: 2, WorkID: "w", ContextDigest: "c", Digest: "v", Checks: []local.VerificationCheck{{Name: "unit", Command: command, Cwd: ".", TestReport: &local.JUnitTestReport{Format: "junit", Selectors: map[string][]local.SelectedTestCase{"local-1": {{ClassName: "p", Name: "test"}}}}}}}
	check := map[string]any{"name": "unit", "command": command, "cwd": ".", "status": "pass"}
	executeCompletionChecks(context.Background(), deps, map[string]any{"checks": []any{check}}, &contract, root)
	if check["status"] != "fail" {
		t.Fatalf("changed endpoint passed: %#v", check)
	}
	files, err := filepath.Glob(filepath.Join(root, ".specgate", "junit-run-*", "keep.txt"))
	if err != nil || len(files) != 1 {
		t.Fatalf("unrelated output removed: %v %v", files, err)
	}
}

func TestJUnitRunCannotReadOrDeleteReportThroughSwappedDirectory(t *testing.T) {
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	outside := t.TempDir()
	report := filepath.Join(outside, "report.xml")
	if err := os.WriteFile(report, []byte(`<testsuite><testcase classname="p" name="test"/></testsuite>`), 0600); err != nil {
		t.Fatal(err)
	}
	deps := &Deps{WorkingDir: root, Printer: output.NewWithColor(io.Discard, io.Discard, output.ModeJSON, false), Stderr: io.Discard}
	deps.RunCheckCommand = func(context.Context, string) (int, string) {
		matches, err := filepath.Glob(filepath.Join(root, ".specgate", "junit-run-*"))
		if err != nil || len(matches) != 1 {
			t.Fatalf("run directory: %v %v", matches, err)
		}
		if err := os.Rename(matches[0], matches[0]+"-moved"); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, matches[0]); err != nil {
			t.Fatal(err)
		}
		return 0, ""
	}
	contract := local.VerificationContract{Version: 2, WorkID: "w", ContextDigest: "c", Digest: "v", Checks: []local.VerificationCheck{{Name: "unit", Command: "true", Cwd: ".", TestReport: &local.JUnitTestReport{Format: "junit", Selectors: map[string][]local.SelectedTestCase{"local-1": {{ClassName: "p", Name: "test"}}}}}}}
	check := map[string]any{"name": "unit", "command": "true", "cwd": ".", "status": "pass"}
	executeCompletionChecks(t.Context(), deps, map[string]any{"checks": []any{check}}, &contract, root)
	if check["status"] != "fail" || check["test_observation"] != nil {
		t.Fatalf("outside report was accepted: %#v", check)
	}
	if _, err := os.Stat(report); err != nil {
		t.Fatalf("outside report was deleted: %v", err)
	}
}

func TestCheckpointDeltaComparesBytesNotDirtyMembership(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		if b, e := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); e != nil {
			t.Fatalf("git: %v %s", e, b)
		}
	}
	git("init", "-q")
	git("config", "user.email", "fixture@example.test")
	git("config", "user.name", "Fixture")
	path := filepath.Join(root, "file.txt")
	write := func(s string) {
		t.Helper()
		if e := os.WriteFile(path, []byte(s), 0600); e != nil {
			t.Fatal(e)
		}
	}
	write("original")
	git("add", ".")
	git("commit", "-qm", "baseline")
	deps := &Deps{WorkingDir: root}
	write("dirty baseline")
	before := captureCheckout(t.Context(), deps)
	write("original")
	after := captureCheckout(t.Context(), deps)
	delta := compareCheckout(t.Context(), deps, before, after)
	if delta.State != "changed" || len(delta.Modified) != 1 || delta.Modified[0] != "file.txt" || len(delta.Removed) != 0 {
		t.Fatalf("dirty->clean delta: %#v", delta)
	}
	git("commit", "--allow-empty", "-qm", "same bytes")
	clean := captureCheckout(t.Context(), deps)
	delta = compareCheckout(t.Context(), deps, after, clean)
	if len(delta.Modified) != 0 || len(delta.Added) != 0 || len(delta.Removed) != 0 {
		t.Fatalf("same content commit invents changes: %#v", delta)
	}
	other := clean
	other.CheckoutID = "different"
	if got := compareCheckout(t.Context(), deps, clean, other); got.State != "noncomparable" {
		t.Fatalf("different checkout: %#v", got)
	}
}

func TestCheckpointDeltaIgnoresMtimeOnlyChange(t *testing.T) {
	root := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "fixture@example.test"}, {"config", "user.name", "Fixture"}} {
		if out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	path := filepath.Join(root, "file.txt")
	if err := os.WriteFile(path, []byte("same bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "file.txt"}, {"commit", "-qm", "baseline"}} {
		if out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	deps := &Deps{WorkingDir: root}
	if err := os.WriteFile(path, []byte("same dirty bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	before := captureCheckout(t.Context(), deps)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	mtime := info.ModTime().Add(-time.Hour)
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	after := captureCheckout(t.Context(), deps)
	if before.Fingerprint == after.Fingerprint {
		t.Fatal("fixture did not change the checkout fingerprint")
	}
	got := compareCheckout(t.Context(), deps, before, after)
	if got.State != "unchanged" || got.AddedCount != 0 || got.RemovedCount != 0 || got.ModifiedCount != 0 {
		t.Fatalf("mtime-only delta: %#v", got)
	}
}

func TestCheckpointCapturesDirtyFileReplacedByDirectory(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		if b, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git: %v %s", err, b)
		}
	}
	git("init", "-q")
	git("config", "user.email", "fixture@example.test")
	git("config", "user.name", "Fixture")
	path := filepath.Join(root, "node")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", "node")
	git("commit", "-qm", "baseline")
	deps := &Deps{WorkingDir: root}
	before := captureCheckout(t.Context(), deps)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "child"), []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	after := captureCheckout(t.Context(), deps)
	if after.State != "available" || after.Files["node"] != "" || after.Files["node/child"] == "" {
		t.Fatalf("dirty file-to-directory snapshot: %#v", after)
	}
	got := compareCheckout(t.Context(), deps, before, after)
	if got.State != "changed" || len(got.Removed) != 1 || got.Removed[0] != "node" || len(got.Added) != 1 || got.Added[0] != "node/child" {
		t.Fatalf("dirty file-to-directory delta: %#v", got)
	}
}

func TestCheckoutBlobDoesNotTreatNestedGitDirectoryAsDeletedFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "nested")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, ".git"), []byte("gitdir: elsewhere"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := checkoutBlob(root, "nested", false); err == nil {
		t.Fatal("nested Git directory was recorded as a deleted file")
	}
}

func TestCheckpointDeltaFromNestedDirectoryAndCommittedRename(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		if b, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git: %v %s", err, b)
		}
	}
	git("init", "-q")
	git("config", "user.email", "fixture@example.test")
	git("config", "user.name", "Fixture")
	git("config", "diff.renames", "true")
	nested := filepath.Join(root, "nested")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(nested, "old.txt")
	write := func(value string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("original")
	git("add", ".")
	git("commit", "-qm", "baseline")
	deps := &Deps{WorkingDir: nested}
	clean := captureCheckout(t.Context(), deps)
	write("changed")
	dirty := captureCheckout(t.Context(), deps)
	for _, pair := range [][2]local.CheckoutSnapshot{{clean, dirty}, {dirty, clean}} {
		got := compareCheckout(t.Context(), deps, pair[0], pair[1])
		if len(got.Modified) != 1 || got.Modified[0] != "nested/old.txt" || len(got.Added) != 0 || len(got.Removed) != 0 {
			t.Errorf("nested delta: %#v", got)
		}
	}
	git("add", ".")
	git("commit", "-qm", "modify")
	committed := captureCheckout(t.Context(), deps)
	got := compareCheckout(t.Context(), deps, clean, committed)
	if len(got.Modified) != 1 || got.Modified[0] != "nested/old.txt" {
		t.Errorf("committed modification: %#v", got)
	}
	git("mv", "nested/old.txt", "nested/new.txt")
	git("commit", "-qm", "rename")
	renamed := captureCheckout(t.Context(), deps)
	got = compareCheckout(t.Context(), &Deps{WorkingDir: root}, committed, renamed)
	if len(got.Added) != 1 || got.Added[0] != "nested/new.txt" || len(got.Removed) != 1 || got.Removed[0] != "nested/old.txt" {
		t.Errorf("rename delta: %#v", got)
	}
	if err := os.Remove(filepath.Join(root, "nested", "new.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "nested", "new.txt"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "nested", "new.txt", "child.txt"), []byte("child"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "-qm", "file to directory")
	directory := captureCheckout(t.Context(), &Deps{WorkingDir: root})
	got = compareCheckout(t.Context(), &Deps{WorkingDir: root}, renamed, directory)
	if len(got.Removed) != 1 || got.Removed[0] != "nested/new.txt" || len(got.Added) != 1 || got.Added[0] != "nested/new.txt/child.txt" || len(got.Modified) != 0 {
		t.Errorf("file to directory delta: %#v", got)
	}
}

func TestReceiptTreeDeltaIgnoresUnchangedDirtyFiles(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		if b, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git: %v %s", err, b)
		}
	}
	git("init", "-q")
	git("config", "user.email", "fixture@example.test")
	git("config", "user.name", "Fixture")
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("base"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-qm", "baseline")
	deps := &Deps{WorkingDir: root}
	before := captureCheckout(t.Context(), deps)
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("dirty"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "untracked.txt"), []byte("dirty"), 0600); err != nil {
		t.Fatal(err)
	}
	after := captureCheckout(t.Context(), deps)
	got := compareCommittedTrees(t.Context(), deps, before, after)
	if got.State != "unchanged" || len(got.Added) != 0 || len(got.Removed) != 0 || len(got.Modified) != 0 {
		t.Fatalf("receipt tree delta included dirty checkout state: %#v", got)
	}
}

func TestCompletionReceiptBaselineUsesOnlyComparableGitTree(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		if b, e := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); e != nil {
			t.Fatalf("git: %v %s", e, b)
		}
	}
	git("init", "-q")
	git("config", "user.email", "fixture@example.test")
	git("config", "user.name", "Fixture")
	git("remote", "add", "origin", "https://example.test/specgate.git")
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("baseline"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-qm", "baseline")
	deps := &Deps{WorkingDir: root}
	stored := collectGitReceipt(t.Context(), nil, root, nil)
	current := captureCheckout(t.Context(), deps)
	baseline, ok := completionReceiptBaseline(stored, stored, current)
	if !ok || baseline.State != "available" || baseline.Head != stored.HeadRevision || baseline.CheckoutID != current.CheckoutID {
		t.Fatalf("baseline = %#v, ok=%t", baseline, ok)
	}
	if baseline.Files != nil {
		t.Fatalf("completion receipt invented a dirty manifest: %#v", baseline.Files)
	}
	different := stored
	different.Branch = "other"
	if _, ok := completionReceiptBaseline(different, stored, current); ok {
		t.Fatal("different branch receipt was comparable")
	}
}

func TestResumeDeltaSummarizesLargePathSetsUnlessDetailed(t *testing.T) {
	delta := checkpointDelta{State: "changed", BaselineKind: "checkpoint"}
	for i := 0; i < 101; i++ {
		delta.Modified = append(delta.Modified, fmt.Sprintf("file-%03d", i))
	}
	compact := resumeDeltaOutput(delta, false)
	if !compact.Truncated || compact.ModifiedCount != 101 || len(compact.Modified) != 0 || !strings.Contains(compact.Limit, "--detail") {
		t.Fatalf("compact delta = %#v", compact)
	}
	detail := resumeDeltaOutput(delta, true)
	if detail.Truncated || detail.ModifiedCount != 101 || len(detail.Modified) != 101 {
		t.Fatalf("detail delta = %#v", detail)
	}
}

func TestCheckpointOutputDoesNotEchoCapturedPathManifest(t *testing.T) {
	checkpoint := local.Checkpoint{ChangedFiles: []string{"one", "two"}, Snapshot: &local.CheckoutSnapshot{State: "available", Files: map[string]string{"one": "hash", "two": "hash"}}}
	output := checkpointOutput(checkpoint)
	if output.Checkpoint.Snapshot.Files != nil || output.Checkpoint.ChangedFiles != nil || output.DirtyFileCount != 2 || output.ChangedFileCount != 2 {
		t.Fatalf("checkpoint output leaked paths: %#v", output)
	}
	encoded, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"dirty_file_count":2`, `"changed_file_count":2`} {
		if !strings.Contains(string(encoded), expected) {
			t.Fatalf("checkpoint JSON omitted %s: %s", expected, encoded)
		}
	}
	for _, forbidden := range []string{`"changed_files"`, `"files"`, `"one"`, `"two"`, `"hash"`} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("checkpoint JSON leaked %s: %s", forbidden, encoded)
		}
	}
}
