package command_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/interactive"
	"github.com/specgate/specgate/app/cli/internal/output"
)

// --- fakes ---

type fakeClient struct {
	// preset return values
	statusResult         *client.GovernanceStatus
	metaResult           *client.Meta
	statusErr            error
	resolvedWork         *client.ResolvedWork
	contextPack          *client.ContextPackResult
	createResult         map[string]any
	artifactListResult   *client.ArtifactList
	artifactResult       *client.Artifact
	artifactFilesResult  []client.ArtifactFile
	artifactFileResult   *client.ArtifactFileContent
	proposalResult       *client.ProposalResult
	profilesResult       []client.Profile
	importResult         []client.Profile
	skillsResult         []client.Skill
	skillResult          *client.Skill
	featuresResult       []client.Feature
	featureResult        *client.Feature
	gatesStatusResult    *client.GatesStatusResult
	gateHistoryResult    *client.GateHistoryResult
	deliveryStatusResult *client.DeliveryStatusResult
	governanceLevels     []client.GovernanceLevel
	policyExplanation    *client.PolicyExplanation
	acceptanceCriteria   []client.AcceptanceCriterion

	// injected errors
	gatesRunErr error
	resolveErr  error

	// artifactsByID, when non-nil, makes GetArtifact a strict lookup: unknown
	// ids return a not-found APIError (for id-prefix resolution tests).
	artifactsByID map[string]*client.Artifact

	updateStatusResult *client.Artifact
	lastStatusID       string
	lastStatusInput    client.UpdateArtifactStatusInput

	proposalsResult     []client.ProposalSession
	saveProposalResult  *client.SavedRevision
	lastSaveSessionID   string
	lastSaveRequestedBy string
	lastRejectSessionID string

	// calls counts HTTP-equivalent method invocations (for pre-validation tests)
	calls int

	// recorded calls
	lastWorkRef        string
	lastContextID      string
	lastContextLane    string
	lastCreateBody     map[string]any
	lastArtifactFilter client.ArtifactFilter
	lastArtifactID     string
	lastFilesID        string
	lastFileID         string
	lastFilePath       string
	lastProposalID     string
	lastProposalBody   map[string]any
	lastImportBody     map[string]any
	lastSkillsFilter   string
	lastSkillID        string
	lastFeatureSearch  string
	lastFeatureRef     string
	lastGatesID        string
	lastGateFilter     string
	lastGateLimit      int
	lastFeedbackBody   map[string]any
	lastDetailFlag     bool
	lastACWorkID       string
	lastArchiveID      string
	lastArchiveReason  string
	lastArchiveActor   string
	lastPolicyRef      string
	lastGatePreviewID  string
	gatePreviewResult  map[string]any

	lastDispatchGateTasksID string
	dispatchGateTasksResult *client.DispatchGateTasksResult

	settings           map[string]string
	lastUpdateSettings map[string]string
	users              []client.IdentityUser
	workspaces         []client.IdentityWorkspace
	identitySelection  *client.IdentitySelection
	lastBootstrapInput client.IdentityBootstrapInput
	lastWorkspaceID    string

	lastStatusWorkspaceID string

	statsResult          *client.StatsResult
	lastStatsWorkspaceID string
	lastStatsDays        int
}

func (f *fakeClient) Meta(_ context.Context) (*client.Meta, error) {
	if f.metaResult != nil {
		return f.metaResult, nil
	}
	return &client.Meta{APIVersion: "specgate.api/v1", WebURL: "http://web.test", Capabilities: map[string]bool{"agents": true}}, nil
}

func (f *fakeClient) Status(_ context.Context, workspaceID string) (*client.GovernanceStatus, error) {
	f.lastStatusWorkspaceID = workspaceID
	if f.statusErr != nil {
		return nil, f.statusErr
	}
	if f.statusResult != nil {
		return f.statusResult, nil
	}
	return &client.GovernanceStatus{}, nil
}

