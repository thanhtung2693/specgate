package command_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestModelSetIgnoresRepositoryServerForEnvironmentSecrets(t *testing.T) {
	t.Setenv("SPECGATE_NO_UPDATE_CHECK", "1")
	t.Setenv("SPECGATE_SERVER", "")
	t.Setenv("GOOGLE_API_KEY", "environment-secret")

	attackerCalled := false
	attacker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attackerCalled = true
		http.Error(w, "unexpected request", http.StatusInternalServerError)
	}))
	defer attacker.Close()

	var trustedSettings map[string]string
	trusted := (&fakeServer{settingsHandler: func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Settings map[string]string `json:"settings"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		trustedSettings = body.Settings
		_ = json.NewEncoder(w).Encode(map[string]any{"settings": body.Settings})
	}}).build(t)

	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".specgate"), 0o755); err != nil {
		t.Fatal(err)
	}
	repoConfig, err := json.Marshal(config.RepoConfig{Server: attacker.URL})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".specgate", "config"), repoConfig, 0o644); err != nil {
		t.Fatal(err)
	}

	deps, out := newTestDeps(t, "")
	deps.WorkingDir = repo
	if err := (config.Config{Mode: config.ModeFull, Server: trusted.URL}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--no-input",
		"model", "set", "--provider", "google", "--model", "gemini-3.1-flash-lite")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if attackerCalled {
		t.Fatal("model setup sent a request to the repository-configured server")
	}
	if trustedSettings["google.api_key"] != "environment-secret" {
		t.Fatalf("trusted server settings = %#v", trustedSettings)
	}
}

func TestLocalWorkspaceCreateSelectAndCurrentNeedNoHTTP(t *testing.T) {
	deps, out := newTestDeps(t, "")
	stateDir := filepath.Join(t.TempDir(), "local")
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--no-input", "init", "--mode", "local", "--local-dir", stateDir, "--workspace-name", "Alpha", "--display-name", "Human", "--username", "human"); code != output.ExitOK {
		t.Fatalf("init exit = %d; output=%s", code, out.String())
	}
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "workspace", "create", "Beta workspace"); code != output.ExitOK {
		t.Fatalf("create exit = %d; output=%s", code, out.String())
	}
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "workspace", "select", "beta-workspace"); code != output.ExitOK {
		t.Fatalf("select exit = %d; output=%s", code, out.String())
	}
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "workspace", "current"); code != output.ExitOK {
		t.Fatalf("current exit = %d; output=%s", code, out.String())
	}
	if deps.Client != nil {
		t.Fatal("Local workspace commands created an HTTP client")
	}
	if !strings.Contains(out.String(), "workspace: beta-workspace (Beta workspace)") {
		t.Fatalf("output = %s", out.String())
	}
}

func TestLocalUserLoginCreatesWorkspaceAndPersistsSelection(t *testing.T) {
	deps, out := newTestDeps(t, "")
	stateDir := filepath.Join(t.TempDir(), "local")
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--no-input", "init", "--mode", "local", "--local-dir", stateDir, "--workspace-name", "Alpha", "--display-name", "Human", "--username", "human"); code != output.ExitOK {
		t.Fatalf("init exit = %d; output=%s", code, out.String())
	}
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--no-input", "user", "login", "--workspace", "Beta", "--display-name", "Second Human", "--username", "second-human"); code != output.ExitOK {
		t.Fatalf("login exit = %d; output=%s", code, out.String())
	}
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "user", "current"); code != output.ExitOK {
		t.Fatalf("current exit = %d; output=%s", code, out.String())
	}
	if deps.Client != nil {
		t.Fatal("Local user commands created an HTTP client")
	}
	if !strings.Contains(out.String(), "user: second-human (Second Human)") {
		t.Fatalf("output = %s", out.String())
	}
	if !strings.Contains(out.String(), "Workspace set to beta") {
		t.Fatalf("output = %s", out.String())
	}
}

func TestLocalWorkspaceBindPersistsProjectStoreAndSelection(t *testing.T) {
	deps, out := newTestDeps(t, "")
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	deps.WorkingDir = repo
	stateDir := filepath.Join(t.TempDir(), "local")
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--no-input", "init", "--mode", "local", "--local-dir", stateDir, "--workspace-name", "Alpha", "--display-name", "Human", "--username", "human"); code != output.ExitOK {
		t.Fatalf("init exit = %d; output=%s", code, out.String())
	}
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--no-input", "workspace", "bind"); code != output.ExitOK {
		t.Fatalf("bind exit = %d; output=%s", code, out.String())
	}
	cfg, err := config.LoadFrom(deps.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	canonicalRepo, ok := config.FindProjectRoot(repo)
	if !ok {
		t.Fatal("project root not found")
	}
	project, ok := cfg.Projects[canonicalRepo]
	if !ok || project.Local.Path != stateDir || project.Local.ID != cfg.Local.ID || project.Workspace.Slug != "alpha" {
		t.Fatalf("project = %#v", project)
	}
	if !strings.Contains(out.String(), "Project workspace set to alpha") {
		t.Fatalf("output = %s", out.String())
	}
	ignore, err := os.ReadFile(filepath.Join(canonicalRepo, ".specgate", ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ignore), "*") || !strings.Contains(string(ignore), "!config") {
		t.Fatalf(".specgate/.gitignore = %q", ignore)
	}
}

func TestLocalProjectWorkspaceBindingOverridesGlobalSelection(t *testing.T) {
	deps, out := newTestDeps(t, "")
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	deps.WorkingDir = repo
	stateDir := filepath.Join(t.TempDir(), "local")
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--no-input", "init", "--mode", "local", "--local-dir", stateDir, "--workspace-name", "Alpha", "--display-name", "Human", "--username", "human"); code != output.ExitOK {
		t.Fatalf("init exit = %d; output=%s", code, out.String())
	}
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "workspace", "create", "Beta"); code != output.ExitOK {
		t.Fatalf("workspace create exit = %d; output=%s", code, out.String())
	}
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "workspace", "bind", "alpha"); code != output.ExitOK {
		t.Fatalf("workspace bind exit = %d; output=%s", code, out.String())
	}
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "workspace", "select", "beta"); code != output.ExitOK {
		t.Fatalf("workspace select exit = %d; output=%s", code, out.String())
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "status"); code != output.ExitOK || !strings.Contains(out.String(), `"slug":"alpha"`) {
		t.Fatalf("project binding did not scope Local status: exit=%d output=%s", code, out.String())
	}
}

func TestLocalArtifactPublishListAndShowNeedNoHTTP(t *testing.T) {
	deps, out := newTestDeps(t, "")
	stateDir := filepath.Join(t.TempDir(), "local")
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--no-input", "init", "--mode", "local", "--local-dir", stateDir, "--workspace-name", "Alpha", "--display-name", "Human", "--username", "human"); code != output.ExitOK {
		t.Fatalf("init exit = %d; output=%s", code, out.String())
	}
	artifactPath := filepath.Join(t.TempDir(), "artifact.json")
	if err := os.WriteFile(artifactPath, []byte(`{"feature_key":"LOCAL-ARTIFACTS","request_type":"new_feature","documents":[{"path":"spec.md","role":"spec","content":"immutable body"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--file", artifactPath); code != output.ExitOK {
		t.Fatalf("publish exit = %d; output=%s", code, out.String())
	}
	var published struct {
		Data struct {
			ArtifactID string `json:"artifact_id"`
			Version    int    `json:"version"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &published); err != nil {
		t.Fatal(err)
	}
	if published.Data.ArtifactID == "" || published.Data.Version != 1 {
		t.Fatalf("published = %#v", published)
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "show", published.Data.ArtifactID); code != output.ExitOK {
		t.Fatalf("show exit = %d; output=%s", code, out.String())
	}
	if !strings.Contains(out.String(), "immutable body") {
		t.Fatalf("show output = %s", out.String())
	}
	if deps.Client != nil {
		t.Fatal("Local artifact commands created an HTTP client")
	}
}

func TestLocalArtifactPublishPreviewCompareNeedNoHTTP(t *testing.T) {
	deps, out := newTestDeps(t, "")
	stateDir := filepath.Join(t.TempDir(), "local")
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--no-input", "init", "--mode", "local", "--local-dir", stateDir, "--workspace-name", "Alpha", "--display-name", "Human", "--username", "human"); code != output.ExitOK {
		t.Fatalf("init exit = %d; output=%s", code, out.String())
	}
	basePath := filepath.Join(t.TempDir(), "base.json")
	if err := os.WriteFile(basePath, []byte(`{"feature_key":"LOCAL-COMPARE","request_type":"change_request","documents":[{"path":"spec.md","role":"design","content":"old body"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--file", basePath); code != output.ExitOK {
		t.Fatalf("publish exit = %d; output=%s", code, out.String())
	}
	var published struct {
		Data struct {
			ArtifactID string `json:"artifact_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &published); err != nil {
		t.Fatal(err)
	}
	previewPath := filepath.Join(t.TempDir(), "preview.json")
	if err := os.WriteFile(previewPath, []byte(`{"feature_key":"LOCAL-COMPARE","request_type":"change_request","base_version":"v1","documents":[{"path":"spec.md","role":"spec","content":"new body"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	out.Reset()
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--file", previewPath, "--preview", "--compare", published.Data.ArtifactID)
	if code != output.ExitOK {
		t.Fatalf("compare preview exit = %d; output=%s", code, out.String())
	}
	for _, want := range []string{`"base_artifact_id":"` + published.Data.ArtifactID + `"`, `"base_version":"v1"`, `"changed":1`, `"changes":["content","role"]`} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("compare preview missing %s: %s", want, out.String())
		}
	}
	if deps.Client != nil {
		t.Fatal("Local compare preview created an HTTP client")
	}
}

func TestLocalWorkspaceOverrideScopesArtifactWithoutChangingSelection(t *testing.T) {
	deps, out := newTestDeps(t, "")
	stateDir := filepath.Join(t.TempDir(), "local")
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--no-input", "init", "--mode", "local", "--local-dir", stateDir, "--workspace-name", "Alpha", "--display-name", "Human", "--username", "human"); code != output.ExitOK {
		t.Fatalf("init exit = %d; output=%s", code, out.String())
	}
	artifactPath := filepath.Join(t.TempDir(), "artifact.json")
	if err := os.WriteFile(artifactPath, []byte(`{"feature_key":"LOCAL-ISOLATION","request_type":"new_feature","documents":[{"path":"spec.md","role":"spec","content":"alpha only"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--file", artifactPath); code != output.ExitOK {
		t.Fatalf("publish exit = %d; output=%s", code, out.String())
	}
	var published struct {
		Data struct {
			ArtifactID string `json:"artifact_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &published); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "workspace", "create", "Beta"); code != output.ExitOK {
		t.Fatalf("workspace create exit = %d; output=%s", code, out.String())
	}
	out.Reset()
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--workspace", "beta", "artifact", "show", published.Data.ArtifactID)
	if code != output.ExitNotFound || !strings.Contains(out.String(), `"code":"not_found"`) {
		t.Fatalf("override show exit = %d; output=%s", code, out.String())
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "workspace", "current"); code != output.ExitOK || !strings.Contains(out.String(), `"slug":"alpha"`) {
		t.Fatalf("override changed selection: exit=%d output=%s", code, out.String())
	}
	if deps.Client != nil {
		t.Fatal("Local workspace override created an HTTP client")
	}
}

func TestLocalReadinessThenHumanApprovalNeedNoHTTP(t *testing.T) {
	deps, out := newTestDeps(t, "")
	stateDir := filepath.Join(t.TempDir(), "local")
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--no-input", "init", "--mode", "local", "--local-dir", stateDir, "--workspace-name", "Alpha", "--display-name", "Human", "--username", "human"); code != output.ExitOK {
		t.Fatalf("init exit = %d; output=%s", code, out.String())
	}
	artifactPath := filepath.Join(t.TempDir(), "artifact.json")
	if err := os.WriteFile(artifactPath, []byte(`{"feature_key":"LOCAL-READINESS","request_type":"new_feature","documents":[{"path":"spec.md","role":"spec","content":"acceptance criteria"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--file", artifactPath); code != output.ExitOK {
		t.Fatalf("publish exit = %d; output=%s", code, out.String())
	}
	var published struct {
		Data struct {
			ArtifactID string `json:"artifact_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &published); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "gates", "check", published.Data.ArtifactID); code != output.ExitOK {
		t.Fatalf("readiness exit = %d; output=%s", code, out.String())
	}
	if !strings.Contains(out.String(), `"aggregate":"not_run"`) {
		t.Fatalf("readiness output = %s", out.String())
	}
	submitLocalGateResults(t, deps, out, published.Data.ArtifactID)
	out.Reset()
	if code := command.ExecuteForCode(
		command.NewRootCommand(deps), "--json", "change", "approve", published.Data.ArtifactID,
		"--title", "Implement local flow", "--ac", "Context Pack is followed",
	); code != output.ExitUsage {
		t.Fatalf("unconfirmed change approve exit = %d; output=%s", code, out.String())
	}
	if !strings.Contains(out.String(), `"code":"confirmation_required"`) {
		t.Fatalf("unconfirmed change approval output = %s", out.String())
	}
	out.Reset()
	if code := command.ExecuteForCode(
		command.NewRootCommand(deps), "--json", "--yes", "change", "approve", published.Data.ArtifactID,
		"--title", "Implement local flow", "--ac", "Context Pack is followed",
	); code != output.ExitOK {
		t.Fatalf("change approve exit = %d; output=%s", code, out.String())
	}
	if !strings.Contains(out.String(), `"state":"ready_for_implementation"`) || deps.Client != nil {
		t.Fatalf("change approval output = %s client=%#v", out.String(), deps.Client)
	}
	var work struct {
		Data struct {
			Key string `json:"work_ref"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &work); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "work", "context", work.Data.Key); code != output.ExitOK {
		t.Fatalf("context exit = %d; output=%s", code, out.String())
	}
	if !strings.Contains(out.String(), `"context_digest":`) || !strings.Contains(out.String(), "acceptance criteria") {
		t.Fatalf("context output = %s", out.String())
	}
	var contextPack struct {
		Data struct {
			Digest string `json:"context_digest"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &contextPack); err != nil {
		t.Fatal(err)
	}
	scaffoldPath := filepath.Join(t.TempDir(), "completion.json")
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "report", work.Data.Key, "--init", scaffoldPath); code != output.ExitOK {
		t.Fatalf("delivery scaffold exit = %d; output=%s", code, out.String())
	}
	if _, err := os.Stat(scaffoldPath); err != nil {
		t.Fatalf("Local space-separated --init path was not written: %v; output=%s", err, out.String())
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "status", work.Data.Key, "--detail"); code != output.ExitOK {
		t.Fatalf("empty delivery status exit = %d; output=%s", code, out.String())
	}
	if !strings.Contains(out.String(), `"found":false`) || !strings.Contains(out.String(), "delivery report") {
		t.Fatalf("empty delivery status output = %s", out.String())
	}
	completionPath := filepath.Join(t.TempDir(), "completion.json")
	completion := fmt.Sprintf(`{"event_type":"coding_agent.completed","context_digest":%q,"agent":{"name":"builder"},"criteria":[{"criterion_id":"local-1","claim":"satisfied","evidence":{"summary":"implemented"}}],"checks":[{"name":"unit","status":"skipped"}]}`, contextPack.Data.Digest)
	if err := os.WriteFile(completionPath, []byte(completion), 0o600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "submit", work.Data.Key, "--file", completionPath); code != output.ExitOK {
		t.Fatalf("delivery submit exit = %d; output=%s", code, out.String())
	}
	if !strings.Contains(out.String(), `"verdict":"passed"`) {
		t.Fatalf("delivery submit output = %s", out.String())
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "delivery", "status", work.Data.Key, "--detail"); code != output.ExitOK {
		t.Fatalf("plain delivery status exit = %d; output=%s", code, out.String())
	}
	for _, want := range []string{
		"Evidence: Ready for human review",
		"Assurance: Agent-reported",
		"Decision: Awaiting human acceptance",
		"Receipt: commit ",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("plain delivery status missing %q: %s", want, out.String())
		}
	}
	peerPath := filepath.Join(t.TempDir(), "peer.json")
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "peer-review", work.Data.Key, "--init="+peerPath); code != output.ExitOK {
		t.Fatalf("peer review init exit = %d; output=%s", code, out.String())
	}
	var peer map[string]any
	data, err := os.ReadFile(peerPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &peer); err != nil {
		t.Fatal(err)
	}
	peer["agent"] = map[string]any{"name": "reviewer"}
	criteria, _ := peer["criteria"].([]any)
	criteria[0].(map[string]any)["claim"] = "satisfied"
	criteria[0].(map[string]any)["evidence"] = map[string]any{"summary": "reviewed implementation"}
	data, err = json.Marshal(peer)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(peerPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "peer-review", work.Data.Key, "--file", peerPath); code != output.ExitOK {
		t.Fatalf("peer review exit = %d; output=%s", code, out.String())
	}
	if !strings.Contains(out.String(), `"agent_name":"reviewer"`) || deps.Client != nil {
		t.Fatalf("peer review output = %s client=%#v", out.String(), deps.Client)
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "status", work.Data.Key, "--detail"); code != output.ExitOK {
		t.Fatalf("delivery status exit = %d; output=%s", code, out.String())
	}
	if !strings.Contains(out.String(), `"peer_review":{"state":"passed"`) {
		t.Fatalf("delivery status missing peer review state: %s", out.String())
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "delivery", "approve", work.Data.Key); code != output.ExitOK {
		t.Fatalf("delivery approve exit = %d; output=%s", code, out.String())
	}
	if !strings.Contains(out.String(), `"human_decision":"approve"`) {
		t.Fatalf("delivery approval output = %s", out.String())
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "submit", work.Data.Key, "--file", completionPath); code != output.ExitConflict || !strings.Contains(out.String(), `"code":"conflict"`) {
		t.Fatalf("post-approval delivery submit exit=%d output=%s", code, out.String())
	}
}

// fakeDeployRunner implements deploy.CommandRunner for command-layer tests.
func TestLocalPluginRequiresExplicitQuickWorkCriteria(t *testing.T) {
	body, err := os.ReadFile("local_plugin_assets/skills/specgate-work-preparation/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "## 2A. Create quick work") ||
		!strings.Contains(string(body), "Quick work is available in Local and Full mode") ||
		!strings.Contains(string(body), "with explicit criteria") {
		t.Fatalf("Local plugin does not explain Local quick-work criteria:\n%s", body)
	}
}

func TestLocalPluginRequiresIDEAgentConfirmedAcceptanceCriteria(t *testing.T) {
	body, err := os.ReadFile("local_plugin_assets/skills/specgate-work-preparation/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `--ac "$CONFIRMED_CRITERION_1"`) ||
		!strings.Contains(string(body), "human-approved preview") {
		t.Fatalf("Local plugin does not document explicit IDE-agent criteria:\n%s", body)
	}
}

func TestLocalGateTaskLoopNeedsNoHTTP(t *testing.T) {
	deps, out := newTestDeps(t, "")
	stateDir := filepath.Join(t.TempDir(), "local")
	execute := func(args ...string) int {
		out.Reset()
		return command.ExecuteForCode(command.NewRootCommand(deps), args...)
	}
	if code := execute("--plain", "--no-input", "init", "--mode", "local", "--local-dir", stateDir, "--workspace-name", "Alpha", "--display-name", "Human", "--username", "human"); code != output.ExitOK {
		t.Fatalf("init exit=%d output=%s", code, out.String())
	}
	artifactPath := filepath.Join(t.TempDir(), "artifact.json")
	if err := os.WriteFile(artifactPath, []byte(`{"feature_key":"LOCAL-GATES","request_type":"new_feature","documents":[{"path":"spec.md","role":"spec","content":"# Local gates\n\nAcceptance criteria: observable"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := execute("--json", "artifact", "publish", "--file", artifactPath); code != output.ExitOK {
		t.Fatalf("publish exit=%d output=%s", code, out.String())
	}
	var published struct {
		Data struct {
			ArtifactID string `json:"artifact_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &published); err != nil {
		t.Fatal(err)
	}
	if code := execute("--json", "gates", "check", published.Data.ArtifactID, "--summary"); code != output.ExitOK {
		t.Fatalf("check exit=%d output=%s", code, out.String())
	}
	for _, want := range []string{`"aggregate":"not_run"`, `"dispatched_to_ide_agent"`, `"pending_task_ids"`} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("check output missing %s: %s", want, out.String())
		}
	}
	if code := execute("--json", "gates", "tasks", "list", published.Data.ArtifactID); code != output.ExitOK {
		t.Fatalf("list exit=%d output=%s", code, out.String())
	}
	if deps.Client != nil {
		t.Fatal("Local gate tasks created an HTTP client")
	}
	var listed struct {
		Data struct {
			Tasks []client.GateTask `json:"tasks"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Data.Tasks) != 4 {
		t.Fatalf("tasks = %#v", listed.Data.Tasks)
	}
	for _, task := range listed.Data.Tasks {
		resultPath := filepath.Join(t.TempDir(), task.TaskID+".json")
		body := fmt.Sprintf(`{"gate":%q,"gate_digest":%q,"input_digest":%q,"state":"pass","evaluator":{"executor":"ide_agent"}}`, task.GateKey, task.GateDigest, task.ArtifactDigest)
		if err := os.WriteFile(resultPath, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if code := execute("--json", "gates", "tasks", "submit-result", task.TaskID, "--file", resultPath); code != output.ExitOK {
			t.Fatalf("submit exit=%d output=%s", code, out.String())
		}
	}
	if code := execute("--json", "gates", "results", published.Data.ArtifactID); code != output.ExitOK {
		t.Fatalf("results exit=%d output=%s", code, out.String())
	}
	if !strings.Contains(out.String(), `"trust":"agent_attested"`) {
		t.Fatalf("results = %s", out.String())
	}
	if code := execute("--json", "gates", "check", published.Data.ArtifactID, "--summary"); code != output.ExitOK {
		t.Fatalf("second check exit=%d output=%s", code, out.String())
	}
	if !strings.Contains(out.String(), `"aggregate":"pass"`) {
		t.Fatalf("second check = %s", out.String())
	}
	if code := execute("--json", "--yes", "artifact", "approve", published.Data.ArtifactID); code != output.ExitOK {
		t.Fatalf("approve exit=%d output=%s", code, out.String())
	}
}

func TestLocalWorkCreatePersistsIDEAgentConfirmedAcceptanceCriteria(t *testing.T) {
	deps, out := newTestDeps(t, "")
	stateDir := filepath.Join(t.TempDir(), "local")
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--no-input", "init", "--mode", "local", "--local-dir", stateDir, "--workspace-name", "Alpha", "--display-name", "Human", "--username", "human"); code != output.ExitOK {
		t.Fatalf("init exit = %d; output=%s", code, out.String())
	}
	artifactPath := filepath.Join(t.TempDir(), "artifact.json")
	artifact := `{"feature_key":"LOCAL-EXPLICIT-AC","request_type":"new_feature","documents":[
		{"path":"spec.md","role":"spec","content":"# Contract\n\nThe source format is intentionally not interpreted by the CLI."},
		{"path":"notes.md","role":"design","content":"Acceptance criteria are confirmed by the IDE agent."}
	]}`
	if err := os.WriteFile(artifactPath, []byte(artifact), 0o600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--file", artifactPath); code != output.ExitOK {
		t.Fatalf("publish exit = %d; output=%s", code, out.String())
	}
	var published struct {
		Data struct {
			ArtifactID string `json:"artifact_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &published); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "gates", "check", published.Data.ArtifactID); code != output.ExitOK {
		t.Fatalf("readiness exit = %d; output=%s", code, out.String())
	}
	submitLocalGateResults(t, deps, out, published.Data.ArtifactID)
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "artifact", "approve", published.Data.ArtifactID); code != output.ExitOK {
		t.Fatalf("approve exit = %d; output=%s", code, out.String())
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "artifact", "promote", published.Data.ArtifactID); code != output.ExitOK {
		t.Fatalf("promote exit = %d; output=%s", code, out.String())
	}
	var promoted struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &promoted); err != nil {
		t.Fatal(err)
	}
	if promoted.Data.ID == "" {
		t.Fatalf("promote omitted feature id: %s", out.String())
	}
	out.Reset()
	if code := command.ExecuteForCode(
		command.NewRootCommand(deps),
		"--json", "feature", "list", "--search", "explicit",
	); code != output.ExitOK {
		t.Fatalf("Local feature list exit = %d; output=%s", code, out.String())
	}
	if !strings.Contains(out.String(), `"key":"LOCAL-EXPLICIT-AC"`) {
		t.Fatalf("Local feature list omitted promoted feature: %s", out.String())
	}
	out.Reset()
	if code := command.ExecuteForCode(
		command.NewRootCommand(deps),
		"--json", "feature", "show", promoted.Data.ID,
	); code != output.ExitOK {
		t.Fatalf("Local feature show exit = %d; output=%s", code, out.String())
	}
	if !strings.Contains(out.String(), `"canonical_artifact_id":"`+published.Data.ArtifactID+`"`) {
		t.Fatalf("Local feature show omitted canonical artifact: %s", out.String())
	}
	out.Reset()
	if code := command.ExecuteForCode(
		command.NewRootCommand(deps),
		"--json", "feature", "show", "missing-feature",
	); code != output.ExitNotFound {
		t.Fatalf("missing Local feature exit = %d, want %d; output=%s", code, output.ExitNotFound, out.String())
	}
	if !strings.Contains(out.String(), "specgate feature list --all") {
		t.Fatalf("missing Local feature recovery is not actionable: %s", out.String())
	}
	out.Reset()
	if code := command.ExecuteForCode(
		command.NewRootCommand(deps),
		"--json", "work", "create",
		"--feature", promoted.Data.ID,
		"--title", "Persist confirmed criteria",
		"--ac", "First criterion",
		"--ac", "Shared criterion",
		"--ac", "Final criterion",
	); code != output.ExitOK {
		t.Fatalf("work create exit = %d; output=%s", code, out.String())
	}
	var work struct {
		Data struct {
			ID                 string   `json:"id"`
			Key                string   `json:"key"`
			ChangeRequestID    string   `json:"change_request_id"`
			ChangeRequestKey   string   `json:"change_request_key"`
			FeatureKey         string   `json:"feature_key"`
			ArtifactID         string   `json:"artifact_id"`
			LeadArtifactID     string   `json:"lead_artifact_id"`
			AcceptanceCriteria []string `json:"acceptance_criteria"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &work); err != nil {
		t.Fatal(err)
	}
	want := []string{"First criterion", "Shared criterion", "Final criterion"}
	if !reflect.DeepEqual(work.Data.AcceptanceCriteria, want) {
		t.Fatalf("criteria = %#v, want %#v", work.Data.AcceptanceCriteria, want)
	}
	if work.Data.ChangeRequestID != work.Data.ID ||
		work.Data.ChangeRequestKey != work.Data.Key ||
		work.Data.FeatureKey != "LOCAL-EXPLICIT-AC" ||
		work.Data.LeadArtifactID != work.Data.ArtifactID {
		t.Fatalf("Local work JSON aliases do not match Full mode: %+v", work.Data)
	}
	if deps.Client != nil {
		t.Fatal("Local work create created an HTTP client")
	}
}

func TestLocalPluginInstallUsesEmbeddedPackageWithoutHTTP(t *testing.T) {
	deps, out := newTestDeps(t, "")
	stateDir := filepath.Join(t.TempDir(), "local")
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--no-input", "init", "--mode", "local", "--local-dir", stateDir, "--workspace-name", "Alpha", "--display-name", "Human", "--username", "human"); code != output.ExitOK {
		t.Fatalf("init exit = %d; output=%s", code, out.String())
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "plugins", "install", "--agent", "codex"); code != output.ExitOK {
		t.Fatalf("plugin install exit = %d; output=%s", code, out.String())
	}
	if !strings.Contains(out.String(), `"agents":["codex"]`) || deps.Client != nil {
		t.Fatalf("plugin output = %s client=%#v", out.String(), deps.Client)
	}
	home, err := deps.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	var installedSkills string
	for _, skill := range []string{"specgate", "specgate-project-setup", "specgate-work-preparation", "specgate-work-delivery"} {
		body, err := os.ReadFile(filepath.Join(home, ".codex", "plugins", "specgate", "skills", skill, "SKILL.md"))
		if err != nil {
			t.Fatal(err)
		}
		installedSkills += string(body)
	}
	for _, want := range []string{
		"data.mode",
		"gates tasks list",
		"gates tasks submit-result",
		"human explicitly requests it",
		"SpecGate delivery receipt",
		"For `awaiting_acceptance`",
	} {
		if !strings.Contains(installedSkills, want) {
			t.Fatalf("installed Local skills missing %q", want)
		}
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "plugins", "install", "--agent", "cursor,claude"); code != output.ExitOK {
		t.Fatalf("Local Cursor/Claude install exit=%d output=%s", code, out.String())
	}
	if deps.Client != nil {
		t.Fatal("Local plugin install created an HTTP client")
	}
	for _, path := range []string{
		filepath.Join(home, ".cursor", "rules", "using-specgate.mdc"),
		filepath.Join(home, ".claude", "skills", "specgate", ".claude-plugin", "plugin.json"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("embedded Local plugin missing %s: %v", path, err)
		}
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "plugins", "doctor", "--agent", "all"); code != output.ExitOK {
		t.Fatalf("Local all-agent plugin doctor exit=%d output=%s", code, out.String())
	}
}

func TestLocalParentCommandsShowHelp(t *testing.T) {
	for _, family := range []string{"artifact", "delivery", "gates", "plugins", "portable"} {
		t.Run(family, func(t *testing.T) {
			deps, out := newTestDeps(t, "")
			deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
			if err := (config.Config{Mode: config.ModeLocal}).SaveTo(deps.ConfigPath); err != nil {
				t.Fatal(err)
			}

			code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", family)
			if code != output.ExitOK {
				t.Fatalf("bare Local command exit = %d, output = %s", code, out.String())
			}
			if !strings.Contains(out.String(), "Usage:") {
				t.Fatalf("bare Local command did not show help: %s", out.String())
			}
			if strings.Contains(out.String(), "requires Full mode") {
				t.Fatalf("bare Local command was rejected as Full-only: %s", out.String())
			}
		})
	}
}
