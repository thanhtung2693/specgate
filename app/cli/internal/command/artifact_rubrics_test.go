package command_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestArtifactPublishPreviewResolvesProjectRubric(t *testing.T) {
	deps, out := newTestDeps(t)
	deps.WorkingDir = t.TempDir()
	for _, dir := range []string{".git", ".specgate/rubrics"} {
		if err := os.MkdirAll(filepath.Join(deps.WorkingDir, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(deps.WorkingDir, ".specgate/rubrics/scope_clear.md"), []byte("Require a rollback boundary."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := (config.Config{Mode: config.ModeLocal}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	manifest := writeTempJSON(t, map[string]any{
		"feature_key": "rubric-test", "documents": []map[string]any{{"path": "spec.md", "role": "spec", "content": "# Spec"}},
	})
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--preview", "--file", manifest); code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), `"source":".specgate/rubrics/scope_clear.md"`) {
		t.Fatalf("project source missing from preview: %s", out.String())
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "artifact", "publish", "--preview", "--file", manifest); code != output.ExitOK {
		t.Fatalf("plain preview exit = %d, output = %s", code, out.String())
	}
	for _, want := range []string{"Policy digest\t", "Rubric\tscope_clear\t.specgate/rubrics/scope_clear.md\t"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("plain preview missing %q: %s", want, out.String())
		}
	}
}

func TestArtifactPublishProjectRubricPreviewMatchesFrozenTask(t *testing.T) {
	deps, out := newTestDeps(t)
	root := deps.WorkingDir
	writeTestFile(t, filepath.Join(root, ".git/HEAD"), "ref: refs/heads/main\n")
	rubricPath := filepath.Join(root, ".specgate/rubrics/scope_clear.md")
	writeTestFile(t, rubricPath, "Require a rollback boundary.\n")
	writeTestFile(t, filepath.Join(root, ".specgate/rubrics/notes.txt"), "Unrelated user notes")
	stateDir := t.TempDir()
	run := func(args ...string) json.RawMessage {
		t.Helper()
		out.Reset()
		if code := command.ExecuteForCode(command.NewRootCommand(deps), append([]string{"--json"}, args...)...); code != output.ExitOK {
			t.Fatalf("%v: exit = %d, output = %s", args, code, out.String())
		}
		var envelope struct{ Data json.RawMessage }
		if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		return envelope.Data
	}
	run("init", "--mode", "local", "--no-input", "--local-dir", stateDir, "--workspace-name", "Alpha", "--display-name", "Human", "--username", "human")
	deps.WorkingDir = filepath.Join(root, "nested")
	writeTestFile(t, filepath.Join(deps.WorkingDir, ".specgate/rubrics/scope_clear.md"), "Wrong nested instructions")
	manifest := writeTempJSON(t, map[string]any{
		"feature_key": "rubric-test", "documents": []map[string]any{{"path": "spec.md", "role": "spec", "content": "# Spec"}, {"path": "plan.md", "role": "plan", "content": "# Plan"}},
	})
	before, err := os.ReadFile(filepath.Join(stateDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	var preview struct{ Policy local.PolicyProjection }
	if err := json.Unmarshal(run("artifact", "publish", "--preview", "--file", manifest), &preview); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(stateDir, "state.db"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("preview wrote to the Local database: %v", err)
	}
	var published struct {
		ArtifactID string `json:"artifact_id"`
	}
	if err := json.Unmarshal(run("artifact", "publish", "--file", manifest), &published); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, rubricPath, "Changed after publication")
	run("gates", "tasks", "dispatch", published.ArtifactID)
	var listed struct{ Tasks []local.GateTask }
	if err := json.Unmarshal(run("gates", "tasks", "list", published.ArtifactID), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Tasks) != 3 {
		t.Fatalf("tasks = %#v", listed.Tasks)
	}
	for _, task := range listed.Tasks {
		if task.PolicyDigest != preview.Policy.PolicyDigest {
			t.Fatalf("published digest %q differs from preview %q", task.PolicyDigest, preview.Policy.PolicyDigest)
		}
		for _, rubric := range preview.Policy.Rubrics {
			if rubric.Gate == task.GateKey {
				hash := sha256.Sum256([]byte(task.SkillContent))
				if hex.EncodeToString(hash[:]) != rubric.Digest {
					t.Fatal("task content differs from previewed effective rubric")
				}
			}
		}
		if task.GateKey == "scope_clear" && (!strings.Contains(task.SkillContent, "Require a rollback boundary.") || strings.Contains(task.SkillContent, "Changed after publication")) {
			t.Fatalf("task read mutable project source: %q", task.SkillContent)
		}
	}
	// Existing cleanup is limited to work files; rubrics remain user sources.
	deps.WorkingDir = root
	writeTestFile(t, filepath.Join(root, ".specgate/work/scratch.txt"), "Transient")
	run("--yes", "cleanup", "--work", "--item", "scratch.txt")
	for path, want := range map[string]string{rubricPath: "Changed after publication", filepath.Join(root, ".specgate/rubrics/notes.txt"): "Unrelated user notes"} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("cleanup changed user rubric source %s: %v", path, err)
		}
	}
}

func TestArtifactPublishRubricsRejectMalformedLocalFile(t *testing.T) {
	deps, out := newTestDeps(t)
	writeTestFile(t, filepath.Join(deps.WorkingDir, ".git/HEAD"), "ref: refs/heads/main\n")
	writeTestFile(t, filepath.Join(deps.WorkingDir, ".specgate/rubrics/scope_clear.md"), "\x00invalid")
	manifest := writeTempJSON(t, map[string]any{"feature_key": "rubric-test", "documents": []map[string]any{{"path": "spec.md", "role": "spec", "content": "# Spec"}}})
	for _, flags := range [][]string{nil, {"--preview"}} {
		out.Reset()
		args := append([]string{"--json", "artifact", "publish", "--file", manifest}, flags...)
		if code := command.ExecuteForCode(command.NewRootCommand(deps), args...); code != output.ExitUsage || !strings.Contains(out.String(), ".specgate/rubrics/scope_clear.md") {
			t.Fatalf("invalid rubric exit=%d output=%s", code, out.String())
		}
	}
}