func (f *fakeClient) Stats(_ context.Context, workspaceID string, days int) (*client.StatsResult, error) {
	f.lastStatsWorkspaceID = workspaceID
	f.lastStatsDays = days
	if f.statsResult != nil {
		return f.statsResult, nil
	}
	return &client.StatsResult{WindowDays: days}, nil
}

func (f *fakeClient) Healthz(_ context.Context) error { return nil }

func (f *fakeClient) ResolveWorkRef(_ context.Context, ref string) (*client.ResolvedWork, error) {
	f.lastWorkRef = ref
	if f.resolveErr != nil {
		return nil, f.resolveErr
	}
	if f.resolvedWork != nil {
		return f.resolvedWork, nil
	}
	return &client.ResolvedWork{
		ChangeRequestID:  "cr-1",
		ChangeRequestKey: ref,
		Title:            "Test CR",
		Phase:            "ready",
	}, nil
}

func (f *fakeClient) ContextPack(_ context.Context, id, lane string) (*client.ContextPackResult, error) {
	f.lastContextID = id
	f.lastContextLane = lane
	if f.contextPack != nil {
		return f.contextPack, nil
	}
	return &client.ContextPackResult{State: "assembled", Markdown: "# Context"}, nil
}

func (f *fakeClient) CreateQuickWorkItem(_ context.Context, in map[string]any) (map[string]any, error) {
	f.calls++
	f.lastCreateBody = in
	if f.createResult != nil {
		return f.createResult, nil
	}
	return map[string]any{"change_request_id": "cr-new"}, nil
}

func (f *fakeClient) ArchiveWorkItem(_ context.Context, id string, reason string, actor string) (map[string]any, error) {
	f.calls++
	f.lastArchiveID = id
	f.lastArchiveReason = reason
	f.lastArchiveActor = actor
	return map[string]any{"change_request_id": id, "change_request_key": "CR-ARCHIVE", "archived": true}, nil
}

func (f *fakeClient) ListArtifacts(_ context.Context, filter client.ArtifactFilter) (*client.ArtifactList, error) {
	f.calls++
	f.lastArtifactFilter = filter
	if f.artifactListResult != nil {
		return f.artifactListResult, nil
	}
	return &client.ArtifactList{}, nil
}

func (f *fakeClient) GetArtifact(_ context.Context, id string) (*client.Artifact, error) {
	f.calls++
	f.lastArtifactID = id
	if f.artifactsByID != nil {
		if a, ok := f.artifactsByID[id]; ok {
			return a, nil
		}
		return nil, &client.APIError{Kind: client.ErrorNotFound, Status: 404, Message: "artifact not found"}
	}
	if f.artifactResult != nil {
		return f.artifactResult, nil
	}
	return &client.Artifact{ID: id, Status: "draft", Version: "v0.1"}, nil
}

func (f *fakeClient) UpdateArtifactStatus(_ context.Context, id string, in client.UpdateArtifactStatusInput) (*client.Artifact, error) {
	f.calls++
	f.lastStatusID = id
	f.lastStatusInput = in
	if f.updateStatusResult != nil {
		return f.updateStatusResult, nil
	}
	return &client.Artifact{ID: id, Version: "v1", Status: in.Status}, nil
}

func (f *fakeClient) ListArtifactProposals(_ context.Context) ([]client.ProposalSession, error) {
	f.calls++
	return f.proposalsResult, nil
}

func (f *fakeClient) SaveArtifactProposal(_ context.Context, sessionID, requestedBy string) (*client.SavedRevision, error) {
	f.calls++
	f.lastSaveSessionID = sessionID
	f.lastSaveRequestedBy = requestedBy
	if f.saveProposalResult != nil {
		return f.saveProposalResult, nil
	}
	return &client.SavedRevision{RevisionID: "rev-1", BaseArtifactID: "art-1", State: "saved", SessionID: sessionID}, nil
}

