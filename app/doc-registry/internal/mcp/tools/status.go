package tools

import (
	"context"
	"encoding/json"

	"github.com/specgate/doc-registry/internal/governanceops"
)

// GovernanceStatusInput is the (empty) MCP tool input for get_governance_status.
type GovernanceStatusInput struct{}

// NewGetGovernanceStatusHandler returns the handler for the get_governance_status
// MCP tool. Delegates to governanceops.Service.
func NewGetGovernanceStatusHandler(svc *governanceops.Service) func(context.Context, GovernanceStatusInput) (string, error) {
	return func(ctx context.Context, _ GovernanceStatusInput) (string, error) {
		out, err := svc.GovernanceStatus(ctx, governanceops.GovernanceStatusInput{})
		if err != nil {
			return "", err
		}
		body, err := json.Marshal(out)
		if err != nil {
			return "", err
		}
		return string(body), nil
	}
}
