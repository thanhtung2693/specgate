package command_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/output"
)

// writeTempJSON writes data as JSON to a temp file and returns the path.
func writeTempJSON(t *testing.T, data any) string {
	t.Helper()
	b, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(t.TempDir(), "body.json")
	if err := os.WriteFile(f, b, 0644); err != nil {
		t.Fatal(err)
	}
	return f
}

// --- artifact list ---

func TestArtifactListPlain(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.artifactListResult = &client.ArtifactList{
		Items: []client.Artifact{
			{ID: "abcdef1234567890", Version: "v0.1", Status: "draft", FeatureName: "Login"},
		},
		Total: 1,
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "artifact", "list")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "abcdef12") {
		t.Errorf("output missing artifact ID prefix:\n%s", got)
	}
	if !strings.Contains(got, "draft") {
		t.Errorf("output missing status:\n%s", got)
	}
}

func TestArtifactListUsesSemanticColorOnlyOnCapableTerminal(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	deps, fc, _, out := newFakeDeps(t)
	deps.StdoutIsTTY = func() bool { return true }
	fc.artifactListResult = &client.ArtifactList{Items: []client.Artifact{{ID: "abcdef1234567890", Version: "v0.1", Status: "approved", FeatureName: "Login"}}}

	if code := command.ExecuteForCode(command.NewRootCommand(deps), "artifact", "list"); code != output.ExitOK {
		t.Fatalf("rich exit = %d, output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "\x1b[") {
		t.Fatalf("rich artifact list has no ANSI styling: %q", out.String())
	}

	out.Reset()
	deps.StdoutIsTTY = func() bool { return false }
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "artifact", "list"); code != output.ExitOK {
		t.Fatalf("portable exit = %d, output = %s", code, out.String())
	}
	if strings.Contains(out.String(), "\x1b[") {
		t.Fatalf("portable artifact list contains ANSI styling: %q", out.String())
	}
}

func TestArtifactListJSON(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.artifactListResult = &client.ArtifactList{
		Items: []client.Artifact{{ID: "art-1", Version: "v0.1", Status: "approved"}},
		Total: 1,
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "list")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var env struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !env.OK {
		t.Fatalf("ok = false: %s", out.String())
	}
}

func TestArtifactCoverageScopesExactArtifactToSelectedWorkspace(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	if err := (config.Config{Workspace: config.CurrentWorkspace{ID: "ws-core", Slug: "core"}}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	fc.artifactResult = &client.Artifact{ID: "artifact-1", Status: "approved"}
	fc.allWorkItems = []client.WorkItemSummary{{Key: "SG-1", LeadArtifactID: "artifact-1", Phase: "delivered", Title: "Delivered work"}}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "coverage", "artifact-1")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if !fc.scopedArtifactLookup || fc.lastArtifactWorkspaceID != "ws-core" {
		t.Fatalf("artifact lookup scoped=%v workspace=%q, want true/ws-core", fc.scopedArtifactLookup, fc.lastArtifactWorkspaceID)
	}
	if !fc.listedArchivedWork {
		t.Fatal("artifact coverage omitted archived delivered work")
	}
	if !strings.Contains(out.String(), `"state":"delivered"`) {
		t.Fatalf("coverage output = %s", out.String())
	}
}

func TestArtifactListStatusFlag(t *testing.T) {
	t.Parallel()
	deps, fc, _, _ := newFakeDeps(t)
	command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "artifact", "list", "--status", "approved")
	if fc.lastArtifactFilter.Status != "approved" {
		t.Fatalf("status filter = %q, want approved", fc.lastArtifactFilter.Status)
	}
}

func TestArtifactListResolvesFeatureKeyToID(t *testing.T) {
	t.Parallel()
	deps, fc, _, _ := newFakeDeps(t)
	fc.featureResult = &client.Feature{ID: "feat-uuid-123", Key: "checkout-risk-review"}
	command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "artifact", "list", "--feature", "checkout-risk-review")
	// The key is resolved via GetFeature, and the resolved id (not the key) is
	// what the server filters on — passing the key alone matched nothing before.
	if fc.lastFeatureRef != "checkout-risk-review" {
		t.Fatalf("GetFeature ref = %q, want the key", fc.lastFeatureRef)
	}
	if fc.lastArtifactFilter.FeatureID != "feat-uuid-123" {
		t.Fatalf("filter FeatureID = %q, want the resolved id", fc.lastArtifactFilter.FeatureID)
	}
}

