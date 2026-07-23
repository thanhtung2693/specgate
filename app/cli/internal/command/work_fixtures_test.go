package command_test

import (
	"context"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/interactive"
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
	reportFeedbackCalls     int
	lastDetailFlag          bool
	lastDeliveryDecisionID  string
	lastDeliveryDecision    client.DeliveryDecisionInput
	deliveryReviewCalls     int
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
	f.callOrder = append(f.callOrder, "context_pack")
	if f.contextPack != nil {
		return f.contextPack, nil
	}
	return &client.ContextPackResult{State: "assembled", Markdown: "# Context"}, nil
}

func (f *fakeClient) CreateWorkItem(_ context.Context, in map[string]any) (map[string]any, error) {
	f.lastCreateWorkItem = in
	f.callOrder = append(f.callOrder, "work_create")
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
	f.reportFeedbackCalls++
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
	f.deliveryReviewCalls++
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
