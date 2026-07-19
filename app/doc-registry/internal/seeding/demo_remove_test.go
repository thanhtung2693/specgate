package seeding_test

import (
	"context"
	"testing"

	"github.com/rs/zerolog"

	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/seeding"
	storagedb "github.com/specgate/doc-registry/internal/storage/db"
	"github.com/specgate/doc-registry/internal/workboard"
)

func workboardFeatureFixture() workboard.Feature {
	return workboard.Feature{
		ID:     "real-feature-1",
		Key:    "REAL-1",
		Name:   "Real feature",
		Status: workboard.FeatureStatusActive,
	}
}

func TestRemoveDemoDeletesSeededData(t *testing.T) {
	ctx := workboard.WithWorkspace(context.Background(), "demo-ws")
	gdb := newDemoTestGormDB(t)
	artifactRepo := storagedb.NewRepository(gdb)
	workBoardRepo := storagedb.NewWorkBoardRepository(gdb)
	integrationRepo := storagedb.NewIntegrationRepository(gdb)
	knowledgeRepo := storagedb.NewKnowledgeRepository(gdb)
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
		WorkspaceID:  "demo-ws",
	}); err != nil {
		t.Fatalf("SeedDemo: %v", err)
	}
	if len(objectStore.objects) == 0 {
		t.Fatal("expected seeded blob objects before removal")
	}

	result, err := seeding.RemoveDemo(ctx, seeding.RemoveDemoDeps{
		DB:           gdb,
		Artifacts:    artifactSvc,
		Integrations: integrationRepo,
	})
	if err != nil {
		t.Fatalf("RemoveDemo: %v", err)
	}
	if result.FeaturesDeleted != 3 || result.ChangeRequestsDeleted != 5 || result.ArtifactsDeleted != 5 || result.IntegrationsDeleted != 2 || result.KnowledgeDeleted != 1 {
		t.Fatalf("unexpected removal result: %+v", result)
	}

	features, err := workBoardRepo.ListFeatures(ctx)
	if err != nil {
		t.Fatalf("ListFeatures: %v", err)
	}
	if len(features) != 0 {
		t.Fatalf("features remaining after removal: %+v", features)
	}
	requests, err := workBoardRepo.ListChangeRequests(ctx, true)
	if err != nil {
		t.Fatalf("ListChangeRequests: %v", err)
	}
	if len(requests) != 0 {
		t.Fatalf("change requests remaining after removal: %+v", requests)
	}
	arts, err := artifactSvc.List(ctx, artifact.ListFilter{Limit: 100})
	if err != nil {
		t.Fatalf("List artifacts: %v", err)
	}
	if len(arts) != 0 {
		t.Fatalf("artifacts remaining after removal: %+v", arts)
	}
	if len(objectStore.objects) != 0 {
		t.Fatalf("blob objects remaining after removal: %d", len(objectStore.objects))
	}
	integrations, err := integrationRepo.ListIntegrations(ctx)
	if err != nil {
		t.Fatalf("ListIntegrations: %v", err)
	}
	if len(integrations) != 0 {
		t.Fatalf("integrations remaining after removal: %+v", integrations)
	}

	second, err := seeding.RemoveDemo(ctx, seeding.RemoveDemoDeps{
		DB:           gdb,
		Artifacts:    artifactSvc,
		Integrations: integrationRepo,
	})
	if err != nil {
		t.Fatalf("second RemoveDemo should be idempotent: %v", err)
	}
	if second.FeaturesDeleted != 0 || second.ChangeRequestsDeleted != 0 || second.ArtifactsDeleted != 0 || second.IntegrationsDeleted != 0 || second.KnowledgeDeleted != 0 {
		t.Fatalf("second removal should delete nothing, got %+v", second)
	}
}

func TestRemoveDemoLeavesNonDemoDataAlone(t *testing.T) {
	ctx := workboard.WithWorkspace(context.Background(), "demo-ws")
	gdb := newDemoTestGormDB(t)
	artifactRepo := storagedb.NewRepository(gdb)
	workBoardRepo := storagedb.NewWorkBoardRepository(gdb)
	integrationRepo := storagedb.NewIntegrationRepository(gdb)
	knowledgeRepo := storagedb.NewKnowledgeRepository(gdb)
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
		WorkspaceID:  "demo-ws",
	}); err != nil {
		t.Fatalf("SeedDemo: %v", err)
	}

	realFeature, err := workBoardRepo.CreateFeature(ctx, workboardFeatureFixture())
	if err != nil {
		t.Fatalf("CreateFeature: %v", err)
	}
	realArtifact, err := artifactSvc.Publish(ctx, artifact.PublishInput{
		FeatureID:            realFeature.ID,
		WorkspaceID:          "demo-ws",
		Version:              "v0.1",
		Status:               artifact.StatusDraft,
		RequestType:          artifact.RequestTypeChangeRequest,
		ImpactLevel:          artifact.ImpactLevelLow,
		ArtifactCompleteness: artifact.ArtifactCompletenessFull,
		CreatedBy:            "human",
		Documents: []artifact.DocumentInput{
			{Path: "spec.md", Role: string(artifact.RoleSpec), Content: []byte("# Real spec\n")},
		},
	})
	if err != nil {
		t.Fatalf("Publish real artifact: %v", err)
	}

	if _, err := seeding.RemoveDemo(ctx, seeding.RemoveDemoDeps{
		DB:           gdb,
		Artifacts:    artifactSvc,
		Integrations: integrationRepo,
	}); err != nil {
		t.Fatalf("RemoveDemo: %v", err)
	}

	features, err := workBoardRepo.ListFeatures(ctx)
	if err != nil {
		t.Fatalf("ListFeatures: %v", err)
	}
	if len(features) != 1 || features[0].Key != "REAL-1" {
		t.Fatalf("real feature should survive removal, got %+v", features)
	}
	if _, err := artifactSvc.Get(ctx, realArtifact.ID); err != nil {
		t.Fatalf("real artifact should survive removal: %v", err)
	}
}
