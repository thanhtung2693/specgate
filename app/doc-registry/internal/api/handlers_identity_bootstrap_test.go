package api

import (
	"context"
	"testing"

	"github.com/rs/zerolog"

	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/governanceops"
	"github.com/specgate/doc-registry/internal/governanceprofile"
	"github.com/specgate/doc-registry/internal/seeding"
	"github.com/specgate/doc-registry/internal/skills"
	storagedb "github.com/specgate/doc-registry/internal/storage/db"
	"github.com/specgate/doc-registry/internal/workspace"
)

func TestBootstrapIdentitySeedsSkillsForFirstArtifactPublish(t *testing.T) {
	gdb := newTestGormDB(t)
	identityRepo := storagedb.NewIdentityRepository(gdb)
	skillsSvc := skills.NewService(storagedb.NewSkillRepository(gdb))
	settingsSvc := testIntegrationSettingsService(t, gdb)
	logger := zerolog.Nop()
	h := &Handlers{
		Identity: identityRepo,
		Skills:   skillsSvc,
		SeedWorkspaceSkills: func(ctx context.Context, workspaceID string) error {
			_, err := seeding.SeedSkills(
				skills.WithWorkspace(ctx, workspaceID),
				skillsSvc,
				settingsSvc,
				&logger,
			)
			return err
		},
	}

	var bootstrap identityBootstrapInput
	bootstrap.Body.WorkspaceName = "Fresh workspace"
	bootstrap.Body.DisplayName = "First User"
	bootstrap.Body.Username = "first-user"
	selection, err := h.BootstrapIdentity(context.Background(), &bootstrap)
	if err != nil {
		t.Fatal(err)
	}

	repo := newMemArtifactRepo()
	store := newMemArtifactStore()
	artifactSvc := artifact.NewService(repo, store, func(featureID, version, path string) string {
		return "artifacts/" + featureID + "/" + version + "/" + path
	})
	publishSvc := &governanceops.Service{
		ArtifactWriter:  &pipelineArtifactWriter{svc: artifactSvc},
		FeatureUpserter: newPipelineFeatureUpserter(),
		ProfileResolver: pipelineProfileResolver{},
		Skills:          skillsSvc,
	}
	ctx := workspace.WithID(context.Background(), selection.Body.Workspace.ID)
	_, err = publishSvc.PublishArtifact(ctx, governanceops.PublishArtifactInput{
		FeatureKey:  "first-publish",
		RequestType: "new_feature",
		ImpactLevel: "high",
		ImpactDeclaration: governanceprofile.ImpactDeclaration{
			ProtectedDomainsStatus: governanceprofile.TriYes,
			ProtectedDomains:       []string{"payments"},
		},
		Documents: []governanceops.DocumentInput{{
			Path:    "spec.md",
			Role:    "spec",
			Content: "# First governed artifact",
		}},
	})
	if err != nil {
		t.Fatalf("first publish after bootstrap: %v", err)
	}
}