func TestArtifactListDefaultExcludesSupersededServerSide(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.artifactListResult = &client.ArtifactList{
		Items: []client.Artifact{
			{ID: "artcurrent111111", Version: "v0.2", Status: "approved", FeatureName: "Login"},
		},
		Total: 1,
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "artifact", "list")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	// The default view filters server-side so limit/total semantics stay
	// correct; the CLI must not drop rows client-side.
	if fc.lastArtifactFilter.ExcludeStatus != "superseded" {
		t.Fatalf("exclude_status sent to API = %q, want superseded", fc.lastArtifactFilter.ExcludeStatus)
	}
	if fc.lastArtifactFilter.Status != "" {
		t.Fatalf("status sent to API = %q, want empty", fc.lastArtifactFilter.Status)
	}
	if !strings.Contains(out.String(), "approved") {
		t.Errorf("default output missing current row:\n%s", out.String())
	}
}

func TestArtifactListStatusAllRequestsEveryStatus(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.artifactListResult = &client.ArtifactList{
		Items: []client.Artifact{
			{ID: "artcurrent111111", Version: "v0.2", Status: "approved", FeatureName: "Login"},
			{ID: "artstale22222222", Version: "v0.1", Status: "superseded", FeatureName: "Login"},
		},
		Total: 2,
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "artifact", "list", "--status", "all")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastArtifactFilter.Status != "" || fc.lastArtifactFilter.ExcludeStatus != "" {
		t.Fatalf("filter = %+v, want no status filters for --status all", fc.lastArtifactFilter)
	}
	if !strings.Contains(out.String(), "superseded") {
		t.Errorf("--status all should include superseded rows:\n%s", out.String())
	}
}

// --- artifact show ---

func TestArtifactShowPlain(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.artifactResult = &client.Artifact{
		ID: "art-99", Version: "v0.2", Status: "approved", RequestType: "new_feature",
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "artifact", "show", "art-99")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "art-99") {
		t.Errorf("output missing artifact ID:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "approved") {
		t.Errorf("output missing status:\n%s", out.String())
	}
}

func TestArtifactShowJSON(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.artifactResult = &client.Artifact{ID: "art-42", Version: "v1.0", Status: "draft"}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "show", "art-42")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var env struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !env.OK {
		t.Fatalf("ok = false: %s", out.String())
	}
}

// --- artifact files ---

func TestArtifactFilesRequireWorkspace(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "artifact", "files", "art-1")
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want usage; output = %s", code, out.String())
	}
	if fc.calls != 0 {
		t.Fatalf("artifact files made %d request(s) without a workspace", fc.calls)
	}
	if !strings.Contains(out.String(), "select a workspace first") {
		t.Fatalf("output = %q, want workspace selection guidance", out.String())
	}
}

