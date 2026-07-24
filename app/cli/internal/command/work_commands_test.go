package command_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

// --- fakes ---

func newFakeDeps(t *testing.T) (*command.Deps, *fakeClient, *fakePrompter, *bytes.Buffer) {
	t.Helper()
	fc := &fakeClient{}
	fp := &fakePrompter{}
	var out bytes.Buffer
	homeDir := t.TempDir()
	// stderr shares the buffer: human-mode errors print there, and tests
	// assert on combined output.
	printer := output.New(&out, &out, output.ModeHuman)
	deps := &command.Deps{
		Stdout:     &out,
		Stderr:     &out,
		Stdin:      strings.NewReader(""),
		Client:     fc,
		Prompter:   fp,
		Opener:     func(_ string) error { return nil },
		Printer:    printer,
		ConfigPath: filepath.Join(t.TempDir(), "config.json"),
		WorkingDir: t.TempDir(),
		UserHomeDir: func() (string, error) {
			return homeDir, nil
		},
	}
	return deps, fc, fp, &out
}

// --- tests ---

// TestWorkListByPhaseEnumeratesItems: `work list --phase ready` must list the
// actual pickup-ready items with refs, so an agent (or human) who did not create
// the work can discover what to pick up. Default `work list` (attention queue)
// cannot do this.
func TestWorkListByPhaseEnumeratesItems(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	setWorkListWorkspace(t, deps)
	fc.workItems = []client.WorkItemSummary{
		{Key: "CR-100", Title: "Queue-backed ingest", Phase: "Ready", WorkType: "feature"},
		{Key: "CR-200", Title: "Already shipped", Phase: "Delivered", WorkType: "bug_fix"},
		{Key: "CR-300", Title: "Drift detection", Phase: "Ready", WorkType: "feature"},
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "work", "list", "--phase", "ready")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "CR-100") || !strings.Contains(got, "CR-300") {
		t.Fatalf("phase filter dropped matching items:\n%s", got)
	}
	if !strings.Contains(got, "Queue-backed ingest") {
		t.Fatalf("output should show titles for pickup:\n%s", got)
	}
	if strings.Contains(got, "CR-200") {
		t.Fatalf("delivered item leaked into ready listing:\n%s", got)
	}
}

func TestWorkListByPhaseAcceptsCommaSeparatedPhases(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	setWorkListWorkspace(t, deps)
	fc.workItems = []client.WorkItemSummary{
		{Key: "CR-100", Title: "A", Phase: "Ready"},
		{Key: "CR-200", Title: "B", Phase: "Ready"},
		{Key: "CR-300", Title: "C", Phase: "Delivered"},
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "work", "list", "--phase", "ready")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "CR-100") || !strings.Contains(got, "CR-200") {
		t.Fatalf("ready filter missing items:\n%s", got)
	}
	if strings.Contains(got, "CR-300") {
		t.Fatalf("delivered leaked:\n%s", got)
	}
}

