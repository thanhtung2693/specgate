package command_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/output"
)

// TestDoctorGreenExitsOK verifies doctor exits 0 when server is healthy.
func TestDoctorFixRejectsLocalMode(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t, "")
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	if err := (config.Config{
		Mode:  config.ModeLocal,
		Local: config.LocalStore{Path: filepath.Join(t.TempDir(), "local")},
	}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "doctor", "--fix")
	if code != output.ExitIncompatible {
		t.Fatalf("exit = %d, want %d, output = %s", code, output.ExitIncompatible, out.String())
	}
	if !strings.Contains(out.String(), "Full appliance") || !strings.Contains(out.String(), "specgate doctor") {
		t.Fatalf("output should explain the mode boundary and recovery command: %s", out.String())
	}
}

func TestDoctorReportsKnowledgeEmbeddingsMissing(t *testing.T) {
	t.Parallel()
	srv := (&fakeServer{
		metaHandler: jsonMeta("specgate.api/v1", map[string]bool{"agents": true}),
		settingsHandler: func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"settings": map[string]string{
					"governance.model_provider": "google_genai",
					"governance.model":          "gemini-3.1-flash-lite",
					"google.api_key":            "***",
					"embedding.model_provider":  "",
					"embedding.model":           "",
				},
			})
		},
	}).build(t)

	deps, out := newTestDeps(t, srv.URL)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--server", srv.URL, "doctor")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var env struct {
		Data struct {
			KnowledgeEmbeddings struct {
				Status  string `json:"status"`
				Message string `json:"message"`
				Command string `json:"command"`
			} `json:"knowledge_embeddings"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("doctor JSON decode failed: %v; output=%s", err, out.String())
	}
	if env.Data.KnowledgeEmbeddings.Status != "missing" {
		t.Fatalf("knowledge_embeddings = %+v, want missing", env.Data.KnowledgeEmbeddings)
	}
	if !strings.Contains(env.Data.KnowledgeEmbeddings.Message, "Knowledge embeddings are not configured") {
		t.Fatalf("unexpected embedding message: %+v", env.Data.KnowledgeEmbeddings)
	}
	if env.Data.KnowledgeEmbeddings.Command != "specgate model set" {
		t.Fatalf("embedding command = %q", env.Data.KnowledgeEmbeddings.Command)
	}
}

// TestDoctorReportsKnowledgeEmbeddingsConfigured proves the key-presence
// normalization: a provider + model + non-empty (redacted) api_key reads as ok,
// using the same rule as the model check — not a comparison against "set".
func TestDoctorReportsKnowledgeEmbeddingsConfigured(t *testing.T) {
	t.Parallel()
	srv := (&fakeServer{
		metaHandler: jsonMeta("specgate.api/v1", map[string]bool{"agents": true}),
		settingsHandler: func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"settings": map[string]string{
					"governance.model_provider": "google_genai",
					"governance.model":          "gemini-3.1-flash-lite",
					"google.api_key":            "***",
					"embedding.model_provider":  "openai",
					"embedding.model":           "text-embedding-3-small",
					"openai.api_key":            "***",
				},
			})
		},
	}).build(t)

	deps, out := newTestDeps(t, srv.URL)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--server", srv.URL, "doctor")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var env struct {
		Data struct {
			KnowledgeEmbeddings struct {
				Status  string `json:"status"`
				Message string `json:"message"`
			} `json:"knowledge_embeddings"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("doctor JSON decode failed: %v; output=%s", err, out.String())
	}
	if env.Data.KnowledgeEmbeddings.Status != "ok" {
		t.Fatalf("knowledge_embeddings = %+v, want ok", env.Data.KnowledgeEmbeddings)
	}
	if !strings.Contains(env.Data.KnowledgeEmbeddings.Message, "openai") || !strings.Contains(env.Data.KnowledgeEmbeddings.Message, "text-embedding-3-small") {
		t.Fatalf("embedding message should name provider+model: %+v", env.Data.KnowledgeEmbeddings)
	}
}

