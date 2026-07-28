package local

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
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
		"policy_version":          "local-standard",
		"required_roles":          []string{"plan", "spec"},
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

func TestLocalDriftGateHasNoDeliverySkillBinding(t *testing.T) {
	snapshot, _, err := localPolicySnapshot()
	if err != nil {
		t.Fatal(err)
	}
	var policy localPolicyDocument
	if err := json.Unmarshal([]byte(snapshot), &policy); err != nil {
		t.Fatal(err)
	}
	if skill, exists := policy.GateSkills["spec_repo_drift"]; exists {
		t.Fatalf("spec_repo_drift skill = %q; drift uses its frozen inline rubric", skill)
	}
	for _, definition := range policy.GateDefinitions {
		if definition.Key == "spec_repo_drift" {
			if !strings.Contains(definition.SkillContent, "approved artifact") {
				t.Fatalf("drift rubric = %q, want frozen drift-specific content", definition.SkillContent)
			}
			return
		}
	}
	t.Fatal("spec_repo_drift gate definition missing")
}

func TestLocalPolicyMatchesFullStandardContract(t *testing.T) {
	snapshot, digest, err := localPolicySnapshot()
	if err != nil {
		t.Fatal(err)
	}
	var policy localPolicyDocument
	if err := json.Unmarshal([]byte(snapshot), &policy); err != nil {
		t.Fatal(err)
	}
	if policy.PolicyVersion != "local-standard" ||
		policy.GovernanceLevel != "standard" ||
		!reflect.DeepEqual(policy.ReasonCodes, []string{"local_fixed_standard"}) ||
		policy.Approval != "human_required" ||
		policy.Evidence != "attested_ok" {
		t.Fatalf("policy = %#v", policy)
	}
	assertStringsEqual(t, policy.RequiredRoles, []string{"plan", "spec"})
	assertStringsEqual(t, policy.RequiredTopics, []string{"acceptance_criteria", "outcomes", "scope", "verification"})
	assertStringsEqual(t, policy.RequiredEvidence, []string{"tests"})
	assertStringsEqual(t, policy.EnabledGates, []string{
		"acceptance_criteria_verifiable",
		"scope_clear",
		"spec_completeness",
		"spec_repo_drift",
	})
	if digest != digestText(snapshot) {
		t.Fatalf("digest = %q, want %q", digest, digestText(snapshot))
	}
}

func assertStringsEqual(t *testing.T, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
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
		"policy_version":          localPolicyVersion,
		"required_roles":          []string{"plan", "spec"},
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
