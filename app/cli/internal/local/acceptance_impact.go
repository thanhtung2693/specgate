package local

import (
	"context"
	"fmt"
)

func artifactImpactForWorkAcceptance(ctx context.Context, q artifactQueryer, workspaceID string, work WorkItem, targetID, baseID string) (ArtifactImpact, error) {
	if work.ArtifactID == "" || (work.ArtifactID != baseID && work.ArtifactID != targetID) {
		return ArtifactImpact{}, fmt.Errorf("%w: impact pair must include the work's lead artifact", ErrVerificationInvalid)
	}
	target, err := getArtifact(ctx, q, workspaceID, targetID)
	if err != nil {
		return ArtifactImpact{}, err
	}
	base, err := getArtifact(ctx, q, workspaceID, baseID)
	if err != nil {
		return ArtifactImpact{}, err
	}
	return artifactImpactPairQuery(ctx, q, workspaceID, target, base)
}
