package command

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type gitReceiptRunner struct {
	outputs map[string][]byte
	errors  map[string]error
}

func (r *gitReceiptRunner) Run(context.Context, string, ...string) error { return nil }

func (r *gitReceiptRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	key := strings.Join(append([]string{name}, args...), " ")
	if err := r.errors[key]; err != nil {
		return nil, err
	}
	return r.outputs[key], nil
}

func (r *gitReceiptRunner) OutputToFile(context.Context, string, string, ...string) error {
	return nil
}

func receiptCommand(dir string, args ...string) string {
	return strings.Join(append([]string{"git", "-C", dir}, args...), " ")
}

func TestGitReceiptCleanCheckout(t *testing.T) {
	dir := t.TempDir()
	runner := &gitReceiptRunner{
		outputs: map[string][]byte{
			receiptCommand(dir, "rev-parse", "--show-toplevel"):                            []byte(dir + "\n"),
			receiptCommand(dir, "remote", "get-url", "origin"):                             []byte("git@github.com:acme/project.git\n"),
			receiptCommand(dir, "branch", "--show-current"):                                []byte("main\n"),
			receiptCommand(dir, "rev-parse", "HEAD"):                                       []byte("0123456789abcdef\n"),
			receiptCommand(dir, "merge-base", "HEAD", "origin/main"):                       []byte("fedcba9876543210\n"),
			receiptCommand(dir, "status", "--porcelain=v1", "-z", "--untracked-files=all"): nil,
		},
	}

	receipt := collectGitReceipt(context.Background(), runner, dir, nil)
	if receipt.Availability != "available" {
		t.Fatalf("availability = %q, want available", receipt.Availability)
	}
	if receipt.Repository != "git@github.com:acme/project.git" {
		t.Errorf("repository = %q", receipt.Repository)
	}
	if receipt.Branch != "main" || receipt.BaseRevision != "fedcba9876543210" || receipt.HeadRevision != "0123456789abcdef" {
		t.Errorf("identity = %+v", receipt)
	}
	if len(receipt.ChangedFiles) != 0 {
		t.Errorf("changed files = %#v, want empty", receipt.ChangedFiles)
	}
	if !strings.HasPrefix(receipt.DiffDigest, "sha256:") {
		t.Errorf("diff digest = %q, want sha256 prefix", receipt.DiffDigest)
	}
	if len(receipt.Warnings) != 0 {
		t.Errorf("warnings = %#v, want none", receipt.Warnings)
	}
	if got := collectGitReceipt(context.Background(), runner, dir, nil).DiffDigest; got != receipt.DiffDigest {
		t.Errorf("digest is not stable: first %q, second %q", receipt.DiffDigest, got)
	}
}

func TestGitReceiptIncludesCommittedFilesBetweenBaseAndHead(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "committed.go"), []byte("package committed\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runner := &gitReceiptRunner{
		outputs: map[string][]byte{
			receiptCommand(dir, "rev-parse", "--show-toplevel"):                            []byte(dir + "\n"),
			receiptCommand(dir, "remote", "get-url", "origin"):                             []byte("https://github.com/acme/project.git\n"),
			receiptCommand(dir, "branch", "--show-current"):                                []byte("main\n"),
			receiptCommand(dir, "rev-parse", "HEAD"):                                       []byte("head\n"),
			receiptCommand(dir, "merge-base", "HEAD", "origin/main"):                       []byte("base\n"),
			receiptCommand(dir, "status", "--porcelain=v1", "-z", "--untracked-files=all"): nil,
			receiptCommand(dir, "diff", "--name-only", "base", "head"):                     []byte("src/committed.go\n"),
		},
	}

	receipt := collectGitReceipt(context.Background(), runner, dir, []string{"src/committed.go"})
	if len(receipt.ChangedFiles) != 1 || receipt.ChangedFiles[0] != "src/committed.go" {
		t.Fatalf("changed files = %#v, want committed path", receipt.ChangedFiles)
	}
	if len(receipt.Warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", receipt.Warnings)
	}
	if !strings.HasPrefix(receipt.DiffDigest, "sha256:") {
		t.Fatalf("diff digest = %q, want sha256 prefix", receipt.DiffDigest)
	}
}

