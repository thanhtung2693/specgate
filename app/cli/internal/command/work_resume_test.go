package command_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/local"
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
			{Path: "spec.md", Role: "spec", Content: []byte("# Separate feature spec")},
			{Path: "plan.md", Role: "plan", Content: []byte("# Separate feature plan")},
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
		if result.Data.Work.Key != ref || result.Data.Work.FeatureKey != w.FeatureKey || result.Data.Verification.Status != "unconfigured" || result.Data.Status.Next == "" {
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
	for _, scope := range []struct {
		ref, want, foreign string
	}{
		{w.Key, "Implement and verify.", "# Separate feature plan"},
		{duplicateWork.Key, "# Separate feature plan", "Implement and verify."},
		{w.Key, "Implement and verify.", "# Separate feature plan"},
	} {
		out.Reset()
		if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "work", "context", scope.ref, "--document", "plan.md", "--role", "plan"); code != 0 {
			t.Fatalf("context %s: %d %s", scope.ref, code, out.String())
		}
		if !strings.Contains(out.String(), scope.want) || strings.Contains(out.String(), scope.foreign) {
			t.Fatalf("same-path documents leaked between feature scopes: %s", out.String())
		}
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

func TestPlainResumeDetailShowsChangedPaths(t *testing.T) {
	deps, _, _, out := newFakeDeps(t)
	root := t.TempDir()
	deps.WorkingDir = root
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "fixture@example.test"}, {"config", "user.name", "Fixture"}} {
		if output, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, output)
		}
	}
	path := filepath.Join(root, "file.txt")
	if err := os.WriteFile(path, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "file.txt"}, {"commit", "-qm", "baseline"}} {
		if output, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, output)
		}
	}
	dir, store, _, work := newLocalChangeWork(t, deps)
	closeLocalChangeStore(t, deps, dir, store)
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--yes", "work", "checkpoint", work.Key); code != 0 {
		t.Fatalf("checkpoint %d: %s", code, out.String())
	}
	if err := os.WriteFile(path, []byte("after"), 0600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "work", "resume", work.Key, "--detail"); code != 0 {
		t.Fatalf("resume %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "file.txt") || !strings.Contains(out.String(), "modified: 1") {
		t.Fatalf("plain resume hid path delta: %s", out.String())
	}
}

