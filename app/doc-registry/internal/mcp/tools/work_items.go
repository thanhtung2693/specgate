package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/specgate/doc-registry/internal/governanceops"
)

// ResolveWorkItemInput is the MCP tool input for resolve_work_item.
type ResolveWorkItemInput struct {
	Provider string `json:"provider"`
	IssueKey string `json:"issue_key,omitempty"`
	IssueURL string `json:"issue_url,omitempty"`
}

// ListWorkItemsInput is the MCP tool input for list_work_items.
type ListWorkItemsInput = governanceops.ListWorkItemsInput

// GetWorkStatusInput is the MCP tool input for get_work_status.
type GetWorkStatusInput struct {
	ChangeRequestID string `json:"change_request_id"`
}

// GetGateHistoryInput is the MCP tool input for get_gate_history.
type GetGateHistoryInput = governanceops.GateHistoryInput

// ReadDeliveryReviewInput is the MCP tool input for read_delivery_review.
type ReadDeliveryReviewInput = governanceops.DeliveryStatusInput

// ReadClarificationInput is the MCP tool input for read_clarification.
// Alias to the service type for JSON schema consistency.
type ReadClarificationInput = governanceops.ClarificationsInput

// CreateQuickWorkItemInput is the MCP tool input for create_quick_work_item.
// Alias to the service type for JSON schema consistency.
type CreateQuickWorkItemInput = governanceops.CreateQuickWorkItemInput

// WorkItemTrackerReader is the tracker-link surface the MCP resolve tool needs.
// Kept for backward-compat with wiring; resolved via governanceops.Service now.
type WorkItemTrackerReader = governanceops.TrackerReader

// NewResolveWorkItemHandler returns a thin adapter over governanceops.Service.ResolveWorkRef.
func NewResolveWorkItemHandler(svc *governanceops.Service) func(context.Context, ResolveWorkItemInput) (string, error) {
	return func(ctx context.Context, in ResolveWorkItemInput) (string, error) {
		provider := strings.ToLower(strings.TrimSpace(in.Provider))
		ref := strings.TrimSpace(in.IssueURL)
		if ref == "" {
			ref = strings.TrimSpace(in.IssueKey)
		}
		if provider == "" {
			return "", errors.New("provider is required")
		}
		if ref == "" {
			return "", errors.New("issue_key or issue_url is required")
		}
		out, err := svc.ResolveWorkRef(ctx, governanceops.ResolveWorkRefInput{Ref: ref, Provider: provider})
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

// NewListWorkItemsHandler returns a thin adapter over governanceops.Service.ListWorkItems.
func NewListWorkItemsHandler(svc *governanceops.Service) func(context.Context, ListWorkItemsInput) (string, error) {
	return func(ctx context.Context, in ListWorkItemsInput) (string, error) {
		out, err := svc.ListWorkItems(ctx, in)
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

// NewGetWorkStatusHandler returns a thin adapter over governanceops.Service.WorkStatus.
func NewGetWorkStatusHandler(svc *governanceops.Service) func(context.Context, GetWorkStatusInput) (string, error) {
	return func(ctx context.Context, in GetWorkStatusInput) (string, error) {
		id := strings.TrimSpace(in.ChangeRequestID)
		if id == "" {
			return "", errors.New("change_request_id is required")
		}
		out, err := svc.WorkStatus(ctx, governanceops.ResolveWorkRefInput{Ref: id})
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

// NewGetGateHistoryHandler returns a thin adapter over governanceops.Service.GateHistory.
func NewGetGateHistoryHandler(svc *governanceops.Service) func(context.Context, GetGateHistoryInput) (string, error) {
	return func(ctx context.Context, in GetGateHistoryInput) (string, error) {
		out, err := svc.GateHistory(ctx, in)
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

// NewReadDeliveryReviewHandler returns a thin adapter over governanceops.Service.DeliveryStatus.
func NewReadDeliveryReviewHandler(svc *governanceops.Service) func(context.Context, ReadDeliveryReviewInput) (string, error) {
	return func(ctx context.Context, in ReadDeliveryReviewInput) (string, error) {
		out, err := svc.DeliveryStatus(ctx, in)
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

// NewReadClarificationHandler returns the handler for the read_clarification MCP tool.
// Delegates to Service.Clarifications and marshals the result to JSON.
func NewReadClarificationHandler(svc *governanceops.Service) func(context.Context, ReadClarificationInput) (string, error) {
	return func(ctx context.Context, in ReadClarificationInput) (string, error) {
		result, err := svc.Clarifications(ctx, in)
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

// NewCreateQuickWorkItemHandler returns the handler for the create_quick_work_item
// MCP tool. Delegates to Service.CreateQuickWorkItem and marshals the result to JSON.
func NewCreateQuickWorkItemHandler(svc *governanceops.Service) func(context.Context, CreateQuickWorkItemInput) (string, error) {
	return func(ctx context.Context, in CreateQuickWorkItemInput) (string, error) {
		result, err := svc.CreateQuickWorkItem(ctx, in)
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