func TestGitReceiptUsesPriorBaseForPushedCommitScope(t *testing.T) {
	dir := t.TempDir()
	runner := &gitReceiptRunner{
		outputs: map[string][]byte{
			receiptCommand(dir, "rev-parse", "--show-toplevel"):                            []byte(dir + "\n"),
			receiptCommand(dir, "remote", "get-url", "origin"):                             []byte("https://github.com/acme/project.git\n"),
			receiptCommand(dir, "branch", "--show-current"):                                []byte("feature/receipt\n"),
			receiptCommand(dir, "rev-parse", "HEAD"):                                       []byte("head\n"),
			receiptCommand(dir, "merge-base", "HEAD", "origin/feature/receipt"):            []byte("head\n"),
			receiptCommand(dir, "merge-base", "--is-ancestor", "prior-base", "head"):       nil,
			receiptCommand(dir, "diff", "--name-only", "prior-base", "head"):               []byte("src/committed.go\n"),
			receiptCommand(dir, "status", "--porcelain=v1", "-z", "--untracked-files=all"): nil,
		},
	}

	receipt := collectGitReceiptWithPriorBase(context.Background(), runner, dir, []string{"src/committed.go"}, "prior-base")
	if receipt.BaseRevision != "prior-base" {
		t.Fatalf("base revision = %q, want prior-base", receipt.BaseRevision)
	}
	if got := receipt.ChangedFiles; len(got) != 1 || got[0] != "src/committed.go" {
		t.Fatalf("changed files = %#v", got)
	}
	if len(receipt.Warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", receipt.Warnings)
	}
}

func TestGitReceiptPriorBaseKeepsUnrelatedDirtyWarning(t *testing.T) {
	dir := t.TempDir()
	runner := &gitReceiptRunner{
		outputs: map[string][]byte{
			receiptCommand(dir, "rev-parse", "--show-toplevel"):                            []byte(dir + "\n"),
			receiptCommand(dir, "remote", "get-url", "origin"):                             []byte("https://github.com/acme/project.git\n"),
			receiptCommand(dir, "branch", "--show-current"):                                []byte("feature/receipt\n"),
			receiptCommand(dir, "rev-parse", "HEAD"):                                       []byte("head\n"),
			receiptCommand(dir, "merge-base", "HEAD", "origin/feature/receipt"):            []byte("head\n"),
			receiptCommand(dir, "merge-base", "--is-ancestor", "prior-base", "head"):       nil,
			receiptCommand(dir, "diff", "--name-only", "prior-base", "head"):               []byte("src/committed.go\n"),
			receiptCommand(dir, "status", "--porcelain=v1", "-z", "--untracked-files=all"): []byte(" M README.md\x00"),
		},
	}

	receipt := collectGitReceiptWithPriorBase(context.Background(), runner, dir, []string{"src/committed.go"}, "prior-base")
	if len(receipt.Warnings) != 1 || !strings.Contains(receipt.Warnings[0], "README.md") || strings.Contains(receipt.Warnings[0], "src/committed.go") {
		t.Fatalf("warnings = %#v, want only unrelated README warning", receipt.Warnings)
	}
}

func TestGitReceiptIgnoresPriorBaseOutsideHeadHistory(t *testing.T) {
	dir := t.TempDir()
	ancestorCheck := receiptCommand(dir, "merge-base", "--is-ancestor", "unrelated", "head")
	runner := &gitReceiptRunner{
		outputs: map[string][]byte{
			receiptCommand(dir, "rev-parse", "--show-toplevel"):                            []byte(dir + "\n"),
			receiptCommand(dir, "remote", "get-url", "origin"):                             []byte("https://github.com/acme/project.git\n"),
			receiptCommand(dir, "branch", "--show-current"):                                []byte("feature/receipt\n"),
			receiptCommand(dir, "rev-parse", "HEAD"):                                       []byte("head\n"),
			receiptCommand(dir, "merge-base", "HEAD", "origin/feature/receipt"):            []byte("head\n"),
			receiptCommand(dir, "status", "--porcelain=v1", "-z", "--untracked-files=all"): nil,
		},
		errors: map[string]error{ancestorCheck: errors.New("not an ancestor")},
	}

	receipt := collectGitReceiptWithPriorBase(context.Background(), runner, dir, nil, "unrelated")
	if receipt.BaseRevision != "head" {
		t.Fatalf("base revision = %q, want current head", receipt.BaseRevision)
	}
}

