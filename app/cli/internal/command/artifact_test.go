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

func TestArtifactListStatusFlag(t *testing.T) {
	t.Parallel()
	deps, fc, _, _ := newFakeDeps(t)
	command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "artifact", "list", "--status", "approved")
	if fc.lastArtifactFilter.Status != "approved" {
		t.Fatalf("status filter = %q, want approved", fc.lastArtifactFilter.Status)
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

func TestArtifactFilesListOnly(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
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
	got := out.String()
	if !strings.Contains(got, "docs/spec.md") {
		t.Errorf("output missing spec.md:\n%s", got)
	}
}

func TestArtifactFilesWithPathsReturnsReferencesByDefault(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.artifactFileResult = &client.ArtifactFileContent{SignedURL: "https://files.example/spec", SizeBytes: 1234, Content: "# Spec content\n"}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "artifact", "files", "art-1", "docs/spec.md")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastFilePath != "docs/spec.md" {
		t.Errorf("lastFilePath = %q, want docs/spec.md", fc.lastFilePath)
	}
	got := out.String()
	if !strings.Contains(got, "docs/spec.md") || !strings.Contains(got, "https://files.example/spec") {
		t.Errorf("output missing file reference:\n%s", got)
	}
	if strings.Contains(got, "# Spec content") {
		t.Errorf("output should omit content by default:\n%s", got)
	}
}

func TestArtifactFilesWithContentFlag(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
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
	fc.artifactFileResult = &client.ArtifactFileContent{SignedURL: "https://files.example/spec", SizeBytes: 42, Content: "# Hello"}
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
	if !strings.Contains(out.String(), "https://files.example/spec") {
		t.Fatalf("JSON output missing file URL: %s", out.String())
	}
}

// --- artifact publish ---

func TestArtifactPublishFromFile(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	f := writeTempJSON(t, map[string]any{"feature_key": "feat-x", "files": map[string]any{}})
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--file", f)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastProposalBody["feature_key"] != "feat-x" {
		t.Fatalf("feature_key = %v, want feat-x", fc.lastProposalBody["feature_key"])
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
	documents, ok := fc.lastProposalBody["documents"].([]any)
	if !ok || len(documents) != 1 {
		t.Fatalf("documents = %#v", fc.lastProposalBody["documents"])
	}
	document := documents[0].(map[string]any)
	if document["content"] != "# Spec\n\nRaw local file content.\n" {
		t.Fatalf("content = %#v", document["content"])
	}
	if _, ok := document["source_file"]; ok {
		t.Fatalf("source_file leaked to server payload: %#v", document)
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
	documents := fc.lastProposalBody["documents"].([]any)
	document := documents[0].(map[string]any)
	if document["content"] != "# Tasks\n\n- Ship it.\n" {
		t.Fatalf("content = %#v", document["content"])
	}
	if _, ok := document["file_url"]; ok {
		t.Fatalf("file_url leaked to server payload: %#v", document)
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

// --- artifact propose ---

func TestArtifactProposeFromFile(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.proposalResult = &client.ProposalResult{Drafted: true, SessionID: "sess-42"}
	f := writeTempJSON(t, map[string]any{"summary": "Update spec", "files": map[string]any{"spec": "# Updated"}})
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "artifact", "propose", "art-1", "--file", f)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastProposalID != "art-1" {
		t.Errorf("lastProposalID = %q, want art-1", fc.lastProposalID)
	}
	if !strings.Contains(out.String(), "sess-42") {
		t.Errorf("output missing session ID:\n%s", out.String())
	}
}

func TestArtifactProposeRejectsInvalidJSONBeforeHTTP(t *testing.T) {
	t.Parallel()
	deps, fc, _, _ := newFakeDeps(t)
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte("{bad}"), 0644); err != nil {
		t.Fatal(err)
	}
	cmd := command.NewRootCommand(deps)
	cmd.SetArgs([]string{"artifact", "propose", "art-1", "--file", bad})
	cmd.Execute() //nolint:errcheck
	if fc.calls != 0 {
		t.Fatalf("calls = %d after invalid JSON, want 0", fc.calls)
	}
}

// --- artifact readiness moved to `gates check` ---

func TestArtifactHelpHasNoQualityCommand(t *testing.T) {
	t.Parallel()
	deps, _, _, _ := newFakeDeps(t)
	root := command.NewRootCommand(deps)
	artifactCmd, _, err := root.Find([]string{"artifact"})
	if err != nil {
		t.Fatalf("find artifact command: %v", err)
	}
	var subcommands []string
	for _, sub := range artifactCmd.Commands() {
		subcommands = append(subcommands, sub.Name())
	}
	joined := strings.Join(subcommands, ",")
	if strings.Contains(joined, "quality") {
		t.Fatalf("quality moved to `gates check`; should not be an artifact subcommand: %v", subcommands)
	}
	if strings.Contains(joined, "readiness") {
		t.Fatalf("readiness moved to `gates check`; should not be an artifact subcommand: %v", subcommands)
	}
}

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

// --- artifact proposals ---

func TestArtifactProposalsListPlain(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.proposalsResult = []client.ProposalSession{{
		ID:              "aes_1",
		BaseArtifactID:  "0123456789abcdef-0000",
		BaseVersion:     "v3",
		State:           "active",
		SourceKind:      "feedback_event",
		LastDiffSummary: "1 file changed",
		UpdatedAt:       "2026-07-01T10:00:00Z",
	}}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "artifact", "proposals")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	got := out.String()
	for _, want := range []string{"SESSION", "ARTIFACT", "VERSION", "SOURCE", "DIFF", "UPDATED",
		"aes_1", "0123456789", "v3", "feedback_event", "1 file changed"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "0123456789a") {
		t.Fatalf("base artifact id not shortened to 10 chars:\n%s", got)
	}
}

