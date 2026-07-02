package tools

import (
	"context"
	"encoding/json"

	"github.com/specgate/doc-registry/internal/governanceops"
	"github.com/specgate/doc-registry/internal/governanceprofile"
)

// Type aliases for backward compat with wiring-layer code that referenced
// the old local types. All field shapes are identical to the service types.
type DocumentInputDTO = governanceops.DocumentInput
type SpecgatePublishInput = governanceops.PublishArtifactInput
type SpecgatePublishResult = governanceops.PublishArtifactResult

// SpecgateResolveProfileInput and SpecgateResolvedProfile are kept as aliases
// for callers that import them directly.
type SpecgateResolveProfileInput = governanceprofile.ResolveInput
type SpecgateResolvedProfile = governanceprofile.ResolvedProfile

// SpecgatePublisher, FeatureUpserter, SpecgateProfileResolver are kept as type
// aliases for wiring-layer backward compat.
type SpecgatePublisher = governanceops.ArtifactWriter
type FeatureUpserter = governanceops.FeatureUpserter
type SpecgateProfileResolver = governanceops.ProfileResolver

// NewSpecgatePublishHandler returns the handler for the specgate_publish MCP tool.
// Delegates to Service.PublishArtifact and marshals the result to JSON.
func NewSpecgatePublishHandler(svc *governanceops.Service) func(ctx context.Context, in SpecgatePublishInput) (string, error) {
	return func(ctx context.Context, in SpecgatePublishInput) (string, error) {
		result, err := svc.PublishArtifact(ctx, in)
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

// SpecgateWhoamiResult is the MCP tool output for specgate_whoami.
type SpecgateWhoamiResult struct {
	OK        bool   `json:"ok"`
	Service   string `json:"service"`
	Tools     int    `json:"tools"`
	Resources int    `json:"resources"`
	Version   string `json:"version,omitempty"`
}

// NewSpecgateWhoamiHandler returns the handler for the specgate_whoami MCP tool.
// It confirms the IDE is connected to SpecGate and reports tool/resource counts.
func NewSpecgateWhoamiHandler(tools, resources int, version string) func(ctx context.Context, in struct{}) (string, error) {
	return func(ctx context.Context, _ struct{}) (string, error) {
		out, err := json.Marshal(SpecgateWhoamiResult{
			OK:        true,
			Service:   "specgate",
			Tools:     tools,
			Resources: resources,
			Version:   version,
		})
		if err != nil {
			return "", err
		}
		return string(out), nil
	}
}
