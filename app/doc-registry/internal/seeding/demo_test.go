package seeding_test

import (
	"context"
	"testing"

	"github.com/rs/zerolog"

	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/knowledge"
	"github.com/specgate/doc-registry/internal/seeding"
	storagedb "github.com/specgate/doc-registry/internal/storage/db"
	"github.com/specgate/doc-registry/internal/workboard"
)

type memoryObjectStore struct {
	objects map[string][]byte
}

func newMemoryObjectStore() *memoryObjectStore {
	return &memoryObjectStore{objects: make(map[string][]byte)}
}

func (s *memoryObjectStore) PutObject(_ context.Context, key string, body []byte) error {
	s.objects[key] = append([]byte(nil), body...)
	return nil
}

func (s *memoryObjectStore) GetObject(_ context.Context, key string, _ int64) ([]byte, error) {
	body, ok := s.objects[key]
	if !ok {
		return nil, artifact.ErrFileNotFound
	}
	return append([]byte(nil), body...), nil
}

func (s *memoryObjectStore) DeleteObject(_ context.Context, key string) error {
	delete(s.objects, key)
	return nil
}

func openDemoSeedDB(t *testing.T) (*storagedb.Repository, *storagedb.WorkBoardRepository, *storagedb.IntegrationRepository, *storagedb.KnowledgeRepository) {
	t.Helper()

	gdb := newDemoTestGormDB(t)
	return storagedb.NewRepository(gdb),
		storagedb.NewWorkBoardRepository(gdb),
		storagedb.NewIntegrationRepository(gdb),
		storagedb.NewKnowledgeRepository(gdb)
}

func TestSeedDemoCreatesRealisticWorkBoardData(t *testing.T) {
	ctx := workboard.WithWorkspace(context.Background(), "demo-ws")
	artifactRepo, workBoardRepo, integrationRepo, knowledgeRepo := openDemoSeedDB(t)
	objectStore := newMemoryObjectStore()
	artifactSvc := artifact.NewService(
		artifactRepo,
		objectStore,
		func(featureID, version, filename string) string {
			return "demo/" + featureID + "/" + version + "/" + filename
		},
	)
	logger := zerolog.Nop()

	result, err := seeding.SeedDemo(ctx, seeding.DemoDeps{
		WorkBoard:    workBoardRepo,
		Artifacts:    artifactSvc,
		Integrations: integrationRepo,
		Knowledge:    knowledgeRepo,
		Logger:       &logger,
		// Real seeding always runs within a workspace (`--seed-demo-workspace-id`).
		// Knowledge freshness warnings are workspace-scoped, so the realistic demo
		// must be scoped too; an unscoped seed correctly produces no linked-knowledge
		// warning (and the seeder logs why).
		WorkspaceID: "demo-ws",
	})
	if err != nil {
		t.Fatalf("SeedDemo: %v", err)
	}
	if result.FeaturesCreated != 3 || result.ChangeRequestsCreated != 5 || result.ArtifactsPublished != 5 {
		t.Fatalf("unexpected seed result: %+v", result)
	}

	features, err := workBoardRepo.ListFeatures(ctx)
	if err != nil {
		t.Fatalf("ListFeatures: %v", err)
	}
	if len(features) != 3 {
		t.Fatalf("features=%d, want 3", len(features))
	}

	requests, err := workBoardRepo.ListChangeRequests(ctx, true)
	if err != nil {
		t.Fatalf("ListChangeRequests: %v", err)
	}
	if len(requests) != 5 {
		t.Fatalf("change requests=%d, want 5", len(requests))
	}

	var staleKnowledgeCR string
	var reviewCR string
	var intakeCR string
	var stalePackCR string
	var deliveredCR string
	for _, cr := range requests {
		switch cr.Key {
		case "DEMO-103":
			reviewCR = cr.ID
			if cr.Phase != workboard.BoardPhaseReview {
				t.Fatalf("DEMO-103 phase=%q, want Review", cr.Phase)
			}
		case "DEMO-102":
			staleKnowledgeCR = cr.ID
			stalePackCR = cr.ID
		case "DEMO-201":
			intakeCR = cr.ID
			if cr.Phase != workboard.BoardPhaseIntake {
				t.Fatalf("DEMO-201 phase=%q, want Intake", cr.Phase)
			}
		case "DEMO-301":
			deliveredCR = cr.ID
		}
	}
	if staleKnowledgeCR == "" || stalePackCR == "" || intakeCR == "" || reviewCR == "" || deliveredCR == "" {
		t.Fatalf("expected seeded CR coverage for DEMO-102/103/201/301, got %+v", requests)
	}

	warnings, err := workBoardRepo.ListStaleWarnings(ctx, workboard.StaleWarningFilter{ChangeRequestID: staleKnowledgeCR})
	if err != nil {
		t.Fatalf("ListStaleWarnings: %v", err)
	}
	foundKnowledgeWarning := false
	for _, warning := range warnings {
		if warning.Code == workboard.WarningLinkedKnowledgeNewer {
			foundKnowledgeWarning = true
		}
	}
	if !foundKnowledgeWarning {
		t.Fatalf("expected linked knowledge freshness warning, got %+v", warnings)
	}

	gateRuns, err := workBoardRepo.ListGateRuns(ctx, deliveredCR, 50)
	if err != nil {
		t.Fatalf("ListGateRuns: %v", err)
	}
	foundDeliveryReview := false
	for _, run := range gateRuns {
		if run.Gate == "delivery_review" && run.State == workboard.NextActionStateNeedsHumanReview {
			foundDeliveryReview = true
		}
	}
	if !foundDeliveryReview {
		t.Fatalf("expected delivery_review gate run, got %+v", gateRuns)
	}

	deliveredWarnings, err := workBoardRepo.ListStaleWarnings(ctx, workboard.StaleWarningFilter{ChangeRequestID: deliveredCR})
	if err != nil {
		t.Fatalf("ListStaleWarnings delivered: %v", err)
	}
	foundTrackerConflict := false
	for _, warning := range deliveredWarnings {
		if warning.Code == workboard.WarningTrackerStatusConflict {
			foundTrackerConflict = true
			break
		}
	}
	if !foundTrackerConflict {
		t.Fatalf("expected tracker_status_conflict warning, got %+v", deliveredWarnings)
	}

	second, err := seeding.SeedDemo(ctx, seeding.DemoDeps{
		WorkBoard:    workBoardRepo,
		Artifacts:    artifactSvc,
		Integrations: integrationRepo,
		Knowledge:    knowledgeRepo,
		Logger:       &logger,
		WorkspaceID:  "demo-ws",
	})
	if err != nil {
		t.Fatalf("second SeedDemo: %v", err)
	}
	if second.FeaturesExisting != 3 || second.FeaturesCreated != 0 || second.ChangeRequestsCreated != 0 {
		t.Fatalf("second seed should be idempotent, got %+v", second)
	}
}