func TestArtifactFilesListOnly(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	if err := (config.Config{Workspace: config.CurrentWorkspace{ID: "ws-1", Slug: "platform"}}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	fc.artifactFilesResult = []client.ArtifactFile{
		{Path: "docs/spec.md", Role: "spec", SizeBytes: 1234},
		{Path: "docs/prd.md", Role: "prd", SizeBytes: 567},
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "artifact", "files", "art-1")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastFilesID != "art-1" {
		t.Errorf("lastFilesID = %q, want art-1", fc.lastFilesID)
	}
	if fc.lastFilesWorkspaceID != "ws-1" {
		t.Errorf("workspace = %q, want ws-1", fc.lastFilesWorkspaceID)
	}
	got := out.String()
	if !strings.Contains(got, "docs/spec.md") {
		t.Errorf("output missing spec.md:\n%s", got)
	}
}

func TestArtifactFilesWithPathsReturnsReferencesByDefault(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	if err := (config.Config{Workspace: config.CurrentWorkspace{ID: "ws-1", Slug: "platform"}}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	fc.artifactFileResult = &client.ArtifactFileContent{SizeBytes: 1234, Content: "# Spec content\n"}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "artifact", "files", "art-1", "docs/spec.md")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastFilePath != "docs/spec.md" {
		t.Errorf("lastFilePath = %q, want docs/spec.md", fc.lastFilePath)
	}
	if fc.lastFileWorkspaceID != "ws-1" {
		t.Errorf("workspace = %q, want ws-1", fc.lastFileWorkspaceID)
	}
	got := out.String()
	if !strings.Contains(got, "docs/spec.md") || !strings.Contains(got, "1234") {
		t.Errorf("output missing file reference:\n%s", got)
	}
	if strings.Contains(got, "# Spec content") {
		t.Errorf("output should omit content by default:\n%s", got)
	}
}

func TestArtifactFilesWithContentFlag(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	if err := (config.Config{Workspace: config.CurrentWorkspace{ID: "ws-1", Slug: "platform"}}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	fc.artifactFileResult = &client.ArtifactFileContent{Content: "# Spec content\n"}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "artifact", "files", "art-1", "docs/spec.md", "--content")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "--- docs/spec.md ---") || !strings.Contains(got, "# Spec content") {
		t.Errorf("output missing content:\n%s", got)
	}
}

func TestArtifactFilesWithPathsJSONOmitsContentByDefault(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	if err := (config.Config{Workspace: config.CurrentWorkspace{ID: "ws-1", Slug: "platform"}}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	fc.artifactFileResult = &client.ArtifactFileContent{SizeBytes: 42, Content: "# Hello"}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "files", "art-1", "docs/spec.md")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	// JSON mode must not contain raw separators.
	if strings.Contains(out.String(), "---") {
		t.Errorf("JSON output must not contain raw separators:\n%s", out.String())
	}
	var env struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !env.OK {
		t.Fatalf("ok = false: %s", out.String())
	}
	if strings.Contains(out.String(), "# Hello") || strings.Contains(out.String(), `"content"`) {
		t.Fatalf("JSON output should omit content by default: %s", out.String())
	}
	if !strings.Contains(out.String(), `"path":"docs/spec.md"`) || !strings.Contains(out.String(), `"size_bytes":42`) {
		t.Fatalf("JSON output missing file metadata: %s", out.String())
	}
}

// --- artifact publish ---

func TestArtifactPublishFromFile(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	f := writeTempJSON(t, map[string]any{"feature_key": "feat-x", "documents": []any{}})
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--file", f)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastPublishBody["feature_key"] != "feat-x" {
		t.Fatalf("feature_key = %v, want feat-x", fc.lastPublishBody["feature_key"])
	}
}

