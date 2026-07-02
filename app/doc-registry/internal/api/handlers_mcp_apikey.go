package api

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"github.com/specgate/doc-registry/internal/mcp"
	"github.com/specgate/doc-registry/internal/settings"
)

// McpApiKeyResponse carries the effective MCP access token and how to use it.
// Like /mcp/info this surface is internal-only ("no auth; do not expose
// publicly") — the token is as private as the network boundary.
type McpApiKeyResponse struct {
	Body struct {
		APIKey        string `json:"api_key" doc:"Effective MCP access token (Bearer for the streamable MCP endpoint)."`
		Path          string `json:"path" doc:"Server-relative streamable MCP path; compose with the registry base URL."`
		EnvOverridden bool   `json:"env_overridden" doc:"True when MCP_API_KEY env drives the token; rotation via settings has no effect."`
	}
}

// mcpStreamPath is the server-relative path of the Bearer-gated streamable MCP
// endpoint (see router.go Mount("/mcp/stream", …)). Coding agents point at
// <registry base URL> + this path.
const mcpStreamPath = "/mcp/stream"

// GetMcpApiKey returns the effective MCP access token so the UI can display a
// copy-paste connect snippet. Internal-only (network-boundary trust).
func (h *Handlers) GetMcpApiKey(ctx context.Context, in *struct{}) (*McpApiKeyResponse, error) {
	_ = ctx
	_ = in
	out := &McpApiKeyResponse{}
	out.Body.APIKey = mcp.ResolveAPIKey(h.Settings)
	out.Body.Path = mcpStreamPath
	out.Body.EnvOverridden = mcp.EnvOverridesAPIKey()
	return out, nil
}

// RotateMcpApiKey mints a fresh token and persists it to settings, invalidating
// the previous one. Note: when MCP_API_KEY env is set it still wins as the
// effective token (ResolveAPIKey precedence) until it is removed from the env.
func (h *Handlers) RotateMcpApiKey(ctx context.Context, in *struct{}) (*McpApiKeyResponse, error) {
	_ = ctx
	_ = in
	if err := h.requireService(h.Settings, "settings"); err != nil {
		return nil, err
	}
	token, err := settings.GenerateToken()
	if err != nil {
		return nil, huma.Error500InternalServerError("generate mcp token", err)
	}
	if err := h.Settings.Update(map[string]string{settings.KeyMCPAPIKey: token}); err != nil {
		return nil, huma.Error500InternalServerError("persist mcp token", err)
	}
	out := &McpApiKeyResponse{}
	out.Body.APIKey = token
	out.Body.Path = mcpStreamPath
	out.Body.EnvOverridden = mcp.EnvOverridesAPIKey()
	return out, nil
}