func TestSeedDemoIsolatedAcrossWorkspaces(t *testing.T) {
	artifactRepo, workBoardRepo, integrationRepo, knowledgeRepo := openDemoSeedDB(t)
	objectStore := newMemoryObjectStore()
	artifactSvc := artifact.NewService(
		artifactRepo,
		objectStore,
		func(featureID, version, filename string) string {
			return "demo/" + featureID + "/" + version + "/" + filename
		},
	)
	logger := zerolog.Nop()

	for _, workspaceID := range []string{"demo-ws-a", "demo-ws-b"} {
		ctx := workboard.WithWorkspace(context.Background(), workspaceID)
		result, err := seeding.SeedDemo(ctx, seeding.DemoDeps{
			WorkBoard:    workBoardRepo,
			Artifacts:    artifactSvc,
			Integrations: integrationRepo,
			Knowledge:    knowledgeRepo,
			Logger:       &logger,
			WorkspaceID:  workspaceID,
		})
		if err != nil {
			t.Fatalf("SeedDemo(%s): %v", workspaceID, err)
		}
		if result.FeaturesCreated != 3 || result.ChangeRequestsCreated != 5 {
			t.Fatalf("SeedDemo(%s) result = %+v", workspaceID, result)
		}
		features, err := workBoardRepo.ListFeatures(ctx)
		if err != nil {
			t.Fatalf("ListFeatures(%s): %v", workspaceID, err)
		}
		if len(features) != 3 {
			t.Fatalf("features(%s) = %d, want 3", workspaceID, len(features))
		}
		integrations, err := integrationRepo.ListIntegrations(ctx)
		if err != nil {
			t.Fatalf("ListIntegrations(%s): %v", workspaceID, err)
		}
		if len(integrations) != 2 {
			t.Fatalf("integrations(%s) = %d, want 2", workspaceID, len(integrations))
		}
	}
}

