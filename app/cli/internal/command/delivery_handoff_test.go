package command_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

// A delivery handoff travels in the repository: the author exports one file, a
// reviewer renders it beside the diff with no server, no workspace, and no
// SQLite state of their own.

func handoffWork(t *testing.T, deps *command.Deps, criterion string, checks []any) (string, local.WorkItem) {
	t.Helper()
	stateDir := t.TempDir()
	store, err := local.Open(filepath.Join(stateDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	selection, err := store.Initialize(t.Context(), local.InitInput{
		WorkspaceName: "Local workspace", DisplayName: "Local developer", Username: "local",
	})
	if err != nil {
		t.Fatal(err)
	}
	work, err := store.CreateQuickWork(t.Context(), selection.Workspace.ID, local.QuickWorkInput{
		Title: "Fix timeout", AcceptanceCriteria: []string{criterion},
	})
	if err != nil {
		t.Fatal(err)
	}
	if checks != nil {
		if _, err := store.SubmitDelivery(t.Context(), selection.Workspace.ID, work.Key, map[string]any{
			"context_digest": work.ContextDigest,
			"agent":          map[string]any{"name": "builder"},
			"criteria": []any{map[string]any{
				"criterion_id": "local-1", "claim": "satisfied",
				"evidence": map[string]any{"summary": "checked by hand"},
			}},
			"checks": checks,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := (config.Config{Mode: config.ModeLocal, Local: config.LocalStore{Path: stateDir}}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	return stateDir, work
}

func TestDeliveryHandoffShowReportsBoundCriterionFailureFromTheFileAlone(t *testing.T) {
	deps, _, _, out := newFakeDeps(t)
	_, work := handoffWork(t, deps, "Retries stop @check:unit",
		[]any{map[string]any{"name": "unit", "status": "skipped", "detail": "no runner"}})
	bundlePath := filepath.Join(t.TempDir(), "handoff.json")

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "handoff", "export", work.Key, "--file", bundlePath)
	if code != output.ExitOK {
		t.Fatalf("export exit = %d, output = %s", code, out.String())
	}

	// The reviewer has no Local store and no workspace of their own.
	reviewer, fc, _, reviewerOut := newFakeDeps(t)
	code = command.ExecuteForCode(command.NewRootCommand(reviewer), "--plain", "delivery", "handoff", "show", "--file", bundlePath)
	if code != output.ExitGovernanceFailed {
		t.Fatalf("show exit = %d, want governance failure; output = %s", code, reviewerOut.String())
	}
	if fc.calls != 0 {
		t.Fatalf("show contacted a server: calls = %d", fc.calls)
	}
	rendered := reviewerOut.String()
	for _, want := range []string{work.Key, "Retries stop", "unit", "failed"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered handoff missing %q:\n%s", want, rendered)
		}
	}
}

// Installs created before handoffs existed keep their own .specgate/.gitignore,
// which the CLI must not rewrite. Exporting into one has to say plainly that the
// reviewer will never receive the file.
func TestDeliveryHandoffExportWarnsWhenGitIgnoresTheBundle(t *testing.T) {
	// No t.Parallel: t.Chdir changes process-wide state.
	deps, _, _, out := newFakeDeps(t)
	_, work := handoffWork(t, deps, "Retries stop @check:unit",
		[]any{map[string]any{"name": "unit", "status": "pass", "command": "go test ./..."}})
	repo := t.TempDir()
	t.Chdir(repo)
	if err := os.Mkdir(".specgate", 0o700); err != nil {
		t.Fatal(err)
	}
	// The pre-handoff ignore file: everything but config.
	if err := os.WriteFile(filepath.Join(".specgate", ".gitignore"), []byte("*\n!.gitignore\n!config\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitInit(t, repo)

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "delivery", "handoff", "export", work.Key)
	if code != output.ExitOK {
		t.Fatalf("export exit = %d, output = %s", code, out.String())
	}
	warning := out.String()
	if !strings.Contains(warning, "ignored by Git") || !strings.Contains(warning, "!handoffs/") {
		t.Fatalf("export did not warn that the reviewer cannot receive the handoff: %q", warning)
	}
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{{"init"}, {"config", "user.email", "t@example.com"}, {"config", "user.name", "T"}} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git unavailable: %v: %s", err, out)
		}
	}
}

// A handoff is committed, so it must not republish the contents of files the
// agent cited — those paths can live outside the repository.
func TestDeliveryHandoffExportDropsEvidenceExcerptsButKeepsGroundingProof(t *testing.T) {
	deps, _, _, out := newFakeDeps(t)
	stateDir := t.TempDir()
	store, err := local.Open(filepath.Join(stateDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	selection, err := store.Initialize(t.Context(), local.InitInput{
		WorkspaceName: "Local workspace", DisplayName: "Local developer", Username: "local",
	})
	if err != nil {
		t.Fatal(err)
	}
	work, err := store.CreateQuickWork(t.Context(), selection.Workspace.ID, local.QuickWorkInput{
		Title: "Fix timeout", AcceptanceCriteria: []string{"Retries stop"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SubmitDelivery(t.Context(), selection.Workspace.ID, work.Key, map[string]any{
		"context_digest": work.ContextDigest,
		"agent":          map[string]any{"name": "builder"},
		"criteria": []any{map[string]any{
			"criterion_id": "local-1", "claim": "satisfied",
			"evidence": map[string]any{
				"kind": "file", "path": "/tmp/private-notes.log",
				"grounding": map[string]any{
					"status": "grounded", "digest": "sha256:abc",
					"excerpt": "SECRET-CUSTOMER-DATA in a file outside the repo",
				},
			},
		}},
		"checks": []any{map[string]any{"name": "unit", "status": "pass", "command": "go test ./..."}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := (config.Config{Mode: config.ModeLocal, Local: config.LocalStore{Path: stateDir}}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	bundlePath := filepath.Join(t.TempDir(), "handoff.json")

	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "handoff", "export", work.Key, "--file", bundlePath); code != output.ExitOK {
		t.Fatalf("export exit = %d, output = %s", code, out.String())
	}
	raw, err := os.ReadFile(bundlePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "SECRET-CUSTOMER-DATA") {
		t.Fatalf("exported handoff republished a grounding excerpt:\n%s", raw)
	}
	for _, want := range []string{"sha256:abc", "grounded", "/tmp/private-notes.log"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("exported handoff lost grounding proof %q:\n%s", want, raw)
		}
	}
}

// The Local SQLite store must never be a valid export destination.
func TestDeliveryHandoffExportRefusesToOverwriteLocalState(t *testing.T) {
	deps, _, _, out := newFakeDeps(t)
	stateDir, work := handoffWork(t, deps, "Retries stop @check:unit",
		[]any{map[string]any{"name": "unit", "status": "pass", "command": "go test ./..."}})
	statePath := filepath.Join(stateDir, "state.db")
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "handoff", "export", work.Key, "--file", statePath)
	if code == output.ExitOK {
		t.Fatalf("export overwrote the Local store: %s", out.String())
	}
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != len(after) {
		t.Fatalf("Local store was modified: %d bytes then %d", len(before), len(after))
	}
}

func TestDeliveryHandoffExportRefusesWorkWithoutDeliveryEvidence(t *testing.T) {
	deps, _, _, out := newFakeDeps(t)
	_, work := handoffWork(t, deps, "Retries stop @check:unit", nil)
	bundlePath := filepath.Join(t.TempDir(), "handoff.json")

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "handoff", "export", work.Key, "--file", bundlePath)
	if code != output.ExitNotFound {
		t.Fatalf("export exit = %d, want not found; output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "delivery report") {
		t.Fatalf("export error does not route to the scaffold: %s", out.String())
	}
	if _, err := os.Stat(bundlePath); !os.IsNotExist(err) {
		t.Fatalf("export wrote a bundle for an unreviewed work item: %v", err)
	}
}

// The checksum detects an edited bundle; a reviewer must never render evidence
// that no longer matches what the author exported.
func TestDeliveryHandoffShowRejectsEditedBundle(t *testing.T) {
	deps, _, _, out := newFakeDeps(t)
	_, work := handoffWork(t, deps, "Retries stop @check:unit",
		[]any{map[string]any{"name": "unit", "status": "pass", "command": "go test ./..."}})
	bundlePath := filepath.Join(t.TempDir(), "handoff.json")
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "handoff", "export", work.Key, "--file", bundlePath); code != output.ExitOK {
		t.Fatalf("export exit = %d, output = %s", code, out.String())
	}

	raw, err := os.ReadFile(bundlePath)
	if err != nil {
		t.Fatal(err)
	}
	var bundle map[string]any
	if err := json.Unmarshal(raw, &bundle); err != nil {
		t.Fatal(err)
	}
	review, _ := bundle["review"].(map[string]any)
	review["verdict"] = "passed"
	review["summary"] = "looks fine to me"
	report, _ := bundle["report"].(map[string]any)
	body, _ := report["body"].(map[string]any)
	checks, _ := body["checks"].([]any)
	checks[0].(map[string]any)["status"] = "skipped"
	edited, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundlePath, edited, 0o600); err != nil {
		t.Fatal(err)
	}

	reviewer, _, _, reviewerOut := newFakeDeps(t)
	code := command.ExecuteForCode(command.NewRootCommand(reviewer), "--plain", "delivery", "handoff", "show", "--file", bundlePath)
	if code == output.ExitOK {
		t.Fatalf("show accepted an edited bundle: %s", reviewerOut.String())
	}
	if !strings.Contains(reviewerOut.String(), "checksum") {
		t.Fatalf("show did not name the integrity failure: %s", reviewerOut.String())
	}
}

func TestDeliveryHandoffExportIsVisibleToGitFromAFreshWorkingDir(t *testing.T) {
	// No t.Parallel: t.Chdir changes process-wide state.
	deps, _, _, out := newFakeDeps(t)
	_, work := handoffWork(t, deps, "Retries stop @check:unit",
		[]any{map[string]any{"name": "unit", "status": "pass", "command": "go test ./..."}})
	repo := t.TempDir()
	t.Chdir(repo)
	if err := config.EnsureSpecgateDirGitignore(".specgate"); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "handoff", "export", work.Key)
	if code != output.ExitOK {
		t.Fatalf("export exit = %d, output = %s", code, out.String())
	}
	var envelope struct {
		Data struct {
			Path string `json:"path"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(envelope.Data.Path) != filepath.Join(".specgate", "handoffs") {
		t.Fatalf("default handoff path = %q, want it under .specgate/handoffs", envelope.Data.Path)
	}
	ignore, err := os.ReadFile(filepath.Join(".specgate", ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	// The working dir ignores transient scaffolds; a handoff is meant to be
	// committed, so it must survive the same ignore file.
	if !strings.Contains(string(ignore), "!handoffs/") || !strings.Contains(string(ignore), "!handoffs/**") {
		t.Fatalf("handoffs are not re-included by the .specgate ignore file:\n%s", ignore)
	}
}
