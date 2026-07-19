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
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

// --- fakes ---

type fakeClient struct {
	// preset return values
	statusResult          *client.GovernanceStatus
	metaResult            *client.Meta
	statusErr             error
	resolvedWork          *client.ResolvedWork
	contextPack           *client.ContextPackResult
	createResult          map[string]any
	publishResult         map[string]any
	artifactListResult    *client.ArtifactList
	artifactListResults   map[int]*client.ArtifactList
	artifactResult        *client.Artifact
	artifactFilesResult   []client.ArtifactFile
	artifactFileResult    *client.ArtifactFileContent
	skillsResult          []client.Skill
	skillResult           *client.Skill
	featuresResult        []client.Feature
	featureResult         *client.Feature
	gatesStatusResult     *client.GatesStatusResult
	gateHistoryResult     *client.GateHistoryResult
	deliveryStatusResult  *client.DeliveryStatusResult
	readinessResult       map[string]any
	governanceLevels      []client.GovernanceLevel
	policyExplanation     *client.PolicyExplanation
	acceptanceCriteria    []client.AcceptanceCriterion
	feedbackEvents        []client.GovernanceFeedbackEvent
	knowledgeListResult   *client.KnowledgeDocumentList
	knowledgeDocResult    *client.KnowledgeDocumentDetail
	knowledgeCreateResult *client.KnowledgeDocument
	knowledgeCurateResult *client.KnowledgeDocument
	knowledgeSearchResult []client.KnowledgeSearchResult

	// work create
	lastCreateWorkItem   map[string]any
	createWorkItemResult map[string]any
	createWorkItemErr    error

	// audit
	auditResult  *client.AuditTrail
	auditErr     error
	lastAuditRef string

	// injected errors
	gatesRunErr error
	resolveErr  error

	// artifactsByID, when non-nil, makes GetArtifact a strict lookup: unknown
	// ids return a not-found APIError (for id-prefix resolution tests).
	artifactsByID map[string]*client.Artifact

	updateStatusResult *client.Artifact
	updateStatusErr    error
	lastStatusID       string
	lastStatusInput    client.UpdateArtifactStatusInput

	promoteResult    *client.Feature
	promoteErr       error
	lastPromoteID    string
	lastPromoteActor string

	// calls counts HTTP-equivalent method invocations (for pre-validation tests)
	calls     int
	callOrder []string

	maintenanceCleanupResult map[string]any
	maintenanceCleanupCalled bool

	removeDemoResult map[string]any
	removeDemoCalled bool

	lastFeatureUpdateID     string
	lastFeatureUpdateStatus string

	// recorded calls
	lastWorkRef             string
	lastContextID           string
	lastCreateBody          map[string]any
	lastArtifactFilter      client.ArtifactFilter
	lastArtifactID          string
	lastArtifactWorkspaceID string
	scopedArtifactLookup    bool
	lastFilesID             string
	lastFilesWorkspaceID    string
	lastFileID              string
	lastFilePath            string
	lastFileWorkspaceID     string
	lastPublishBody         map[string]any
	lastSkillsFilter        string
	lastSkillID             string
	lastFeatureSearch       string
	lastFeatureRef          string
	lastGatesID             string
	lastGateFilter          string
	lastGateLimit           int
	lastFeedbackBody        map[string]any
	lastDetailFlag          bool
	lastDeliveryDecisionID  string
	lastDeliveryDecision    client.DeliveryDecisionInput
	deliveryDecisionCalls   int
	lastACWorkID            string
	lastACWorkspaceID       string
	lastArchiveID           string
	lastArchiveReason       string
	lastArchiveActor        string
	lastPolicyRef           string

	lastDispatchGateTasksID string
	dispatchGateTasksResult *client.DispatchGateTasksResult
	gateTasks               []client.GateTask
	submittedGateResults    int

	settings                     map[string]string
	lastUpdateSettings           map[string]string
	users                        []client.IdentityUser
	workspaces                   []client.IdentityWorkspace
	workspaceMembers             *client.WorkspaceMembersResult
	getWorkspaceErr              error
	identitySelection            *client.IdentitySelection
	lastBootstrapInput           client.IdentityBootstrapInput
	lastWorkspaceID              string
	lastWorkspaceMembersID       string
	lastWorkspaceMembersUserID   string
	lastWorkspaceMembersUsername string
	lastCreateWorkspaceID        string

	lastStatusWorkspaceID string

	statsResult          *client.StatsResult
	lastStatsWorkspaceID string
	lastStatsDays        int

	workItems                []client.WorkItemSummary
	allWorkItems             []client.WorkItemSummary
	lastWorkItemsWorkspaceID string
	listedArchivedWork       bool
	lastKnowledgeListFilter  client.KnowledgeListFilter
	lastKnowledgeShowID      string
	lastKnowledgeShowVersion string
	lastKnowledgeCreateInput client.KnowledgeCreateTextInput
	lastKnowledgeCurateID    string
	lastKnowledgeCurateInput client.KnowledgeCurateLinksInput
	lastKnowledgeSearchInput client.KnowledgeSearchInput
}

