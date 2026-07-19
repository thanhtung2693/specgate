package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/specgate/doc-registry/internal/artifact"
	storagedb "github.com/specgate/doc-registry/internal/storage/db"
	"github.com/specgate/doc-registry/internal/workboard"
)

type fakeWorkBoardGateArtifactService struct {
	artifacts map[string]*artifact.Artifact
	files     map[string]map[string][]byte
}

func (s *fakeWorkBoardGateArtifactService) Publish(context.Context, artifact.PublishInput) (*artifact.Artifact, error) {
	return nil, errors.New("not implemented")
}
func (s *fakeWorkBoardGateArtifactService) Get(_ context.Context, id string) (*artifact.Artifact, error) {
	row, ok := s.artifacts[id]
	if !ok {
		return nil, artifact.ErrNotFound
	}
	cp := *row
	cp.Files = append([]artifact.File(nil), row.Files...)
	return &cp, nil
}
func (s *fakeWorkBoardGateArtifactService) GetInWorkspace(_ context.Context, workspaceID, id string) (*artifact.Artifact, error) {
	row, ok := s.artifacts[id]
	if !ok || row.WorkspaceID != workspaceID {
		return nil, artifact.ErrNotFound
	}
	cp := *row
	cp.Files = append([]artifact.File(nil), row.Files...)
	return &cp, nil
}
func (s *fakeWorkBoardGateArtifactService) List(context.Context, artifact.ListFilter) ([]artifact.Artifact, error) {
	return nil, nil
}
func (s *fakeWorkBoardGateArtifactService) Count(context.Context, artifact.ListFilter) (int64, error) {
	return 0, nil
}
func (s *fakeWorkBoardGateArtifactService) LatestArtifact(context.Context, string) (*artifact.Artifact, error) {
	return nil, nil
}
func (s *fakeWorkBoardGateArtifactService) NextVersion(context.Context, string) (string, error) {
	return "v0.1", nil
}
func (s *fakeWorkBoardGateArtifactService) ResolveNextVersion(context.Context, string, string) (string, error) {
	return "v0.1", nil
}
func (s *fakeWorkBoardGateArtifactService) UpdateStatus(context.Context, string, artifact.StatusUpdate) (*artifact.Artifact, error) {
	return nil, errors.New("not implemented")
}
func (s *fakeWorkBoardGateArtifactService) Delete(context.Context, string) error { return nil }
func (s *fakeWorkBoardGateArtifactService) FileContent(_ context.Context, id string, path string) ([]byte, error) {
	byArtifact, ok := s.files[id]
	if !ok {
		return nil, artifact.ErrNotFound
	}
	body, ok := byArtifact[path]
	if !ok {
		return nil, artifact.ErrNotFound
	}
	return append([]byte(nil), body...), nil
}
func (s *fakeWorkBoardGateArtifactService) CheckConflicts(context.Context, []string) (*artifact.ConflictReport, error) {
	return nil, nil
}
func (s *fakeWorkBoardGateArtifactService) ListEvents(context.Context, artifact.EventFilter) ([]artifact.Event, error) {
	return nil, nil
}
func (s *fakeWorkBoardGateArtifactService) RefreshReadinessRuns(context.Context, string, []artifact.ReadinessEvaluation) ([]artifact.ReadinessRun, error) {
	return nil, nil
}
func (s *fakeWorkBoardGateArtifactService) ListReadinessRuns(context.Context, string, int) ([]artifact.ReadinessRun, error) {
	return nil, nil
}

func TestWorkBoardDeleteRoutesRemoved(t *testing.T) {
	t.Parallel()
	rt := &Router{Handlers: &Handlers{}, Config: testConfig()}
	srv := rt.Build()
	// change-requests DELETE is intentionally not exposed; features DELETE is.
	for _, tc := range []string{
		"/workboard/change-requests/cr-1",
	} {
		rec := serveJSON(srv, http.MethodDelete, tc, "")
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("path=%s status=%d body=%s", tc, rec.Code, rec.Body.String())
		}
	}
}

