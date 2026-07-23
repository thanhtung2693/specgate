package command

import (
	"context"
	"strings"
	"testing"
)

func TestCompareCheckoutReceiptMatchesCurrentCheckout(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	runner := &gitReceiptRunner{outputs: map[string][]byte{
		receiptCommand(dir, "rev-parse", "--show-toplevel"):                            []byte(dir + "\n"),
		receiptCommand(dir, "remote", "get-url", "origin"):                             []byte("https://github.com/acme/project.git\n"),
		receiptCommand(dir, "branch", "--show-current"):                                []byte("feature/delivery\n"),
		receiptCommand(dir, "rev-parse", "HEAD"):                                       []byte("head-1\n"),
		receiptCommand(dir, "merge-base", "HEAD", "origin/feature/delivery"):           []byte("base-1\n"),
		receiptCommand(dir, "status", "--porcelain=v1", "-z", "--untracked-files=all"): nil,
		receiptCommand(dir, "diff", "--name-only", "base-1", "head-1"):                 []byte("src/health.go\n"),
	}}
	deps := &Deps{WorkingDir: dir, DeployRunner: runner}
	current := collectGitReceipt(context.Background(), runner, dir, nil)

	got := compareCheckoutReceipt(context.Background(), deps, current)
	if !got.Checked || !got.Matches || got.Stale {
		t.Fatalf("comparison = %#v, want checked match", got)
	}
	if !strings.Contains(strings.ToLower(got.Message), "matches the current checkout") {
		t.Fatalf("message = %q", got.Message)
	}
}

func TestCompareCheckoutReceiptReportsChangedHead(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	runner := &gitReceiptRunner{outputs: map[string][]byte{
		receiptCommand(dir, "rev-parse", "--show-toplevel"):                            []byte(dir + "\n"),
		receiptCommand(dir, "remote", "get-url", "origin"):                             []byte("https://github.com/acme/project.git\n"),
		receiptCommand(dir, "branch", "--show-current"):                                []byte("feature/delivery\n"),
		receiptCommand(dir, "rev-parse", "HEAD"):                                       []byte("head-2\n"),
		receiptCommand(dir, "merge-base", "HEAD", "origin/feature/delivery"):           []byte("base-1\n"),
		receiptCommand(dir, "status", "--porcelain=v1", "-z", "--untracked-files=all"): nil,
		receiptCommand(dir, "diff", "--name-only", "base-1", "head-2"):                 []byte("src/health.go\n"),
	}}
	deps := &Deps{WorkingDir: dir, DeployRunner: runner}
	stored := gitReceipt{
		Availability: "available",
		Repository:   "https://github.com/acme/project.git",
		Branch:       "feature/delivery",
		BaseRevision: "base-1",
		HeadRevision: "head-1",
		DiffDigest:   "sha256:stored",
	}

	got := compareCheckoutReceipt(context.Background(), deps, stored)
	if !got.Checked || got.Matches || !got.Stale {
		t.Fatalf("comparison = %#v, want checked mismatch", got)
	}
	if !strings.Contains(got.Message, "HEAD") || !strings.Contains(got.Reason, "Checkout differs") {
		t.Fatalf("comparison = %#v, want actionable mismatch", got)
	}
}

func TestCompareCheckoutReceiptDoesNotClaimCheckWhenGitUnavailable(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	runner := &gitReceiptRunner{errors: map[string]error{
		receiptCommand(dir, "rev-parse", "--show-toplevel"): context.Canceled,
	}}
	deps := &Deps{WorkingDir: dir, DeployRunner: runner}
	stored := gitReceipt{Availability: "available", HeadRevision: "head-1"}

	got := compareCheckoutReceipt(context.Background(), deps, stored)
	if got.Checked || got.Matches || got.Stale {
		t.Fatalf("comparison = %#v, want unavailable comparison", got)
	}
	if !strings.Contains(strings.ToLower(got.Message), "could not compare") {
		t.Fatalf("message = %q", got.Message)
	}
}