func TestDoctorGreenExitsOK(t *testing.T) {
	t.Parallel()
	srv := (&fakeServer{
		metaHandler: jsonMeta("specgate.api/v1", map[string]bool{"agents": true}),
	}).build(t)

	deps, out := newTestDeps(t, srv.URL)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--server", srv.URL, "doctor")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, want 0", code)
	}
	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			Mode   string `json:"mode"`
			Server struct {
				Status string `json:"status"`
			} `json:"server"`
			Plugins struct {
				Command string `json:"command"`
			} `json:"plugins"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("doctor JSON decode failed: %v; output=%s", err, out.String())
	}
	if !env.OK || env.Data.Mode != "full" || env.Data.Server.Status != "ok" || env.Data.Plugins.Command != "specgate plugins doctor" {
		t.Fatalf("doctor JSON should expose setup summary, got: %s", out.String())
	}
}

func TestDoctorReportsIncompatibleDatabaseSchema(t *testing.T) {
	t.Parallel()
	srv := (&fakeServer{
		metaHandler: jsonMeta("specgate.api/v1", map[string]bool{"agents": true}),
		schemaHandler: func(w http.ResponseWriter, r *http.Request) {
			if got := r.URL.Query().Get("workspace_id"); got != "ws-1" {
				http.Error(w, "workspace_id is required", http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":  "incompatible",
				"message": "database schema is missing required artifact columns: artifacts.policy_snapshot_json",
			})
		},
	}).build(t)

	deps, out := newTestDeps(t, srv.URL)
	if err := (config.Config{Workspace: config.CurrentWorkspace{ID: "ws-1", Slug: "platform", Name: "Platform"}}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--server", srv.URL, "doctor")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var env struct {
		Data struct {
			DatabaseSchema struct {
				Status  string `json:"status"`
				Message string `json:"message"`
				Command string `json:"command"`
			} `json:"database_schema"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("doctor JSON decode failed: %v; output=%s", err, out.String())
	}
	if env.Data.DatabaseSchema.Status != "incompatible" {
		t.Fatalf("database_schema = %+v, want incompatible", env.Data.DatabaseSchema)
	}
	if !strings.Contains(env.Data.DatabaseSchema.Message, "artifacts.policy_snapshot_json") {
		t.Fatalf("database schema message missing required column: %+v", env.Data.DatabaseSchema)
	}
	if env.Data.DatabaseSchema.Command != "specgate doctor" {
		t.Fatalf("database schema command = %q", env.Data.DatabaseSchema.Command)
	}
}