func TestSeedDemoAttributesChangeRequestsToWorkspace(t *testing.T) {
	ctx := workboard.WithWorkspace(context.Background(), "ws-1")
	artifactRepo, workBoardRepo, integrationRepo, knowledgeRepo := openDemoSeedDB(t)
	objectStore := newMemoryObjectStore()
	artifactSvc := artifact.NewService(
		artifactRepo,
		objectStore,
		func(featureID, version, filename string) string {
			return "demo/" + featureID + "/" + version + "/" + filename
		},
	)
	logger := zerolog.Nop()

	_, err := seeding.SeedDemo(ctx, seeding.DemoDeps{
		WorkBoard:    workBoardRepo,
		Artifacts:    artifactSvc,
		Integrations: integrationRepo,
		Knowledge:    knowledgeRepo,
		Logger:       &logger,
		WorkspaceID:  "ws-1",
		CreatedBy:    "thanhtung2693",
	})
	if err != nil {
		t.Fatalf("SeedDemo: %v", err)
	}

	requests, err := workBoardRepo.ListChangeRequests(ctx, true)
	if err != nil {
		t.Fatalf("ListChangeRequests: %v", err)
	}
	if len(requests) == 0 {
		t.Fatal("expected demo change requests")
	}
	for _, cr := range requests {
		if cr.WorkspaceID != "ws-1" {
			t.Fatalf("%s workspace_id=%q, want ws-1", cr.Key, cr.WorkspaceID)
		}
		if cr.CreatedBy != "thanhtung2693" {
			t.Fatalf("%s created_by=%q, want thanhtung2693", cr.Key, cr.CreatedBy)
		}
	}
}

func TestSeedDemoScopesKnowledgeToWorkspace(t *testing.T) {
	ctx := workboard.WithWorkspace(context.Background(), "ws-1")
	artifactRepo, workBoardRepo, integrationRepo, knowledgeRepo := openDemoSeedDB(t)
	objectStore := newMemoryObjectStore()
	artifactSvc := artifact.NewService(
		artifactRepo,
		objectStore,
		func(featureID, version, filename string) string {
			return "demo/" + featureID + "/" + version + "/" + filename
		},
	)
	logger := zerolog.Nop()

	result, err := seeding.SeedDemo(ctx, seeding.DemoDeps{
		WorkBoard:    workBoardRepo,
		Artifacts:    artifactSvc,
		Integrations: integrationRepo,
		Knowledge:    knowledgeRepo,
		Logger:       &logger,
		WorkspaceID:  "ws-1",
		CreatedBy:    "thanhtung2693",
	})
	if err != nil {
		t.Fatalf("SeedDemo: %v", err)
	}
	if result.KnowledgeCreated == 0 {
		t.Fatal("expected demo knowledge to be seeded")
	}

	// Scoped list must find exactly the seeded docs; each must carry ws-1.
	// If the seeder left workspace_id empty, the workspace filter would exclude
	// every legacy row and this count would drop to zero.
	docs, err := knowledgeRepo.List(ctx, knowledge.ListFilter{WorkspaceID: "ws-1"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(docs) != result.KnowledgeCreated {
		t.Fatalf("workspace-scoped knowledge docs=%d, want %d", len(docs), result.KnowledgeCreated)
	}
	for _, d := range docs {
		if d.WorkspaceID != "ws-1" {
			t.Fatalf("%s workspace_id=%q, want ws-1", d.DocumentID, d.WorkspaceID)
		}
	}
}

func TestSeedDemoBackfillsExistingDemoChangeRequestAttribution(t *testing.T) {
	ctx := workboard.WithWorkspace(context.Background(), "ws-1")
	artifactRepo, workBoardRepo, integrationRepo, knowledgeRepo := openDemoSeedDB(t)
	objectStore := newMemoryObjectStore()
	artifactSvc := artifact.NewService(
		artifactRepo,
		objectStore,
		func(featureID, version, filename string) string {
			return "demo/" + featureID + "/" + version + "/" + filename
		},
	)
	logger := zerolog.Nop()

	if _, err := seeding.SeedDemo(ctx, seeding.DemoDeps{
		WorkBoard:    workBoardRepo,
		Artifacts:    artifactSvc,
		Integrations: integrationRepo,
		Knowledge:    knowledgeRepo,
		Logger:       &logger,
		WorkspaceID:  "ws-1",
	}); err != nil {
		t.Fatalf("initial SeedDemo: %v", err)
	}
	if _, err := seeding.SeedDemo(ctx, seeding.DemoDeps{
		WorkBoard:    workBoardRepo,
		Artifacts:    artifactSvc,
		Integrations: integrationRepo,
		Knowledge:    knowledgeRepo,
		Logger:       &logger,
		WorkspaceID:  "ws-1",
		CreatedBy:    "thanhtung2693",
	}); err != nil {
		t.Fatalf("second SeedDemo: %v", err)
	}

	requests, err := workBoardRepo.ListChangeRequests(ctx, true)
	if err != nil {
		t.Fatalf("ListChangeRequests: %v", err)
	}
	for _, cr := range requests {
		if cr.WorkspaceID != "ws-1" {
			t.Fatalf("%s workspace_id=%q, want ws-1", cr.Key, cr.WorkspaceID)
		}
		if cr.CreatedBy != "thanhtung2693" {
			t.Fatalf("%s created_by=%q, want thanhtung2693", cr.Key, cr.CreatedBy)
		}
	}
}