func TestWorkBoardChangeRequestsArchiveVisibilityAndGet(t *testing.T) {
	t.Parallel()
	db := newTestGormDB(t)
	workBoardRepo := storagedb.NewWorkBoardRepository(db)
	feature, err := workBoardRepo.CreateFeature(context.Background(), workboard.Feature{
		ID:          "feat-archive-http",
		Key:         "FEAT-ARCHIVE-HTTP",
		Name:        "Archive HTTP",
		Status:      workboard.FeatureStatusPlanned,
		WorkspaceID: "ws-archive",
	})
	if err != nil {
		t.Fatal(err)
	}
	activeCR, err := workBoardRepo.CreateChangeRequest(context.Background(), workboard.ChangeRequest{
		ID:          "cr-active-http",
		Key:         "CR-ACTIVE-HTTP",
		FeatureID:   feature.ID,
		WorkspaceID: "ws-archive",
		WorkType:    workboard.WorkTypeNewFeature,
		Title:       "Active",
	})
	if err != nil {
		t.Fatal(err)
	}
	archivedCR, err := workBoardRepo.CreateChangeRequest(context.Background(), workboard.ChangeRequest{
		ID:          "cr-archived-http",
		Key:         "CR-ARCHIVED-HTTP",
		FeatureID:   feature.ID,
		WorkspaceID: "ws-archive",
		WorkType:    workboard.WorkTypeCleanup,
		Title:       "Archived",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = workBoardRepo.UpdateChangeRequest(context.Background(), workboard.ChangeRequest{
		ID:            archivedCR.ID,
		Archived:      true,
		ArchivedBy:    "pm@example.com",
		ArchiveReason: "not now",
	})
	if err != nil {
		t.Fatal(err)
	}
	artifactSvc := &fakeWorkBoardGateArtifactService{
		artifacts: map[string]*artifact.Artifact{
			"art-canonical-http": {ID: "art-canonical-http", Version: "v1.0"},
			"art-lead-http":      {ID: "art-lead-http", Version: "v1.1"},
		},
		files: map[string]map[string][]byte{
			"art-canonical-http": {"spec.md": []byte("# Spec\ncanonical\n")},
			"art-lead-http":      {"spec.md": []byte("# Spec\nlead\n")},
		},
	}
	rt := &Router{
		Handlers: &Handlers{WorkBoard: workBoardRepo, Artifacts: artifactSvc},
		Config:   testConfig(),
	}
	srv := rt.Build()

	rec := serveJSON(srv, http.MethodGet, "/workboard/change-requests?workspace_id=ws-archive", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("default list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var listOut struct {
		Items []workboard.ChangeRequest `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listOut); err != nil {
		t.Fatal(err)
	}
	if len(listOut.Items) != 1 || listOut.Items[0].ID != activeCR.ID {
		t.Fatalf("default list items=%+v", listOut.Items)
	}

	rec = serveJSON(srv, http.MethodGet, "/workboard/change-requests?include_archived=true&workspace_id=ws-archive", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("include archived status=%d body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listOut); err != nil {
		t.Fatal(err)
	}
	if len(listOut.Items) != 2 {
		t.Fatalf("expected 2 items with include_archived, got %d", len(listOut.Items))
	}

	rec = serveJSON(srv, http.MethodGet, "/workboard/change-requests/"+archivedCR.ID+"?workspace_id=ws-archive", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get archived status=%d body=%s", rec.Code, rec.Body.String())
	}
	var getOut workboard.ChangeRequest
	if err := json.Unmarshal(rec.Body.Bytes(), &getOut); err != nil {
		t.Fatal(err)
	}
	if !getOut.Archived || getOut.ArchivedBy != "pm@example.com" || getOut.ArchiveReason != "not now" || getOut.ArchivedAt == nil {
		t.Fatalf("archived metadata missing: %+v", getOut)
	}
}

func TestWorkBoardChangeRequestsWorkspaceFilter(t *testing.T) {
	t.Parallel()
	db := newTestGormDB(t)
	workBoardRepo := storagedb.NewWorkBoardRepository(db)
	feature, err := workBoardRepo.CreateFeature(context.Background(), workboard.Feature{
		ID: "feat-workspace-http", WorkspaceID: "ws-current", Key: "FEAT-WORKSPACE-HTTP",
		Name: "Workspace HTTP", Status: workboard.FeatureStatusPlanned,
	})
	if err != nil {
		t.Fatal(err)
	}
	inWorkspace, err := workBoardRepo.CreateChangeRequest(context.Background(), workboard.ChangeRequest{
		ID:          "cr-workspace-http",
		Key:         "CR-WORKSPACE-HTTP",
		FeatureID:   feature.ID,
		WorkspaceID: "ws-current",
		WorkType:    workboard.WorkTypeNewFeature,
		Title:       "Current workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	otherFeature, err := workBoardRepo.CreateFeature(context.Background(), workboard.Feature{
		ID: "feat-workspace-other", WorkspaceID: "ws-other", Key: "FEAT-WORKSPACE-OTHER",
		Name: "Other workspace", Status: workboard.FeatureStatusPlanned,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = workBoardRepo.CreateChangeRequest(context.Background(), workboard.ChangeRequest{
		ID:          "cr-other-workspace-http",
		Key:         "CR-OTHER-WORKSPACE-HTTP",
		FeatureID:   otherFeature.ID,
		WorkspaceID: "ws-other",
		WorkType:    workboard.WorkTypeCleanup,
		Title:       "Other workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	rt := &Router{
		Handlers: &Handlers{WorkBoard: workBoardRepo},
		Config:   testConfig(),
	}
	srv := rt.Build()

	rec := serveJSON(srv, http.MethodGet, "/workboard/change-requests", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing workspace status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = serveJSON(srv, http.MethodGet, "/workboard/change-requests?workspace_id=ws-current", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("workspace list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var listOut struct {
		Items []workboard.ChangeRequest `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listOut); err != nil {
		t.Fatal(err)
	}
	if len(listOut.Items) != 1 || listOut.Items[0].ID != inWorkspace.ID {
		t.Fatalf("workspace list items=%+v", listOut.Items)
	}
}

func TestWorkBoardFeatureSummaryRouteIsNotRegistered(t *testing.T) {
	t.Parallel()
	db := newTestGormDB(t)
	workBoardRepo := storagedb.NewWorkBoardRepository(db)
	feature, err := workBoardRepo.CreateFeature(context.Background(), workboard.Feature{
		ID: "feature-summary-route", Key: "FEATURE-SUMMARY-ROUTE", Name: "Summary route", WorkspaceID: "ws-summary",
	})
	if err != nil {
		t.Fatal(err)
	}
	rt := &Router{Handlers: &Handlers{WorkBoard: workBoardRepo}, Config: testConfig()}
	rec := serveJSON(rt.Build(), http.MethodPut, "/workboard/features/"+feature.ID+"/summary?workspace_id=ws-summary", `{"summary_md":"unsupported"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("feature summary route status=%d body=%s, want 404", rec.Code, rec.Body.String())
	}
}

func TestWorkBoardPromoteArtifactToCanonical(t *testing.T) {
	t.Parallel()
	db := newTestGormDB(t)
	now := time.Now().UTC()
	// Approved and draft artifacts carry the feature UUID in feature_id, matching
	// how the governanceops publish path stores it. A third artifact has no
	// feature at all. (Key-valued feature_id resolution is covered by the
	// storage/db service test.)
	seed := []artifact.Artifact{
		{ID: "art-promote-approved", WorkspaceID: "ws-promote", FeatureID: "feat-promote-http", Version: "v1.0", Status: artifact.StatusApproved, RequestType: artifact.RequestTypeNewFeature, ImpactLevel: artifact.ImpactLevelMedium, CreatedBy: "tester", CreatedAt: now, UpdatedAt: now},
		{ID: "art-promote-draft", WorkspaceID: "ws-promote", FeatureID: "feat-promote-http", Version: "v1.1", Status: artifact.StatusDraft, RequestType: artifact.RequestTypeChangeRequest, ImpactLevel: artifact.ImpactLevelMedium, CreatedBy: "tester", CreatedAt: now, UpdatedAt: now},
		{ID: "art-promote-nofeature", WorkspaceID: "ws-promote", FeatureID: "", Version: "v1.0", Status: artifact.StatusApproved, RequestType: artifact.RequestTypeNewFeature, ImpactLevel: artifact.ImpactLevelMedium, CreatedBy: "tester", CreatedAt: now, UpdatedAt: now},
	}
	for i := range seed {
		if err := db.Create(&seed[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	workBoardRepo := storagedb.NewWorkBoardRepository(db)
	feature, err := workBoardRepo.CreateFeature(context.Background(), workboard.Feature{
		ID:          "feat-promote-http",
		Key:         "FEAT-PROMOTE-HTTP",
		Name:        "Promote HTTP",
		Status:      workboard.FeatureStatusPlanned,
		WorkspaceID: "ws-promote",
	})
	if err != nil {
		t.Fatal(err)
	}
	artifactSvc := &fakeWorkBoardGateArtifactService{artifacts: map[string]*artifact.Artifact{
		"art-promote-approved":  {ID: "art-promote-approved", WorkspaceID: "ws-promote", FeatureID: "feat-promote-http", Version: "v1.0", Status: artifact.StatusApproved},
		"art-promote-draft":     {ID: "art-promote-draft", WorkspaceID: "ws-promote", FeatureID: "feat-promote-http", Version: "v1.1", Status: artifact.StatusDraft},
		"art-promote-nofeature": {ID: "art-promote-nofeature", WorkspaceID: "ws-promote", FeatureID: "", Version: "v1.0", Status: artifact.StatusApproved},
	}}
	rt := &Router{Handlers: &Handlers{WorkBoard: workBoardRepo, Artifacts: artifactSvc}, Config: testConfig()}
	srv := rt.Build()

	// Approved artifact promotes: feature canonical is set and planned -> active.
	rec := serveJSON(srv, http.MethodPost, "/workboard/artifacts/art-promote-approved/promote-canonical?workspace_id=ws-promote", `{"approved_by":"pm"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("promote approved status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out workboard.Feature
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.ID != feature.ID || out.CanonicalArtifactID != "art-promote-approved" {
		t.Fatalf("promotion did not set canonical: %+v", out)
	}
	if out.Status != workboard.FeatureStatusActive {
		t.Fatalf("feature should be active after promotion, got %q", out.Status)
	}

	// Non-approved artifact is rejected with a 400 validation error.
	rec = serveJSON(srv, http.MethodPost, "/workboard/artifacts/art-promote-draft/promote-canonical?workspace_id=ws-promote", `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("promote draft: want 400, got %d body=%s", rec.Code, rec.Body.String())
	}

	// Featureless artifact is rejected with a 400.
	rec = serveJSON(srv, http.MethodPost, "/workboard/artifacts/art-promote-nofeature/promote-canonical?workspace_id=ws-promote", `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("promote featureless: want 400, got %d body=%s", rec.Code, rec.Body.String())
	}

	// Missing artifact -> 404.
	rec = serveJSON(srv, http.MethodPost, "/workboard/artifacts/missing/promote-canonical?workspace_id=ws-promote", `{}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("promote missing: want 404, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestWorkBoardGateRunsRefreshAndList(t *testing.T) {
	t.Parallel()
	db := newTestGormDB(t)
	now := time.Now().UTC()
	if err := db.Create(&artifact.Artifact{
		ID:          "art-canonical-http",
		WorkspaceID: "ws-gate",
		FeatureID:   "feat-gate-http",
		Version:     "v1.0",
		Status:      artifact.StatusApproved,
		RequestType: artifact.RequestTypeNewFeature,
		ImpactLevel: artifact.ImpactLevelMedium,
		CreatedBy:   "tester",
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&artifact.Artifact{
		ID:          "art-lead-http",
		WorkspaceID: "ws-gate",
		FeatureID:   "feat-gate-http",
		Version:     "v1.1",
		Status:      artifact.StatusApproved,
		RequestType: artifact.RequestTypeChangeRequest,
		ImpactLevel: artifact.ImpactLevelMedium,
		CreatedBy:   "tester",
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	workBoardRepo := storagedb.NewWorkBoardRepository(db)
	feature, err := workBoardRepo.CreateFeature(context.Background(), workboard.Feature{
		ID:                  "feat-gate-http",
		Key:                 "FEAT-GATE-HTTP",
		Name:                "Gate HTTP",
		Status:              workboard.FeatureStatusPlanned,
		CanonicalArtifactID: "art-canonical-http",
		WorkspaceID:         "ws-gate",
	})
	if err != nil {
		t.Fatal(err)
	}
	cr, err := workBoardRepo.CreateChangeRequest(context.Background(), workboard.ChangeRequest{
		ID:             "cr-gate-http",
		Key:            "CR-GATE-HTTP",
		FeatureID:      feature.ID,
		WorkType:       workboard.WorkTypeFeatureChange,
		Title:          "Gate run snapshot",
		LeadArtifactID: "art-lead-http",
		WorkspaceID:    "ws-gate",
	})
	if err != nil {
		t.Fatal(err)
	}
	artifactSvc := &fakeWorkBoardGateArtifactService{
		artifacts: map[string]*artifact.Artifact{
			"art-canonical-http": {ID: "art-canonical-http", WorkspaceID: "ws-gate", Version: "v1.0"},
			"art-lead-http":      {ID: "art-lead-http", WorkspaceID: "ws-gate", Version: "v1.1"},
		},
		files: map[string]map[string][]byte{
			"art-canonical-http": {"spec.md": []byte("# Spec\ncanonical\n")},
			"art-lead-http":      {"spec.md": []byte("# Spec\nlead\n")},
		},
	}
	rt := &Router{
		Handlers: &Handlers{WorkBoard: workBoardRepo, Artifacts: artifactSvc},
		Config:   testConfig(),
	}
	srv := rt.Build()

	rec := serveJSON(srv, http.MethodPost, "/workboard/change-requests/"+cr.ID+"/gate-runs/refresh", `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing workspace status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = serveJSON(srv, http.MethodPost, "/workboard/change-requests/"+cr.ID+"/gate-runs/refresh?workspace_id=ws-gate", `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh gate runs status=%d body=%s", rec.Code, rec.Body.String())
	}
	var refreshOut struct {
		Items []workboard.GateRun `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &refreshOut); err != nil {
		t.Fatal(err)
	}
	if len(refreshOut.Items) == 0 {
		t.Fatal("expected gate run snapshot rows")
	}

	rec = serveJSON(srv, http.MethodGet, "/workboard/change-requests/"+cr.ID+"/gate-runs?limit=10&workspace_id=ws-gate", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list gate runs status=%d body=%s", rec.Code, rec.Body.String())
	}
	var listOut struct {
		Items []workboard.GateRun `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listOut); err != nil {
		t.Fatal(err)
	}
	if len(listOut.Items) == 0 {
		t.Fatal("expected persisted gate run rows")
	}
	foundCanonicalSpec := false
	for _, row := range listOut.Items {
		if row.Gate == "canonical_spec" {
			if !strings.Contains(row.EvidenceJSON, `"source_artifact_id":"art-lead-http"`) {
				t.Fatalf("missing evidence source artifact in %s", row.EvidenceJSON)
			}
			if !strings.Contains(row.EvidenceJSON, `"linked_knowledge"`) {
				t.Fatalf("missing linked knowledge section in %s", row.EvidenceJSON)
			}
			if !strings.Contains(row.EvidenceJSON, `"judge_model":"deterministic-v1"`) {
				t.Fatalf("missing judge model in %s", row.EvidenceJSON)
			}
			foundCanonicalSpec = true
			break
		}
	}
	if !foundCanonicalSpec {
		t.Fatalf("expected canonical_spec gate run in %v", listOut.Items)
	}

	rec = serveJSON(srv, http.MethodPost, "/workboard/change-requests/"+cr.ID+"/gate-runs/refresh?workspace_id=ws-gate", `{
		"evaluations":[
			{
				"gate":"canonical_spec",
				"state":"needs_human_review",
				"hint":"Judge confidence below threshold",
				"confidence":0.42,
				"judge_model":"gpt-5-mini",
				"eval_suite_version":"gate-calibration-2026-05-31"
			}
		]
	}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh gate runs with evaluation status=%d body=%s", rec.Code, rec.Body.String())
	}
	var evalRefreshOut struct {
		Items []workboard.GateRun `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &evalRefreshOut); err != nil {
		t.Fatal(err)
	}
	foundEvalRow := false
	for _, row := range evalRefreshOut.Items {
		if row.Gate != "canonical_spec" {
			continue
		}
		if row.State != workboard.NextActionStateNeedsHumanReview {
			t.Fatalf("expected overridden state, got %s", row.State)
		}
		if !strings.Contains(row.EvidenceJSON, `"judge_model":"gpt-5-mini"`) {
			t.Fatalf("expected judged model evidence in %s", row.EvidenceJSON)
		}
		if !strings.Contains(row.EvidenceJSON, `"confidence":0.42`) {
			t.Fatalf("expected judged confidence in %s", row.EvidenceJSON)
		}
		foundEvalRow = true
		break
	}
	if !foundEvalRow {
		t.Fatal("expected canonical_spec row from evaluated refresh")
	}

	rec = serveJSON(srv, http.MethodPost, "/workboard/change-requests/"+cr.ID+"/gate-runs/refresh?workspace_id=ws-gate", `{
		"evaluations":[{"gate":"canonical_spec","state":"warn","confidence":1.2}]
	}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected bad request for invalid confidence, got status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func serveJSON(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}