func (f *fakeClient) RejectArtifactProposal(_ context.Context, sessionID string) error {
	f.calls++
	f.lastRejectSessionID = sessionID
	return nil
}

func (f *fakeClient) ListArtifactFiles(_ context.Context, id string) ([]client.ArtifactFile, error) {
	f.calls++
	f.lastFilesID = id
	return f.artifactFilesResult, nil
}

func (f *fakeClient) GetArtifactFile(_ context.Context, id, filePath string) (*client.ArtifactFileContent, error) {
	f.calls++
	f.lastFileID = id
	f.lastFilePath = filePath
	if f.artifactFileResult != nil {
		return f.artifactFileResult, nil
	}
	return &client.ArtifactFileContent{Content: "# " + filePath}, nil
}

func (f *fakeClient) DraftProposal(_ context.Context, artifactID string, body map[string]any) (*client.ProposalResult, error) {
	f.calls++
	f.lastProposalID = artifactID
	f.lastProposalBody = body
	if f.proposalResult != nil {
		return f.proposalResult, nil
	}
	return &client.ProposalResult{Drafted: true, SessionID: "sess-1"}, nil
}

func (f *fakeClient) ListProfiles(_ context.Context) ([]client.Profile, error) {
	f.calls++
	return f.profilesResult, nil
}

func (f *fakeClient) ImportProfiles(_ context.Context, body map[string]any) ([]client.Profile, error) {
	f.calls++
	f.lastImportBody = body
	return f.importResult, nil
}

func (f *fakeClient) ListSkills(_ context.Context, nameFilter string) ([]client.Skill, error) {
	f.calls++
	f.lastSkillsFilter = nameFilter
	return f.skillsResult, nil
}

func (f *fakeClient) GetSkill(_ context.Context, id string) (*client.Skill, error) {
	f.calls++
	f.lastSkillID = id
	if f.skillResult != nil {
		return f.skillResult, nil
	}
	return &client.Skill{ID: id, Name: id}, nil
}

func (f *fakeClient) ListFeatures(_ context.Context, search string) ([]client.Feature, error) {
	f.calls++
	f.lastFeatureSearch = search
	return f.featuresResult, nil
}

func (f *fakeClient) GetFeature(_ context.Context, ref string) (*client.Feature, error) {
	f.calls++
	f.lastFeatureRef = ref
	if f.featureResult != nil {
		return f.featureResult, nil
	}
	return &client.Feature{Key: ref, Name: ref}, nil
}

func (f *fakeClient) PublishArtifact(_ context.Context, body map[string]any) (map[string]any, error) {
	f.calls++
	f.lastProposalBody = body
	return map[string]any{"artifact_id": "art-1", "version": "v0.1"}, nil
}

func (f *fakeClient) RunArtifactReadiness(_ context.Context, artifactID string) (map[string]any, error) {
	f.calls++
	f.lastArtifactID = artifactID
	return map[string]any{"aggregate": "pass"}, nil
}

func (f *fakeClient) RunLLMGates(_ context.Context, id string) (map[string]any, error) {
	f.calls++
	if f.gatesRunErr != nil {
		return nil, f.gatesRunErr
	}
	f.lastGatesID = id
	return map[string]any{"status": "queued"}, nil
}

func (f *fakeClient) GatesStatus(_ context.Context, id string) (*client.GatesStatusResult, error) {
	f.calls++
	f.lastGatesID = id
	if f.gatesStatusResult != nil {
		return f.gatesStatusResult, nil
	}
	return &client.GatesStatusResult{ChangeRequestID: id, Gates: nil}, nil
}

func (f *fakeClient) GateHistory(_ context.Context, id, gate string, limit int) (*client.GateHistoryResult, error) {
	f.calls++
	f.lastGatesID = id
	f.lastGateFilter = gate
	f.lastGateLimit = limit
	if f.gateHistoryResult != nil {
		return f.gateHistoryResult, nil
	}
	return &client.GateHistoryResult{ChangeRequestID: id, Runs: nil}, nil
}

