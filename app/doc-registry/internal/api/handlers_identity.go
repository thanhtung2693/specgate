package api

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"github.com/specgate/doc-registry/internal/identity"
)

type identityBootstrapInput struct {
	Body struct {
		WorkspaceName string `json:"workspace_name" required:"true"`
		DisplayName   string `json:"display_name" required:"true"`
		Username      string `json:"username" required:"true"`
		Email         string `json:"email,omitempty"`
	}
}

type identitySelectionOutput struct {
	Body identity.Selection `json:"body"`
}

type identityUsersOutput struct {
	Body struct {
		Items []identity.User `json:"items"`
	} `json:"body"`
}

type identityWorkspacesOutput struct {
	Body struct {
		Items []identity.Workspace `json:"items"`
	} `json:"body"`
}

type identityUserInput struct {
	ID string `path:"id"`
}

type identityUserOutput struct {
	Body identity.User `json:"body"`
}

type identityWorkspaceInput struct {
	ID string `path:"id"`
}

type identityWorkspaceOutput struct {
	Body identity.Workspace `json:"body"`
}

func (h *Handlers) BootstrapIdentity(ctx context.Context, in *identityBootstrapInput) (*identitySelectionOutput, error) {
	if err := h.requireService(h.Identity, "identity"); err != nil {
		return nil, err
	}
	selection, err := h.Identity.Bootstrap(ctx, identity.BootstrapInput{
		WorkspaceName: in.Body.WorkspaceName,
		DisplayName:   in.Body.DisplayName,
		Username:      in.Body.Username,
		Email:         in.Body.Email,
	})
	if err != nil {
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}
	return &identitySelectionOutput{Body: *selection}, nil
}

func (h *Handlers) ListIdentityUsers(ctx context.Context, _ *struct{}) (*identityUsersOutput, error) {
	if err := h.requireService(h.Identity, "identity"); err != nil {
		return nil, err
	}
	users, err := h.Identity.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	out := &identityUsersOutput{}
	out.Body.Items = users
	return out, nil
}

func (h *Handlers) GetIdentityUser(ctx context.Context, in *identityUserInput) (*identityUserOutput, error) {
	if err := h.requireService(h.Identity, "identity"); err != nil {
		return nil, err
	}
	user, err := h.Identity.GetUser(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, huma.Error404NotFound("user not found")
	}
	return &identityUserOutput{Body: *user}, nil
}

func (h *Handlers) ListIdentityWorkspaces(ctx context.Context, _ *struct{}) (*identityWorkspacesOutput, error) {
	if err := h.requireService(h.Identity, "identity"); err != nil {
		return nil, err
	}
	workspaces, err := h.Identity.ListWorkspaces(ctx)
	if err != nil {
		return nil, err
	}
	out := &identityWorkspacesOutput{}
	out.Body.Items = workspaces
	return out, nil
}

func (h *Handlers) GetIdentityWorkspace(ctx context.Context, in *identityWorkspaceInput) (*identityWorkspaceOutput, error) {
	if err := h.requireService(h.Identity, "identity"); err != nil {
		return nil, err
	}
	workspace, err := h.Identity.GetWorkspace(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	if workspace == nil {
		return nil, huma.Error404NotFound("workspace not found")
	}
	return &identityWorkspaceOutput{Body: *workspace}, nil
}
