package seeding_test

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/specgate/doc-registry/internal/artifact"
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

func (s *memoryObjectStore) GetObject(_ context.Context, key string) ([]byte, error) {
	body, ok := s.objects[key]
	if !ok {
		return nil, artifact.ErrFileNotFound
	}
	return append([]byte(nil), body...), nil
}

func (s *memoryObjectStore) PresignGet(_ context.Context, key string) (string, error) {
	return "memory://" + key, nil
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
	ctx := context.Background()
	artifactRepo, workBoardRepo, integrationRepo, knowledgeRepo := openDemoSeedDB(t)
	objectStore := newMemoryObjectStore()
	artifactSvc := artifact.NewService(
		artifactRepo,
		objectStore,
		func(featureID, version, filename string) string {
			return "demo/" + featureID + "/" + version + "/" + filename
		},
		time.Hour,
	)
	logger := zerolog.Nop()

	result, err := seeding.SeedDemo(ctx, seeding.DemoDeps{
		WorkBoard:    workBoardRepo,
		Artifacts:    artifactSvc,
		Integrations: integrationRepo,
		Knowledge:    knowledgeRepo,
		Logger:       &logger,
	})
	if err != nil {
		t.Fatalf("SeedDemo: %v", err)
	}
	if result.FeaturesCreated != 3 || result.ChangeRequestsCreated != 5 || result.ArtifactsPublished < 7 {
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
	var draftingCR string
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
			draftingCR = cr.ID
			if cr.Phase != workboard.BoardPhaseDraft {
				t.Fatalf("DEMO-201 phase=%q, want Draft", cr.Phase)
			}
		case "DEMO-301":
			deliveredCR = cr.ID
		}
	}
	if staleKnowledgeCR == "" || stalePackCR == "" || draftingCR == "" || reviewCR == "" || deliveredCR == "" {
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

	packWarnings, err := workBoardRepo.ListStaleWarnings(ctx, workboard.StaleWarningFilter{ChangeRequestID: stalePackCR})
	if err != nil {
		t.Fatalf("ListStaleWarnings stale pack: %v", err)
	}
	foundPackWarning := false
	for _, warning := range packWarnings {
		if warning.Code == workboard.WarningContextPackStale {
			foundPackWarning = true
			break
		}
	}
	if !foundPackWarning {
		t.Fatalf("expected context_pack_stale warning, got %+v", packWarnings)
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
	})
	if err != nil {
		t.Fatalf("second SeedDemo: %v", err)
	}
	if second.FeaturesExisting != 3 || second.FeaturesCreated != 0 || second.ChangeRequestsCreated != 0 {
		t.Fatalf("second seed should be idempotent, got %+v", second)
	}
}

func TestSeedDemoAttributesChangeRequestsToWorkspace(t *testing.T) {
	ctx := context.Background()
	artifactRepo, workBoardRepo, integrationRepo, knowledgeRepo := openDemoSeedDB(t)
	objectStore := newMemoryObjectStore()
	artifactSvc := artifact.NewService(
		artifactRepo,
		objectStore,
		func(featureID, version, filename string) string {
			return "demo/" + featureID + "/" + version + "/" + filename
		},
		time.Hour,
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

func TestSeedDemoBackfillsExistingDemoChangeRequestAttribution(t *testing.T) {
	ctx := context.Background()
	artifactRepo, workBoardRepo, integrationRepo, knowledgeRepo := openDemoSeedDB(t)
	objectStore := newMemoryObjectStore()
	artifactSvc := artifact.NewService(
		artifactRepo,
		objectStore,
		func(featureID, version, filename string) string {
			return "demo/" + featureID + "/" + version + "/" + filename
		},
		time.Hour,
	)
	logger := zerolog.Nop()

	if _, err := seeding.SeedDemo(ctx, seeding.DemoDeps{
		WorkBoard:    workBoardRepo,
		Artifacts:    artifactSvc,
		Integrations: integrationRepo,
		Knowledge:    knowledgeRepo,
		Logger:       &logger,
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