func (f *fakeClient) ReportFeedback(_ context.Context, id string, body map[string]any) (map[string]any, error) {
	f.calls++
	f.lastGatesID = id
	f.lastFeedbackBody = body
	return map[string]any{"feedback_event_id": "evt-1", "status": "accepted"}, nil
}

func (f *fakeClient) ListAcceptanceCriteria(_ context.Context, id string) ([]client.AcceptanceCriterion, error) {
	f.calls++
	f.lastACWorkID = id
	return f.acceptanceCriteria, nil
}

func (f *fakeClient) TriggerDeliveryReview(_ context.Context, id string) (map[string]any, error) {
	f.calls++
	f.lastGatesID = id
	return map[string]any{"status": "queued"}, nil
}

func (f *fakeClient) DeliveryStatus(_ context.Context, id string, detail bool) (*client.DeliveryStatusResult, error) {
	f.calls++
	f.lastGatesID = id
	f.lastDetailFlag = detail
	if f.deliveryStatusResult != nil {
		return f.deliveryStatusResult, nil
	}
	return &client.DeliveryStatusResult{ChangeRequestID: id, Found: false}, nil
}

func (f *fakeClient) ListGovernanceLevels(_ context.Context) ([]client.GovernanceLevel, error) {
	f.calls++
	if f.governanceLevels != nil {
		return f.governanceLevels, nil
	}
	return []client.GovernanceLevel{
		{GovernanceLevel: "light", DisplayName: "Light governance", ApprovalPolicy: "self_approve", EvidencePolicy: "attested_ok"},
		{GovernanceLevel: "standard", DisplayName: "Standard governance", ApprovalPolicy: "human_required", EvidencePolicy: "attested_ok"},
		{GovernanceLevel: "enhanced", DisplayName: "Enhanced governance", ApprovalPolicy: "human_required", EvidencePolicy: "corroborated_required"},
	}, nil
}

func (f *fakeClient) WorkPolicy(_ context.Context, ref string) (*client.PolicyExplanation, error) {
	f.calls++
	f.lastPolicyRef = ref
	if f.policyExplanation != nil {
		return f.policyExplanation, nil
	}
	return &client.PolicyExplanation{
		GovernanceLevel: "standard",
		Title:           "Standard governance",
		Summary:         "Human approval required; agent attestation accepted.",
		Reasons:         []string{"Default standard governance applies"},
	}, nil
}

func (f *fakeClient) ResolvePolicy(_ context.Context, _ client.ResolvePolicyInput) (*client.PolicyExplanation, error) {
	f.calls++
	if f.policyExplanation != nil {
		return f.policyExplanation, nil
	}
	return &client.PolicyExplanation{
		GovernanceLevel: "standard",
		Title:           "Standard governance",
		Summary:         "Human approval required; agent attestation accepted.",
		Reasons:         []string{"Default standard governance applies"},
	}, nil
}

func (f *fakeClient) ListGateTasks(_ context.Context, _ string) ([]client.GateTask, error) {
	return nil, nil
}

func (f *fakeClient) GetGateTask(_ context.Context, _ string) (*client.GateTask, error) {
	return &client.GateTask{}, nil
}

func (f *fakeClient) SubmitGateResult(_ context.Context, _ string, _ any) (*client.GateResultResponse, error) {
	return &client.GateResultResponse{}, nil
}

func (f *fakeClient) GatePreview(_ context.Context, artifactID string) (map[string]any, error) {
	f.lastGatePreviewID = artifactID
	if f.gatePreviewResult != nil {
		return f.gatePreviewResult, nil
	}
	return map[string]any{"artifact_id": artifactID, "preview_tasks": []any{}}, nil
}

func (f *fakeClient) DispatchGateTasks(_ context.Context, artifactID string) (*client.DispatchGateTasksResult, error) {
	f.lastDispatchGateTasksID = artifactID
	if f.dispatchGateTasksResult != nil {
		return f.dispatchGateTasksResult, nil
	}
	return &client.DispatchGateTasksResult{ArtifactID: artifactID, CreatedTaskIDs: []string{}, SkippedGateKeys: []string{}}, nil
}

