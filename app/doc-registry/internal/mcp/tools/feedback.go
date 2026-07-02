package tools

import (
	"context"
	"encoding/json"

	"github.com/specgate/doc-registry/internal/governanceops"
)

// FeedbackStore is re-exported from governanceops for wiring-layer backward compat.
type FeedbackStore = governanceops.FeedbackStore

// Type aliases so callers that reference tools.FeedbackEvidence etc. keep compiling.
type FeedbackEvidence = governanceops.FeedbackEvidence
type FeedbackCheck = governanceops.FeedbackCheck
type FeedbackCriterion = governanceops.FeedbackCriterion
type FeedbackAgent = governanceops.FeedbackAgent

// ReportImplementationFeedbackInput is the MCP tool input for
// report_implementation_feedback. Alias to the service type so the JSON schema
// stays identical and all callers keep compiling unchanged.
type ReportImplementationFeedbackInput = governanceops.ReportFeedbackInput

// NewReportImplementationFeedbackHandler returns the handler for the
// report_implementation_feedback MCP tool. It delegates to
// Service.ReportFeedback and marshals the result to JSON.
func NewReportImplementationFeedbackHandler(
	svc *governanceops.Service,
) func(context.Context, ReportImplementationFeedbackInput) (string, error) {
	return func(ctx context.Context, in ReportImplementationFeedbackInput) (string, error) {
		result, err := svc.ReportFeedback(ctx, in)
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
