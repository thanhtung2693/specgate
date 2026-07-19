package seeding

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/integrations"
	storagedb "github.com/specgate/doc-registry/internal/storage/db"
	"github.com/specgate/doc-registry/internal/workboard"
)

// RemoveDemoDeps are the stores RemoveDemo needs. Artifacts and integrations
// are removed through their production services so blob objects and cascades
// behave exactly like ordinary deletes. Feature and change-request rows are
// removed with SQL scoped to the fixed demo identifiers because their general
// delete paths stay deliberately disabled.
type RemoveDemoDeps struct {
	DB           *gorm.DB
	Artifacts    artifact.Service
	Integrations *storagedb.IntegrationRepository
}

type RemoveDemoResult struct {
	FeaturesDeleted       int
	ChangeRequestsDeleted int
	ArtifactsDeleted      int
	IntegrationsDeleted   int
	KnowledgeDeleted      int
	FeedbackDeleted       int
}

// RemoveDemo is the mirror of SeedDemo: it deletes exactly the rows and blob
// objects the demo seed creates for the selected workspace, and leaves
// everything else untouched. It is idempotent — running it with no demo data
// present deletes nothing and returns zero counts.
func RemoveDemo(ctx context.Context, deps RemoveDemoDeps) (RemoveDemoResult, error) {
	if deps.DB == nil || deps.Artifacts == nil || deps.Integrations == nil {
		return RemoveDemoResult{}, fmt.Errorf("seeding: demo removal dependencies are required")
	}
	workspaceID := workboard.WorkspaceID(ctx)
	if workspaceID == "" {
		return RemoveDemoResult{}, fmt.Errorf("seeding: workspace_id is required")
	}
	var featureIDs, crIDs, knowledgeIDs []string
	for _, seed := range demoFeaturesForWorkspace(workspaceID) {
		featureIDs = append(featureIDs, seed.ID)
		for _, cr := range seed.CRs {
			crIDs = append(crIDs, cr.ID)
		}
		if seed.Knowledge != nil {
			knowledgeIDs = append(knowledgeIDs, seed.Knowledge.DocumentID)
		}
	}
	integrationIDs := []string{
		demoScopedID(workspaceID, "integration:linear"),
		demoScopedID(workspaceID, "integration:gitlab"),
	}

	var result RemoveDemoResult

	// Collect demo artifacts before removing their owning workboard rows.
	var artifactIDs []string
	for _, featureID := range featureIDs {
		arts, err := deps.Artifacts.List(ctx, artifact.ListFilter{FeatureID: featureID, Limit: 500})
		if err != nil {
			return result, fmt.Errorf("removing demo: list artifacts for %s: %w", featureID, err)
		}
		for _, a := range arts {
			artifactIDs = append(artifactIDs, a.ID)
		}
	}

	exec := func(step, query string, args ...any) (int64, error) {
		res := deps.DB.WithContext(ctx).Exec(query, args...)
		if res.Error != nil {
			return 0, fmt.Errorf("removing demo: %s: %w", step, res.Error)
		}
		return res.RowsAffected, nil
	}

	// Rows without FK cascade, children first, all scoped to the workspace demo IDs.
	// Feature-, integration-, and artifact-scoped rows first; the shared
	// helper then covers everything keyed by change-request id.
	feedback, err := exec("delete feedback events",
		"DELETE FROM governance_feedback_events WHERE workspace_id = ? AND (integration_id IN ? OR change_request_id IN ? OR feature_id IN ?)",
		workspaceID, integrationIDs, crIDs, featureIDs)
	if err != nil {
		return result, err
	}
	result.FeedbackDeleted = int(feedback)

	if _, err := exec("delete feature lifecycle events", "DELETE FROM workboard_lifecycle_events WHERE workspace_id = ? AND entity_id IN ?", workspaceID, featureIDs); err != nil {
		return result, err
	}
	if err := storagedb.DeleteChangeRequestChildRows(deps.DB.WithContext(ctx), crIDs, workspaceID); err != nil {
		return result, fmt.Errorf("removing demo: delete change request children: %w", err)
	}

	crs, err := exec("delete change requests", "DELETE FROM change_requests WHERE workspace_id = ? AND id IN ?", workspaceID, crIDs)
	if err != nil {
		return result, err
	}
	result.ChangeRequestsDeleted = int(crs)

	features, err := exec("delete features", "DELETE FROM features WHERE workspace_id = ? AND id IN ?", workspaceID, featureIDs)
	if err != nil {
		return result, err
	}
	result.FeaturesDeleted = int(features)

	// The demo-owned workboard references are gone, so the production artifact
	// service can safely remove metadata and blob objects.
	for _, artifactID := range artifactIDs {
		if err := deps.Artifacts.Delete(ctx, artifactID); err != nil {
			return result, fmt.Errorf("removing demo: delete artifact %s: %w", artifactID, err)
		}
		result.ArtifactsDeleted++
	}
	if len(artifactIDs) > 0 {
		if _, err := exec("delete artifact gate runs", "DELETE FROM gate_runs WHERE workspace_id = ? AND subject_id IN ?", workspaceID, artifactIDs); err != nil {
			return result, err
		}
		if _, err := exec("delete gate tasks", "DELETE FROM gate_tasks WHERE workspace_id = ? AND artifact_id::text IN ?", workspaceID, artifactIDs); err != nil {
			return result, err
		}
	}

	if len(knowledgeIDs) > 0 {
		docs, err := exec("delete knowledge documents", "DELETE FROM documents WHERE workspace_id = ? AND document_id IN ?", workspaceID, knowledgeIDs)
		if err != nil {
			return result, err
		}
		result.KnowledgeDeleted = int(docs)
	}

	// Integrations last: the FK cascade removes demo resources plus tracker and
	// delivery links tied to the demo integrations.
	for _, id := range integrationIDs {
		err := deps.Integrations.DeleteIntegration(ctx, id)
		if errors.Is(err, integrations.ErrNotFound) {
			continue
		}
		if err != nil {
			return result, fmt.Errorf("removing demo: delete integration %s: %w", id, err)
		}
		result.IntegrationsDeleted++
	}

	return result, nil
}