func TestGitReceiptDigestChangesWhenContentChanges(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tracked.txt")
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	runner := &gitReceiptRunner{
		outputs: map[string][]byte{
			receiptCommand(dir, "rev-parse", "--show-toplevel"):                            []byte(dir + "\n"),
			receiptCommand(dir, "remote", "get-url", "origin"):                             []byte("https://github.com/acme/project.git\n"),
			receiptCommand(dir, "branch", "--show-current"):                                []byte("main\n"),
			receiptCommand(dir, "rev-parse", "HEAD"):                                       []byte("head\n"),
			receiptCommand(dir, "merge-base", "HEAD", "origin/main"):                       []byte("base\n"),
			receiptCommand(dir, "status", "--porcelain=v1", "-z", "--untracked-files=all"): []byte(" M tracked.txt\x00"),
		},
	}
	first := collectGitReceipt(context.Background(), runner, dir, []string{"tracked.txt"}).DiffDigest
	if err := os.WriteFile(path, []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	second := collectGitReceipt(context.Background(), runner, dir, []string{"tracked.txt"}).DiffDigest
	if first == second {
		t.Fatalf("digest did not change after same-size content edit: %q", first)
	}
}

func TestGitReceiptDigestReadsStatusPathsFromRepositoryRoot(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "nested")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "root.txt")
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	runner := &gitReceiptRunner{
		outputs: map[string][]byte{
			receiptCommand(dir, "rev-parse", "--show-toplevel"):                            []byte(root + "\n"),
			receiptCommand(dir, "remote", "get-url", "origin"):                             []byte("https://github.com/acme/project.git\n"),
			receiptCommand(dir, "branch", "--show-current"):                                []byte("main\n"),
			receiptCommand(dir, "rev-parse", "HEAD"):                                       []byte("head\n"),
			receiptCommand(dir, "merge-base", "HEAD", "origin/main"):                       []byte("base\n"),
			receiptCommand(dir, "status", "--porcelain=v1", "-z", "--untracked-files=all"): []byte(" M root.txt\x00"),
		},
	}
	first := collectGitReceipt(context.Background(), runner, dir, []string{"root.txt"}).DiffDigest
	if err := os.WriteFile(path, []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	second := collectGitReceipt(context.Background(), runner, dir, []string{"root.txt"}).DiffDigest
	if first == second {
		t.Fatalf("digest did not include root-relative file content: %q", first)
	}
}

func TestGitReceiptDirtyCheckoutSortsChangedFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"z.go", "staged.go", "new.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0644); err != nil {
			t.Fatal(err)
		}
	}
	runner := &gitReceiptRunner{
		outputs: map[string][]byte{
			receiptCommand(dir, "rev-parse", "--show-toplevel"):                            []byte(dir + "\n"),
			receiptCommand(dir, "remote", "get-url", "origin"):                             []byte("https://github.com/acme/project.git\n"),
			receiptCommand(dir, "branch", "--show-current"):                                []byte("feature/receipt\n"),
			receiptCommand(dir, "rev-parse", "HEAD"):                                       []byte("head\n"),
			receiptCommand(dir, "merge-base", "HEAD", "origin/feature/receipt"):            []byte("base\n"),
			receiptCommand(dir, "status", "--porcelain=v1", "-z", "--untracked-files=all"): []byte(" M z.go\x00A  staged.go\x00?? new.txt\x00"),
		},
	}

	receipt := collectGitReceipt(context.Background(), runner, dir, []string{"z.go", "staged.go", "new.txt"})
	want := []string{"new.txt", "staged.go", "z.go"}
	if strings.Join(receipt.ChangedFiles, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("changed files = %#v, want %#v", receipt.ChangedFiles, want)
	}
	if len(receipt.Warnings) != 0 {
		t.Errorf("warnings = %#v, want none", receipt.Warnings)
	}
}

func TestGitReceiptWarnsOnReportedFileMismatch(t *testing.T) {
	dir := t.TempDir()
	runner := &gitReceiptRunner{
		outputs: map[string][]byte{
			receiptCommand(dir, "rev-parse", "--show-toplevel"):                            []byte(dir + "\n"),
			receiptCommand(dir, "remote", "get-url", "origin"):                             []byte("https://github.com/acme/project.git\n"),
			receiptCommand(dir, "branch", "--show-current"):                                []byte("main\n"),
			receiptCommand(dir, "rev-parse", "HEAD"):                                       []byte("head\n"),
			receiptCommand(dir, "merge-base", "HEAD", "origin/main"):                       []byte("base\n"),
			receiptCommand(dir, "status", "--porcelain=v1", "-z", "--untracked-files=all"): []byte(" M actual.go\x00"),
		},
	}

	receipt := collectGitReceipt(context.Background(), runner, dir, []string{"reported.go"})
	if len(receipt.Warnings) == 0 || !strings.Contains(receipt.Warnings[0], "reported") || !strings.Contains(receipt.Warnings[0], "actual.go") {
		t.Errorf("warnings = %#v, want reported-file mismatch warning", receipt.Warnings)
	}
}

