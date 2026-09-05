package command_test

import (
	"context"
	"encoding/json"
	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalVerificationPinScaffoldAndRejectChangedCommand(t *testing.T) {
	deps, _, _, out := newFakeDeps(t)
	stateDir, store, sel, _ := newLocalChangeWork(t, deps)
	w, err := store.CreateQuickWork(t.Context(), sel.Workspace.ID, local.QuickWorkInput{Title: "Pinned", AcceptanceCriteria: []string{"Works @check:unit"}})
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	deps.WorkingDir = root
	closeLocalChangeStore(t, deps, stateDir, store)
	file := writeDeliveryJSON(t, map[string]any{"context_digest": w.ContextDigest, "shell": "sh", "checks": []map[string]any{{"name": "unit", "command": "printf checked", "cwd": "."}}})
	args := []string{"--json", "--yes", "work", "verification", w.Key, "--file", file}
	if code := command.ExecuteForCode(command.NewRootCommand(deps), args...); code != 0 {
		t.Fatalf("pin %d: %s", code, out.String())
	}
	var pin struct {
		Data local.VerificationContract `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &pin); err != nil {
		t.Fatal(err)
	}
	if pin.Data.Status != "pinned" || pin.Data.Digest == "" {
		t.Fatalf("bad pin: %s", out.String())
	}
	out.Reset()
	reportPath := filepath.Join(t.TempDir(), "completion.json")
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "report", w.Key, "--init", reportPath); code != 0 {
		t.Fatalf("scaffold %d: %s", code, out.String())
	}
	raw, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), pin.Data.Digest) || !strings.Contains(string(raw), "printf checked") {
		t.Fatalf("pin missing in scaffold: %s", raw)
	}
	nested := filepath.Join(root, "nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	deps.WorkingDir = nested
	for _, cmdText := range []string{"true", "printf checked"} {
		out.Reset()
		calls := 0
		deps.RunCheckCommand = func(_ context.Context, cmd string) (int, string) {
			calls++
			if !strings.Contains(cmd, "printf checked") {
				t.Errorf("runner command: %s", cmd)
			}
			if !strings.Contains(cmd, "cd '"+realRoot+"' &&") {
				t.Errorf("pinned command did not run at repository root: %s", cmd)
			}
			return 0, "checked"
		}
		body := map[string]any{"event_type": "coding_agent.completed", "context_digest": w.ContextDigest, "verification_contract_digest": pin.Data.Digest, "criteria": []map[string]any{{"criterion_id": "local-1", "claim": "satisfied", "evidence": map[string]any{"heading": "proof"}}}, "checks": []map[string]any{{"name": "unit", "command": cmdText, "cwd": ".", "status": "pass"}}}
		f := writeDeliveryJSON(t, body)
		code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "change", "submit", w.Key, "--file", f, "--run-checks")
		if cmdText == "true" {
			if code == 0 || calls != 0 {
				t.Fatalf("mismatch executed: %d %d %s", code, calls, out.String())
			}
		} else if code != 0 || calls != 1 {
			t.Fatalf("valid submit: %d %d %s", code, calls, out.String())
		}
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "status", w.Key); code != 0 {
		t.Fatalf("status %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), `"verification_contract":"pinned"`) {
		t.Fatalf("status omitted contract: %s", out.String())
	}
}

func TestLocalSubmitPreflightBeforeRunner(t *testing.T) {
	for _, kind := range []string{"unknown", "context", "workspace", "delivered"} {
		t.Run(kind, func(t *testing.T) {
			deps, _, _, out := newFakeDeps(t)
			stateDir, store, selection, work := newLocalChangeWork(t, deps)
			ref, digest, want := work.Key, work.ContextDigest, output.ExitUsage
			if kind == "unknown" {
				ref = "BAD-REF"
				want = output.ExitNotFound
			}
			if kind == "context" {
				digest = "wrong"
			}
			if kind == "delivered" {
				submitLocalChangeDelivery(t, store, selection.Workspace.ID, work, "builder", "head-a")
				if err := store.DecideDelivery(t.Context(), selection.Workspace.ID, work.Key, "approve", "reviewer", "", localReviewID(t, store, selection.Workspace.ID, work.Key)); err != nil {
					t.Fatal(err)
				}
				want = output.ExitConflict
			}
			closeLocalChangeStore(t, deps, stateDir, store)
			calls := 0
			deps.RunCheckCommand = func(context.Context, string) (int, string) { calls++; return 0, "ok" }
			file := writeDeliveryJSON(t, map[string]any{"event_type": "coding_agent.completed", "context_digest": digest,
				"criteria": []map[string]any{{"criterion_id": "local-1", "claim": "satisfied", "evidence": map[string]any{"heading": "verified"}}},
				"checks":   []map[string]any{{"name": "unit", "command": "true", "status": "pass"}}})
			args := []string{"--json", "--yes", "change", "submit", ref, "--file", file, "--run-checks"}
			if kind == "workspace" {
				args = append(args, "--workspace", "missing-workspace")
				want = output.ExitNotFound
			}
			code := command.ExecuteForCode(command.NewRootCommand(deps), args...)
			if calls != 0 {
				t.Fatalf("invalid %s executed %d commands (exit %d): %s", kind, calls, code, out.String())
			}
			if code != want {
				t.Fatalf("exit %d want %d: %s", code, want, out.String())
			}
		})
	}
}
