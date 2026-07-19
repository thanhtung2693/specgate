package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/governanceops"
	"github.com/specgate/doc-registry/internal/governanceprofile"
	"github.com/specgate/doc-registry/internal/integrations"
	linearprovider "github.com/specgate/doc-registry/internal/integrations/linear"
	"github.com/specgate/doc-registry/internal/workboard"
	"github.com/specgate/doc-registry/internal/workspace"
)

// --- fakes for governanceops dependencies ---

type cliTestWorkBoard struct {
	crs               []workboard.ChangeRequest
	features          map[string]*workboard.Feature
	runs              []workboard.GateRun
	deliveryDecisions []workboard.DeliveryDecisionInput
}

func (f *cliTestWorkBoard) ListChangeRequests(_ context.Context, _ bool) ([]workboard.ChangeRequest, error) {
	return f.crs, nil
}
func (f *cliTestWorkBoard) GetChangeRequest(_ context.Context, id string) (*workboard.ChangeRequest, error) {
	for i, cr := range f.crs {
		if cr.ID == id {
			return &f.crs[i], nil
		}
	}
	return nil, workboard.ErrNotFound
}
func (f *cliTestWorkBoard) GetFeature(_ context.Context, id string) (*workboard.Feature, error) {
	if feat, ok := f.features[id]; ok {
		return feat, nil
	}
	return nil, workboard.ErrNotFound
}
func (f *cliTestWorkBoard) ListAcceptanceCriteria(_ context.Context, _ string) ([]workboard.AcceptanceCriterion, error) {
	return nil, nil
}
func (f *cliTestWorkBoard) ListGateRuns(_ context.Context, _ string, _ int) ([]workboard.GateRun, error) {
	return f.runs, nil
}
func (f *cliTestWorkBoard) ListStaleWarnings(_ context.Context, _ workboard.StaleWarningFilter) ([]workboard.StaleWarning, error) {
	return nil, nil
}
func (f *cliTestWorkBoard) UpdateChangeRequest(_ context.Context, in workboard.ChangeRequest) (*workboard.ChangeRequest, error) {
	for i := range f.crs {
		if f.crs[i].ID != in.ID {
			continue
		}
		if in.Archived {
			f.crs[i].Archived = true
			f.crs[i].ArchiveReason = in.ArchiveReason
			f.crs[i].ArchivedBy = in.ArchivedBy
		}
		return &f.crs[i], nil
	}
	return nil, workboard.ErrNotFound
}

type cliTestConflictWriter struct{}

func (w *cliTestConflictWriter) LatestArtifact(_ context.Context, _ string) (*artifact.Artifact, error) {
	return nil, nil
}
func (w *cliTestConflictWriter) NextVersion(_ context.Context, _ string) (string, error) {
	return "v0.1", nil
}
func (w *cliTestConflictWriter) ResolveNextVersion(_ context.Context, _ string, _ string) (string, error) {
	return "", fmt.Errorf("%w: stale", governanceops.ErrVersionConflict)
}
func (w *cliTestConflictWriter) Publish(_ context.Context, _ artifact.PublishInput) (*artifact.Artifact, error) {
	return nil, fmt.Errorf("%w: stale", governanceops.ErrVersionConflict)
}
func (w *cliTestConflictWriter) UpdateStatus(_ context.Context, _ string, _ artifact.StatusUpdate) (*artifact.Artifact, error) {
	return nil, nil
}

type cliTestFeedbackStore struct {
	events []integrations.GovernanceFeedbackEvent
}

func (s *cliTestFeedbackStore) CreateGovernanceFeedbackEvent(_ context.Context, ev integrations.GovernanceFeedbackEvent) (*integrations.GovernanceFeedbackEvent, error) {
	s.events = append(s.events, ev)
	ev.ID = "feedback-1"
	return &ev, nil
}
func (s *cliTestFeedbackStore) ListGovernanceFeedbackEvents(_ context.Context, _ integrations.GovernanceFeedbackFilter) ([]integrations.GovernanceFeedbackEvent, error) {
	return s.events, nil
}

type cliTestFeatureUpserter struct{}

func (u *cliTestFeatureUpserter) UpsertFeatureByKeyInWorkspaceForPublish(_ context.Context, workspaceID, key, name string) (*workboard.Feature, bool, error) {
	n := name
	if n == "" {
		n = key
	}
	return &workboard.Feature{ID: "feat-1", WorkspaceID: workspaceID, Key: key, Name: n, Status: workboard.FeatureStatusCandidate}, true, nil
}

func (u *cliTestFeatureUpserter) DeleteCandidateFeatureIfUnreferenced(_ context.Context, _, _ string) error {
	return nil
}

