package seeding

import (
	"context"

	"github.com/specgate/doc-registry/internal/workboard"
)

// WorkBoardSeedStore is the narrow subset of workboard.Store that demo seeding needs.
type WorkBoardSeedStore interface {
	ListFeatures(context.Context) ([]workboard.Feature, error)
	CreateFeature(context.Context, workboard.Feature) (*workboard.Feature, error)
	SetFeatureCanonicalArtifact(context.Context, string, string, string) (*workboard.Feature, error)
	ListChangeRequests(context.Context, bool) ([]workboard.ChangeRequest, error)
	CreateChangeRequest(context.Context, workboard.ChangeRequest) (*workboard.ChangeRequest, error)
	SetChangeRequestAttribution(context.Context, string, string, string) (*workboard.ChangeRequest, error)
	RefreshGateRuns(context.Context, workboard.RefreshGateRunsInput) ([]workboard.GateRun, error)
}