func TestWorkListByPhaseJSONReturnsItems(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	setWorkListWorkspace(t, deps)
	fc.workItems = []client.WorkItemSummary{
		{Key: "CR-100", Title: "A", Phase: "Ready", WorkType: "feature"},
		{Key: "CR-200", Title: "B", Phase: "Delivered"},
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "work", "list", "--phase", "ready")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			Items []struct {
				Key   string `json:"key"`
				Phase string `json:"phase"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v; out=%s", err, out.String())
	}
	if !env.OK || len(env.Data.Items) != 1 || env.Data.Items[0].Key != "CR-100" {
		t.Fatalf("json items = %+v", env.Data.Items)
	}
}

func TestWorkListByPhaseRejectsAllWorkspaces(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "work", "list", "--all-workspaces", "--phase", "ready")
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want usage; output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "cannot be combined") {
		t.Fatalf("output missing flag guidance:\n%s", out.String())
	}
	if fc.lastWorkItemsWorkspaceID != "" || fc.lastStatusWorkspaceID != "" {
		t.Fatalf("unexpected API call: work workspace=%q status workspace=%q", fc.lastWorkItemsWorkspaceID, fc.lastStatusWorkspaceID)
	}
}

func setWorkListWorkspace(t *testing.T, deps *command.Deps) {
	t.Helper()
	if err := (config.Config{Workspace: config.CurrentWorkspace{ID: "ws-phase", Slug: "phase"}}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
}

func TestWorkListShowsNeedsAttentionItems(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.statusResult = &client.GovernanceStatus{
		Counts: client.PhaseCounts{Total: 2, Ready: 1},
		NeedsAttention: []client.NeedsAttentionItem{
			{ID: "cr-1", Key: "CR-101", Title: "Add login", Phase: "ready", Issues: []string{"agent_pickup"}},
			{ID: "cr-2", Key: "CR-102", Title: "Fix crash", Phase: "in_progress", Issues: []string{"review_needed"}},
		},
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "work", "list", "--all-workspaces")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "CR-101") {
		t.Errorf("output missing CR-101:\n%s", got)
	}
	if !strings.Contains(got, "CR-102") {
		t.Errorf("output missing CR-102:\n%s", got)
	}
}

func TestWorkListUsesSelectedWorkspace(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	err := (config.Config{
		Workspace: config.CurrentWorkspace{ID: "ws-1", Slug: "platform"},
	}).SaveTo(deps.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	fc.statusResult = &client.GovernanceStatus{}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "work", "list")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastStatusWorkspaceID != "ws-1" {
		t.Fatalf("status workspace = %q, want ws-1", fc.lastStatusWorkspaceID)
	}
}

func TestWorkListEmptyStateExplainsOtherPhases(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.statusResult = &client.GovernanceStatus{
		Counts: client.PhaseCounts{Total: 4, Ready: 4},
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "work", "list", "--all-workspaces")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	got := out.String()
	for _, want := range []string{
		"No work items need attention.",
		"4 work item(s) are tracked in other phases",
		"ready 4",
		"Next: run `specgate status`",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}

func TestWorkListWithoutWorkspaceRequiresExplicitAllWorkspaces(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	fc.statusResult = &client.GovernanceStatus{}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "work", "list")
	if code == output.ExitOK {
		t.Fatalf("work list unexpectedly succeeded without workspace: %s", out.String())
	}
	if !strings.Contains(out.String(), "select a workspace first") || !strings.Contains(out.String(), "--all-workspaces") {
		t.Fatalf("output missing scope guidance:\n%s", out.String())
	}
}

func TestWorkListAllWorkspacesSkipsSelectedWorkspace(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	err := (config.Config{
		Workspace: config.CurrentWorkspace{ID: "ws-1", Slug: "platform"},
	}).SaveTo(deps.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	fc.statusResult = &client.GovernanceStatus{}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "work", "list", "--all-workspaces")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastStatusWorkspaceID != "" {
		t.Fatalf("status workspace = %q, want all-workspaces empty filter", fc.lastStatusWorkspaceID)
	}
}

func TestWorkListJSONEnvelope(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.statusResult = &client.GovernanceStatus{
		Counts: client.PhaseCounts{Total: 1, Ready: 1},
		NeedsAttention: []client.NeedsAttentionItem{
			{ID: "cr-1", Key: "CR-101", Title: "Add login", Phase: "ready"},
		},
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "work", "list", "--all-workspaces")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var env struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v, output: %s", err, out.String())
	}
	if !env.OK {
		t.Fatalf("ok = false, output: %s", out.String())
	}
}

func TestWorkShowResolvesRef(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "work", "show", "CR-101")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastWorkRef != "CR-101" {
		t.Fatalf("lastWorkRef = %q, want CR-101", fc.lastWorkRef)
	}
}

func TestWorkShowPromptsWhenRefMissing(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.statusResult = &client.GovernanceStatus{
		NeedsAttention: []client.NeedsAttentionItem{
			{ID: "cr-1", Key: "CR-101", Title: "Add login", Phase: "ready"},
		},
	}
	deps.Prompter = &fakePrompter{selectedValue: "CR-101"}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "work", "show")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastWorkRef != "CR-101" {
		t.Fatalf("lastWorkRef = %q, want CR-101", fc.lastWorkRef)
	}
}

func TestWorkShowNoInputRequiresRef(t *testing.T) {
	t.Parallel()
	deps, _, _, _ := newFakeDeps(t)

	cmd := command.NewRootCommand(deps)
	cmd.SetArgs([]string{"--no-input", "work", "show"})
	err := cmd.Execute()
	if !errors.Is(err, command.ErrInputRequired) {
		t.Fatalf("error = %v, want ErrInputRequired", err)
	}
}

func TestWorkArchiveArchivesEachResolvedRef(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	err := (config.Config{CurrentUser: config.CurrentUser{Username: "thanhtung2693"}}).SaveTo(deps.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(
		command.NewRootCommand(deps),
		"--plain",
		"--yes",
		"work",
		"archive",
		"--reason",
		"done",
		"CR-101",
	)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastArchiveID != "cr-1" {
		t.Fatalf("lastArchiveID = %q, want cr-1", fc.lastArchiveID)
	}
	if fc.lastArchiveReason != "done" {
		t.Fatalf("lastArchiveReason = %q, want done", fc.lastArchiveReason)
	}
	if fc.lastArchiveActor != "thanhtung2693" {
		t.Fatalf("lastArchiveActor = %q, want thanhtung2693", fc.lastArchiveActor)
	}
	if !strings.Contains(out.String(), "Archived CR-ARCHIVE") {
		t.Fatalf("output = %q, want archive confirmation", out.String())
	}
}

func TestWorkArchiveJSONWithoutYesProceeds(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "work", "archive", "CR-101")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastArchiveID != "cr-1" {
		t.Fatalf("lastArchiveID = %q, want cr-1", fc.lastArchiveID)
	}
	if strings.Contains(out.String(), "Archive 1 work item") || strings.Contains(out.String(), "Cancelled.") {
		t.Fatalf("json mode must not prompt or print human cancellation text:\n%s", out.String())
	}
	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out.String())
	}
	if !env.OK || len(env.Data.Items) != 1 {
		t.Fatalf("envelope = %+v, output = %s", env, out.String())
	}
}

func TestWorkCreateQuickAddsCurrentIdentity(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	err := (config.Config{
		CurrentUser: config.CurrentUser{Username: "thanhtung2693"},
		Workspace:   config.CurrentWorkspace{ID: "5367ce6c-53cd-4891-a56a-229bb25d3f41", Slug: "specgate"},
	}).SaveTo(deps.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	body := filepath.Join(t.TempDir(), "work.json")
	if err := os.WriteFile(body, []byte(`{"title":"Fix bug","description":"Bug details"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "work", "create-quick", "--file", body)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if got := fc.lastCreateBody["created_by"]; got != "thanhtung2693" {
		t.Fatalf("created_by = %v, want thanhtung2693", got)
	}
	if got := fc.lastCreateBody["workspace_id"]; got != "5367ce6c-53cd-4891-a56a-229bb25d3f41" {
		t.Fatalf("workspace_id = %v", got)
	}
}

func TestWorkCreateQuickKeepsExplicitWorkspaceIDWithoutLookup(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	err := (config.Config{
		CurrentUser: config.CurrentUser{Username: "thanhtung2693"},
		Workspace:   config.CurrentWorkspace{Slug: "platform"},
	}).SaveTo(deps.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	body := filepath.Join(t.TempDir(), "work.json")
	if err := os.WriteFile(body, []byte(`{"title":"Fix bug","description":"Bug details","workspace_id":"explicit-ws"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "work", "create-quick", "--file", body)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastWorkspaceID != "" {
		t.Fatalf("workspace lookup = %q, want none for explicit workspace_id", fc.lastWorkspaceID)
	}
	if got := fc.lastCreateBody["workspace_id"]; got != "explicit-ws" {
		t.Fatalf("workspace_id = %v, want explicit-ws", got)
	}
	if got := fc.lastCreateWorkspaceID; got != "explicit-ws" {
		t.Fatalf("request workspace = %q, want explicit-ws", got)
	}
	if got := fc.lastCreateBody["created_by"]; got != "thanhtung2693" {
		t.Fatalf("created_by = %v, want thanhtung2693", got)
	}
}

func TestWorkCreateQuickResolvesWorkspaceOverrideSlug(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	fc.workspaces = []client.IdentityWorkspace{{ID: "ws-platform", Slug: "platform", Name: "Platform"}}
	err := (config.Config{
		CurrentUser: config.CurrentUser{Username: "thanhtung2693"},
		Workspace:   config.CurrentWorkspace{ID: "ws-global", Slug: "global"},
	}).SaveTo(deps.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--workspace", "platform", "work", "create-quick", "Fix bug")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastWorkspaceID != "platform" {
		t.Fatalf("workspace lookup = %q, want platform", fc.lastWorkspaceID)
	}
	if got := fc.lastCreateBody["workspace_id"]; got != "ws-platform" {
		t.Fatalf("workspace_id = %v, want ws-platform", got)
	}
}

func TestWorkContextFetchesPackByID(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.contextPack = &client.ContextPackResult{State: "assembled", Markdown: "# My Context Pack"}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "work", "context", "CR-101")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "# My Context Pack") {
		t.Errorf("output missing context markdown:\n%s", out.String())
	}
	if fc.lastContextID != "cr-1" {
		t.Errorf("lastContextID = %q, want cr-1 (resolved ID)", fc.lastContextID)
	}
}

func TestWorkCreateQuickFromFile(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)

	f := filepath.Join(t.TempDir(), "issue.json")
	if err := os.WriteFile(f, []byte(`{"title":"Fix crash","description":"Crashes on login"}`), 0644); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "work", "create-quick", "--file", f)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastCreateBody["title"] != "Fix crash" {
		t.Fatalf("title = %v, want Fix crash", fc.lastCreateBody["title"])
	}
}

func TestWorkCreateQuickRunsEntirelyInLocalMode(t *testing.T) {
	deps, fc, _, out := newFakeDeps(t)
	stateDir := t.TempDir()
	store, err := local.Open(filepath.Join(stateDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Initialize(t.Context(), local.InitInput{
		WorkspaceName: "Local workspace", DisplayName: "Local developer", Username: "local",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := (config.Config{Mode: config.ModeLocal, Local: config.LocalStore{Path: stateDir}}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(
		command.NewRootCommand(deps),
		"--json", "work", "create-quick", "Fix crash",
		"--description", "Prevent login crash",
		"--ac", "Login succeeds @check:unit",
	)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.calls != 0 {
		t.Fatalf("Local create-quick made %d remote calls", fc.calls)
	}
	var envelope struct {
		Data struct {
			Key             string `json:"change_request_key"`
			LeadArtifactID  string `json:"lead_artifact_id"`
			AcceptanceCount int    `json:"acceptance_count"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Key == "" || envelope.Data.LeadArtifactID != "" || envelope.Data.AcceptanceCount != 1 {
		t.Fatalf("result = %#v, output = %s", envelope.Data, out.String())
	}
}

func TestWorkCreateQuickNoInputRequiresFile(t *testing.T) {
	t.Parallel()
	deps, _, _, _ := newFakeDeps(t)

	cmd := command.NewRootCommand(deps)
	cmd.SetArgs([]string{"--no-input", "work", "create-quick"})
	err := cmd.Execute()
	if !errors.Is(err, command.ErrInputRequired) {
		t.Fatalf("error = %v, want ErrInputRequired", err)
	}
}

func TestWorkCreateQuickPositionalTitleSkipsPrompts(t *testing.T) {
	t.Parallel()
	deps, fc, fp, out := newFakeDeps(t)

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain",
		"work", "create-quick", "Fix crash",
		"--description", "Crashes on login",
		"--ac", "Login succeeds with valid creds",
		"--ac", "Crash regression test added")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fp.inputCalls != 0 {
		t.Fatalf("inputCalls = %d, want 0 (title arg skips prompting)", fp.inputCalls)
	}
	if fc.lastCreateBody["title"] != "Fix crash" {
		t.Fatalf("title = %v", fc.lastCreateBody["title"])
	}
	if fc.lastCreateBody["description"] != "Crashes on login" {
		t.Fatalf("description = %v", fc.lastCreateBody["description"])
	}
	acs, ok := fc.lastCreateBody["acceptance_criteria"].([]map[string]string)
	if !ok || len(acs) != 2 || acs[0]["text"] != "Login succeeds with valid creds" {
		t.Fatalf("acceptance_criteria = %v", fc.lastCreateBody["acceptance_criteria"])
	}
	if _, ok := acs[0]["verification_binding"]; ok {
		t.Fatalf("plain criterion unexpectedly carried binding: %#v", acs[0])
	}
}

func TestWorkCreateQuickParsesAcceptanceCriterionCheckBinding(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain",
		"work", "create-quick", "Fix crash",
		"--ac", "Login succeeds with valid creds @check:integration",
		"--ac", "Crash regression test added")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}

	acs, ok := fc.lastCreateBody["acceptance_criteria"].([]map[string]string)
	if !ok || len(acs) != 2 {
		t.Fatalf("acceptance_criteria = %#v", fc.lastCreateBody["acceptance_criteria"])
	}
	if acs[0]["text"] != "Login succeeds with valid creds" {
		t.Fatalf("bound criterion text = %q", acs[0]["text"])
	}
	if acs[0]["verification_binding"] != "integration" {
		t.Fatalf("bound criterion binding = %q, want integration", acs[0]["verification_binding"])
	}
	if acs[1]["text"] != "Crash regression test added" {
		t.Fatalf("unbound criterion text = %q", acs[1]["text"])
	}
	if _, ok := acs[1]["verification_binding"]; ok {
		t.Fatalf("unbound criterion unexpectedly carried binding: %#v", acs[1])
	}
}

func TestWorkCreateQuickTitleArgWorksNoInput(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json",
		"work", "create-quick", "Fix crash", "--ac", "One AC")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastCreateBody["title"] != "Fix crash" {
		t.Fatalf("title = %v", fc.lastCreateBody["title"])
	}
	if fc.lastCreateBody["description"] != "Fix crash" {
		t.Fatalf("description = %v", fc.lastCreateBody["description"])
	}
}