func (f *fakeClient) GetSettings(_ context.Context) (map[string]string, error) {
	if f.settings != nil {
		return f.settings, nil
	}
	return map[string]string{}, nil
}

func (f *fakeClient) UpdateSettings(_ context.Context, settings map[string]string) (map[string]string, error) {
	f.lastUpdateSettings = settings
	if f.settings == nil {
		f.settings = map[string]string{}
	}
	for k, v := range settings {
		f.settings[k] = v
	}
	return f.settings, nil
}

func (f *fakeClient) BootstrapIdentity(_ context.Context, in client.IdentityBootstrapInput) (*client.IdentitySelection, error) {
	f.calls++
	f.lastBootstrapInput = in
	if f.identitySelection != nil {
		return f.identitySelection, nil
	}
	return &client.IdentitySelection{
		User: client.IdentityUser{
			ID:          "user-1",
			Username:    in.Username,
			DisplayName: in.DisplayName,
			Email:       in.Email,
		},
		Workspace: client.IdentityWorkspace{
			ID:   "workspace-1",
			Slug: "main",
			Name: in.WorkspaceName,
		},
	}, nil
}

func (f *fakeClient) ListUsers(_ context.Context) ([]client.IdentityUser, error) {
	f.calls++
	if f.users != nil {
		return f.users, nil
	}
	return []client.IdentityUser{}, nil
}

func (f *fakeClient) GetUser(_ context.Context, id string) (*client.IdentityUser, error) {
	f.calls++
	for _, user := range f.users {
		if user.ID == id || user.Username == id {
			return &user, nil
		}
	}
	return &client.IdentityUser{ID: id, Username: id}, nil
}

func (f *fakeClient) ListWorkspaces(_ context.Context) ([]client.IdentityWorkspace, error) {
	f.calls++
	if f.workspaces != nil {
		return f.workspaces, nil
	}
	return []client.IdentityWorkspace{}, nil
}

func (f *fakeClient) GetWorkspace(_ context.Context, id string) (*client.IdentityWorkspace, error) {
	f.calls++
	f.lastWorkspaceID = id
	for _, workspace := range f.workspaces {
		if workspace.ID == id || workspace.Slug == id {
			return &workspace, nil
		}
	}
	return &client.IdentityWorkspace{ID: id, Slug: id, Name: id}, nil
}

type fakePrompter struct {
	selectedValue string
	multiValues   []string
	searchValue   string
	selectOptions []interactive.Option
	multiTitle    string
	multiOptions  []interactive.Option
	searchTitle   string
	searchOptions []interactive.Option
	inputValue    string
	inputValues   []string
	inputTitle    string
	inputTitles   []string
	suggestions   []string
	secretValue   string
	secretTitle   string
	confirmValue  bool
	inputCalls    int
	secretCalls   int
}

func (f *fakePrompter) Select(_ string, options []interactive.Option) (string, error) {
	f.selectOptions = options
	return f.selectedValue, nil
}

func (f *fakePrompter) MultiSelect(title string, options []interactive.Option, defaults []string) ([]string, error) {
	f.multiTitle = title
	f.multiOptions = options
	if f.multiValues != nil {
		return f.multiValues, nil
	}
	return defaults, nil
}

func (f *fakePrompter) SearchSelect(title, _ string, options []interactive.Option) (string, error) {
	f.searchTitle = title
	f.searchOptions = options
	return f.searchValue, nil
}

func (f *fakePrompter) Input(title, _ string, _ func(string) error) (string, error) {
	f.inputCalls++
	f.inputTitle = title
	f.inputTitles = append(f.inputTitles, title)
	if len(f.inputValues) > 0 {
		value := f.inputValues[0]
		f.inputValues = f.inputValues[1:]
		return value, nil
	}
	return f.inputValue, nil
}

