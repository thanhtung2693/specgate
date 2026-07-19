package client

import (
	"context"
	"net/url"
)

// BootstrapIdentity calls POST /api/v1/identity/bootstrap.
func (c *Client) BootstrapIdentity(ctx context.Context, in IdentityBootstrapInput) (*IdentitySelection, error) {
	var r IdentitySelection
	if err := c.post(ctx, "/api/v1/identity/bootstrap", in, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// ListUsers calls GET /api/v1/users.
func (c *Client) ListUsers(ctx context.Context) ([]IdentityUser, error) {
	var r struct {
		Items []IdentityUser `json:"items"`
	}
	if err := c.get(ctx, "/api/v1/users", &r); err != nil {
		return nil, err
	}
	return r.Items, nil
}

// GetUser calls GET /api/v1/users/{id}. id may be a UUID or username.
func (c *Client) GetUser(ctx context.Context, id string) (*IdentityUser, error) {
	var r IdentityUser
	if err := c.get(ctx, "/api/v1/users/"+url.PathEscape(id), &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// ListWorkspaces calls GET /api/v1/workspaces.
func (c *Client) ListWorkspaces(ctx context.Context) ([]IdentityWorkspace, error) {
	var r struct {
		Items []IdentityWorkspace `json:"items"`
	}
	if err := c.get(ctx, "/api/v1/workspaces", &r); err != nil {
		return nil, err
	}
	return r.Items, nil
}

// GetWorkspace calls GET /api/v1/workspaces/{id-or-slug}. The CLI normally uses
// slugs, while internal workflows may use the workspace UUID.
func (c *Client) GetWorkspace(ctx context.Context, id string) (*IdentityWorkspace, error) {
	var r IdentityWorkspace
	if err := c.get(ctx, "/api/v1/workspaces/"+url.PathEscape(id), &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// ListWorkspaceMembers calls GET /api/v1/workspaces/{id}/members.
func (c *Client) ListWorkspaceMembers(ctx context.Context, id, currentUserID, currentUsername string) (*WorkspaceMembersResult, error) {
	path := "/api/v1/workspaces/" + url.PathEscape(id) + "/members"
	q := url.Values{}
	if currentUserID != "" {
		q.Set("current_user_id", currentUserID)
	}
	if currentUsername != "" {
		q.Set("current_username", currentUsername)
	}
	if encoded := q.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var r WorkspaceMembersResult
	if err := c.get(ctx, path, &r); err != nil {
		return nil, err
	}
	return &r, nil
}