func TestWorkCreateQuickTitleAndFileConflict(t *testing.T) {
	t.Parallel()
	deps, fc, _, _ := newFakeDeps(t)
	f := filepath.Join(t.TempDir(), "issue.json")
	if err := os.WriteFile(f, []byte(`{"title":"From file"}`), 0644); err != nil {
		t.Fatal(err)
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain",
		"work", "create-quick", "Arg title", "--file", f)
	if code == output.ExitOK {
		t.Fatal("expected non-zero exit when both a title argument and --file are given")
	}
	if fc.calls != 0 {
		t.Fatalf("calls = %d, want 0", fc.calls)
	}
}

func TestWorkCreateQuickInteractiveCollectsAcceptanceCriteria(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fp := &fakePrompter{inputValues: []string{
		"My title",  // title
		"My desc",   // description
		"First AC",  // criterion 1
		"Second AC", // criterion 2
		"",          // empty → finish
	}}
	deps.Prompter = fp

	code := command.ExecuteForCode(command.NewRootCommand(deps), "work", "create-quick")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastCreateBody["title"] != "My title" {
		t.Fatalf("title = %v", fc.lastCreateBody["title"])
	}
	acs, ok := fc.lastCreateBody["acceptance_criteria"].([]map[string]string)
	if !ok || len(acs) != 2 || acs[0]["text"] != "First AC" || acs[1]["text"] != "Second AC" {
		t.Fatalf("acceptance_criteria = %v", fc.lastCreateBody["acceptance_criteria"])
	}
}

