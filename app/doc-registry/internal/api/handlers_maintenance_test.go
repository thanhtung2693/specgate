package api

import (
	"context"
	"errors"
	"testing"
)

func TestMaintenanceCleanupRunsCleaner(t *testing.T) {
	t.Parallel()
	h := &Handlers{
		MaintenanceCleanupFn: func(_ context.Context, _ string) (MaintenanceCleanupCounts, error) {
			return MaintenanceCleanupCounts{
				ExpiredArtifactsDeleted:       3,
				ReferencedSkipped:             1,
				DemoFeaturesDeleted:           3,
				DemoChangeRequestsDeleted:     5,
				DemoArtifactsDeleted:          12,
				ArchivedChangeRequestsDeleted: 2,
			}, nil
		},
	}
	out, err := h.MaintenanceCleanup(context.Background(), &maintenanceWorkspaceInput{WorkspaceID: "workspace-test"})
	if err != nil {
		t.Fatalf("MaintenanceCleanup: %v", err)
	}
	if out.Body.ExpiredArtifactsDeleted != 3 || out.Body.ArchivedChangeRequestsDeleted != 2 || out.Body.DemoArtifactsDeleted != 12 {
		t.Fatalf("unexpected counts: %+v", out.Body)
	}
}

func TestMaintenanceCleanupRequiresWorkspace(t *testing.T) {
	t.Parallel()
	h := &Handlers{
		MaintenanceCleanupFn: func(_ context.Context, _ string) (MaintenanceCleanupCounts, error) {
			return MaintenanceCleanupCounts{}, nil
		},
	}
	if _, err := h.MaintenanceCleanup(context.Background(), &maintenanceWorkspaceInput{}); err == nil {
		t.Fatal("expected cleanup without workspace_id to be rejected")
	}
}

func TestMaintenanceCleanupUnconfiguredIsUnavailable(t *testing.T) {
	t.Parallel()
	h := &Handlers{}
	if _, err := h.MaintenanceCleanup(context.Background(), &maintenanceWorkspaceInput{}); err == nil {
		t.Fatal("expected error when cleanup is not configured")
	}
}

func TestMaintenanceCleanupPropagatesError(t *testing.T) {
	t.Parallel()
	h := &Handlers{
		MaintenanceCleanupFn: func(_ context.Context, _ string) (MaintenanceCleanupCounts, error) {
			return MaintenanceCleanupCounts{}, errors.New("boom")
		},
	}
	if _, err := h.MaintenanceCleanup(context.Background(), &maintenanceWorkspaceInput{WorkspaceID: "workspace-test"}); err == nil {
		t.Fatal("expected error to propagate")
	}
}

func TestMaintenanceDemoRemoveRunsRemover(t *testing.T) {
	t.Parallel()
	h := &Handlers{
		MaintenanceDemoRemoveFn: func(_ context.Context, _ string) (MaintenanceDemoRemoveCounts, error) {
			return MaintenanceDemoRemoveCounts{
				FeaturesDeleted:       3,
				ChangeRequestsDeleted: 5,
				ArtifactsDeleted:      12,
				IntegrationsDeleted:   2,
				KnowledgeDeleted:      1,
				FeedbackDeleted:       4,
			}, nil
		},
	}
	out, err := h.MaintenanceDemoRemove(context.Background(), &maintenanceWorkspaceInput{WorkspaceID: "workspace-test"})
	if err != nil {
		t.Fatalf("MaintenanceDemoRemove: %v", err)
	}
	if out.Body.FeaturesDeleted != 3 || out.Body.ArtifactsDeleted != 12 || out.Body.IntegrationsDeleted != 2 {
		t.Fatalf("unexpected counts: %+v", out.Body)
	}
}

func TestMaintenanceDemoRemoveUnconfiguredIsUnavailable(t *testing.T) {
	t.Parallel()
	h := &Handlers{}
	if _, err := h.MaintenanceDemoRemove(context.Background(), &maintenanceWorkspaceInput{}); err == nil {
		t.Fatal("expected error when demo removal is not configured")
	}
}

func TestMaintenanceDemoRemovePropagatesError(t *testing.T) {
	t.Parallel()
	h := &Handlers{
		MaintenanceDemoRemoveFn: func(_ context.Context, _ string) (MaintenanceDemoRemoveCounts, error) {
			return MaintenanceDemoRemoveCounts{}, errors.New("boom")
		},
	}
	if _, err := h.MaintenanceDemoRemove(context.Background(), &maintenanceWorkspaceInput{WorkspaceID: "workspace-test"}); err == nil {
		t.Fatal("expected error to propagate")
	}
}
