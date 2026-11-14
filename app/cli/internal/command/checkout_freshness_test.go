package command

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
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
	deps := &Deps{WorkingDir: dir, CommandRunner: runner}
	current := collectGitReceipt(context.Background(), runner, dir, nil, "")

	got := compareCheckoutReceipt(context.Background(), deps, current, "")
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
	deps := &Deps{WorkingDir: dir, CommandRunner: runner}
	stored := gitReceipt{
		Availability: "available",
		Repository:   "https://github.com/acme/project.git",
		Branch:       "feature/delivery",
		BaseRevision: "base-1",
		HeadRevision: "head-1",
		DiffDigest:   "sha256:stored",
	}

	got := compareCheckoutReceipt(context.Background(), deps, stored, "")
	if !got.Checked || got.Matches || !got.Stale {
		t.Fatalf("comparison = %#v, want checked mismatch", got)
	}
	if !strings.Contains(got.Message, "HEAD") || !strings.Contains(got.Reason, "Checkout differs") {
		t.Fatalf("comparison = %#v, want actionable mismatch", got)
	}
}

func TestCompareCheckoutReceiptMatchesOriginlessLocalCheckout(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "local.go"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	runner := &gitReceiptRunner{
		outputs: map[string][]byte{
			receiptCommand(dir, "rev-parse", "--show-toplevel"):                            []byte(dir + "\n"),
			receiptCommand(dir, "branch", "--show-current"):                                []byte("solo\n"),
			receiptCommand(dir, "rev-parse", "HEAD"):                                       []byte("head-1\n"),
			receiptCommand(dir, "status", "--porcelain=v1", "-z", "--untracked-files=all"): []byte(" M local.go\x00"),
		},
		errors: map[string]error{
			receiptCommand(dir, "remote", "get-url", "origin"): context.Canceled,
		},
	}
	deps := &Deps{WorkingDir: dir, CommandRunner: runner}
	stored := collectGitReceipt(context.Background(), runner, dir, nil, "")

	got := compareCheckoutReceipt(context.Background(), deps, stored, "")
	if !got.Checked || !got.Matches || got.Stale {
		t.Fatalf("comparison = %#v, want checked local match", got)
	}
	if !strings.Contains(strings.ToLower(got.Message), "this local checkout") {
		t.Fatalf("message = %q", got.Message)
	}
}

func TestOriginlessReceiptRequiresSameCheckoutAfterReadback(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "no-config"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	git("-C", root, "init", "-q")
	git("-C", root, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-qm", "fixture")
	other := filepath.Join(t.TempDir(), "clone")
	git("clone", "--quiet", "--no-hardlinks", root, other)
	git("-C", other, "remote", "remove", "origin")
	stored := collectGitReceipt(context.Background(), nil, root, nil, "")
	current := collectGitReceipt(context.Background(), nil, other, nil, "")
	if stored.CheckoutID == "" || stored.CheckoutID == current.CheckoutID || stored.HeadRevision != current.HeadRevision || stored.DiffDigest != current.DiffDigest || stored.Branch != current.Branch {
		t.Fatalf("expected distinct clean originless checkouts with equal endpoints: %#v / %#v", stored, current)
	}
	encoded, err := json.Marshal(map[string]any{"git_receipt": stored})
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(encoded, &body); err != nil {
		t.Fatal(err)
	}
	readback := mapGitReceipt(body)
	missingIdentity := readback
	missingIdentity.CheckoutID = ""
	if got := compareCheckoutReceipt(context.Background(), &Deps{WorkingDir: root}, readback, ""); !got.Checked || !got.Matches || got.Stale {
		t.Fatalf("same checkout after readback: %#v", got)
	}
	for _, test := range []struct {
		name    string
		dir     string
		receipt gitReceipt
	}{
		{"different checkout", other, readback},
		{"missing identity", root, missingIdentity},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := compareCheckoutReceipt(context.Background(), &Deps{WorkingDir: test.dir}, test.receipt, "")
			if got.Checked || got.Matches || got.Stale {
				t.Fatalf("noncomparable receipt must remain unchecked: %#v", got)
			}
			status := applyCheckoutFreshness(context.Background(), &Deps{WorkingDir: test.dir}, changeStatusResult{CompletionID: "completion-1", Receipt: "Recorded Git receipt"}, test.receipt, "")
			for _, risk := range acceptanceRisks(status) {
				if risk.Category == "unknown" && risk.Count != 1 {
					t.Fatalf("noncomparable legacy risk: %#v", risk)
				}
			}
		})
	}
}