func TestArtifactProposalsListEmpty(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "artifact", "proposals")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "No pending artifact-update proposals.") {
		t.Fatalf("output = %q, want empty state", out.String())
	}
}

func TestArtifactProposalsApproveSavesSession(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	writeIdentityConfig(t, deps, "thanhtung2693")

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain",
		"artifact", "proposals", "approve", "aes_1")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastSaveSessionID != "aes_1" {
		t.Fatalf("lastSaveSessionID = %q, want aes_1", fc.lastSaveSessionID)
	}
	if fc.lastSaveRequestedBy != "thanhtung2693" {
		t.Fatalf("lastSaveRequestedBy = %q, want thanhtung2693", fc.lastSaveRequestedBy)
	}
	if !strings.Contains(out.String(), "Approved proposal aes_1") {
		t.Fatalf("output = %q, want approval confirmation", out.String())
	}
}

func TestArtifactProposalsRejectInteractiveDeclineCancels(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	deps.StdinIsTTY = func() bool { return true }

	code := command.ExecuteForCode(command.NewRootCommand(deps), "artifact", "proposals", "reject", "aes_1")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastRejectSessionID != "" {
		t.Fatalf("lastRejectSessionID = %q, want empty (declined)", fc.lastRejectSessionID)
	}
	if !strings.Contains(out.String(), "Cancelled.") {
		t.Fatalf("output = %q, want Cancelled.", out.String())
	}
}

func TestArtifactProposalsRejectDeletesSession(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--yes",
		"artifact", "proposals", "reject", "aes_1")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastRejectSessionID != "aes_1" {
		t.Fatalf("lastRejectSessionID = %q, want aes_1", fc.lastRejectSessionID)
	}
	if !strings.Contains(out.String(), "Rejected proposal aes_1") {
		t.Fatalf("output = %q, want rejection confirmation", out.String())
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
	if _, ok := fc.lastProposalBody["impact_declaration"]; ok {
		t.Fatalf("impact_declaration should not be synthesized without a prompt: %v", fc.lastProposalBody)
	}
}