// TestWorkListRendersStatusAttentionSection pins `work list` to the exact
// attention rendering used by the `status` board (shared helper).
func TestWorkListRendersStatusAttentionSection(t *testing.T) {
	t.Parallel()
	st := &client.GovernanceStatus{
		Counts: client.PhaseCounts{Total: 2, Ready: 1},
		NeedsAttention: []client.NeedsAttentionItem{
			{ID: "cr-1", Key: "CR-101", Title: "Add login", Phase: "ready", Issues: []string{"agent_pickup"}},
			{ID: "cr-2", Key: "CR-102", Title: "Fix crash", Phase: "in_progress", Issues: []string{"review_needed"}},
		},
	}

	depsStatus, fcStatus, _, outStatus := newFakeDeps(t)
	fcStatus.statusResult = st
	if code := command.ExecuteForCode(command.NewRootCommand(depsStatus), "--plain", "status", "--all-workspaces"); code != output.ExitOK {
		t.Fatalf("status exit = %d, output = %s", code, outStatus.String())
	}

	depsList, fcList, _, outList := newFakeDeps(t)
	fcList.statusResult = st
	if code := command.ExecuteForCode(command.NewRootCommand(depsList), "--plain", "work", "list", "--all-workspaces"); code != output.ExitOK {
		t.Fatalf("work list exit = %d, output = %s", code, outList.String())
	}

	for _, want := range []string{
		"  ! CR-101 — Add login (agent_pickup)",
		"  ! CR-102 — Fix crash (review_needed)",
	} {
		if !strings.Contains(outStatus.String(), want) {
			t.Fatalf("status output missing %q:\n%s", want, outStatus.String())
		}
		if !strings.Contains(outList.String(), want) {
			t.Fatalf("work list output missing %q:\n%s", want, outList.String())
		}
	}
}

