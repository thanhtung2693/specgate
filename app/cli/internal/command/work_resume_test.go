package command_test

import (
	"encoding/json"
	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/local"
	"strings"
	"testing"
)

func TestLocalResumeAndContextProjection(t *testing.T) {
	deps, fc, _, out := newFakeDeps(t)
	stateDir, store, sel, w := newLocalChangeWork(t, deps)
	other, err := store.CreateWork(t.Context(), sel.Workspace.ID, local.WorkInput{FeatureRef: w.FeatureKey, Title: "Separate work", Description: "Keep scope separate", AcceptanceCriteria: []string{"Other criterion"}})
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.ContextPack(t.Context(), sel.Workspace.ID, w.Key)
	if err != nil {
		t.Fatal(err)
	}
	duplicateArtifact, err := store.PublishArtifact(t.Context(), sel.Workspace.ID, local.ArtifactInput{
		FeatureKey: "DUPLICATE-PATH", RequestType: "bugfix",
		Documents: []local.ArtifactDocumentInput{
			{Path: "shared.md", Role: "spec", Content: []byte("# Spec copy")},
			{Path: "shared.md", Role: "plan", Content: []byte("# Plan copy")},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RunReadiness(t.Context(), sel.Workspace.ID, duplicateArtifact.ID); err != nil {
		t.Fatal(err)
	}
	duplicateTasks, err := store.ListGateTasks(t.Context(), sel.Workspace.ID, duplicateArtifact.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range duplicateTasks {
		gate := local.GateResultInput{Gate: task.GateKey, GateDigest: task.GateDigest, InputDigest: task.ArtifactDigest, State: "pass", Summary: "reviewed"}
		gate.Evaluator.Executor = task.Executor
		if _, err := store.SubmitGateResult(t.Context(), sel.Workspace.ID, task.TaskID, gate); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.ApproveArtifact(t.Context(), sel.Workspace.ID, duplicateArtifact.ID, "human", "approved"); err != nil {
		t.Fatal(err)
	}
	duplicateFeature, err := store.PromoteArtifact(t.Context(), sel.Workspace.ID, duplicateArtifact.ID)
	if err != nil {
		t.Fatal(err)
	}
	duplicateWork, err := store.CreateWork(t.Context(), sel.Workspace.ID, local.WorkInput{FeatureRef: duplicateFeature.Key, Title: "Ambiguous path", AcceptanceCriteria: []string{"Read each role"}})
	if err != nil {
		t.Fatal(err)
	}
	closeLocalChangeStore(t, deps, stateDir, store)
	for _, ref := range []string{w.Key, other.Key} {
		out.Reset()
		if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "work", "resume", ref); code != 0 {
			t.Fatalf("resume %d: %s", code, out.String())
		}
		var result struct {
			Data struct {
				Work         local.WorkItem             `json:"work"`
				Verification local.VerificationContract `json:"verification_contract"`
				Status       struct {
					Next string `json:"next_command"`
				} `json:"status"`
			} `json:"data"`
		}
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Data.Work.Key != ref || result.Data.Verification.Status != "unconfigured" || result.Data.Status.Next == "" {
			t.Fatalf("bad resume: %s", out.String())
		}
		if ref == other.Key && !strings.Contains(out.String(), "Keep scope separate") {
			t.Fatal("scope omitted")
		}
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "work", "context", w.Key, "--summary"); code != 0 {
		t.Fatalf("summary %d: %s", code, out.String())
	}
	if strings.Contains(out.String(), "Implement and verify.") || !strings.Contains(out.String(), before.Digest) || !strings.Contains(out.String(), "plan.md") {
		t.Fatalf("summary wrong: %s", out.String())
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "work", "context", w.Key, "--document", "plan.md"); code != 0 {
		t.Fatalf("document %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "Implement and verify.") {
		t.Fatalf("snapshot body missing: %s", out.String())
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "work", "context", w.Key, "--document", "../missing"); code != 3 {
		t.Fatalf("missing document %d: %s", code, out.String())
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "work", "context", duplicateWork.Key, "--document", "shared.md"); code != 2 {
		t.Fatalf("ambiguous document %d: %s", code, out.String())
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "work", "context", duplicateWork.Key, "--document", "shared.md", "--role", "plan"); code != 0 || !strings.Contains(out.String(), "# Plan copy") {
		t.Fatalf("role-selected document %d: %s", code, out.String())
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "work", "resume"); code != 2 {
		t.Fatalf("ambiguous resume %d: %s", code, out.String())
	}
	if fc.calls != 0 {
		t.Fatal("Local projection used HTTP")
	}
}
