package command

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/local"
)

func TestRealGitReceiptCredentialRotationKeepsRepositoryAndDigest(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "absent"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...)
		if raw, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("fixture git failed: %v: %s", err, raw)
		}
	}
	git("init", "--initial-branch=main", "--template=")
	git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "fixture")
	git("remote", "add", "origin", "https://user:synthetic-one@example.invalid/repo.git?token=synthetic-query")
	first := collectGitReceipt(t.Context(), nil, dir, nil)
	git("remote", "set-url", "origin", "https://synthetic-two@example.invalid/repo.git#synthetic-fragment")
	second := collectGitReceipt(t.Context(), nil, dir, nil)
	if first.Availability != "available" || first.Repository != "https://example.invalid/repo.git" || first.DiffDigest == "" || first.DiffDigest != second.DiffDigest || first.Repository != second.Repository {
		t.Fatalf("credential rotation changed provenance: first=%+v second=%+v", first, second)
	}
	raw, _ := json.Marshal([]gitReceipt{first, second})
	if strings.Contains(string(raw), "synthetic") {
		t.Fatalf("real Git receipt leaked credential: %s", raw)
	}
}

func TestGitReceiptDoesNotPublishOriginCredentials(t *testing.T) {
	for _, tc := range []struct{ origin, want string }{
		{"https://synthetic-token@example.invalid/org/repo.git", "https://example.invalid/org/repo.git"},
		{"https://user:synthetic-secret@example.invalid/org/repo.git", "https://example.invalid/org/repo.git"},
		{"https://user:synthetic%2Dsecret@example.invalid/org/repo.git?token=synthetic-query#synthetic-fragment", "https://example.invalid/org/repo.git"},
		{"ssh://git:synthetic-secret@[::1]:2222/org/repo.git", "ssh://git@[::1]:2222/org/repo.git"},
		{"git+ssh://git:synthetic-secret@example.invalid/org/repo.git", "git+ssh://git@example.invalid/org/repo.git"},
		{"ssh+git://git@example.invalid/org/repo.git", "ssh+git://git@example.invalid/org/repo.git"},
		{"https://example.invalid/org/repo.git", "https://example.invalid/org/repo.git"},
		{"git@example.invalid:org/repo.git", "git@example.invalid:org/repo.git"},
		{"ssh://git@example.invalid:2222/org/repo.git", "ssh://git@example.invalid:2222/org/repo.git"},
		{"/srv/git/repo.git", "/srv/git/repo.git"},
		{"/srv/git/repo::name", "/srv/git/repo::name"},
		{"./https://repo.git", "./https://repo.git"},
		{`C:\repos\project.git`, `C:\repos\project.git`},
		{"https://user:synthetic%zz@example.invalid/org/repo.git", ""},
		{"ext::synthetic-secret", ""},
		{"https:synthetic-secret@example.invalid/repo.git", ""},
	} {
		t.Run(tc.origin, func(t *testing.T) {
			dir := t.TempDir()
			runner := &gitReceiptRunner{outputs: map[string][]byte{
				receiptCommand(dir, "rev-parse", "--show-toplevel"):      []byte(dir),
				receiptCommand(dir, "remote", "get-url", "origin"):       []byte(tc.origin),
				receiptCommand(dir, "branch", "--show-current"):          []byte("main"),
				receiptCommand(dir, "rev-parse", "HEAD"):                 []byte("head"),
				receiptCommand(dir, "merge-base", "HEAD", "origin/main"): []byte("head"),
			}}
			receipt := collectGitReceipt(t.Context(), runner, dir, nil)
			if receipt.Repository != tc.want {
				t.Errorf("repository identity not credential-free: got=%q want=%q", receipt.Repository, tc.want)
			}
			raw, err := json.Marshal(gitReceiptPayload(receipt))
			if err != nil || strings.Contains(string(raw), "synthetic") {
				t.Errorf("serialized receipt retained credential: %s err=%v", raw, err)
			}
		})
	}
}

func TestLegacyHandoffRedactsRepositoryWithoutChangingStoredEvidence(t *testing.T) {
	receipt := map[string]any{"repository": "https://user:synthetic-secret@example.invalid/org/repo.git?token=synthetic-query", "diff_digest": "sha256:legacy", "head_revision": "head"}
	report := local.DeliveryReport{ID: "report", Body: map[string]any{
		"git_receipt": receipt, "summary": "Keep unrelated evidence", "unrelated_count": int64(9007199254740993),
	}}
	before, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	exported, err := redactHandoffEvidence(report)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(exported)
	if err != nil || strings.Contains(string(raw), "synthetic") {
		t.Errorf("legacy handoff retained credential: %s err=%v", raw, err)
	}
	got, _ := exported.Body["git_receipt"].(map[string]any)
	if got["repository"] != "https://example.invalid/org/repo.git" || got["diff_digest"] != "sha256:legacy" {
		t.Errorf("projection changed opaque evidence or lost repository: %v", got)
	}
	if !strings.Contains(string(raw), "9007199254740993") {
		t.Errorf("redaction changed unrelated exact evidence: %s", raw)
	}
	after, _ := json.Marshal(report)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("redaction rewrote immutable source report")
	}
}
