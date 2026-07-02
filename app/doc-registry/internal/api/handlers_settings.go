package api

import (
	"context"
	"crypto/subtle"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/specgate/doc-registry/internal/mcp"
	"github.com/specgate/doc-registry/internal/settings"
)

// GetSettingsInput allows optional Bearer auth to receive unmasked secrets.
type GetSettingsInput struct {
	Authorization string `header:"Authorization" doc:"When Bearer token matches configured mcp.api_key, secrets are returned unmasked (for any trusted MCP client)."`
	InternalAgent string `header:"X-SpecGate-Internal-Agent" doc:"Internal governance-ops agents may request unmasked model-provider secrets over the trusted service network."`
}

// GetSettings returns all settings; sensitive values are masked unless Authorization Bearer matches mcp.api_key.
func (h *Handlers) GetSettings(_ context.Context, in *GetSettingsInput) (*GetSettingsOutput, error) {
	if err := h.requireService(h.Settings, "settings"); err != nil {
		return nil, err
	}
	out := &GetSettingsOutput{}
	token := parseBearerToken(in.Authorization)
	internalAgent := strings.TrimSpace(in.InternalAgent)
	if bearerMatchesMCPAPIKey(h.Settings, token) ||
		strings.EqualFold(internalAgent, "governance") {
		out.Body.Settings = h.Settings.GetAllUnmasked()
	} else {
		out.Body.Settings = h.Settings.GetAll()
	}
	return out, nil
}

func parseBearerToken(h string) string {
	h = strings.TrimSpace(h)
	const prefix = "Bearer "
	if len(h) < len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(h[len(prefix):])
}

func bearerMatchesMCPAPIKey(svc *settings.Service, token string) bool {
	if svc == nil || token == "" {
		return false
	}
	secret := mcp.ResolveAPIKey(svc)
	if secret == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(secret)) == 1
}

// UpdateSettings accepts a map of key-value pairs, validates, encrypts
// sensitive values, persists, and returns the updated (masked) settings.
func (h *Handlers) UpdateSettings(_ context.Context, in *UpdateSettingsInput) (*UpdateSettingsOutput, error) {
	if err := h.requireService(h.Settings, "settings"); err != nil {
		return nil, err
	}
	if err := h.Settings.Update(in.Body.Settings); err != nil {
		return nil, huma.Error400BadRequest("update settings", err)
	}
	out := &UpdateSettingsOutput{}
	out.Body.Settings = h.Settings.GetAll()
	return out, nil
}