func TestReportedFilesWarningLabelsUnrelatedCheckoutChanges(t *testing.T) {
	warning := reportedFilesWarning([]string{"reported.go"}, []string{"reported.go", "unrelated.go"})
	if !strings.Contains(warning, "unrelated changes outside reported delivery scope") || !strings.Contains(warning, "unrelated.go") {
		t.Fatalf("warning = %q", warning)
	}
	if strings.Contains(warning, "do not match") {
		t.Fatalf("scoped report should not be labeled mismatched: %q", warning)
	}
}

func TestGitReceiptWarnsWhenReportedFilesAreEmpty(t *testing.T) {
	dir := t.TempDir()
	runner := &gitReceiptRunner{
		outputs: map[string][]byte{
			receiptCommand(dir, "rev-parse", "--show-toplevel"):                            []byte(dir + "\n"),
			receiptCommand(dir, "remote", "get-url", "origin"):                             []byte("https://github.com/acme/project.git\n"),
			receiptCommand(dir, "branch", "--show-current"):                                []byte("main\n"),
			receiptCommand(dir, "rev-parse", "HEAD"):                                       []byte("head\n"),
			receiptCommand(dir, "merge-base", "HEAD", "origin/main"):                       []byte("base\n"),
			receiptCommand(dir, "status", "--porcelain=v1", "-z", "--untracked-files=all"): []byte(" M actual.go\x00"),
		},
	}
	receipt := collectGitReceipt(context.Background(), runner, dir, []string{})
	if len(receipt.Warnings) == 0 || !strings.Contains(receipt.Warnings[0], "actual.go") {
		t.Errorf("warnings = %#v, want empty-reported mismatch warning", receipt.Warnings)
	}
}

func TestGitReceiptSkipsScopeComparisonWhenReportedFilesAreUnspecified(t *testing.T) {
	dir := t.TempDir()
	runner := &gitReceiptRunner{
		outputs: map[string][]byte{
			receiptCommand(dir, "rev-parse", "--show-toplevel"):                            []byte(dir + "\n"),
			receiptCommand(dir, "remote", "get-url", "origin"):                             []byte("https://github.com/acme/project.git\n"),
			receiptCommand(dir, "branch", "--show-current"):                                []byte("main\n"),
			receiptCommand(dir, "rev-parse", "HEAD"):                                       []byte("head\n"),
			receiptCommand(dir, "merge-base", "HEAD", "origin/main"):                       []byte("base\n"),
			receiptCommand(dir, "status", "--porcelain=v1", "-z", "--untracked-files=all"): []byte(" M actual.go\x00"),
		},
	}
	receipt := collectGitReceipt(context.Background(), runner, dir, nil)
	if len(receipt.Warnings) != 0 {
		t.Errorf("warnings = %#v, want none until delivery scope is supplied", receipt.Warnings)
	}
}

func TestGitReceiptUnavailableOutsideGit(t *testing.T) {
	dir := t.TempDir()
	runner := &gitReceiptRunner{
		errors: map[string]error{
			receiptCommand(dir, "rev-parse", "--show-toplevel"): errors.New("fatal: not a git repository"),
		},
	}

	receipt := collectGitReceipt(context.Background(), runner, dir, []string{"reported.go"})
	if receipt.Availability != "unavailable" {
		t.Fatalf("availability = %q, want unavailable", receipt.Availability)
	}
	if receipt.Repository != "" || receipt.Branch != "" || receipt.BaseRevision != "" || receipt.HeadRevision != "" {
		t.Errorf("receipt fabricated git identity: %+v", receipt)
	}
	if len(receipt.Warnings) == 0 || !strings.Contains(strings.ToLower(receipt.Warnings[0]), "git") {
		t.Errorf("warnings = %#v, want non-git warning", receipt.Warnings)
	}
}

func TestGitReceiptUnavailableWithoutOrigin(t *testing.T) {
	dir := t.TempDir()
	runner := &gitReceiptRunner{
		outputs: map[string][]byte{
			receiptCommand(dir, "rev-parse", "--show-toplevel"): []byte(dir + "\n"),
		},
		errors: map[string]error{
			receiptCommand(dir, "remote", "get-url", "origin"): errors.New("fatal: No such remote 'origin'"),
		},
	}

	receipt := collectGitReceipt(context.Background(), runner, dir, nil)
	if receipt.Availability != "unavailable" {
		t.Fatalf("availability = %q, want unavailable", receipt.Availability)
	}
	if receipt.Repository != "" || receipt.HeadRevision != "" {
		t.Errorf("receipt fabricated remote/revision: %+v", receipt)
	}
	if len(receipt.Warnings) == 0 || !strings.Contains(strings.ToLower(receipt.Warnings[0]), "origin") {
		t.Errorf("warnings = %#v, want missing-origin warning", receipt.Warnings)
	}
}