type cliTestProfileResolver struct{}

func (p *cliTestProfileResolver) ResolveProfile(_ context.Context, _ governanceprofile.ResolveInput) (*governanceprofile.ResolvedProfile, error) {
	return &governanceprofile.ResolvedProfile{
		Key:     "generic_change",
		FullKey: "generic_change",
		Version: "1",
		Digest:  "sha256:generic",
	}, nil
}

// cliTestStatsSource is a canned governanceops.StatsReader for handler tests.
type cliTestStatsSource struct {
	runs   []workboard.StatsGateRun
	events []workboard.StatsFeedbackEvent
}

func (s *cliTestStatsSource) ListGateRunsForStats(_ context.Context, _ string, _ time.Time) ([]workboard.StatsGateRun, error) {
	return s.runs, nil
}

func (s *cliTestStatsSource) ListDeliveryRunsForStats(_ context.Context, _ string) ([]workboard.StatsGateRun, error) {
	out := make([]workboard.StatsGateRun, 0, len(s.runs))
	for _, run := range s.runs {
		if run.Gate == "delivery_review" {
			out = append(out, run)
		}
	}
	return out, nil
}

func (s *cliTestStatsSource) ListAmbiguityFeedbackForStats(_ context.Context, _ string, _ time.Time) ([]workboard.StatsFeedbackEvent, error) {
	return s.events, nil
}

// newCLIGovernanceSvc builds a Service wired for CLI handler tests.
func newCLIGovernanceSvc() *governanceops.Service {
	governance := &cliTestWorkBoard{
		crs: []workboard.ChangeRequest{
			{ID: "cr-1", Title: "Test CR", Phase: "Ready", FeatureID: "feat-1", WorkspaceID: "ws-1"},
		},
		features: map[string]*workboard.Feature{
			"feat-1": {ID: "feat-1", Key: "test-feat", Name: "Test Feature", WorkspaceID: "ws-1"},
		},
	}
	return &governanceops.Service{WorkBoard: governance}
}

func newCLITestRouter(t *testing.T, govSvc *governanceops.Service) http.Handler {
	t.Helper()
	rt := &Router{
		Handlers: &Handlers{
			Governance: govSvc,
		},
		Config: testConfig(),
	}
	h := rt.Build()
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if strings.HasPrefix(req.URL.Path, "/api/v1/") && req.URL.Query().Get("workspace_id") == "" {
			query := req.URL.Query()
			query.Set("workspace_id", "ws-1")
			req.URL.RawQuery = query.Encode()
		}
		h.ServeHTTP(w, req)
	})
}

type cliArtifactReadService struct {
	fakeArtifactService
	workspaces []string
}

func (s *cliArtifactReadService) List(ctx context.Context, _ artifact.ListFilter) ([]artifact.Artifact, error) {
	s.recordWorkspace(ctx)
	return nil, nil
}

func (s *cliArtifactReadService) Count(ctx context.Context, _ artifact.ListFilter) (int64, error) {
	s.recordWorkspace(ctx)
	return 0, nil
}

func (s *cliArtifactReadService) GetInWorkspace(ctx context.Context, workspaceID, _ string) (*artifact.Artifact, error) {
	s.recordWorkspace(ctx)
	s.workspaces = append(s.workspaces, workspaceID)
	return &artifact.Artifact{ID: "art-1"}, nil
}

func (s *cliArtifactReadService) recordWorkspace(ctx context.Context) {
	workspaceID, ok := artifact.WorkspaceFromContext(ctx)
	if !ok {
		workspaceID = ""
	}
	s.workspaces = append(s.workspaces, workspaceID)
}

func TestCLIArtifactReadsUseBoundWorkspace(t *testing.T) {
	t.Parallel()
	svc := &cliArtifactReadService{}
	h := &Handlers{Artifacts: svc}
	ctx := workspace.WithID(context.Background(), "ws-a")

	if _, err := h.CLIListArtifacts(ctx, &CLIListArtifactsInput{}); err != nil {
		t.Fatalf("list artifacts: %v", err)
	}
	if _, err := h.CLIGetArtifact(ctx, &CLIGetArtifactInput{ID: "art-1"}); err != nil {
		t.Fatalf("get artifact: %v", err)
	}
	if _, err := h.CLIListArtifactFiles(ctx, &CLIListArtifactFilesInput{ID: "art-1"}); err != nil {
		t.Fatalf("list artifact files: %v", err)
	}

	for i, workspaceID := range svc.workspaces {
		if workspaceID != "ws-a" {
			t.Fatalf("workspace at call %d = %q, want ws-a", i, workspaceID)
		}
	}
}

// --- tests ---

