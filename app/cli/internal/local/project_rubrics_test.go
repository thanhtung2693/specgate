package local

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestProjectRubricsPreserveExactDefaultPolicy(t *testing.T) {
	if rubrics, err := ResolveProjectRubrics(""); err != nil || len(rubrics) != 0 {
		t.Fatalf("no explicit root should resolve no project files: %#v, %v", rubrics, err)
	}
	root := t.TempDir()
	for _, emptyDir := range []string{"", ".specgate/rubrics"} {
		if emptyDir != "" {
			if err := os.MkdirAll(filepath.Join(root, emptyDir), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		rubrics, err := ResolveProjectRubrics(root)
		if err != nil || len(rubrics) != 0 {
			t.Fatalf("absent rubrics = %#v, %v", rubrics, err)
		}
		snapshot, digest, err := localPolicySnapshot(rubrics...)
		if err != nil || digest != "e8e3b8fe1776d0536fe6305186af0057f39bc93d4047457456851c10d9f21a22" {
			t.Fatalf("default policy changed: digest %q, error %v", digest, err)
		}
		baseline, _, _ := localPolicySnapshot()
		if snapshot != baseline {
			t.Fatal("no-file snapshot differs from the embedded default")
		}
		if emptyDir == "" {
			if _, err := os.Stat(filepath.Join(root, ".specgate")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("resolver created project state: %v", err)
			}
		}
	}
}

func TestProjectRubricsRejectPolicyExtensionAndDuplicateInputs(t *testing.T) {
	for _, input := range [][]ProjectRubric{
		{{Gate: "new_gate", Content: "Invent a new check"}},
		{{Gate: "scope_clear", Content: ""}},
		{{Gate: "scope_clear", Content: "First"}, {Gate: "scope_clear", Content: "Second"}},
	} {
		if _, _, err := localPolicySnapshot(input...); err == nil {
			t.Fatalf("unsupported policy input accepted: %#v", input)
		}
	}
}

func TestProjectRubricsOnlyAppendToExistingGateDefinitions(t *testing.T) {
	root := t.TempDir()
	var inputs []ProjectRubric
	for _, gate := range localSemanticGates {
		writeProjectRubric(t, root, gate.Key, "Require project-specific evidence.\n")
		inputs = append(inputs, ProjectRubric{Gate: gate.Key, Content: "Require project-specific evidence.\n"})
	}
	resolved, err := ResolveProjectRubrics(root)
	if err != nil || !reflect.DeepEqual(resolved, inputs) {
		t.Fatalf("resolved = %#v, %v", resolved, err)
	}
	snapshot, digest, err := localPolicySnapshot(resolved...)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := PreviewPolicy(resolved...)
	if err != nil || preview.PolicyDigest != digest {
		t.Fatalf("preview digest = %q, want %q, error %v", preview.PolicyDigest, digest, err)
	}
	var policy localPolicyDocument
	if err := json.Unmarshal([]byte(snapshot), &policy); err != nil {
		t.Fatal(err)
	}
	for i, gate := range policy.GateDefinitions {
		base := localSemanticGates[i]
		if gate.Key != base.Key || gate.Version != base.Version || !strings.HasPrefix(gate.SkillContent, base.SkillContent+"\n\n") || !strings.HasSuffix(gate.SkillContent, inputs[i].Content) {
			t.Fatalf("gate no longer extends the original: %#v", gate)
		}
		if preview.Rubrics[i].Source != projectRubricPath(base.Key) || preview.Rubrics[i].Digest != digestText(gate.SkillContent) {
			t.Fatalf("preview does not identify the effective rubric: %#v", preview.Rubrics[i])
		}
		policy.GateDefinitions[i] = base
	}
	reset, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	baseline, _, _ := localPolicySnapshot()
	if string(reset) != baseline {
		t.Fatal("project text changed a structural policy field")
	}
}

func TestProjectRubricsRejectUnsafeInputsAndPreserveSources(t *testing.T) {
	for name, content := range map[string]string{
		"empty": "", "whitespace": " \n\t", "NUL": "a\x00b", "invalid UTF-8": "\xff", "binary control": "a\x01b", "oversize": strings.Repeat("x", maxProjectRubricBytes+1),
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			path := writeProjectRubric(t, root, "scope_clear", content)
			_, err := ResolveProjectRubrics(root)
			if err == nil || !strings.Contains(err.Error(), projectRubricPath("scope_clear")) {
				t.Fatalf("unsafe content error = %v", err)
			}
			got, err := os.ReadFile(path)
			if err != nil || string(got) != content {
				t.Fatalf("resolver changed user source: %v", err)
			}
		})
	}
	for _, target := range []string{".specgate", ".specgate/rubrics", ".specgate/rubrics/scope_clear.md"} {
		t.Run("symlink "+target, func(t *testing.T) {
			root, outside := t.TempDir(), t.TempDir()
			path := filepath.Join(root, target)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			external := writeProjectRubric(t, outside, "scope_clear", "Outside instructions")
			if target != ".specgate/rubrics/scope_clear.md" {
				external = outside
			}
			if err := os.Symlink(external, path); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			if _, err := ResolveProjectRubrics(root); err == nil || !strings.Contains(err.Error(), target) {
				t.Fatalf("symlink error = %v", err)
			}
		})
	}
	root := t.TempDir()
	path := writeProjectRubric(t, root, "scope_clear", strings.Repeat("x", maxProjectRubricBytes))
	if _, err := ResolveProjectRubrics(root); err != nil {
		t.Fatalf("exact size boundary rejected: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveProjectRubrics(root); err == nil {
		t.Fatal("directory masquerading as active rubric accepted")
	}
	writeProjectRubric(t, root, "scope_cler", "Typo")
	if _, err := ResolveProjectRubrics(root); err == nil || !strings.Contains(err.Error(), "scope_cler.md") {
		t.Fatalf("unknown Markdown filename error = %v", err)
	}
}

func TestProjectRubricsFreezeTasksAcrossEditsAndRemoval(t *testing.T) {
	store, selection, original := internalGateFixture(t)
	ctx := context.Background()
	root := t.TempDir()
	writeProjectRubric(t, root, "scope_clear", "First project instructions")
	writeProjectRubric(t, root, "spec_repo_drift", "Inspect project API contracts")
	publish := func(base string) Artifact {
		t.Helper()
		rubrics, err := ResolveProjectRubrics(root)
		if err != nil {
			t.Fatal(err)
		}
		artifact, err := store.PublishArtifact(ctx, selection.Workspace.ID, ArtifactInput{
			FeatureKey: original.FeatureKey, RequestType: original.RequestType, BaseVersion: base, ProjectRubrics: rubrics,
			Documents: []ArtifactDocumentInput{{Path: "spec.md", Role: "spec", Content: []byte("Spec")}, {Path: "plan.md", Role: "plan", Content: []byte("Plan")}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return artifact
	}
	first := publish("v1")
	writeProjectRubric(t, root, "scope_clear", "Second project instructions")
	second := publish("v2")
	if first.PolicyDigest == second.PolicyDigest || first.PolicyDigest == original.PolicyDigest {
		t.Fatal("new versions did not freeze distinct project policy")
	}
	if err := os.RemoveAll(filepath.Join(root, ".specgate/rubrics")); err != nil {
		t.Fatal(err)
	}
	for _, artifact := range []Artifact{first, second, original} {
		if _, err := store.DispatchGateTasks(ctx, selection.Workspace.ID, artifact.ID); err != nil {
			t.Fatal(err)
		}
		tasks, err := store.ListGateTasks(ctx, selection.Workspace.ID, artifact.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, task := range tasks {
			if task.PolicyDigest != artifact.PolicyDigest {
				t.Fatal("task escaped its historical policy")
			}
			if task.GateKey == "scope_clear" {
				want := "First project instructions"
				if artifact.ID == second.ID {
					want = "Second project instructions"
				}
				if artifact.ID == original.ID {
					want = localSemanticGates[1].SkillContent
				}
				if !strings.HasSuffix(task.SkillContent, want) {
					t.Fatalf("task reread working files: %q", task.SkillContent)
				}
				wrong := internalGateResult(task)
				wrong.GateDigest = "wrong-frozen-digest"
				if _, err := store.SubmitGateResult(ctx, selection.Workspace.ID, task.TaskID, wrong); !errors.Is(err, ErrGateTaskInvalid) {
					t.Fatalf("mismatched digest error = %v", err)
				}
			}
			if _, err := store.SubmitGateResult(ctx, selection.Workspace.ID, task.TaskID, internalGateResult(task)); err != nil {
				t.Fatalf("historical task no longer works: %v", err)
			}
		}
	}
	if err := store.ApproveArtifact(ctx, selection.Workspace.ID, first.ID, "human", "approved"); err != nil {
		t.Fatal(err)
	}
	dispatch, err := store.DispatchGateTasks(ctx, selection.Workspace.ID, first.ID)
	if err != nil || len(dispatch.CreatedTaskIDs) != 1 {
		t.Fatalf("drift dispatch = %#v, %v", dispatch, err)
	}
	drift, err := store.GetGateTask(ctx, selection.Workspace.ID, dispatch.CreatedTaskIDs[0])
	if err != nil || !strings.HasSuffix(drift.SkillContent, "Inspect project API contracts") {
		t.Fatalf("drift did not use frozen custom rubric: %#v, %v", drift, err)
	}
}

func TestProjectRubricsRejectCaseVariantsAndPreserveUnrelatedFiles(t *testing.T) {
	for _, name := range []string{"scope_clear.MD", "SCOPE_CLEAR.md", "Scope_Clear.Md", "unknown.MD"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, ".specgate", "rubrics")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			for _, filename := range []string{name, "notes.TXT"} {
				if err := os.WriteFile(filepath.Join(dir, filename), []byte(filename), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := ResolveProjectRubrics(root); err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("case-variant %s did not return a filename error: %v", name, err)
			}
			if err := os.Remove(filepath.Join(dir, name)); err != nil {
				t.Fatal(err)
			}
			if rubrics, err := ResolveProjectRubrics(root); err != nil || len(rubrics) != 0 {
				t.Fatalf("unrelated file affected the default rubric: %+v, %v", rubrics, err)
			}
			if contents, err := os.ReadFile(filepath.Join(dir, "notes.TXT")); err != nil || string(contents) != "notes.TXT" {
				t.Fatalf("unrelated file changed: %q, %v", contents, err)
			}
		})
	}
}

func writeProjectRubric(t *testing.T, root, gate, content string) string {
	t.Helper()
	path := filepath.Join(root, projectRubricPath(gate))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
