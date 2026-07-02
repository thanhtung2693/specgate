package tools

import (
	"context"
	"encoding/json"

	"github.com/specgate/doc-registry/internal/governanceops"
)

// LLMGatesRunner, DeliveryReviewTrigger, and QuickWorkItemCreator are kept as
// type aliases for wiring-layer backward compat. The implementations delegate
// through governanceops.AgentsRunner.
type LLMGatesRunner interface {
	RunLLMGates(ctx context.Context, changeRequestID string) (map[string]any, error)
}

type DeliveryReviewTrigger interface {
	ReviewDelivery(ctx context.Context, changeRequestID string) (map[string]any, error)
}

type QuickWorkItemCreator interface {
	CreateQuickWorkItem(ctx context.Context, title, description, issueURL, issueKey, featureKey, featureName string, acceptanceCriteria []string, createdBy string, workspaceID string) (map[string]any, error)
}

// RunLLMGatesInput is the MCP tool input for run_llm_gates.
type RunLLMGatesInput struct {
	ChangeRequestID string `json:"change_request_id"`
}

// TriggerDeliveryReviewInput is the MCP tool input for trigger_delivery_review.
type TriggerDeliveryReviewInput struct {
	ChangeRequestID string `json:"change_request_id"`
}

// NewRunLLMGatesHandler returns the handler for the run_llm_gates MCP tool.
// Delegates to Service.RunLLMGates and marshals the result to JSON.
func NewRunLLMGatesHandler(svc *governanceops.Service) func(ctx context.Context, in RunLLMGatesInput) (string, error) {
	return func(ctx context.Context, in RunLLMGatesInput) (string, error) {
		result, err := svc.RunLLMGates(ctx, in.ChangeRequestID)
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

// NewTriggerDeliveryReviewHandler returns the handler for the
// trigger_delivery_review MCP tool. Delegates to Service.ReviewDelivery.
func NewTriggerDeliveryReviewHandler(svc *governanceops.Service) func(ctx context.Context, in TriggerDeliveryReviewInput) (string, error) {
	return func(ctx context.Context, in TriggerDeliveryReviewInput) (string, error) {
		result, err := svc.ReviewDelivery(ctx, in.ChangeRequestID)
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