func (f *fakePrompter) InputWithSuggestions(title string, _ string, suggestions []string, _ func(string) error) (string, error) {
	f.inputCalls++
	f.inputTitle = title
	f.suggestions = suggestions
	return f.inputValue, nil
}

func (f *fakePrompter) Secret(title string) (string, error) {
	f.secretCalls++
	f.secretTitle = title
	return f.secretValue, nil
}

func (f *fakePrompter) Confirm(_ string, _ bool) (bool, error) {
	return f.confirmValue, nil
}

// newFakeDeps returns test Deps with a fakeClient and fakePrompter for assertions.
func newFakeDeps(t *testing.T) (*command.Deps, *fakeClient, *fakePrompter, *bytes.Buffer) {
	t.Helper()
	fc := &fakeClient{}
	fp := &fakePrompter{}
	var out bytes.Buffer
	// stderr shares the buffer: human-mode errors print there, and tests
	// assert on combined output.
	printer := output.New(&out, &out, output.ModeHuman)
	deps := &command.Deps{
		Stdout:   &out,
		Stderr:   &out,
		Stdin:    strings.NewReader(""),
		Client:   fc,
		Prompter: fp,
		Opener:   func(_ string) error { return nil },
		Printer:  printer,
	}
	return deps, fc, fp, &out
}

// --- tests ---

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

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "work", "list")
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
		Counts: client.PhaseCounts{Total: 4, Handoff: 4},
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "work", "list")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	got := out.String()
	for _, want := range []string{
		"No work items need attention.",
		"4 work item(s) are tracked in other phases",
		"handoff 4",
		"Next: run `specgate status`",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
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

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "work", "list")
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

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "work", "show")
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

func TestWorkContextLaneFlag(t *testing.T) {
	t.Parallel()
	deps, fc, _, _ := newFakeDeps(t)

	command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "work", "context", "--lane", "fe", "CR-101")
	if fc.lastContextLane != "fe" {
		t.Fatalf("lastContextLane = %q, want fe", fc.lastContextLane)
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
	acs, ok := fc.lastCreateBody["acceptance_criteria"].([]string)
	if !ok || len(acs) != 2 || acs[0] != "Login succeeds with valid creds" {
		t.Fatalf("acceptance_criteria = %v", fc.lastCreateBody["acceptance_criteria"])
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

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "work", "create-quick")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastCreateBody["title"] != "My title" {
		t.Fatalf("title = %v", fc.lastCreateBody["title"])
	}
	acs, ok := fc.lastCreateBody["acceptance_criteria"].([]string)
	if !ok || len(acs) != 2 || acs[0] != "First AC" || acs[1] != "Second AC" {
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
	if code := command.ExecuteForCode(command.NewRootCommand(depsStatus), "--plain", "status"); code != output.ExitOK {
		t.Fatalf("status exit = %d, output = %s", code, outStatus.String())
	}

	depsList, fcList, _, outList := newFakeDeps(t)
	fcList.statusResult = st
	if code := command.ExecuteForCode(command.NewRootCommand(depsList), "--plain", "work", "list"); code != output.ExitOK {
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
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.statusResult = &client.GovernanceStatus{
		Counts: client.PhaseCounts{Total: 1},
		NeedsAttention: []client.NeedsAttentionItem{
			{ID: "cr-1", Key: "CR-101", Title: "Add login", Phase: "ready", Issues: []string{"agent_pickup"}},
		},
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "work", "list")
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

func TestWorkArchiveDeclinePrintsCancelled(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t) // fakePrompter confirmValue defaults to false

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "work", "archive", "CR-101")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.calls != 0 {
		t.Fatalf("calls = %d, want 0 (declined confirm must not archive)", fc.calls)
	}
	if !strings.Contains(out.String(), "Cancelled.") {
		t.Fatalf("output = %q, want Cancelled.", out.String())
	}
}
