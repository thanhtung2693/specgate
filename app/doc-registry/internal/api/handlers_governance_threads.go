package api

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/specgate/doc-registry/internal/governancethreads"
)

const governanceThreadTitleMaxLen = 160
const governanceThreadPreviewMaxLen = 280

func (h *Handlers) ListGovernanceThreads(
	ctx context.Context,
	in *ListGovernanceThreadsInput,
) (*ListGovernanceThreadsOutput, error) {
	if h.GovernanceThreads == nil {
		return nil, huma.Error503ServiceUnavailable("governance-chat threads store unavailable")
	}
	workspaceID, err := requireWorkspaceID(in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	items, total, err := h.GovernanceThreads.List(ctx, governancethreads.ListFilter{
		WorkspaceID:     workspaceID,
		Limit:           in.Limit,
		Offset:          in.Offset,
		IncludeArchived: in.All,
	})
	if err != nil {
		return nil, huma.Error500InternalServerError("list governance-chat threads", err)
	}
	out := &ListGovernanceThreadsOutput{}
	out.Body.Total = total
	out.Body.Items = make([]GovernanceThreadDTO, 0, len(items))
	for i := range items {
		out.Body.Items = append(out.Body.Items, governanceThreadDTO(&items[i]))
	}
	return out, nil
}

func (h *Handlers) UpsertGovernanceThread(
	ctx context.Context,
	in *UpsertGovernanceThreadInput,
) (*UpsertGovernanceThreadOutput, error) {
	if h.GovernanceThreads == nil {
		return nil, huma.Error503ServiceUnavailable("governance-chat threads store unavailable")
	}
	threadID := strings.TrimSpace(in.ThreadID)
	if threadID == "" {
		return nil, huma.Error400BadRequest("thread_id is required")
	}
	workspaceID, err := requireWorkspaceID(in.Body.WorkspaceID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	updatedAt := now
	if in.Body.UpdatedAt != nil && !in.Body.UpdatedAt.IsZero() {
		updatedAt = in.Body.UpdatedAt.UTC()
	}
	title := trimGovernanceThreadText(in.Body.Title, governanceThreadTitleMaxLen)
	if title == "" {
		title = "New thread"
	}
	thread, err := h.GovernanceThreads.Upsert(ctx, governancethreads.Thread{
		ThreadID:    threadID,
		WorkspaceID: workspaceID,
		Title:       title,
		Preview:     trimGovernanceThreadText(in.Body.Preview, governanceThreadPreviewMaxLen),
		Archived:    false,
		CreatedAt:   now,
		UpdatedAt:   updatedAt,
	})
	if err != nil {
		if errors.Is(err, governancethreads.ErrNotFound) {
			return nil, huma.Error404NotFound("governance-chat thread not found")
		}
		return nil, huma.Error500InternalServerError("upsert governance-chat thread", err)
	}
	return &UpsertGovernanceThreadOutput{Body: governanceThreadDTO(thread)}, nil
}

func (h *Handlers) DeleteGovernanceThread(
	ctx context.Context,
	in *DeleteGovernanceThreadInput,
) (*DeleteGovernanceThreadOutput, error) {
	if h.GovernanceThreads == nil {
		return nil, huma.Error503ServiceUnavailable("governance-chat threads store unavailable")
	}
	workspaceID, err := requireWorkspaceID(in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if err := h.GovernanceThreads.Delete(ctx, workspaceID, strings.TrimSpace(in.ThreadID), time.Now().UTC()); err != nil {
		if errors.Is(err, governancethreads.ErrNotFound) {
			return nil, huma.Error404NotFound("governance-chat thread not found")
		}
		return nil, huma.Error500InternalServerError("delete governance-chat thread", err)
	}
	return &DeleteGovernanceThreadOutput{}, nil
}

func trimGovernanceThreadText(raw string, maxLen int) string {
	clean := strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	if len(clean) <= maxLen {
		return clean
	}
	return clean[:maxLen]
}

func governanceThreadDTO(thread *governancethreads.Thread) GovernanceThreadDTO {
	return GovernanceThreadDTO{
		ThreadID:    thread.ThreadID,
		WorkspaceID: thread.WorkspaceID,
		Title:       thread.Title,
		Preview:     thread.Preview,
		Archived:    thread.Archived,
		CreatedAt:   thread.CreatedAt,
		UpdatedAt:   thread.UpdatedAt,
	}
}
