package command_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/output"
)

// writeTempJSON writes data as JSON to a temp file and returns the path.
func TestArtifactPublishRejectsSourceFileOutsidePackage(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
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
	if fc.calls != 0 {
		t.Fatalf("unsafe source made %d API calls", fc.calls)
	}
	if !strings.Contains(out.String(), "must stay within the artifact package directory") {
		t.Fatalf("missing recovery guidance: %s", out.String())
	}
}

func TestArtifactPublishRejectsSymlinkedSourceFile(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
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
	if fc.calls != 0 || !strings.Contains(out.String(), "symlink") {
		t.Fatalf("unsafe symlink result: calls=%d output=%s", fc.calls, out.String())
	}
}

func TestArtifactPublishRejectsOversizedSourceBeforePublication(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
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
	if fc.calls != 0 || !strings.Contains(out.String(), "exceeds the 1 MiB limit") {
		t.Fatalf("oversized source result: calls=%d output=%s", fc.calls, out.String())
	}
}

func TestArtifactPublishPreservesEmptySourceFileDocument(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
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

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--file", packagePath)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	documents := fc.lastPublishBody["documents"].([]any)
	document := documents[0].(map[string]any)
	content, present := document["content"]
	if !present || content != "" {
		t.Fatalf("content = %#v (present %t), want explicit empty string", content, present)
	}
}

func TestArtifactPublishReadsFileURLDocuments(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
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

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--file", f)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	documents := fc.lastPublishBody["documents"].([]any)
	document := documents[0].(map[string]any)
	if document["content"] != "# Tasks\n\n- Ship it.\n" {
		t.Fatalf("content = %#v", document["content"])
	}
	if _, ok := document["file_url"]; ok {
		t.Fatalf("file_url leaked to server payload: %#v", document)
	}
	if _, ok := document["source_path"]; ok {
		t.Fatalf("local source path leaked to server payload: %#v", document)
	}
}

func TestArtifactPublishPreviewShowsAbsoluteFileURLSource(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
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
	if fc.calls != 0 || !strings.Contains(out.String(), `"source_path":`+strconv.Quote(source)) {
		t.Fatalf("preview hid external source: calls=%d output=%s", fc.calls, out.String())
	}
}

func TestArtifactPublishRejectsContentWithSourceFile(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
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
	if fc.calls != 0 {
		t.Fatalf("calls = %d after invalid publish package, want 0", fc.calls)
	}
}

func TestArtifactPublishRejectsInvalidJSONBeforeHTTP(t *testing.T) {
	t.Parallel()
	deps, fc, _, _ := newFakeDeps(t)
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte("{"), 0644); err != nil {
		t.Fatal(err)
	}
	cmd := command.NewRootCommand(deps)
	cmd.SetArgs([]string{"artifact", "publish", "--file", bad})
	cmd.Execute() //nolint:errcheck
	if fc.calls != 0 {
		t.Fatalf("calls = %d after invalid JSON, want 0 (should fail before HTTP)", fc.calls)
	}
}

func TestArtifactPublishRequiresFile(t *testing.T) {
	t.Parallel()
	deps, _, _, _ := newFakeDeps(t)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish")
	if code == output.ExitOK {
		t.Fatal("expected non-zero exit when --file is missing")
	}
}

// --- artifact readiness moved to `gates check` ---

// --- artifact approve / request-changes ---

func writeIdentityConfig(t *testing.T, deps *command.Deps, username string) {
	t.Helper()
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	err := (config.Config{CurrentUser: config.CurrentUser{Username: username}}).SaveTo(deps.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
}

func TestArtifactApprovePatchesStatus(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	writeIdentityConfig(t, deps, "thanhtung2693")
	fc.updateStatusResult = &client.Artifact{ID: "art-1", Version: "v2", Status: "approved"}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain",
		"artifact", "approve", "art-1", "--note", "lgtm")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastStatusID != "art-1" {
		t.Fatalf("lastStatusID = %q, want art-1", fc.lastStatusID)
	}
	in := fc.lastStatusInput
	if in.Status != "approved" || in.ApprovedBy != "thanhtung2693" || in.Note != "lgtm" || in.ActorKind != "human" {
		t.Fatalf("status input = %+v", in)
	}
	if !strings.Contains(out.String(), "Approved art-1 (v2)") {
		t.Fatalf("output = %q, want approval confirmation", out.String())
	}
}

func TestArtifactApproveInteractiveDeclineCancels(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	deps.StdinIsTTY = func() bool { return true } // prompter confirmValue defaults to false

	code := command.ExecuteForCode(command.NewRootCommand(deps), "artifact", "approve", "art-1")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.calls != 0 {
		t.Fatalf("calls = %d, want 0 (declined confirm must not hit the server)", fc.calls)
	}
	if !strings.Contains(out.String(), "Cancelled.") {
		t.Fatalf("output = %q, want Cancelled.", out.String())
	}
}

func TestArtifactPromotePromotesCanonical(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	writeIdentityConfig(t, deps, "thanhtung2693")
	fc.promoteResult = &client.Feature{ID: "feat-uuid", Key: "FEAT-X", Version: 3, CanonicalArtifactID: "art-1"}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain",
		"artifact", "promote", "art-1")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastPromoteID != "art-1" || fc.lastPromoteActor != "thanhtung2693" {
		t.Fatalf("promote call: id=%q actor=%q", fc.lastPromoteID, fc.lastPromoteActor)
	}
	if !strings.Contains(out.String(), "Promoted art-1 to canonical for feature FEAT-X (v3)") {
		t.Fatalf("output = %q, want promote confirmation", out.String())
	}
}

