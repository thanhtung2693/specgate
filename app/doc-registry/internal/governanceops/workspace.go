package governanceops

import (
	"context"
	"strings"

	"github.com/specgate/doc-registry/internal/workboard"
	"github.com/specgate/doc-registry/internal/workspace"
)

func trustedWorkspace(ctx context.Context) string {
	return workspace.ID(ctx)
}

func requireChangeRequestWorkspace(ctx context.Context, cr *workboard.ChangeRequest) error {
	if cr == nil {
		return workboard.ErrNotFound
	}
	if selected := trustedWorkspace(ctx); selected != "" && strings.TrimSpace(cr.WorkspaceID) != selected {
		return workboard.ErrNotFound
	}
	return nil
}

func requireFeatureWorkspace(ctx context.Context, feature *workboard.Feature) error {
	if feature == nil {
		return workboard.ErrNotFound
	}
	if selected := trustedWorkspace(ctx); selected != "" && strings.TrimSpace(feature.WorkspaceID) != selected {
		return workboard.ErrNotFound
	}
	return nil
}
