package command_test

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func localReviewID(t *testing.T, store *local.Store, workspaceID, ref string) string {
	t.Helper()
	review, err := store.DeliveryStatus(t.Context(), workspaceID, ref)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
	return review.ID
}

func TestLocalSubmitDoesNotTrustCallerCheckProvenance(t *testing.T) {
	for _, runChecks := range []bool{false, true} {
		t.Run(map[bool]string{false: "reported", true: "skipped"}[runChecks], func(t *testing.T) {
			deps, _, _, out := newFakeDeps(t)
			stateDir, store, _, work := newLocalChangeWork(t, deps)
			closeLocalChangeStore(t, deps, stateDir, store)
			status := "pass"
			if runChecks {
				status = "skipped"
			}
			file := writeDeliveryJSON(t, map[string]any{
				"event_type": "coding_agent.completed", "context_digest": work.ContextDigest,
				"criteria": []map[string]any{{"criterion_id": "local-1", "claim": "satisfied", "evidence": map[string]any{"heading": "verified"}}},
				"checks":   []map[string]any{{"name": "unit", "command": "false", "status": status, "reason": "not run", "source": "specgate_cli", "claimed_status": "fail"}},
			})
			args := []string{"--json", "--yes", "change", "submit", work.Key, "--file", file}
			if runChecks {
				args = append(args, "--run-checks")
			}
			if code := command.ExecuteForCode(command.NewRootCommand(deps), args...); code != output.ExitOK {
				t.Fatalf("submit = %d: %s", code, out.String())
			}
			out.Reset()
			got := runChangeStatusJSON(t, deps, out, work.Key)
			if got.Assurance != "Agent-reported" {
				t.Fatalf("unexecuted check got assurance %q", got.Assurance)
			}
		})
	}
}

func TestLocalWorkListAcceptsMultiplePhases(t *testing.T) {
	deps, _, _, out := newFakeDeps(t)
	stateDir, store, _, work := newLocalChangeWork(t, deps)
	closeLocalChangeStore(t, deps, stateDir, store)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "work", "list", "--phase", " DELIVERED, "+strings.ToUpper(work.Phase))
	if code != output.ExitOK {
		t.Fatalf("list = %d: %s", code, out.String())
	}
	var result struct {
		Data struct {
			Items []struct {
				Key string `json:"key"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Data.Items) != 1 || result.Data.Items[0].Key != work.Key {
		t.Fatalf("phase CSV hid work: %s", out.String())
	}
}

func TestLocalWorkListDoesNotSilentlyIgnoreAllWorkspaces(t *testing.T) {
	deps, _, _, out := newFakeDeps(t)
	stateDir, store, _, _ := newLocalChangeWork(t, deps)
	closeLocalChangeStore(t, deps, stateDir, store)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "work", "list", "--all-workspaces")
	if code != output.ExitIncompatible {
		t.Fatalf("unsupported aggregate = %d: %s", code, out.String())
	}
}

func TestLocalDecisionRejectsUnseenReplacementReview(t *testing.T) {
	for _, verb := range []string{"accept", "request-changes"} {
		t.Run(verb, func(t *testing.T) {
			deps, _, _, out := newFakeDeps(t)
			stateDir, store, selection, work := newLocalChangeWork(t, deps)
			submitLocalChangeDelivery(t, store, selection.Workspace.ID, work, "builder", "head-a")
			first, err := store.DeliveryStatus(t.Context(), selection.Workspace.ID, work.Key)
			if err != nil {
				t.Fatal(err)
			}
			submitLocalChangeDelivery(t, store, selection.Workspace.ID, work, "builder", "head-b")
			closeLocalChangeStore(t, deps, stateDir, store)
			code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "change", verb, work.Key, "--review-id", first.ID)
			if code != output.ExitConflict {
				t.Fatalf("stale decision = %d, want conflict: %s", code, out.String())
			}
			out.Reset()
			got := runChangeStatusJSON(t, deps, out, work.Key)
			if got.State != "awaiting_acceptance" {
				t.Fatalf("replacement was decided: %#v", got)
			}
		})
	}
}