// Huma v2 returns the Body struct fields at the JSON top level (no "body" wrapper).

func TestCLI_StatusEndpoint(t *testing.T) {
	t.Parallel()
	svc := newCLIGovernanceSvc()
	srv := newCLITestRouter(t, svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var got governanceops.GovernanceStatusResult
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v — body: %s", err, rec.Body.String())
	}
	if got.Counts.Total != 1 {
		t.Fatalf("counts.total = %d, want 1 — body: %s", got.Counts.Total, rec.Body.String())
	}
}

func TestCLI_StatusEndpointFiltersWorkspace(t *testing.T) {
	t.Parallel()
	svc := newCLIGovernanceSvc()
	governance := svc.WorkBoard.(*cliTestWorkBoard)
	governance.crs = []workboard.ChangeRequest{
		{ID: "cr-1", Title: "Platform CR", Phase: "Ready", FeatureID: "feat-1", WorkspaceID: "ws-platform"},
		{ID: "cr-2", Title: "Docs CR", Phase: "Ready", FeatureID: "feat-1", WorkspaceID: "ws-docs"},
	}
	srv := newCLITestRouter(t, svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/status?workspace_id=ws-platform", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var got governanceops.GovernanceStatusResult
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v — body: %s", err, rec.Body.String())
	}
	if got.Counts.Total != 1 || got.Counts.Ready != 1 {
		t.Fatalf("counts = %#v, want one ready workspace item", got.Counts)
	}
}

func TestCLI_StatsEndpoint(t *testing.T) {
	t.Parallel()
	svc := newCLIGovernanceSvc()
	svc.StatsSource = &cliTestStatsSource{
		runs: []workboard.StatsGateRun{
			{
				RunID:            "run-1",
				ChangeRequestID:  "cr-1",
				ChangeRequestKey: "CR-101",
				Gate:             "delivery_review",
				State:            workboard.NextActionStatePass,
				Hint:             "clean",
				RunCreatedAt:     time.Now().UTC(),
				CRCreatedAt:      time.Now().UTC().Add(-6 * time.Hour),
			},
		},
	}
	srv := newCLITestRouter(t, svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/stats?days=7&workspace_id=ws-1", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var got governanceops.StatsResult
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v — body: %s", err, rec.Body.String())
	}
	if got.WindowDays != 7 || got.WorkspaceID != "ws-1" {
		t.Fatalf("window echo = (%d, %q), want (7, ws-1) — body: %s", got.WindowDays, got.WorkspaceID, rec.Body.String())
	}
	if got.ReviewedItems != 1 || got.FirstPass != 1 {
		t.Fatalf("reviewed/first_pass = (%d, %d), want (1, 1) — body: %s", got.ReviewedItems, got.FirstPass, rec.Body.String())
	}
}

func TestCLI_StatsEndpointUnavailableWithoutSource(t *testing.T) {
	t.Parallel()
	srv := newCLITestRouter(t, newCLIGovernanceSvc())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body = %s", rec.Code, rec.Body.String())
	}
}

func TestCLI_MetaEndpoint(t *testing.T) {
	t.Parallel()
	srv := (&Router{
		Handlers: &Handlers{
			Governance: newCLIGovernanceSvc(),
			AppBaseURL: "https://specgate.company.test",
		},
		Config: testConfig(),
	}).Build()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/meta", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var got CLIMetaDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v — body: %s", err, rec.Body.String())
	}
	// server_version must be present (defaults to "dev" in non-release builds)
	if got.ServerVersion == "" {
		t.Errorf("meta.server_version is empty — body: %s", rec.Body.String())
	}
	if got.WebURL != "https://specgate.company.test" {
		t.Errorf("meta.web_url = %q", got.WebURL)
	}
	if got.CapabilityDetails["core"].State != CapabilityStateAvailable {
		t.Errorf("meta.capability_details.core = %+v", got.CapabilityDetails["core"])
	}
	if got.CapabilityDetails["agents"].State != CapabilityStateUnavailable {
		t.Errorf("meta.capability_details.agents = %+v", got.CapabilityDetails["agents"])
	}
	if got.CapabilityDetails["platform_model"].State != CapabilityStateUnavailable {
		t.Errorf("meta.capability_details.platform_model = %+v", got.CapabilityDetails["platform_model"])
	}
	if got.CapabilityDetails["web_ui"].State != CapabilityStateAvailable {
		t.Errorf("meta.capability_details.web_ui = %+v", got.CapabilityDetails["web_ui"])
	}
	if body := rec.Body.String(); strings.Contains(body, `"capabilities":`) || strings.Contains(body, `"minimum_cli_version":`) {
		t.Fatalf("meta retained alpha compatibility fields: %s", body)
	}
}

