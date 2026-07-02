package tools

import (
	"context"
	"encoding/json"

	"github.com/specgate/doc-registry/internal/agentsclient"
	"github.com/specgate/doc-registry/internal/governanceops"
)

// ReadinessRunner is re-exported from agentsclient for wiring-layer backward compat.
type ReadinessRunner interface {
	RunReadiness(ctx context.Context, artifactID string) (*agentsclient.Verdict, error)
}

// SpecgateCheckReadinessInput is the MCP tool input for specgate_check_readiness.
type SpecgateCheckReadinessInput struct {
	ArtifactID string `json:"artifact_id"`
}

// SpecgateCheckReadinessResult is the MCP tool output.
// Alias to the service type for JSON schema consistency.
type SpecgateCheckReadinessResult = governanceops.ReadinessResult

// NewSpecgateCheckReadinessHandler returns the handler for the
// specgate_check_readiness MCP tool. Delegates to Service.RunReadiness.
func NewSpecgateCheckReadinessHandler(svc *governanceops.Service) func(ctx context.Context, in SpecgateCheckReadinessInput) (string, error) {
	return func(ctx context.Context, in SpecgateCheckReadinessInput) (string, error) {
		result, err := svc.RunReadiness(ctx, in.ArtifactID)
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