func TestArtifactPromoteInteractiveDeclineCancels(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	deps.StdinIsTTY = func() bool { return true } // confirm defaults to false

	code := command.ExecuteForCode(command.NewRootCommand(deps), "artifact", "promote", "art-1")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.calls != 0 {
		t.Fatalf("calls = %d, want 0 (declined confirm must not hit the server)", fc.calls)
	}
}

func TestArtifactRequestChangesPatchesStatus(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	writeIdentityConfig(t, deps, "thanhtung2693")

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain",
		"artifact", "request-changes", "art-1", "--note", "tighten copy")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	in := fc.lastStatusInput
	if in.Status != "needs_changes" || in.ApprovedBy != "thanhtung2693" || in.Note != "tighten copy" || in.ActorKind != "human" {
		t.Fatalf("status input = %+v", in)
	}
	if !strings.Contains(out.String(), "Requested changes on art-1") {
		t.Fatalf("output = %q, want request-changes confirmation", out.String())
	}
}

func TestArtifactApproveJSONEnvelope(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.updateStatusResult = &client.Artifact{ID: "art-1", Version: "v2", Status: "approved"}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "approve", "art-1")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var env struct {
		OK   bool            `json:"ok"`
		Data client.Artifact `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v, output: %s", err, out.String())
	}
	if !env.OK || env.Data.Status != "approved" {
		t.Fatalf("unexpected envelope: %s", out.String())
	}
}

// --- artifact list columns + show prefix ---

func TestArtifactListRendersColumns(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.artifactListResult = &client.ArtifactList{
		Items: []client.Artifact{{
			ID:          "0123456789abcdef-0000",
			Version:     "v1.2",
			Status:      "approved",
			FeatureName: "Checkout redesign",
			UpdatedAt:   "2026-06-30T10:12:00Z",
		}},
		Total: 1,
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "artifact", "list")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	got := out.String()
	for _, want := range []string{"ID", "VERSION", "STATUS", "FEATURE", "UPDATED",
		"0123456789", "v1.2", "approved", "Checkout redesign"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "0123456789a") {
		t.Fatalf("artifact id not shortened to 10 chars:\n%s", got)
	}
}

func TestArtifactShowResolvesUniqueIDPrefix(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	full := "11111111-2222-3333-4444-555555555555"
	fc.artifactsByID = map[string]*client.Artifact{
		full: {ID: full, Version: "v2", Status: "approved"},
	}
	fc.artifactListResult = &client.ArtifactList{
		Items: []client.Artifact{{ID: full, Version: "v2", Status: "approved"}},
		Total: 1,
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "artifact", "show", "11111111-2")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastArtifactID != full {
		t.Fatalf("lastArtifactID = %q, want %q", fc.lastArtifactID, full)
	}
	if !strings.Contains(out.String(), full) {
		t.Fatalf("output missing full artifact id:\n%s", out.String())
	}
}

func TestArtifactShowTreatsArtifactIDsAsOpaque(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	full := strings.Repeat("a", 40)
	ref := strings.Repeat("a", 36)
	fc.artifactsByID = map[string]*client.Artifact{
		full: {ID: full, Version: "v2", Status: "approved"},
	}
	fc.artifactListResult = &client.ArtifactList{
		Items: []client.Artifact{{ID: full, Version: "v2", Status: "approved"}},
		Total: 1,
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "artifact", "show", ref)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastArtifactID != full {
		t.Fatalf("lastArtifactID = %q, want %q", fc.lastArtifactID, full)
	}
}

func TestArtifactShowAmbiguousPrefixListsCandidates(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	idA := "11111111-2222-3333-4444-555555555555"
	idB := "11111111-9999-8888-7777-666666666666"
	fc.artifactsByID = map[string]*client.Artifact{}
	fc.artifactListResult = &client.ArtifactList{
		Items: []client.Artifact{{ID: idA}, {ID: idB}},
		Total: 2,
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "artifact", "show", "11111111")
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want %d, output = %s", code, output.ExitUsage, out.String())
	}
	if !strings.Contains(out.String(), idA) || !strings.Contains(out.String(), idB) {
		t.Fatalf("output must list ambiguous candidates:\n%s", out.String())
	}
}

func TestArtifactShowUnknownPrefixSuggestsList(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.artifactsByID = map[string]*client.Artifact{}
	fc.artifactListResult = &client.ArtifactList{}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "artifact", "show", "deadbeef")
	if code != output.ExitNotFound {
		t.Fatalf("exit = %d, want %d, output = %s", code, output.ExitNotFound, out.String())
	}
	if !strings.Contains(out.String(), "specgate artifact list") {
		t.Fatalf("output = %q, want `specgate artifact list` hint", out.String())
	}
}

func TestArtifactPublishSkipsImpactPromptWhenNotTTY(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	dir := t.TempDir()
	path := dir + "/artifact.json"
	if err := os.WriteFile(path, []byte(`{"feature_key":"k","documents":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// Fake deps have a non-TTY stdin and no --no-input flag: the impact prompt
	// must be skipped, not block the session.
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "artifact", "publish", "--file", path)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if _, ok := fc.lastPublishBody["impact_declaration"]; ok {
		t.Fatalf("impact_declaration should not be synthesized without a prompt: %v", fc.lastPublishBody)
	}
}