func TestCLI_ResolveWorkRef(t *testing.T) {
	t.Parallel()
	svc := newCLIGovernanceSvc()
	srv := newCLITestRouter(t, svc)

	body, _ := json.Marshal(map[string]string{"ref": "cr-1"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/work-items/resolve?workspace_id=ws-1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var got governanceops.ResolvedWork
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ChangeRequestID != "cr-1" {
		t.Fatalf("change_request_id = %q, want cr-1 — body: %s", got.ChangeRequestID, rec.Body.String())
	}
}

func TestCLI_ContextPackLaneQuery(t *testing.T) {
	t.Parallel()
	svc := newCLIGovernanceSvc()
	srv := newCLITestRouter(t, svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/work-items/cr-1/context-pack?lane=fe", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var got governanceops.ContextPackResult
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.State == "" {
		t.Errorf("context_pack state is empty — body: %s", rec.Body.String())
	}
}

func TestCLI_ContextPackMissingWorkItemReturnsNotFound(t *testing.T) {
	t.Parallel()
	srv := newCLITestRouter(t, &governanceops.Service{WorkBoard: &cliTestWorkBoard{}})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/work-items/missing/context-pack", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", rec.Code, rec.Body.String())
	}
}

func TestCLI_AuditEndpointResolvesRef(t *testing.T) {
	t.Parallel()
	svc := newCLIGovernanceSvc()
	srv := newCLITestRouter(t, svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit/cr-1", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var got governanceops.AuditTrail
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ChangeRequestID != "cr-1" || got.FeatureKey != "test-feat" {
		t.Fatalf("audit header wrong: %+v — body: %s", got, rec.Body.String())
	}
	if got.Events == nil {
		t.Fatalf("events must be a (possibly empty) array, got nil")
	}
}

func TestCLI_AuditEndpointUnknownRef404(t *testing.T) {
	t.Parallel()
	svc := newCLIGovernanceSvc()
	srv := newCLITestRouter(t, svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit/CR-9999", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", rec.Code, rec.Body.String())
	}
}

func TestCLI_ArchiveWorkItem(t *testing.T) {
	t.Parallel()
	svc := newCLIGovernanceSvc()
	srv := newCLITestRouter(t, svc)

	body, _ := json.Marshal(map[string]string{"reason": "done", "actor": "thanhtung2693"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/work-items/cr-1/archive", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var got governanceops.ResolvedWork
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ChangeRequestID != "cr-1" {
		t.Fatalf("change_request_id = %q, want cr-1 — body: %s", got.ChangeRequestID, rec.Body.String())
	}
	governance := svc.WorkBoard.(*cliTestWorkBoard)
	if !governance.crs[0].Archived {
		t.Fatalf("expected archived work item, got %+v", governance.crs[0])
	}
	if governance.crs[0].ArchiveReason != "done" {
		t.Fatalf("archive_reason = %q, want done", governance.crs[0].ArchiveReason)
	}
	if governance.crs[0].ArchivedBy != "thanhtung2693" {
		t.Fatalf("archived_by = %q, want thanhtung2693", governance.crs[0].ArchivedBy)
	}
}

func TestCLI_FeedbackStripsSource(t *testing.T) {
	t.Parallel()
	store := &cliTestFeedbackStore{}
	svc := newCLIGovernanceSvc()
	svc.FeedbackStore = store
	srv := newCLITestRouter(t, svc)

	payload := map[string]any{
		"change_request_id": "cr-1",
		"event_type":        "coding_agent.completed",
		"severity":          "info",
		"summary":           "done",
		"agent":             map[string]any{"name": "builder"},
		"evidence": []map[string]any{
			{"kind": "file", "path": "main.go", "source": "malicious-agent"},
		},
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/work-items/cr-1/feedback", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if len(store.events) != 1 {
		t.Fatalf("stored events = %d, want 1", len(store.events))
	}
	// Source must be stripped (server-stamped, not agent-supplied)
	var storedPayload map[string]any
	if err := json.Unmarshal([]byte(store.events[0].PayloadJSON), &storedPayload); err == nil {
		if evidence, ok := storedPayload["evidence"].([]any); ok && len(evidence) > 0 {
			if ev, ok := evidence[0].(map[string]any); ok {
				if ev["source"] == "malicious-agent" {
					t.Error("agent-supplied source was not stripped")
				}
			}
		}
	}
}

func TestCLI_FeedbackValidationErrorsReturnBadRequest(t *testing.T) {
	t.Parallel()
	store := &cliTestFeedbackStore{}
	svc := newCLIGovernanceSvc()
	svc.FeedbackStore = store
	srv := newCLITestRouter(t, svc)

	body := []byte(`{"change_request_id":"cr-1","event_type":"coding_agent.completed","severity":"info","summary":"","criteria":[]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/work-items/cr-1/feedback", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
	if len(store.events) != 0 {
		t.Fatalf("stored events = %d, want 0", len(store.events))
	}
}

func TestCLI_PublishConflict409(t *testing.T) {
	t.Parallel()
	svc := newCLIGovernanceSvc()
	svc.ArtifactWriter = &cliTestConflictWriter{}
	svc.FeatureUpserter = &cliTestFeatureUpserter{}
	svc.ProfileResolver = &cliTestProfileResolver{}
	srv := newCLITestRouter(t, svc)

	payload := map[string]any{
		"feature_key":  "test-feat",
		"base_version": "v0.1",
		"documents": []map[string]any{
			{"path": "spec.md", "role": "spec", "content": "# Spec"},
		},
		"source_kind": "cli",
		// The CLI annotates every request body with the selected workspace_id;
		// the publish input must accept it (reaching the 409 below), not 422.
		"workspace_id": "ws-1",
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts/publish", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body = %s", rec.Code, rec.Body.String())
	}
}

func TestCLI_ReadinessNilAgents503(t *testing.T) {
	t.Parallel()
	svc := newCLIGovernanceSvc()
	// AgentsRunner intentionally nil
	srv := newCLITestRouter(t, svc)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/work-items/cr-1/readiness", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body = %s", rec.Code, rec.Body.String())
	}
}

func TestCLI_SkillGetByID_Unavailable(t *testing.T) {
	t.Parallel()
	// Skills nil → 503
	rt := &Router{
		Handlers: &Handlers{Governance: newCLIGovernanceSvc()},
		Config:   testConfig(),
	}
	srv := rt.Build()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/skills/some-id?workspace_id=ws-a", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body = %s", rec.Code, rec.Body.String())
	}
}

func TestCLI_SkillSearchByName_Unavailable(t *testing.T) {
	t.Parallel()
	rt := &Router{
		Handlers: &Handlers{Governance: newCLIGovernanceSvc()},
		Config:   testConfig(),
	}
	srv := rt.Build()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/skills?name=test&workspace_id=ws-a", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body = %s", rec.Code, rec.Body.String())
	}
}

type cliTestWorkItemStore struct {
	feature *workboard.Feature
	created *workboard.ChangeRequest
}

type cliTestArtifactStore struct {
	fakeArtifactService
	artifact artifact.Artifact
}

func (s *cliTestArtifactStore) Get(_ context.Context, id string) (*artifact.Artifact, error) {
	if s.artifact.ID == id {
		return &s.artifact, nil
	}
	return nil, artifact.ErrNotFound
}

func (s *cliTestWorkItemStore) GetFeature(_ context.Context, id string) (*workboard.Feature, error) {
	if s.feature != nil && s.feature.ID == id {
		return s.feature, nil
	}
	return nil, workboard.ErrNotFound
}

func (s *cliTestWorkItemStore) GetFeatureByKey(_ context.Context, key string) (*workboard.Feature, error) {
	if s.feature != nil && s.feature.Key == key {
		return s.feature, nil
	}
	return nil, workboard.ErrNotFound
}

func (s *cliTestWorkItemStore) CreateChangeRequest(_ context.Context, in workboard.ChangeRequest) (*workboard.ChangeRequest, error) {
	in.ID = "cr-new"
	in.Key = "CR-NEW"
	s.created = &in
	return &in, nil
}

// POST /api/v1/work-items/create creates a feature-backed CR bound to the
// canonical; unknown feature is 404 (per spec §6).
func TestCLI_WorkCreateEndpoint(t *testing.T) {
	t.Parallel()
	svc := newCLIGovernanceSvc()
	svc.WorkItems = &cliTestWorkItemStore{
		feature: &workboard.Feature{ID: "feat-1", WorkspaceID: "ws-1", Key: "test-feat", CanonicalArtifactID: "art-canon"},
	}
	svc.Artifacts = &cliTestArtifactStore{artifact: artifact.Artifact{
		ID: "art-canon", FeatureID: "feat-1", WorkspaceID: "ws-1", Status: artifact.StatusApproved,
	}}
	srv := newCLITestRouter(t, svc)

	body, _ := json.Marshal(map[string]any{
		"feature": "test-feat", "title": "Do it",
		"acceptance_criteria": []string{"AC one"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/work-items/create", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got governanceops.CreateWorkItemResult
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ChangeRequestKey != "CR-NEW" || got.LeadArtifactID != "art-canon" || len(got.AcceptanceCriteria) != 1 {
		t.Fatalf("result = %+v", got)
	}

	// Unknown feature → 404.
	body, _ = json.Marshal(map[string]any{"feature": "nope", "title": "T"})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/work-items/create", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown feature status = %d, want 404", rec.Code)
	}
}

func TestCLI_WorkspaceScopeIsRequiredAndCannotBeOverridden(t *testing.T) {
	t.Parallel()
	svc := newCLIGovernanceSvc()
	svc.WorkItems = &cliTestWorkItemStore{
		feature: &workboard.Feature{ID: "feat-1", WorkspaceID: "ws-1", Key: "test-feat", CanonicalArtifactID: "art-canon"},
	}
	srv := (&Router{Handlers: &Handlers{Governance: svc}, Config: testConfig()}).Build()

	missing := httptest.NewRequest(http.MethodPost, "/api/v1/work-items/resolve", strings.NewReader(`{"ref":"cr-1"}`))
	missing.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, missing)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing workspace status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}

	mismatch := httptest.NewRequest(http.MethodPost, "/api/v1/work-items/create?workspace_id=ws-1", strings.NewReader(`{"feature":"test-feat","title":"Do it","workspace_id":"ws-2"}`))
	mismatch.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, mismatch)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("workspace mismatch status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
}

func TestCLI_WorkspaceScopeRejectsUnsafePathSegments(t *testing.T) {
	t.Parallel()
	nextCalled := false
	srv := cliWorkspaceMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		nextCalled = true
	}))

	for _, workspaceID := range []string{"../ws-b", "ws/a", `ws\a`, "ws..b", "."} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/status?workspace_id="+url.QueryEscape(workspaceID), nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("workspace_id %q status = %d, want 400", workspaceID, rec.Code)
		}
	}
	if nextCalled {
		t.Fatal("unsafe workspace scope reached the CLI handler")
	}
}

func TestCLI_AllWorkspacesScopeMustBeExplicitAndReadOnly(t *testing.T) {
	t.Parallel()
	svc := newCLIGovernanceSvc()
	srv := (&Router{Handlers: &Handlers{Governance: svc}, Config: testConfig()}).Build()

	all := httptest.NewRequest(http.MethodGet, "/api/v1/status?all_workspaces=true", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, all)
	if rec.Code != http.StatusOK {
		t.Fatalf("all-workspaces status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	invalid := httptest.NewRequest(http.MethodPost, "/api/v1/work-items/resolve?all_workspaces=true", strings.NewReader(`{"ref":"cr-1"}`))
	invalid.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, invalid)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("write all-workspaces status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
}

// A feature without a canonical artifact is 422 (per the work-create-cli spec),
// not the service-wide 400 validation mapping.
func TestCLI_WorkCreateNoCanonical422(t *testing.T) {
	t.Parallel()
	svc := newCLIGovernanceSvc()
	svc.WorkItems = &cliTestWorkItemStore{
		feature: &workboard.Feature{ID: "feat-1", WorkspaceID: "ws-1", Key: "test-feat"}, // no canonical
	}
	srv := newCLITestRouter(t, svc)
	body, _ := json.Marshal(map[string]any{"feature": "test-feat", "title": "T"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/work-items/create", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "promote") {
		t.Fatalf("error must name approve+promote: %s", rec.Body.String())
	}
}

func (f *cliTestWorkBoard) RecordDeliveryDecision(_ context.Context, in workboard.DeliveryDecisionInput) (*workboard.GateRun, error) {
	f.deliveryDecisions = append(f.deliveryDecisions, in)
	state := workboard.NextActionStatePass
	if in.Decision == workboard.DeliveryDecisionReject {
		state = workboard.NextActionStateFail
	}
	return &workboard.GateRun{
		ID: "gr-decision", SubjectKind: "change_request", SubjectID: in.ChangeRequestID,
		Gate: "delivery_review", State: state, Executor: workboard.GateRunExecutorHuman,
	}, nil
}

type cliLinearTransitionStore struct {
	integrations.Store
	integration *integrations.Integration
	resource    *integrations.Resource
	links       []integrations.TrackerLink
}

func (s *cliLinearTransitionStore) GetIntegration(context.Context, string) (*integrations.Integration, error) {
	return s.integration, nil
}

func (s *cliLinearTransitionStore) GetResource(_ context.Context, integrationID, resourceID string) (*integrations.Resource, error) {
	if s.resource == nil || s.resource.IntegrationID != integrationID || s.resource.ID != resourceID {
		return nil, integrations.ErrNotFound
	}
	return s.resource, nil
}

func (s *cliLinearTransitionStore) ListTrackerLinksByChangeRequest(context.Context, string) ([]integrations.TrackerLink, error) {
	return s.links, nil
}

// The delivery-decision facade must accept the CLI's actual wire body —
// {decision, actor, note} with the id in the path only. This is the exact JSON
// specgate delivery approve sends; huma validates it against the schema, so a
// required change_request_id in the body kills the command before the handler
// can backfill from the path. (Found live: the human decision step 422'd.)
func TestCLI_DeliveryDecisionAcceptsCLIWireBody(t *testing.T) {
	t.Parallel()
	svc := newCLIGovernanceSvc()
	svc.FeedbackStore = &cliTestFeedbackStore{events: []integrations.GovernanceFeedbackEvent{{
		ID: "completion-1", ChangeRequestID: "cr-1",
		EventType:   integrations.FeedbackEventCodingAgentCompleted,
		PayloadJSON: `{"agent":{"name":"builder"},"summary":"done"}`,
		CreatedAt:   time.Unix(90, 0).UTC(),
	}}}
	svc.WorkBoard.(*cliTestWorkBoard).runs = []workboard.GateRun{{
		ID: "platform-review", Gate: "delivery_review", State: workboard.NextActionStatePass,
		Executor:     workboard.GateRunExecutorPlatform,
		EvidenceJSON: `{"completion_feedback_event_id":"completion-1"}`,
		CreatedAt:    time.Unix(100, 0).UTC(),
	}}
	srv := newCLITestRouter(t, svc)

	body := []byte(`{"decision":"approve","actor":"lead","note":"looks good"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/work-items/cr-1/delivery-decision", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — the CLI wire body must validate; body = %s", rec.Code, rec.Body.String())
	}
}

type cliAutoTransitionStore struct {
	integrations.Store
	requestedChangeRequests []string
}

func (s *cliAutoTransitionStore) ListTrackerLinksByChangeRequest(
	_ context.Context,
	changeRequestID string,
) ([]integrations.TrackerLink, error) {
	s.requestedChangeRequests = append(s.requestedChangeRequests, changeRequestID)
	return nil, nil
}

func TestCLI_DeliveryDecisionTransitionsTrackersOnlyAfterHumanApproval(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		decision string
		wantCall bool
	}{
		{name: "approve", decision: "approve", wantCall: true},
		{name: "reject", decision: "reject", wantCall: false},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			svc := newCLIGovernanceSvc()
			svc.FeedbackStore = &cliTestFeedbackStore{events: []integrations.GovernanceFeedbackEvent{{
				ID: "completion-1", ChangeRequestID: "cr-1",
				EventType:   integrations.FeedbackEventCodingAgentCompleted,
				PayloadJSON: `{"agent":{"name":"builder"},"summary":"done"}`,
				CreatedAt:   time.Unix(90, 0).UTC(),
			}}}
			svc.WorkBoard.(*cliTestWorkBoard).runs = []workboard.GateRun{{
				ID: "platform-review", Gate: "delivery_review", State: workboard.NextActionStatePass,
				Executor:     workboard.GateRunExecutorPlatform,
				EvidenceJSON: `{"completion_feedback_event_id":"completion-1"}`,
				CreatedAt:    time.Unix(100, 0).UTC(),
			}}
			store := &cliAutoTransitionStore{}
			h := &Handlers{
				Governance:   svc,
				Integrations: integrations.NewService(store),
			}
			in := &CLIDeliveryDecisionInput{ID: "cr-1"}
			in.Body.Decision = tc.decision
			in.Body.Actor = "lead"

			if _, err := h.CLIDeliveryDecision(context.Background(), in); err != nil {
				t.Fatalf("CLIDeliveryDecision: %v", err)
			}
			gotCall := len(store.requestedChangeRequests) > 0
			if gotCall != tc.wantCall {
				t.Fatalf("tracker transition called=%v, want %v", gotCall, tc.wantCall)
			}
		})
	}
}

