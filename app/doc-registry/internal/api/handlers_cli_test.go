package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/governanceops"
	"github.com/specgate/doc-registry/internal/governanceprofile"
	"github.com/specgate/doc-registry/internal/integrations"
	"github.com/specgate/doc-registry/internal/workboard"
)

// --- fakes for governanceops dependencies ---

type cliTestWorkBoard struct {
	crs      []workboard.ChangeRequest
	features map[string]*workboard.Feature
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
	return nil, nil
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

func (u *cliTestFeatureUpserter) UpsertFeatureByKey(_ context.Context, key, name string) (*workboard.Feature, error) {
	n := name
	if n == "" {
		n = key
	}
	return &workboard.Feature{ID: "feat-1", Key: key, Name: n, Status: workboard.FeatureStatusCandidate}, nil
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
			{ID: "cr-1", Title: "Test CR", Phase: "Ready", FeatureID: "feat-1"},
		},
		features: map[string]*workboard.Feature{
			"feat-1": {ID: "feat-1", Key: "test-feat", Name: "Test Feature"},
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
	return rt.Build()
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
	srv := newCLITestRouter(t, newCLIGovernanceSvc())

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
	// version must be present (defaults to "dev" in non-release builds)
	if got.Version == "" {
		t.Errorf("meta.version is empty — body: %s", rec.Body.String())
	}
}

func TestCLI_ResolveWorkRef(t *testing.T) {
	t.Parallel()
	svc := newCLIGovernanceSvc()
	srv := newCLITestRouter(t, svc)

	body, _ := json.Marshal(map[string]string{"ref": "cr-1"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/work-items/resolve", bytes.NewReader(body))
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

	req := httptest.NewRequest(http.MethodGet, "/api/v1/skills/some-id", nil)
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

	req := httptest.NewRequest(http.MethodGet, "/api/v1/skills?name=test", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body = %s", rec.Code, rec.Body.String())
	}
}