func TestWorkListHumanUsesAttentionDashboard(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	deps, fc, _, out := newFakeDeps(t)
	deps.StdoutIsTTY = func() bool { return true }
	fc.statusResult = &client.GovernanceStatus{
		Counts: client.PhaseCounts{Total: 1},
		NeedsAttention: []client.NeedsAttentionItem{
			{ID: "cr-1", Key: "CR-101", Title: "Add login", Phase: "ready", Issues: []string{"agent_pickup"}},
		},
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "work", "list", "--all-workspaces")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	got := out.String()
	for _, want := range []string{"Needs Attention", "CR-101", "agent_pickup"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}

func TestWorkShowListsAcceptanceCriteria(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.acceptanceCriteria = []client.AcceptanceCriterion{
		{ID: "ac-uuid-1", Text: "The env example documents the chat key.", Done: true},
		{ID: "ac-uuid-2", Text: "The README explains the chat panel."},
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "work", "show", "CR-1")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "Acceptance criteria:") ||
		!strings.Contains(got, "The env example documents the chat key.") ||
		!strings.Contains(got, "The README explains the chat panel.") {
		t.Fatalf("output missing acceptance criteria:\n%s", got)
	}
}

func TestWorkShowRichOutputStylesPhaseAndCriteria(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	deps, out := newTestDeps(t, "")
	deps.StdoutIsTTY = func() bool { return true }
	deps.Client = &fakeClient{
		resolvedWork: &client.ResolvedWork{
			ChangeRequestID:  "cr-1",
			ChangeRequestKey: "CR-1",
			Title:            "Color the CLI",
			Phase:            "ready",
		},
		acceptanceCriteria: []client.AcceptanceCriterion{
			{ID: "ac-1", Text: "Rich output is clear", Done: true},
			{ID: "ac-2", Text: "Plain output stays portable"},
		},
	}

	if code := command.ExecuteForCode(command.NewRootCommand(deps), "work", "show", "CR-1"); code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	for _, want := range []string{"CR-1", "Color the CLI", "ready", "Rich output is clear", "Plain output stays portable", "\x1b["} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q: %q", want, out.String())
		}
	}
}