func TestCLI_DeliveryDecisionPersistsWhenLinearDoneTransitionFails(t *testing.T) {
	oldGraphQLURL := linearprovider.GraphQLURL
	t.Cleanup(func() { linearprovider.GraphQLURL = oldGraphQLURL })
	linear := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "Linear unavailable", http.StatusBadGateway)
	}))
	t.Cleanup(linear.Close)
	linearprovider.GraphQLURL = linear.URL

	svc := newCLIGovernanceSvc()
	svc.FeedbackStore = &cliTestFeedbackStore{events: []integrations.GovernanceFeedbackEvent{{
		ID: "completion-1", ChangeRequestID: "cr-1", EventType: integrations.FeedbackEventCodingAgentCompleted,
		PayloadJSON: `{"agent":{"name":"builder"},"summary":"done"}`, CreatedAt: time.Unix(90, 0).UTC(),
	}}}
	board := svc.WorkBoard.(*cliTestWorkBoard)
	board.runs = []workboard.GateRun{{
		ID: "platform-review", Gate: "delivery_review", State: workboard.NextActionStatePass,
		Executor: workboard.GateRunExecutorPlatform, EvidenceJSON: `{"completion_feedback_event_id":"completion-1"}`,
		CreatedAt: time.Unix(100, 0).UTC(),
	}}
	token, err := integrations.EncryptSecret("linear-token")
	if err != nil {
		t.Fatal(err)
	}
	transitionStore := &cliLinearTransitionStore{
		integration: &integrations.Integration{ID: "int-linear", Provider: integrations.ProviderLinear, Status: integrations.StatusConnected, APITokenEncrypted: token},
		resource:    &integrations.Resource{ID: "team-selected", IntegrationID: "int-linear", ResourceType: integrations.ResourceTypeTeam, ExternalID: "team-selected"},
		links:       []integrations.TrackerLink{{IntegrationID: "int-linear", ResourceID: "team-selected", ExternalID: "issue-1", State: integrations.TrackerStateOpened}},
	}
	h := &Handlers{Governance: svc, Integrations: integrations.NewService(transitionStore)}
	in := &CLIDeliveryDecisionInput{ID: "cr-1"}
	in.Body.Decision = "approve"
	in.Body.Actor = "lead"
	result, err := h.CLIDeliveryDecision(context.Background(), in)
	if err != nil {
		t.Fatalf("human acceptance must survive Linear Done failure: %v", err)
	}
	if result.Body == nil || result.Body.Executor != workboard.GateRunExecutorHuman || result.Body.Verdict != string(workboard.NextActionStatePass) || len(board.deliveryDecisions) != 1 {
		t.Fatalf("delivery decision result=%#v persisted=%#v", result, board.deliveryDecisions)
	}
}