func (f *fakeClient) Meta(_ context.Context) (*client.Meta, error) {
	if f.metaResult != nil {
		return f.metaResult, nil
	}
	return &client.Meta{
		APIVersion: "specgate.api/v1",
		WebURL:     "http://web.test",
		CapabilityDetails: map[string]client.CapabilityDetail{
			"agents": {State: "available"},
		},
	}, nil
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

func (f *fakeClient) ListWorkItems(_ context.Context, workspaceID string) ([]client.WorkItemSummary, error) {
	f.lastWorkItemsWorkspaceID = workspaceID
	return f.workItems, nil
}

func (f *fakeClient) ListWorkItemsIncludingArchived(_ context.Context, workspaceID string) ([]client.WorkItemSummary, error) {
	f.lastWorkItemsWorkspaceID = workspaceID
	f.listedArchivedWork = true
	if f.allWorkItems == nil {
		return f.workItems, nil
	}
	return f.allWorkItems, nil
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

func (f *fakeClient) AuditTrail(_ context.Context, ref string, _ bool) (*client.AuditTrail, error) {
	f.lastAuditRef = ref
	if f.auditErr != nil {
		return nil, f.auditErr
	}
	if f.auditResult != nil {
		return f.auditResult, nil
	}
	return &client.AuditTrail{Ref: ref, ChangeRequestKey: ref, Events: []client.AuditEvent{}}, nil
}

func (f *fakeClient) ContextPack(_ context.Context, id string) (*client.ContextPackResult, error) {
	f.lastContextID = id
	if f.contextPack != nil {
		return f.contextPack, nil
	}
	return &client.ContextPackResult{State: "assembled", Markdown: "# Context"}, nil
}

func (f *fakeClient) CreateWorkItem(_ context.Context, in map[string]any) (map[string]any, error) {
	f.lastCreateWorkItem = in
	if f.createWorkItemErr != nil {
		return nil, f.createWorkItemErr
	}
	if f.createWorkItemResult != nil {
		return f.createWorkItemResult, nil
	}
	return map[string]any{"change_request_key": "CR-FB", "feature_key": "f", "lead_artifact_id": "a", "acceptance_criteria": []any{}}, nil
}

func (f *fakeClient) CreateQuickWorkItem(ctx context.Context, in map[string]any) (map[string]any, error) {
	f.calls++
	f.lastCreateWorkspaceID = client.WorkspaceID(ctx)
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
	if page := f.artifactListResults[filter.Offset]; page != nil {
		return page, nil
	}
	if f.artifactListResult != nil {
		return f.artifactListResult, nil
	}
	return &client.ArtifactList{}, nil
}

func (f *fakeClient) GetArtifact(ctx context.Context, id string) (*client.Artifact, error) {
	f.calls++
	f.lastArtifactID = id
	f.lastArtifactWorkspaceID = client.WorkspaceID(ctx)
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

func (f *fakeClient) GetArtifactInWorkspace(ctx context.Context, workspaceID, id string) (*client.Artifact, error) {
	f.scopedArtifactLookup = true
	f.lastArtifactWorkspaceID = workspaceID
	return f.GetArtifact(ctx, id)
}

func (f *fakeClient) UpdateArtifactStatus(_ context.Context, id string, in client.UpdateArtifactStatusInput) (*client.Artifact, error) {
	f.calls++
	f.callOrder = append(f.callOrder, "artifact_status")
	f.lastStatusID = id
	f.lastStatusInput = in
	if f.updateStatusErr != nil {
		return nil, f.updateStatusErr
	}
	if f.updateStatusResult != nil {
		return f.updateStatusResult, nil
	}
	return &client.Artifact{ID: id, Version: "v1", Status: in.Status}, nil
}

func (f *fakeClient) PromoteArtifactCanonical(_ context.Context, artifactID, approvedBy string) (*client.Feature, error) {
	f.calls++
	f.callOrder = append(f.callOrder, "artifact_promote")
	f.lastPromoteID = artifactID
	f.lastPromoteActor = approvedBy
	if f.promoteErr != nil {
		return nil, f.promoteErr
	}
	if f.promoteResult != nil {
		return f.promoteResult, nil
	}
	return &client.Feature{ID: "feat-uuid", Key: "FEAT-X", Version: 2, CanonicalArtifactID: artifactID}, nil
}

func (f *fakeClient) ListArtifactFiles(ctx context.Context, id string) ([]client.ArtifactFile, error) {
	f.calls++
	f.lastFilesID = id
	f.lastFilesWorkspaceID = client.WorkspaceID(ctx)
	return f.artifactFilesResult, nil
}

func (f *fakeClient) GetArtifactFile(ctx context.Context, id, filePath string) (*client.ArtifactFileContent, error) {
	f.calls++
	f.lastFileID = id
	f.lastFilePath = filePath
	f.lastFileWorkspaceID = client.WorkspaceID(ctx)
	if f.artifactFileResult != nil {
		return f.artifactFileResult, nil
	}
	return &client.ArtifactFileContent{Content: "# " + filePath}, nil
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

func (f *fakeClient) UpdateFeatureStatus(_ context.Context, id, status string) (*client.Feature, error) {
	f.calls++
	f.lastFeatureUpdateID = id
	f.lastFeatureUpdateStatus = status
	updated := *f.featureResult
	updated.Status = status
	return &updated, nil
}

func (f *fakeClient) ListKnowledgeDocuments(_ context.Context, filter client.KnowledgeListFilter) (*client.KnowledgeDocumentList, error) {
	f.calls++
	f.lastKnowledgeListFilter = filter
	if f.knowledgeListResult != nil {
		return f.knowledgeListResult, nil
	}
	return &client.KnowledgeDocumentList{}, nil
}

func (f *fakeClient) GetKnowledgeDocument(_ context.Context, id, version string) (*client.KnowledgeDocumentDetail, error) {
	f.calls++
	f.lastKnowledgeShowID = id
	f.lastKnowledgeShowVersion = version
	if f.knowledgeDocResult != nil {
		return f.knowledgeDocResult, nil
	}
	return &client.KnowledgeDocumentDetail{Document: client.KnowledgeDocument{DocumentID: id, Version: "v1", Title: id}}, nil
}

func (f *fakeClient) CreateTextKnowledgeDocument(_ context.Context, in client.KnowledgeCreateTextInput) (*client.KnowledgeDocument, error) {
	f.calls++
	f.lastKnowledgeCreateInput = in
	if f.knowledgeCreateResult != nil {
		return f.knowledgeCreateResult, nil
	}
	return &client.KnowledgeDocument{DocumentID: "doc-1", Version: "v1", WorkspaceID: in.WorkspaceID, Title: in.Title, Status: "uploaded"}, nil
}

func (f *fakeClient) CurateKnowledgeLinks(_ context.Context, id string, in client.KnowledgeCurateLinksInput) (*client.KnowledgeDocument, error) {
	f.calls++
	f.lastKnowledgeCurateID = id
	f.lastKnowledgeCurateInput = in
	if f.knowledgeCurateResult != nil {
		return f.knowledgeCurateResult, nil
	}
	return &client.KnowledgeDocument{DocumentID: id, Version: "v1.1", ParentVersion: "v1", LinkedFeatureID: in.LinkedFeatureID, LinkedRequestID: in.LinkedRequestID, Status: "uploaded"}, nil
}

func (f *fakeClient) SearchKnowledge(_ context.Context, in client.KnowledgeSearchInput) ([]client.KnowledgeSearchResult, error) {
	f.calls++
	f.lastKnowledgeSearchInput = in
	return f.knowledgeSearchResult, nil
}

func (f *fakeClient) PublishArtifact(_ context.Context, body map[string]any) (map[string]any, error) {
	f.calls++
	f.lastPublishBody = body
	if f.publishResult != nil {
		return f.publishResult, nil
	}
	return map[string]any{"artifact_id": "art-1", "version": "v0.1"}, nil
}

func (f *fakeClient) RunArtifactReadiness(_ context.Context, artifactID string) (map[string]any, error) {
	f.calls++
	f.lastArtifactID = artifactID
	if f.readinessResult != nil {
		return f.readinessResult, nil
	}
	return map[string]any{"aggregate": "pass"}, nil
}

func (f *fakeClient) ListArtifactReadinessRuns(_ context.Context, artifactID string) (map[string]any, error) {
	f.calls++
	f.lastArtifactID = artifactID
	return map[string]any{"items": []any{map[string]any{
		"id": "run-1", "artifact_id": artifactID, "gate": "spec_completeness",
		"state": "pass", "evidence_json": `{"topics":["scope"]}`,
	}}}, nil
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

func (f *fakeClient) ListGovernanceFeedbackEvents(_ context.Context, _ string) ([]client.GovernanceFeedbackEvent, error) {
	f.calls++
	return f.feedbackEvents, nil
}

func (f *fakeClient) ListAcceptanceCriteria(ctx context.Context, id string) ([]client.AcceptanceCriterion, error) {
	f.calls++
	f.lastACWorkID = id
	f.lastACWorkspaceID = client.WorkspaceID(ctx)
	return f.acceptanceCriteria, nil
}

func (f *fakeClient) TriggerDeliveryReview(_ context.Context, id string) (map[string]any, error) {
	f.calls++
	f.lastGatesID = id
	return map[string]any{"status": "queued"}, nil
}

func (f *fakeClient) DecideDelivery(_ context.Context, id string, in client.DeliveryDecisionInput) (*client.DeliveryDecisionResult, error) {
	f.calls++
	f.deliveryDecisionCalls++
	f.lastDeliveryDecisionID = id
	f.lastDeliveryDecision = in
	return &client.DeliveryDecisionResult{
		ChangeRequestID: id,
		GateRunID:       "run-human",
		Verdict:         "pass",
		Executor:        "human",
		Actor:           in.Actor,
		Note:            in.Note,
		Summary:         "delivery accepted by " + in.Actor,
	}, nil
}

func (f *fakeClient) MaintenanceCleanup(_ context.Context) (map[string]any, error) {
	f.calls++
	f.maintenanceCleanupCalled = true
	if f.maintenanceCleanupResult != nil {
		return f.maintenanceCleanupResult, nil
	}
	return map[string]any{}, nil
}

func (f *fakeClient) RemoveDemo(_ context.Context) (map[string]any, error) {
	f.calls++
	f.removeDemoCalled = true
	if f.removeDemoResult != nil {
		return f.removeDemoResult, nil
	}
	return map[string]any{}, nil
}

func (f *fakeClient) DeliveryStatus(_ context.Context, id string, detail bool) (*client.DeliveryStatusResult, error) {
	f.calls++
	f.lastGatesID = id
	f.lastDetailFlag = detail
	if f.deliveryStatusResult != nil {
		return f.deliveryStatusResult, nil
	}
	return &client.DeliveryStatusResult{ChangeRequestID: id, Found: true, Verdict: "pass"}, nil
}

func (f *fakeClient) ListGovernanceLevels(_ context.Context) ([]client.GovernanceLevel, error) {
	f.calls++
	if f.governanceLevels != nil {
		return f.governanceLevels, nil
	}
	return []client.GovernanceLevel{
		{GovernanceLevel: "light", DisplayName: "Light governance", ApprovalPolicy: "human_required", EvidencePolicy: "attested_ok"},
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

func (f *fakeClient) ListGateTasks(_ context.Context, _ string) ([]client.GateTask, error) {
	return f.gateTasks, nil
}

func (f *fakeClient) GetGateTask(_ context.Context, _ string) (*client.GateTask, error) {
	return &client.GateTask{}, nil
}

func (f *fakeClient) SubmitGateResult(_ context.Context, _ string, _ any) (*client.GateResultResponse, error) {
	f.submittedGateResults++
	return &client.GateResultResponse{}, nil
}

func (f *fakeClient) DispatchGateTasks(_ context.Context, artifactID string) (*client.DispatchGateTasksResult, error) {
	f.callOrder = append(f.callOrder, "dispatch_gates")
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
	if f.getWorkspaceErr != nil {
		return nil, f.getWorkspaceErr
	}
	for _, workspace := range f.workspaces {
		if workspace.ID == id || workspace.Slug == id {
			return &workspace, nil
		}
	}
	return &client.IdentityWorkspace{ID: id, Slug: id, Name: id}, nil
}

func (f *fakeClient) ListWorkspaceMembers(_ context.Context, id, currentUserID, currentUsername string) (*client.WorkspaceMembersResult, error) {
	f.calls++
	f.lastWorkspaceMembersID = id
	f.lastWorkspaceMembersUserID = currentUserID
	f.lastWorkspaceMembersUsername = currentUsername
	if f.workspaceMembers != nil {
		return f.workspaceMembers, nil
	}
	return &client.WorkspaceMembersResult{Workspace: client.IdentityWorkspace{ID: id}}, nil
}

type fakePrompter struct {
	selectObserver func()
	selectedValue  string
	multiValues    []string
	multiDefaults  []string
	searchValue    string
	selectOptions  []interactive.Option
	multiTitle     string
	multiOptions   []interactive.Option
	searchTitle    string
	searchOptions  []interactive.Option
	inputValue     string
	inputValues    []string
	inputTitle     string
	inputTitles    []string
	suggestions    []string
	secretValue    string
	secretTitle    string
	confirmValue   bool
	confirmTitle   string
	inputCalls     int
	secretCalls    int
}

func (f *fakePrompter) Select(_ string, options []interactive.Option) (string, error) {
	if f.selectObserver != nil {
		f.selectObserver()
	}
	f.selectOptions = options
	return f.selectedValue, nil
}

func (f *fakePrompter) MultiSelect(title string, options []interactive.Option, defaults []string) ([]string, error) {
	f.multiTitle = title
	f.multiOptions = options
	f.multiDefaults = defaults
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

func (f *fakePrompter) Confirm(title string, _ bool) (bool, error) {
	f.confirmTitle = title
	return f.confirmValue, nil
}

// newFakeDeps returns test Deps with a fakeClient and fakePrompter for assertions.
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

func TestWorkArchiveDeclinePrintsCancelled(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t) // fakePrompter confirmValue defaults to false
	deps.StdinIsTTY = func() bool { return true }

	code := command.ExecuteForCode(command.NewRootCommand(deps), "work", "archive", "CR-101")
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

func TestWorkCreateSendsFeatureBodyAndRendersResult(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.createWorkItemResult = map[string]any{
		"change_request_key": "CR-77", "feature_key": "my-feat",
		"lead_artifact_id": "art-1", "acceptance_criteria": []any{"a", "b"},
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain",
		"work", "create", "--feature", "my-feat", "--title", "Do it", "--ac", "a")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, out = %s", code, out.String())
	}
	if fc.lastCreateWorkItem["feature"] != "my-feat" {
		t.Fatalf("body = %+v", fc.lastCreateWorkItem)
	}
	if !strings.Contains(out.String(), "Created CR-77") || !strings.Contains(out.String(), "2 acceptance criteria") {
		t.Fatalf("render = %s", out.String())
	}
}

func TestWorkListMissingWorkspaceOverridePrintsJSONError(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.getWorkspaceErr = &client.APIError{Kind: client.ErrorNotFound, Status: 404, Message: "workspace not found"}

	code := command.ExecuteForCode(
		command.NewRootCommand(deps),
		"--json", "--workspace", "missing-workspace", "work", "list",
	)
	if code != output.ExitNotFound {
		t.Fatalf("exit = %d, want not found; output = %q", code, out.String())
	}
	var envelope struct {
		OK    bool `json:"ok"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("workspace selection error is not a JSON envelope: %q: %v", out.String(), err)
	}
	if envelope.OK || envelope.Error.Code != "not_found" {
		t.Fatalf("workspace selection envelope = %+v; output = %s", envelope, out.String())
	}
}

func TestWorkCreateRequiresFeatureAndTitle(t *testing.T) {
	t.Parallel()
	deps, _, _, _ := newFakeDeps(t)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "work", "create", "--title", "T")
	if code == output.ExitOK {
		t.Fatal("missing --feature must fail")
	}
}

// Coding agents consume work show via --json; the envelope must carry the
// acceptance criteria (the JSON path used to return bare ResolvedWork and agents
// saw none even when normalized rows existed).
func TestWorkShowJSONIncludesAcceptanceCriteria(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.acceptanceCriteria = []client.AcceptanceCriterion{{ID: "ac-1", Text: "First AC"}}
	fc.deliveryStatusResult = &client.DeliveryStatusResult{
		Found:        true,
		PerCriterion: []client.CriterionReview{{CriterionID: "ac-1", Verdict: "met"}},
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "work", "show", "CR-1")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, out = %s", code, out.String())
	}
	var env struct {
		Data struct {
			AcceptanceCriteria []client.AcceptanceCriterion `json:"acceptance_criteria"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if len(env.Data.AcceptanceCriteria) != 1 ||
		env.Data.AcceptanceCriteria[0].Text != "First AC" ||
		!env.Data.AcceptanceCriteria[0].Done {
		t.Fatalf("acceptance_criteria missing from JSON envelope: %s", out.String())
	}
}
