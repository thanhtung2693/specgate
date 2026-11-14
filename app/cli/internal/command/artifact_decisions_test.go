package command_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestArtifactPublishReadsRepoFileFromGitRoot(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(repo, "docs", "framework", "spec.md")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("# Framework spec\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source, err := filepath.EvalSymlinks(source)
	if err != nil {
		t.Fatal(err)
	}
	packageDir := filepath.Join(repo, ".specgate", "work")
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	packagePath := filepath.Join(packageDir, "artifact.json")
	body, err := json.Marshal(map[string]any{
		"feature_key": "framework-fit",

		"documents": []map[string]any{{
			"path": "docs/framework/spec.md", "role": "spec",
			"repo_file": "docs/framework/spec.md",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(packagePath, body, 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("preview from nested directory", func(t *testing.T) {
		deps, out := newTestDeps(t)
		deps.WorkingDir = filepath.Join(repo, "docs", "framework")
		code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--preview", "--file", packagePath)
		if code != output.ExitOK {
			t.Fatalf("exit = %d, output = %s", code, out.String())
		}
		if !strings.Contains(out.String(), `"source_path":`+strconv.Quote(source)) {
			t.Fatalf("preview source path missing: %s", out.String())
		}
	})

	t.Run("publish stores content and immutable path", func(t *testing.T) {
		deps, out := newTestDeps(t)
		deps.WorkingDir = repo
		store, workspace := artifactSourceStore(t, deps)
		code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--file", packagePath)
		if code != output.ExitOK {
			t.Fatalf("exit = %d, output = %s", code, out.String())
		}
		assertArtifactDocument(t, store, workspace, "docs/framework/spec.md", "# Framework spec\n")
		if strings.Contains(out.String(), source) {
			t.Fatalf("publish response leaked local absolute path: %s", out.String())
		}
	})
}

func TestArtifactPublishRejectsUnsafeRepoFile(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(repo, "docs", "link.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(outside), filepath.Join(repo, "linked-outside")); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		repoFile string
	}{
		{"absolute", filepath.Join(repo, "docs", "spec.md")},
		{"traversal", "../outside.md"},
		{"windows traversal", `..\outside.md`},
		{"windows absolute", `C:\outside.md`},
		{"directory", "docs"},
		{"final symlink", "docs/link.md"},
		{"symlink escape", "linked-outside/outside.md"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			deps, out := newTestDeps(t)
			deps.WorkingDir = repo
			packagePath := writeTempJSON(t, map[string]any{
				"feature_key": "unsafe-repo-file",
				"documents": []map[string]any{{
					"path": "spec.md", "role": "spec", "repo_file": tc.repoFile,
				}},
			})
			code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--preview", "--file", packagePath)
			if code != output.ExitUsage {
				t.Fatalf("exit = %d, want usage; output = %s", code, out.String())
			}
		})
	}
}

func TestArtifactPublishRepoFileRequiresGitRepository(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	deps.WorkingDir = t.TempDir()
	packagePath := writeTempJSON(t, map[string]any{
		"feature_key": "no-git-root",
		"documents": []map[string]any{{
			"path": "spec.md", "role": "spec", "repo_file": "docs/spec.md",
		}},
	})
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--preview", "--file", packagePath)
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want usage; output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "requires a Git repository") {
		t.Fatalf("result: output=%s", out.String())
	}
}

// writeTempJSON writes data as JSON to a temp file and returns the path.
func TestArtifactPublishRejectsSourceFileOutsidePackage(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	root := t.TempDir()
	packageDir := filepath.Join(root, "package")
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "private.txt"), []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	packagePath := filepath.Join(packageDir, "artifact.json")
	body, err := json.Marshal(map[string]any{
		"feature_key": "feat-traversal",
		"documents": []map[string]any{{
			"path": "spec.md", "role": "spec", "source_file": "../private.txt",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(packagePath, body, 0o600); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--preview", "--file", packagePath)
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want usage; output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "must stay within the artifact package directory") {
		t.Fatalf("missing recovery guidance: %s", out.String())
	}
}

func TestArtifactPublishRejectsSymlinkedSourceFile(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	dir := t.TempDir()
	external := filepath.Join(t.TempDir(), "private.txt")
	if err := os.WriteFile(external, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(dir, "spec.md")); err != nil {
		t.Fatal(err)
	}
	packagePath := filepath.Join(dir, "artifact.json")
	body, err := json.Marshal(map[string]any{
		"feature_key": "feat-symlink",
		"documents": []map[string]any{{
			"path": "spec.md", "role": "spec", "source_file": "spec.md",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(packagePath, body, 0o600); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--preview", "--file", packagePath)
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want usage; output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "symlink") {
		t.Fatalf("unsafe symlink result: output=%s", out.String())
	}
}

func TestArtifactPublishRejectsOversizedSourceBeforePublication(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "spec.md"), make([]byte, (1<<20)+1), 0o600); err != nil {
		t.Fatal(err)
	}
	packagePath := filepath.Join(dir, "artifact.json")
	body, err := json.Marshal(map[string]any{
		"feature_key": "feat-large",
		"documents": []map[string]any{{
			"path": "spec.md", "role": "spec", "source_file": "spec.md",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(packagePath, body, 0o600); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--file", packagePath)
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want usage; output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "exceeds the 1 MiB limit") {
		t.Fatalf("oversized source result: output=%s", out.String())
	}
}

func TestArtifactPublishPreservesEmptySourceFileDocument(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "empty.md"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	packagePath := filepath.Join(dir, "package.json")
	body, err := json.Marshal(map[string]any{
		"feature_key": "feat-empty-source-file",
		"documents":   []map[string]any{{"path": "empty.md", "role": "reference", "source_file": "empty.md"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(packagePath, body, 0644); err != nil {
		t.Fatal(err)
	}

	store, workspace := artifactSourceStore(t, deps)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--file", packagePath)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	assertArtifactDocument(t, store, workspace, "empty.md", "")
}

func TestArtifactPublishReadsFileURLDocuments(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	dir := t.TempDir()
	source := filepath.Join(dir, "tasks.md")
	if err := os.WriteFile(source, []byte("# Tasks\n\n- Ship it.\n"), 0644); err != nil {
		t.Fatal(err)
	}
	f := writeTempJSON(t, map[string]any{
		"feature_key": "feat-file-url",
		"documents": []map[string]any{
			{"path": "tasks.md", "role": "plan", "file_url": "file://" + source},
		},
	})

	store, workspace := artifactSourceStore(t, deps)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--file", f)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	assertArtifactDocument(t, store, workspace, "tasks.md", "# Tasks\n\n- Ship it.\n")
}

func TestArtifactPublishPreviewShowsAbsoluteFileURLSource(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	source := filepath.Join(t.TempDir(), "tasks.md")
	if err := os.WriteFile(source, []byte("# Tasks\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	packagePath := writeTempJSON(t, map[string]any{
		"feature_key": "feat-file-url-preview",

		"documents": []map[string]any{
			{"path": "tasks.md", "role": "plan", "file_url": "file://" + source},
		},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--preview", "--file", packagePath)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), `"source_path":`+strconv.Quote(source)) {
		t.Fatalf("preview hid external source: output=%s", out.String())
	}
}

func TestArtifactPublishRejectsContentWithSourceFile(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	f := writeTempJSON(t, map[string]any{
		"feature_key": "feat-conflict",
		"documents": []map[string]any{
			{"path": "spec.md", "content": "# Spec", "source_file": "spec.md"},
		},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--file", f)
	if code == output.ExitOK {
		t.Fatalf("expected publish to fail, output = %s", out.String())
	}
}

func TestArtifactPublishRejectsInvalidJSONBeforeHTTP(t *testing.T) {
	t.Parallel()
	deps, _ := newTestDeps(t)
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte("{"), 0644); err != nil {
		t.Fatal(err)
	}
	cmd := command.NewRootCommand(deps)
	cmd.SetArgs([]string{"artifact", "publish", "--file", bad})
	cmd.Execute() //nolint:errcheck
}

func TestArtifactPublishRequiresFile(t *testing.T) {
	t.Parallel()
	deps, _ := newTestDeps(t)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish")
	if code == output.ExitOK {
		t.Fatal("expected non-zero exit when --file is missing")
	}
}

func artifactSourceStore(t *testing.T, deps *command.Deps) (*local.Store, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := local.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	selected, err := store.Initialize(t.Context(), local.InitInput{WorkspaceName: "Source test", DisplayName: "Human", Username: "human"})
	if err != nil {
		t.Fatal(err)
	}
	if err := (config.Config{Mode: config.ModeLocal, Local: config.LocalStore{Path: dir}}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	return store, selected.Workspace.ID
}

func assertArtifactDocument(t *testing.T, store *local.Store, workspace, path, content string) {
	t.Helper()
	artifacts, err := store.ListArtifacts(t.Context(), workspace)
	if err != nil || len(artifacts) != 1 {
		t.Fatalf("published artifacts = %v, %v", artifacts, err)
	}
	artifact, err := store.GetArtifact(t.Context(), workspace, artifacts[0].ID)
	if err != nil || len(artifact.Documents) != 1 {
		t.Fatalf("exact artifact documents = %v, %v", artifact.Documents, err)
	}
	doc := artifact.Documents[0]
	if doc.Path != path || string(doc.Content) != content {
		t.Fatalf("frozen document = %q %q, want %q %q", doc.Path, doc.Content, path, content)
	}
}