// Every CLI facade must accept the CLI's LITERAL wire body. This is the bug
// class that killed `delivery approve` (facade schema required a field the CLI
// sends only in the path) and rejected every publish (schema lacked the
// workspace_id the CLI annotates onto every body). Bodies below are copied
// from what app/cli actually sends — including the created_by/workspace_id
// identity annotation. The assertion is narrow on purpose: the request must
// never die in huma schema validation; downstream service errors are fine.
func TestCLI_FacadesAcceptCLIWireBodies(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		path string
		body string
	}{
		{"resolve", "/api/v1/work-items/resolve",
			`{"ref":"CR-1"}`},
		{"create-quick", "/api/v1/work-items",
			`{"title":"T","description":"D","acceptance_criteria":[{"text":"AC","verification_binding":"tests"}],"created_by":"dev","workspace_id":"ws-1"}`},
		{"work-create", "/api/v1/work-items/create",
			`{"feature":"test-feat","title":"T","description":"D","acceptance_criteria":["AC"],"created_by":"dev","workspace_id":"ws-1"}`},
		{"archive", "/api/v1/work-items/cr-1/archive",
			`{"reason":"done","actor":"dev"}`},
		{"delivery-report", "/api/v1/work-items/cr-1/feedback",
			`{"change_request_id":"cr-1","event_type":"coding_agent.completed","severity":"info","summary":"done","agent":{"name":"builder"},"affected_files":["a.go"],"checks":[{"name":"tests","status":"pass","detail":"ok"}],"criteria":[{"criterion_id":"ac-0","text":"AC","claim":"satisfied","evidence":{"kind":"test","path":"a_test.go","line":10}}]}`},
		{"delivery-decision", "/api/v1/work-items/cr-1/delivery-decision",
			`{"decision":"approve","actor":"lead","note":"ok"}`},
		{"publish", "/api/v1/artifacts/publish",
			`{"feature_key":"f","feature_name":"F","request_type":"new_feature","impact_level":"medium","documents":[{"path":"spec.md","role":"spec","content":"# S"}],"created_by":"dev","workspace_id":"ws-1"}`},
	}
	svc := newCLIGovernanceSvc()
	svc.WorkItems = &cliTestWorkItemStore{
		feature: &workboard.Feature{ID: "feat-1", WorkspaceID: "ws-1", Key: "test-feat", CanonicalArtifactID: "art-canon"},
	}
	srv := newCLITestRouter(t, svc)
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.path, bytes.NewReader([]byte(tc.body)))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)
			out := rec.Body.String()
			if strings.Contains(out, "expected required property") || strings.Contains(out, "unexpected property") {
				t.Fatalf("CLI wire body rejected by schema (status %d): %s", rec.Code, out)
			}
		})
	}
}