func TestMapGitReceiptPreservesFreshnessScope(t *testing.T) {
	got := mapGitReceipt(map[string]any{"git_receipt": map[string]any{
		"availability":    "available",
		"freshness_scope": "local_checkout",
		"head_revision":   "head-1",
	}})
	if got.FreshnessScope != "local_checkout" {
		t.Fatalf("freshness scope = %q, want local_checkout", got.FreshnessScope)
	}
}

func TestMapGitReceiptDoesNotInventMetadata(t *testing.T) {
	for _, value := range []any{nil, 42, false, []any{"head"}, map[string]any{"head": "value"}} {
		raw := map[string]any{}
		for _, field := range []string{"repository", "checkout_id", "availability", "freshness_scope", "branch", "base_revision", "head_revision", "diff_digest"} {
			raw[field] = value
		}
		if got := mapGitReceipt(map[string]any{"git_receipt": raw}); !reflect.DeepEqual(got, gitReceipt{}) {
			t.Fatalf("metadata value %#v produced invented receipt: %#v", value, got)
		}
	}
	got := mapGitReceipt(map[string]any{"git_receipt": map[string]any{}})
	if !reflect.DeepEqual(got, gitReceipt{}) {
		t.Fatalf("empty metadata = %#v, want absent fields", got)
	}
	if comparison := compareCheckoutReceipt(context.Background(), &Deps{}, got, ""); comparison.Checked || comparison.Matches || comparison.Stale {
		t.Fatalf("missing HEAD must remain unchecked: %#v", comparison)
	}
}

func TestMapGitReceiptPreservesLegacyOptionalFields(t *testing.T) {
	dir := t.TempDir()
	runner := &gitReceiptRunner{outputs: map[string][]byte{
		receiptCommand(dir, "rev-parse", "--show-toplevel"): []byte(dir + "\n"),
		receiptCommand(dir, "remote", "get-url", "origin"):  []byte("https://github.com/acme/project.git\n"),
		receiptCommand(dir, "branch", "--show-current"):     []byte("main\n"),
		receiptCommand(dir, "rev-parse", "HEAD"):            []byte("head-1\n"),
	}}
	stored := mapGitReceipt(map[string]any{"git_receipt": map[string]any{"head_revision": " head-1 "}})
	if receiptFreshnessScope(stored) != "shared_repository" {
		t.Fatalf("legacy scope = %q", receiptFreshnessScope(stored))
	}
	comparison := compareCheckoutReceipt(context.Background(), &Deps{WorkingDir: dir, CommandRunner: runner}, stored, "")
	if comparison.Checked || comparison.Matches || comparison.Stale {
		t.Fatalf("legacy HEAD without a working-tree digest claimed checkout freshness: %#v", comparison)
	}
}

func TestCompareCheckoutReceiptDoesNotClaimCheckWhenGitUnavailable(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	runner := &gitReceiptRunner{errors: map[string]error{
		receiptCommand(dir, "rev-parse", "--show-toplevel"): context.Canceled,
	}}
	deps := &Deps{WorkingDir: dir, CommandRunner: runner}
	stored := gitReceipt{Availability: "available", HeadRevision: "head-1"}

	got := compareCheckoutReceipt(context.Background(), deps, stored, "")
	if got.Checked || got.Matches || got.Stale {
		t.Fatalf("comparison = %#v, want unavailable comparison", got)
	}
	if !strings.Contains(strings.ToLower(got.Message), "could not compare") {
		t.Fatalf("message = %q", got.Message)
	}
}