func TestArtifactPublishPreviewUsesExplicitPathsAndRolesWithoutPublishing(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "any/layout"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "any/layout/contract.md"), []byte("# Spec"), 0o644); err != nil {
		t.Fatal(err)
	}
	packagePath := filepath.Join(dir, "artifact.json")
	body, err := json.Marshal(map[string]any{
		"feature_key": "feat-x",
		"source_kind": "optional-label",
		"documents": []map[string]any{{
			"path": "any/layout/contract.md", "role": "spec", "source_file": "any/layout/contract.md",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(packagePath, body, 0o644); err != nil {
		t.Fatal(err)
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--preview", "--file", packagePath)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.calls != 0 {
		t.Fatalf("preview made %d HTTP calls", fc.calls)
	}
	if !strings.Contains(out.String(), `"path":"any/layout/contract.md"`) || !strings.Contains(out.String(), `"human_confirmation_required":true`) {
		t.Fatalf("preview missing source/confirmation fields: %s", out.String())
	}
	if !strings.Contains(out.String(), `"omitted":["impact_declaration"]`) ||
		!strings.Contains(out.String(), `"governance_hint":"Impact declaration missing; Full mode may select stricter governance."`) {
		t.Fatalf("preview hid stricter-governance risk: %s", out.String())
	}
}

func TestArtifactPublishPreviewRejectsDocumentWithoutContentSource(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	packagePath := writeTempJSON(t, map[string]any{
		"feature_key": "feat-x",
		"documents": []map[string]any{{
			"path": "docs/spec.md", "role": "spec",
		}},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--preview", "--file", packagePath)
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want usage; output = %s", code, out.String())
	}
	if fc.calls != 0 {
		t.Fatalf("invalid preview made %d API calls", fc.calls)
	}
	for _, want := range []string{"documents[0]", "content", "source_file", "file_url"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing actionable source error %q: %s", want, out.String())
		}
	}
}

func TestArtifactPublishPreviewRejectsUnknownPackageField(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	packagePath := writeTempJSON(t, map[string]any{
		"feature_key": "feat-x",
		"non_goals":   []string{"silently ignored before validation"},
		"documents": []map[string]any{{
			"path": "spec.md", "role": "spec", "content": "# Spec",
		}},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--preview", "--file", packagePath)

	if code == output.ExitOK {
		t.Fatalf("unknown package field was silently ignored: %s", out.String())
	}
	if fc.calls != 0 {
		t.Fatalf("invalid preview made %d API calls", fc.calls)
	}
	if !strings.Contains(out.String(), "unknown artifact package field") || !strings.Contains(out.String(), "non_goals") {
		t.Fatalf("missing actionable unknown-field error: %s", out.String())
	}
}

func TestArtifactPublishPreviewRejectsUnknownRequestType(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	packagePath := writeTempJSON(t, map[string]any{
		"feature_key":  "feat-x",
		"request_type": "feature",
		"documents": []map[string]any{{
			"path": "spec.md", "role": "spec", "content": "# Spec",
		}},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--preview", "--file", packagePath)

	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want usage; output = %s", code, out.String())
	}
	if fc.calls != 0 {
		t.Fatalf("invalid preview made %d API calls", fc.calls)
	}
	for _, want := range []string{"request_type", "new_feature", "change_request", "bugfix", "unknown"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing allowed request type %q: %s", want, out.String())
		}
	}
}

func TestArtifactPublishCompareRequiresPreview(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	packagePath := writeTempJSON(t, map[string]any{
		"documents": []map[string]any{{"path": "spec.md", "role": "spec", "content": "# Spec"}},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--file", packagePath, "--compare", "art-base")
	if code == output.ExitOK {
		t.Fatalf("exit = 0, want usage error: %s", out.String())
	}
	if fc.calls != 0 {
		t.Fatalf("compare without preview made %d calls", fc.calls)
	}
	if !strings.Contains(out.String(), "--compare requires --preview") {
		t.Fatalf("output missing compare/preview error: %s", out.String())
	}
}

func TestArtifactPublishPreviewCompareIsReadOnly(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docs/spec.md"), []byte("# New spec"), 0o644); err != nil {
		t.Fatal(err)
	}
	packagePath := filepath.Join(dir, "artifact.json")
	body, err := json.Marshal(map[string]any{
		"workspace_id": "ws-core",
		"feature_key":  "feat-x",
		"base_version": "v0.2",
		"documents":    []map[string]any{{"path": "docs/spec.md", "role": "spec", "source_file": "docs/spec.md"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(packagePath, body, 0o644); err != nil {
		t.Fatal(err)
	}
	fc.artifactResult = &client.Artifact{ID: "art-base", Version: "v0.2", SnapshotDigest: "sha256:package"}
	fc.artifactFilesResult = []client.ArtifactFile{{Path: "docs/spec.md", Role: "design", ContentSHA256: "sha256:old"}}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--file", packagePath, "--preview", "--compare", "art-base")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.calls != 2 {
		t.Fatalf("compare preview made %d calls, want 2 reads", fc.calls)
	}
	if fc.lastArtifactID != "art-base" || fc.lastFilesID != "art-base" {
		t.Fatalf("read ids = artifact %q files %q", fc.lastArtifactID, fc.lastFilesID)
	}
	if fc.lastPublishBody != nil {
		t.Fatalf("compare preview published body: %#v", fc.lastPublishBody)
	}
	for _, want := range []string{`"base_artifact_id":"art-base"`, `"base_version":"v0.2"`, `"base_snapshot_digest":"sha256:package"`, `"changed":1`, `"changes":["content","role"]`} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("preview missing %s: %s", want, out.String())
		}
	}
}

func TestArtifactPublishPreviewCompareUsesSelectedWorkspace(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	if err := (config.Config{
		Workspace: config.CurrentWorkspace{ID: "ws-core", Slug: "core"},
	}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	fc.artifactResult = &client.Artifact{ID: "art-base", Version: "v0.2"}
	packagePath := writeTempJSON(t, map[string]any{
		"documents": []map[string]any{{"path": "spec.md", "role": "spec", "content": "# Spec"}},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--file", packagePath, "--preview", "--compare", "art-base")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastArtifactWorkspaceID != "ws-core" {
		t.Fatalf("artifact comparison workspace = %q, want ws-core", fc.lastArtifactWorkspaceID)
	}
	if fc.lastFilesWorkspaceID != "ws-core" {
		t.Fatalf("artifact file comparison workspace = %q, want ws-core", fc.lastFilesWorkspaceID)
	}
}

func TestArtifactPublishPreviewCompareRequiresWorkspaceForStoredReads(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	packagePath := writeTempJSON(t, map[string]any{
		"documents": []map[string]any{{"path": "spec.md", "role": "spec", "content": "# Spec"}},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--file", packagePath, "--preview", "--compare", "art-base")
	if code == output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.calls != 0 {
		t.Fatalf("comparison made %d stored-artifact read(s) without a workspace", fc.calls)
	}
	if !strings.Contains(out.String(), "select a workspace first") {
		t.Fatalf("output = %q, want workspace selection guidance", out.String())
	}
}

func TestArtifactPublishPreviewCompareRejectsBaseVersionMismatch(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.artifactResult = &client.Artifact{ID: "art-base", Version: "v0.2"}
	packagePath := writeTempJSON(t, map[string]any{
		"workspace_id": "ws-core",
		"base_version": "v0.1",
		"documents":    []map[string]any{{"path": "spec.md", "role": "spec", "content": "# Spec"}},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--file", packagePath, "--preview", "--compare", "art-base")
	if code == output.ExitOK {
		t.Fatalf("exit = 0, want mismatch error: %s", out.String())
	}
	if fc.lastPublishBody != nil {
		t.Fatalf("mismatched preview published body: %#v", fc.lastPublishBody)
	}
	if !strings.Contains(out.String(), "base_version") || !strings.Contains(out.String(), "v0.2") {
		t.Fatalf("output missing mismatch detail: %s", out.String())
	}
}

func TestArtifactPublishPreviewCompareReportsBaseLookupFailure(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.artifactsByID = map[string]*client.Artifact{}
	packagePath := writeTempJSON(t, map[string]any{
		"workspace_id": "ws-core",
		"documents":    []map[string]any{{"path": "spec.md", "role": "spec", "content": "# Spec"}},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--file", packagePath, "--preview", "--compare", "missing")
	if code == output.ExitOK {
		t.Fatalf("exit = 0, want lookup error: %s", out.String())
	}
	if fc.calls != 1 || fc.lastPublishBody != nil {
		t.Fatalf("lookup failure calls = %d, publish body = %#v", fc.calls, fc.lastPublishBody)
	}
	if !strings.Contains(out.String(), "artifact not found") {
		t.Fatalf("output missing lookup error: %s", out.String())
	}
}

func TestArtifactPublishPreviewComparePlainOutput(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.artifactResult = &client.Artifact{ID: "art-base", Version: "v0.2"}
	fc.artifactFilesResult = []client.ArtifactFile{{Path: "spec.md", Role: "spec", ContentSHA256: "sha256:old"}}
	packagePath := writeTempJSON(t, map[string]any{
		"workspace_id": "ws-core",
		"documents":    []map[string]any{{"path": "spec.md", "role": "spec", "content": "# New spec"}},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "artifact", "publish", "--file", packagePath, "--preview", "--compare", "art-base")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	for _, want := range []string{"Comparison with art-base (v0.2)", "changed", "spec.md", "No publication performed", "Human confirmation required"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("plain preview missing %q:\n%s", want, out.String())
		}
	}
}

func TestArtifactPublishAddsCurrentIdentity(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	writeIdentityConfig(t, deps, "thanhtung2693")
	f := writeTempJSON(t, map[string]any{
		"feature_key": "feat-x",
		"documents":   []map[string]any{{"path": "spec.md", "content": "# Spec"}},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--file", f)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if got := fc.lastPublishBody["created_by"]; got != "thanhtung2693" {
		t.Fatalf("created_by = %v, want thanhtung2693", got)
	}
}

func TestArtifactPublishPlainWarnsOnMissingRoles(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.publishResult = map[string]any{
		"artifact_id":    "art-1",
		"missing_roles":  []any{"plan", "verification"},
		"readiness_hint": "missing required roles: plan, verification",
	}
	f := writeTempJSON(t, map[string]any{"feature_key": "feat-x", "documents": []any{}})
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "artifact", "publish", "--file", f)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	// Publish stays non-blocking (spec-first drafts are legitimate), but a human
	// in plain mode must see the role gap now, not at gate time.
	if !strings.Contains(out.String(), "missing required roles: plan, verification") {
		t.Fatalf("plain publish output must surface missing roles:\n%s", out.String())
	}
}

func TestArtifactPublishPlainStaysQuietWhenRolesComplete(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	f := writeTempJSON(t, map[string]any{"feature_key": "feat-x", "documents": []any{}})
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "artifact", "publish", "--file", f)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if strings.Contains(out.String(), "missing required roles") {
		t.Fatalf("no warning expected when roles are complete:\n%s", out.String())
	}
}

func TestArtifactPublishAliasesWorkTypeToRequestType(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	f := writeTempJSON(t, map[string]any{
		"feature_key": "feat-x",
		"work_type":   "new_feature",
		"documents":   []map[string]any{{"path": "spec.md", "content": "# Spec"}},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--file", f)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if got := fc.lastPublishBody["request_type"]; got != "new_feature" {
		t.Fatalf("request_type = %v, want new_feature", got)
	}
	if _, ok := fc.lastPublishBody["work_type"]; ok {
		t.Fatalf("work_type leaked to server payload: %#v", fc.lastPublishBody)
	}
}

func TestArtifactPublishDefaultsMissingRequestTypeToUnknown(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	f := writeTempJSON(t, map[string]any{
		"feature_key": "feat-x",
		"documents":   []map[string]any{{"path": "spec.md", "content": "# Spec"}},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--file", f)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if got := fc.lastPublishBody["request_type"]; got != "unknown" {
		t.Fatalf("request_type = %v, want unknown", got)
	}
}

func TestArtifactPublishRejectsVersionFieldBeforeHTTP(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	f := writeTempJSON(t, map[string]any{
		"feature_key": "feat-x",
		"version":     "v0.1",
		"documents":   []map[string]any{{"path": "spec.md", "content": "# Spec"}},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--file", f)
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want %d; output = %s", code, output.ExitUsage, out.String())
	}
	if fc.calls != 0 {
		t.Fatalf("calls = %d after invalid publish package, want 0", fc.calls)
	}
	if !strings.Contains(out.String(), "version is server-assigned") {
		t.Fatalf("output missing version guidance: %s", out.String())
	}
}

func TestArtifactPublishReadsSourceFileDocuments(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	dir := t.TempDir()
	source := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(source, []byte("# Spec\n\nRaw local file content.\n"), 0644); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{
		"feature_key": "feat-source-file",
		"documents": []map[string]any{
			{"path": "spec.md", "role": "spec", "source_file": "spec.md"},
		},
	}
	packagePath := filepath.Join(dir, "package.json")
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(packagePath, raw, 0644); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--file", packagePath)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	documents, ok := fc.lastPublishBody["documents"].([]any)
	if !ok || len(documents) != 1 {
		t.Fatalf("documents = %#v", fc.lastPublishBody["documents"])
	}
	document := documents[0].(map[string]any)
	if document["content"] != "# Spec\n\nRaw local file content.\n" {
		t.Fatalf("content = %#v", document["content"])
	}
	if _, ok := document["source_file"]; ok {
		t.Fatalf("source_file leaked to server payload: %#v", document)
	}
}