func TestPlainArtifactImpactShowsDocumentAndWorkIdentity(t *testing.T) {
	deps, _, _, out := newFakeDeps(t)
	dir, store, sel, work := newLocalChangeWork(t, deps)
	target, err := store.PublishArtifact(t.Context(), sel.Workspace.ID, local.ArtifactInput{
		FeatureKey: "CHANGE", RequestType: "change_request", BaseVersion: "v1",
		Documents: []local.ArtifactDocumentInput{{Path: "spec.md", Role: "spec", Content: []byte("# Updated spec")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	closeLocalChangeStore(t, deps, dir, store)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "artifact", "impact", target.ID, "--compare", work.ArtifactID)
	if code != 0 {
		t.Fatalf("impact %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "spec:spec.md") || !strings.Contains(out.String(), work.Key) || !strings.Contains(out.String(), "unavailable") {
		t.Fatalf("plain impact hid recorded facts: %s", out.String())
	}
}

func TestLocalPublishPreviewIncludesValidatedImpact(t *testing.T) {
	deps, _, _, out := newFakeDeps(t)
	dir, store, sel, _ := newLocalChangeWork(t, deps)
	base, err := store.PublishArtifact(t.Context(), sel.Workspace.ID, local.ArtifactInput{
		FeatureKey: "CHANGE", RequestType: "change_request", BaseVersion: "v1",
		Documents:      []local.ArtifactDocumentInput{{Path: "spec.md", Role: "spec", Content: []byte("# Known requirement inventory")}},
		SourceCriteria: []local.SourceCriterion{{ID: "old", Text: "Old requirement", SourcePath: "spec.md"}},
	})
	if err != nil {
		t.Fatalf("publish known inventory base: %v", err)
	}
	closeLocalChangeStore(t, deps, dir, store)
	manifest := map[string]any{
		"feature_key": "CHANGE", "request_type": "change_request", "base_version": "v2",
		"documents":       []any{map[string]any{"path": "spec.md", "role": "spec", "content": "updated"}},
		"source_criteria": []any{map[string]any{"id": "new", "text": "New requirement", "source_path": "spec.md"}},
		"source_lineage":  map[string]any{"version": 1, "base_artifact_id": base.ID, "base_digest": base.SnapshotDigest, "rows": []any{map[string]any{"base_id": "old", "target_ids": []any{}, "reason": "replaced"}}, "added": []any{"new"}},
	}
	path := filepath.Join(t.TempDir(), "artifact.json")
	write := func() {
		t.Helper()
		body, err := json.Marshal(manifest)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--preview", "--compare", base.ID, "--file", path); code != 0 {
		t.Fatalf("preview %d: %s", code, out.String())
	}
	var result struct {
		Data struct {
			Impact struct {
				RequirementImpact string                `json:"requirement_impact"`
				TargetStatus      string                `json:"target_status"`
				Added             []struct{ ID string } `json:"added_requirements"`
			} `json:"impact"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Data.Impact.RequirementImpact != "declared" || result.Data.Impact.TargetStatus != "draft" || len(result.Data.Impact.Added) != 1 || result.Data.Impact.Added[0].ID != "new" {
		t.Fatalf("preview omitted target impact: %s", out.String())
	}
	manifest["source_lineage"].(map[string]any)["added"] = []any{"missing"}
	write()
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--preview", "--compare", base.ID, "--file", path); code == 0 {
		t.Fatalf("invalid lineage preview succeeded: %s", out.String())
	}
}

func TestResumeShowsNewerArtifactWithoutChangingPinnedScope(t *testing.T) {
	for _, state := range []string{"draft", "approved", "promoted"} {
		t.Run(state, func(t *testing.T) {
			deps, fc, _, out := newFakeDeps(t)
			dir, store, sel, work := newLocalChangeWork(t, deps)
			before, err := store.ContextPack(t.Context(), sel.Workspace.ID, work.Key)
			if err != nil {
				t.Fatal(err)
			}
			newer, err := store.PublishArtifact(t.Context(), sel.Workspace.ID, local.ArtifactInput{
				FeatureKey: "CHANGE", RequestType: "change_request", BaseVersion: "v1",
				Documents: []local.ArtifactDocumentInput{
					{Path: "spec.md", Role: "spec", Content: []byte("new version")},
					{Path: "plan.md", Role: "plan", Content: []byte("replacement plan")},
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			wantStatus := "draft"
			if state != "draft" {
				if _, err := store.RunReadiness(t.Context(), sel.Workspace.ID, newer.ID); err != nil {
					t.Fatal(err)
				}
				tasks, err := store.ListGateTasks(t.Context(), sel.Workspace.ID, newer.ID)
				if err != nil {
					t.Fatal(err)
				}
				for _, task := range tasks {
					result := local.GateResultInput{Gate: task.GateKey, GateDigest: task.GateDigest, InputDigest: task.ArtifactDigest, State: "pass", Summary: "fixture review"}
					result.Evaluator.Executor = task.Executor
					if _, err := store.SubmitGateResult(t.Context(), sel.Workspace.ID, task.TaskID, result); err != nil {
						t.Fatal(err)
					}
				}
				if err := store.ApproveArtifact(t.Context(), sel.Workspace.ID, newer.ID, "human", "fixture approval"); err != nil {
					t.Fatal(err)
				}
				wantStatus = "approved"
			}
			if state == "promoted" {
				feature, err := store.PromoteArtifact(t.Context(), sel.Workspace.ID, newer.ID)
				if err != nil {
					t.Fatal(err)
				}
				next, err := store.CreateWork(t.Context(), sel.Workspace.ID, local.WorkInput{FeatureRef: feature.Key, Title: "New scope", AcceptanceCriteria: []string{"Replacement criterion"}})
				if err != nil || next.ArtifactID != newer.ID {
					t.Fatalf("new work did not pin the promoted version: %+v, %v", next, err)
				}
			}
			closeLocalChangeStore(t, deps, dir, store)
			if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "work", "resume", work.Key); code != 0 {
				t.Fatalf("resume %d: %s", code, out.String())
			}
			var result struct {
				Data struct {
					Work  local.WorkItem `json:"work"`
					Newer struct {
						ID      string `json:"id"`
						Version int    `json:"version"`
						Status  string `json:"status"`
					} `json:"newer_artifact"`
				} `json:"data"`
			}
			if err := json.Unmarshal(out.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(result.Data.Work, work) || result.Data.Newer.ID != newer.ID || result.Data.Newer.Version != 2 || result.Data.Newer.Status != wantStatus {
				t.Fatalf("new version replaced/vanished from pinned scope: %s", out.String())
			}
			out.Reset()
			if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "work", "context", work.Key); code != 0 {
				t.Fatalf("context %d: %s", code, out.String())
			}
			var contextResult struct {
				Data struct {
					Digest   string `json:"context_digest"`
					Markdown string `json:"markdown"`
				} `json:"data"`
			}
			if err := json.Unmarshal(out.Bytes(), &contextResult); err != nil {
				t.Fatal(err)
			}
			if contextResult.Data.Digest != before.Digest || contextResult.Data.Markdown != before.Markdown {
				t.Fatalf("new artifact changed the old Context Pack: %s", out.String())
			}
			out.Reset()
			if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "work", "context", work.Key, "--document", "plan.md", "--role", "plan"); code != 0 || !strings.Contains(out.String(), "Implement and verify.") || strings.Contains(out.String(), "replacement plan") {
				t.Fatalf("selected document did not retain the approved snapshot: %d %s", code, out.String())
			}
			if fc.calls != 0 {
				t.Fatal("Local snapshot reads used HTTP")
			}
		})
	}
}

func TestLocalChangeStatusShowsAcceptanceMaterial(t *testing.T) {
	deps, _, _, out := newFakeDeps(t)
	dir, store, sel, work := newLocalChangeWork(t, deps)
	submitLocalChangeDelivery(t, store, sel.Workspace.ID, work, "builder", "head")
	base, err := store.GetArtifact(t.Context(), sel.Workspace.ID, work.ArtifactID)
	if err != nil {
		t.Fatal(err)
	}
	closeLocalChangeStore(t, deps, dir, store)
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "change", "status", work.Key); code != 0 {
		t.Fatalf("status %d: %s", code, out.String())
	}
	var result struct {
		Data struct {
			ArtifactID        string `json:"artifact_id"`
			ArtifactDigest    string `json:"artifact_digest"`
			ContextDigest     string `json:"context_digest"`
			CompletionID      string `json:"completion_id"`
			CriterionEvidence []struct {
				ID    string `json:"id"`
				Claim string `json:"claim"`
			} `json:"criterion_evidence"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Data.ArtifactID != base.ID || result.Data.ArtifactDigest != base.SnapshotDigest || result.Data.ContextDigest != work.ContextDigest || result.Data.CompletionID == "" || len(result.Data.CriterionEvidence) != 1 || result.Data.CriterionEvidence[0].Claim != "satisfied" {
		t.Fatalf("status hid acceptance material: %s", out.String())
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "change", "status", work.Key, "--detail"); code != 0 {
		t.Fatalf("status detail %d: %s", code, out.String())
	}
	for _, want := range []string{base.ID, work.ContextDigest, "Completion:", "Source coverage:", "Criterion evidence:"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("status detail missing %q: %s", want, out.String())
		}
	}
}