func TestHistoricalSharedReceiptHintsAtMatchingCommitsWithoutClaimingFreshness(t *testing.T) {
	root := t.TempDir()
	runner := &gitReceiptRunner{outputs: map[string][]byte{
		"git -C " + root + " rev-parse --show-toplevel":                      []byte(root),
		"git -C " + root + " remote get-url origin":                          []byte("https://example.test/project.git"),
		"git -C " + root + " branch --show-current":                          []byte("main"),
		"git -C " + root + " rev-parse HEAD":                                 []byte("head"),
		"git -C " + root + " status --porcelain=v1 -z --untracked-files=all": []byte(""),
		"git -C " + root + " merge-base HEAD origin/main":                    []byte("base"),
	}}
	stored := collectGitReceipt(t.Context(), runner, root, nil, "source")
	stored.DiffDigest = "source dirty checkout differs"
	deps := &Deps{WorkingDir: root, CommandRunner: runner}
	got := compareCheckoutReceipt(t.Context(), deps, stored, "receiver")
	if got.Checked || got.Matches || got.Stale || !strings.Contains(got.Message, "Shared-repository Git endpoints match the historical receipt. Working-tree comparability is unverified.") {
		t.Fatalf("historical Git endpoint comparison: %+v", got)
	}
	matching := collectGitReceipt(t.Context(), runner, root, nil, "source")
	status := applyCheckoutFreshness(t.Context(), deps, changeStatusResult{Assurance: "Agent-reported; locally reproduced"}, matching, "receiver")
	if status.FreshnessUnchecked || !status.HistoricalVerification || !strings.Contains(status.Freshness, "working-tree digest match the historical receipt") || strings.Contains(status.Assurance, "locally reproduced") {
		t.Fatalf("full endpoint match lost historical caveat: %+v", status)
	}
	noBase := matching
	noBase.BaseRevision = ""
	if got := compareCheckoutReceipt(t.Context(), deps, noBase, "receiver"); got.Checked || got.Matches || strings.Contains(got.Message, "endpoints match") {
		t.Fatalf("missing historical base produced a Git endpoint match: %+v", got)
	}
	detached := stored
	detached.Branch = ""
	if got := compareCheckoutReceipt(t.Context(), deps, detached, "receiver"); !got.Stale || got.Matches || !strings.Contains(got.Message, "branch") {
		t.Fatalf("detached source and named destination appeared equivalent: %+v", got)
	}
	oldReceipt := matching
	oldReceipt.DiffDigest = ""
	if got := compareCheckoutReceipt(t.Context(), deps, oldReceipt, "source"); got.Checked || got.Matches {
		t.Fatalf("same-generation receipt without a digest reported a match: %+v", got)
	}
	if err := os.WriteFile(filepath.Join(root, "dirty"), []byte("old symlink target content"), 0600); err != nil {
		t.Fatal(err)
	}
	runner.outputs[receiptCommand(root, "status", "--porcelain=v1", "-z", "--untracked-files=all")] = []byte("?? dirty\x00")
	legacy := collectGitReceipt(t.Context(), runner, root, nil, "")
	if got := compareCheckoutReceipt(t.Context(), deps, legacy, "receiver"); got.Checked || got.Matches {
		t.Fatalf("pre-generation receipt positively matched dirty content with an ambiguous legacy digest: %+v", got)
	}
	runner.outputs[receiptCommand(root, "status", "--porcelain=v1", "-z", "--untracked-files=all")] = nil
	if sameGeneration := compareCheckoutReceipt(t.Context(), deps, stored, "source"); !sameGeneration.Stale {
		t.Fatalf("same-generation comparison lost dirty checkout check: %+v", sameGeneration)
	}
}
