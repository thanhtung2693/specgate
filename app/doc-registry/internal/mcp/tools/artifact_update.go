package tools

import (
	"context"
	"encoding/json"

	"github.com/specgate/doc-registry/internal/governanceops"
)

const CodingAgentUpdateSourceKind = "coding_agent_update"

// ArtifactProposalReader is re-exported from governanceops for wiring-layer compat.
type ArtifactProposalReader = governanceops.ContextPackArtifactReader

// DraftArtifactUpdateInput is the MCP tool input for draft_artifact_update.
// Alias to the service type for JSON schema consistency.
type DraftArtifactUpdateInput = governanceops.DraftArtifactUpdateInput

// NewDraftArtifactUpdateHandler returns the handler for the draft_artifact_update
// MCP tool. Delegates to Service.DraftArtifactUpdate and marshals the result to JSON.
func NewDraftArtifactUpdateHandler(svc *governanceops.Service) func(context.Context, DraftArtifactUpdateInput) (string, error) {
	return func(ctx context.Context, in DraftArtifactUpdateInput) (string, error) {
		result, err := svc.DraftArtifactUpdate(ctx, in)
		if err != nil {
			return "", err
		}
		out, err := json.Marshal(result)
		if err != nil {
			return "", err
		}
		return string(out), nil
	}
}
