package local

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestDispatchGateTasksUsesFrozenArtifactPolicy(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	selection, err := store.Initialize(ctx, InitInput{WorkspaceName: "Alpha", DisplayName: "Human", Username: "human"})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := store.PublishArtifact(ctx, selection.Workspace.ID, ArtifactInput{
		FeatureKey:  "FROZEN-POLICY",
		RequestType: "new_feature",
		Documents:   []ArtifactDocumentInput{{Path: "spec.md", Role: "spec", Content: []byte("# Frozen policy")}},
	})
	if err != nil {
		t.Fatal(err)
	}

	frozen := map[string]any{
		"snapshot_schema_version": "specgate.local_policy/v1",
		"policy_version":          "frozen@v9",
		"enabled_gates":           []string{"scope_clear"},
		"gate_definitions": []map[string]string{{
			"key": "scope_clear", "version": "frozen-v9", "skill_content": "Use the frozen rubric.",
		}},
		"approval_policy": "human_required",
		"evidence_policy": "attested_ok",
	}
	body, err := json.Marshal(frozen)
	if err != nil {
		t.Fatal(err)
	}
	policyDigest := digestText(string(body))
	if _, err := store.db.ExecContext(ctx, `UPDATE artifacts SET policy_snapshot_json = ?, policy_digest = ? WHERE id = ?`, string(body), policyDigest, artifact.ID); err != nil {
		t.Fatal(err)
	}

	dispatched, err := store.DispatchGateTasks(ctx, selection.Workspace.ID, artifact.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(dispatched.CreatedTaskIDs) != 1 {
		t.Fatalf("created tasks = %#v, want one frozen task", dispatched.CreatedTaskIDs)
	}
	task, err := store.GetGateTask(ctx, selection.Workspace.ID, dispatched.CreatedTaskIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	if task.GateKey != "scope_clear" || task.GateVersion != "frozen-v9" || task.SkillContent != "Use the frozen rubric." || task.PolicyDigest != policyDigest {
		t.Fatalf("task = %#v", task)
	}
}

func TestFrozenLocalPolicyRequiresAuthorityContracts(t *testing.T) {
	snapshot, _, err := localPolicySnapshot()
	if err != nil {
		t.Fatal(err)
	}
	var valid map[string]any
	if err := json.Unmarshal([]byte(snapshot), &valid); err != nil {
		t.Fatal(err)
	}

	cases := map[string]func(map[string]any){
		"missing approval policy":     func(policy map[string]any) { delete(policy, "approval_policy") },
		"unsupported evidence policy": func(policy map[string]any) { policy["evidence_policy"] = "corroborated_required" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			policy := make(map[string]any, len(valid))
			for key, value := range valid {
				policy[key] = value
			}
			mutate(policy)
			body, err := json.Marshal(policy)
			if err != nil {
				t.Fatal(err)
			}
			artifact := Artifact{ID: "artifact-policy", PolicySnapshot: string(body), PolicyDigest: digestText(string(body))}
			if _, err := frozenLocalGateDefinitions(artifact); err == nil {
				t.Fatal("invalid authority policy was accepted")
			}
		})
	}
}

func TestApproveArtifactRevalidatesFrozenPolicy(t *testing.T) {
	store, selection, artifact := internalGateFixture(t)
	ctx := context.Background()
	if _, err := store.RunReadiness(ctx, selection.Workspace.ID, artifact.ID); err != nil {
		t.Fatal(err)
	}
	tasks, err := store.ListGateTasks(ctx, selection.Workspace.ID, artifact.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		if _, err := store.SubmitGateResult(ctx, selection.Workspace.ID, task.TaskID, internalGateResult(task)); err != nil {
			t.Fatal(err)
		}
	}

	var policy map[string]any
	if err := json.Unmarshal([]byte(artifact.PolicySnapshot), &policy); err != nil {
		t.Fatal(err)
	}
	delete(policy, "approval_policy")
	body, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE artifacts SET policy_snapshot_json = ?, policy_digest = ? WHERE id = ?`, string(body), digestText(string(body)), artifact.ID); err != nil {
		t.Fatal(err)
	}

	err = store.ApproveArtifact(ctx, selection.Workspace.ID, artifact.ID, "human", "approved")
	if err == nil || !strings.Contains(err.Error(), "policy") {
		t.Fatalf("approval error = %v, want invalid policy rejection", err)
	}
	stored, err := store.GetArtifact(ctx, selection.Workspace.ID, artifact.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != "draft" {
		t.Fatalf("artifact status = %q, want draft", stored.Status)
	}
}

func TestSubmitGateResultRejectsTaskFromDifferentFrozenPolicy(t *testing.T) {
	store, selection, artifact := internalGateFixture(t)
	ctx := context.Background()
	dispatch, err := store.DispatchGateTasks(ctx, selection.Workspace.ID, artifact.ID)
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.GetGateTask(ctx, selection.Workspace.ID, dispatch.CreatedTaskIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	setAlternateValidLocalPolicy(t, ctx, store, artifact.ID)

	_, err = store.SubmitGateResult(ctx, selection.Workspace.ID, task.TaskID, internalGateResult(task))
	if !errors.Is(err, ErrGateTaskStale) {
		t.Fatalf("submission error = %v, want ErrGateTaskStale", err)
	}
}

func TestApproveArtifactRejectsReadinessFromDifferentFrozenPolicy(t *testing.T) {
	store, selection, artifact := internalGateFixture(t)
	ctx := context.Background()
	if _, err := store.RunReadiness(ctx, selection.Workspace.ID, artifact.ID); err != nil {
		t.Fatal(err)
	}
	tasks, err := store.ListGateTasks(ctx, selection.Workspace.ID, artifact.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		if _, err := store.SubmitGateResult(ctx, selection.Workspace.ID, task.TaskID, internalGateResult(task)); err != nil {
			t.Fatal(err)
		}
	}
	setAlternateValidLocalPolicy(t, ctx, store, artifact.ID)

	err = store.ApproveArtifact(ctx, selection.Workspace.ID, artifact.ID, "human", "approved")
	if err == nil || !strings.Contains(err.Error(), "not_run") {
		t.Fatalf("approval error = %v, want stale readiness rejection", err)
	}
	stored, err := store.GetArtifact(ctx, selection.Workspace.ID, artifact.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != "draft" {
		t.Fatalf("artifact status = %q, want draft", stored.Status)
	}
}

func setAlternateValidLocalPolicy(t *testing.T, ctx context.Context, store *Store, artifactID string) {
	t.Helper()
	policy := map[string]any{
		"snapshot_schema_version": localPolicySchemaVersion,
		"policy_version":          "alternate@v1",
		"enabled_gates":           []string{"alternate_gate"},
		"gate_definitions": []map[string]string{{
			"key": "alternate_gate", "version": "v1", "skill_content": "Evaluate the alternate contract.",
		}},
		"gate_skills":     map[string]string{"alternate_gate": "alternate-rubric"},
		"approval_policy": localApprovalPolicy,
		"evidence_policy": localEvidencePolicy,
	}
	body, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE artifacts SET policy_snapshot_json = ?, policy_digest = ? WHERE id = ?`, string(body), digestText(string(body)), artifactID); err != nil {
		t.Fatal(err)
	}
}