func TestDoctorShowsActionableSetupSummary(t *testing.T) {
	t.Parallel()
	srv := (&fakeServer{
		metaHandler: jsonMeta("specgate.api/v1", map[string]bool{"agents": true}),
		statusHandler: func(w http.ResponseWriter, r *http.Request) {
			if got := r.URL.Query().Get("workspace_id"); got != "ws-1" {
				http.Error(w, "workspace_id is required", http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{"counts":{},"attention":[]}`))
		},
		settingsHandler: func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"settings": map[string]string{
					"governance.model_provider":         "google_genai",
					"governance.model":                  "gemini-3.1-flash-lite",
					"governance.default_thinking_level": "medium",
					"google.api_key":                    "***",
				},
			})
		},
	}).build(t)

	deps, out := newTestDeps(t, srv.URL)
	if err := (config.Config{
		CurrentUser: config.CurrentUser{ID: "user-1", Username: "thanhtung2693", DisplayName: "Thanh Tung"},
		Workspace:   config.CurrentWorkspace{ID: "ws-1", Slug: "platform", Name: "Platform"},
	}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--server", srv.URL, "doctor")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	got := out.String()
	for _, want := range []string{
		"Server:",
		"Identity:",
		"Workspace:",
		"Model:",
		// The Model line surfaces the thinking-level lever so operators can see
		// and tune the governance judgment budget.
		"thinking: medium",
		"IDE plugins:",
		"Next:",
		"specgate plugins doctor",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, got)
		}
	}
}

// TestDoctorWarnsSelfAttestedWhenModelKeyMissing: with a provider/model set
// but no API key, the Model line must state the governance consequence —
// self-attested delivery verdicts and no server-side model review — not just
// that a key is missing.
func TestDoctorWarnsSelfAttestedWhenModelKeyMissing(t *testing.T) {
	t.Parallel()
	srv := (&fakeServer{
		metaHandler: jsonMeta("specgate.api/v1", map[string]bool{"agents": true}),
		settingsHandler: func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"settings": map[string]string{
					"governance.model_provider": "google_genai",
					"governance.model":          "gemini-3.1-flash-lite",
					"google.api_key":            "",
				},
			})
		},
	}).build(t)

	deps, out := newTestDeps(t, srv.URL)
	if err := (config.Config{
		CurrentUser: config.CurrentUser{ID: "user-1", Username: "thanhtung2693", DisplayName: "Thanh Tung"},
		Workspace:   config.CurrentWorkspace{ID: "ws-1", Slug: "platform", Name: "Platform"},
	}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--server", srv.URL, "doctor") //nolint:errcheck
	got := out.String()
	for _, want := range []string{"self-attested", "server-side model review", "specgate model set", "specgate gates tasks dispatch <artifact-id>"} {
		if !strings.Contains(got, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, got)
		}
	}
}

// TestDoctorShowsFullApplianceSection verifies doctor renders a "Full appliance"
// section (same data as `local-status`) when a CLI-managed deployment exists.
func TestDoctorShowsFullApplianceSection(t *testing.T) {
	t.Parallel()
	srv := (&fakeServer{
		metaHandler: jsonMeta("specgate.api/v1", map[string]bool{"agents": true}),
	}).build(t)

	dir := t.TempDir()
	setupTestBundle(t, dir)

	deps, out := newTestDeps(t, srv.URL)
	if err := (config.Config{DeploymentDir: dir}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	deps.DeployRunner = &fakeDeployRunner{
		OutputData: []byte(`[{"Name":"doc-registry","Status":"running (healthy)"}]`),
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--server", srv.URL, "doctor")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	got := out.String()
	for _, want := range []string{"Full appliance", dir, "doc-registry", "running (healthy)"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}

// TestDoctorSkipsFullApplianceWithoutDeployment verifies doctor omits the Full
// appliance section when no CLI-managed deployment exists.
func TestDoctorSkipsFullApplianceWithoutDeployment(t *testing.T) {
	t.Parallel()
	srv := (&fakeServer{
		metaHandler: jsonMeta("specgate.api/v1", map[string]bool{"agents": true}),
	}).build(t)

	deps, out := newTestDeps(t, srv.URL)
	// DeploymentDir points at an empty dir: no compose.yml → no section.
	if err := (config.Config{DeploymentDir: t.TempDir()}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	deps.DeployRunner = &fakeDeployRunner{}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--server", srv.URL, "doctor")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if strings.Contains(out.String(), "Full appliance") {
		t.Fatalf("output should not contain a Full appliance section:\n%s", out.String())
	}
}

func TestDoctorFixOffersFullApplianceRepairChecklist(t *testing.T) {
	var statusCalls int
	srv := (&fakeServer{
		metaHandler: jsonMeta("specgate.api/v1", map[string]bool{"agents": true}),
		statusHandler: func(w http.ResponseWriter, r *http.Request) {
			if got := r.URL.Query().Get("workspace_id"); got != "ws-1" {
				http.Error(w, "workspace_id is required", http.StatusBadRequest)
				return
			}
			statusCalls++
			if statusCalls == 1 {
				http.Error(w, "starting", http.StatusServiceUnavailable)
				return
			}
			_, _ = w.Write([]byte(`{"counts":{},"attention":[]}`))
		},
	}).build(t)

	dir := t.TempDir()
	setupTestBundle(t, dir)

	runner := &fakeDeployRunner{}
	prompter := &fakePrompter{multiValues: []string{"full-appliance"}}
	deps, out := newTestDeps(t, srv.URL)
	deps.DeployRunner = runner
	deps.Prompter = prompter
	if err := (config.Config{DeploymentDir: dir, Workspace: config.CurrentWorkspace{ID: "ws-1", Slug: "platform", Name: "Platform"}}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--server", srv.URL, "doctor", "--fix")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if prompter.multiTitle != "Repair SpecGate environment" {
		t.Fatalf("multi-select title = %q", prompter.multiTitle)
	}
	gotCommands := strings.Join(runner.Commands, "\n")
	if !strings.Contains(gotCommands, "docker compose -f "+filepath.Join(dir, "compose.yml")+" up -d --wait") {
		t.Fatalf("missing compose up repair command in:\n%s", gotCommands)
	}
	if statusCalls < 2 {
		t.Fatalf("doctor should re-run checks after repair; status calls = %d", statusCalls)
	}
	if !strings.Contains(out.String(), "Environment repaired successfully.") {
		t.Fatalf("output missing repaired message:\n%s", out.String())
	}
}

func TestDoctorFixYesRepairsWithoutPrompt(t *testing.T) {
	var statusCalls int
	srv := (&fakeServer{
		metaHandler: jsonMeta("specgate.api/v1", map[string]bool{"agents": true}),
		statusHandler: func(w http.ResponseWriter, r *http.Request) {
			if got := r.URL.Query().Get("workspace_id"); got != "ws-1" {
				http.Error(w, "workspace_id is required", http.StatusBadRequest)
				return
			}
			statusCalls++
			if statusCalls == 1 {
				http.Error(w, "starting", http.StatusServiceUnavailable)
				return
			}
			_, _ = w.Write([]byte(`{"counts":{},"attention":[]}`))
		},
	}).build(t)

	dir := t.TempDir()
	setupTestBundle(t, dir)

	runner := &fakeDeployRunner{}
	prompter := &fakePrompter{}
	deps, out := newTestDeps(t, srv.URL)
	deps.DeployRunner = runner
	deps.Prompter = prompter
	if err := (config.Config{DeploymentDir: dir, Workspace: config.CurrentWorkspace{ID: "ws-1", Slug: "platform", Name: "Platform"}}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "--server", srv.URL, "doctor", "--fix")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if prompter.multiTitle != "" {
		t.Fatalf("--yes should not prompt, got title %q", prompter.multiTitle)
	}
	gotCommands := strings.Join(runner.Commands, "\n")
	if !strings.Contains(gotCommands, "docker compose -f "+filepath.Join(dir, "compose.yml")+" up -d --wait") {
		t.Fatalf("missing compose up repair command in:\n%s", gotCommands)
	}
	if statusCalls < 2 {
		t.Fatalf("doctor should re-run checks after repair; status calls = %d", statusCalls)
	}
}

// TestDoctorAgentsUnavailableExits5 verifies agents.state=unavailable → exit 5.
func TestDoctorAgentsUnavailableExits5(t *testing.T) {
	t.Parallel()
	srv := (&fakeServer{
		metaHandler: jsonMeta("specgate.api/v1", map[string]bool{"agents": false}),
	}).build(t)

	deps, _ := newTestDeps(t, srv.URL)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--server", srv.URL, "doctor")
	if code != output.ExitUnavailable {
		t.Fatalf("exit = %d, want %d (ExitUnavailable)", code, output.ExitUnavailable)
	}
}

func TestDoctorMissingAgentsCapabilityExits6(t *testing.T) {
	t.Parallel()
	srv := (&fakeServer{
		metaHandler: jsonMeta("specgate.api/v1", nil),
	}).build(t)

	deps, _ := newTestDeps(t, srv.URL)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--server", srv.URL, "doctor")
	if code != output.ExitIncompatible {
		t.Fatalf("exit = %d, want %d (ExitIncompatible)", code, output.ExitIncompatible)
	}
}

// TestDoctorBadAPIVersionExits6 verifies a mismatched api_version → exit 6.
func TestDoctorBadAPIVersionExits6(t *testing.T) {
	t.Parallel()
	srv := (&fakeServer{
		metaHandler: jsonMeta("specgate.api/v0", nil),
	}).build(t)

	deps, _ := newTestDeps(t, srv.URL)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--server", srv.URL, "doctor")
	if code != output.ExitIncompatible {
		t.Fatalf("exit = %d, want %d (ExitIncompatible)", code, output.ExitIncompatible)
	}
}

// TestDoctorServerDownExits5 verifies unreachable server → exit 5.
func TestDoctorServerDownExits5(t *testing.T) {
	t.Parallel()
	deps, _ := newTestDeps(t, "")
	// Point at an address that refuses connections.
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--server", "http://127.0.0.1:1", "doctor")
	if code != output.ExitUnavailable {
		t.Fatalf("exit = %d, want %d (ExitUnavailable)", code, output.ExitUnavailable)
	}
}

func TestDoctorUsesInContainerDiagnosticsWhenGatewayProbeFails(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/doc-registry/healthz/components" {
			http.Error(w, "gateway unavailable", http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	deployDir := t.TempDir()
	setupTestBundle(t, deployDir)
	runner := &fakeDeployRunner{OutputByCommand: map[string][]byte{}}
	commandLine := "docker compose -f " + filepath.Join(deployDir, "compose.yml") + " exec -T specgate curl --fail --silent --show-error --max-time 5 http://127.0.0.1:9090/healthz/components"
	runner.OutputByCommand[commandLine] = []byte(`{"status":"degraded","components":{"nginx":{"status":"fail","state":"failed"}}}`)
	deps, out := newTestDeps(t, srv.URL+"/api/doc-registry")
	deps.DeployRunner = runner
	if err := (config.Config{DeploymentDir: deployDir}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--server", srv.URL+"/api/doc-registry", "doctor")
	if code != output.ExitUnavailable {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "nginx=fail") {
		t.Fatalf("doctor output does not identify nginx: %s", out.String())
	}
}

func TestDoctorFailsWhenBoardEndpointIsDown(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.statusErr = errors.New("Internal Server Error: governance-status")
	if err := (config.Config{Workspace: config.CurrentWorkspace{ID: "ws-1", Slug: "platform", Name: "Platform"}}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "doctor")

	if code == output.ExitOK {
		t.Fatalf("doctor reported OK while the board endpoint is failing:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "board") {
		t.Fatalf("output should name the board probe:\n%s", out.String())
	}
}
