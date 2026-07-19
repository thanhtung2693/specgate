package api

import (
	"context"
	"strings"

	"github.com/danielgtaylor/huma/v2"
)

// GetSettingsInput allows the trusted governance service to receive model keys.
type GetSettingsInput struct {
	InternalAgent string `header:"X-SpecGate-Internal-Agent" doc:"Internal governance-ops agents may request unmasked model-provider secrets over the trusted service network."`
}

// GetSettings returns masked settings except for the trusted governance service.
func (h *Handlers) GetSettings(_ context.Context, in *GetSettingsInput) (*GetSettingsOutput, error) {
	if err := h.requireService(h.Settings, "settings"); err != nil {
		return nil, err
	}
	out := &GetSettingsOutput{}
	internalAgent := strings.TrimSpace(in.InternalAgent)
	if strings.EqualFold(internalAgent, "governance") {
		out.Body.Settings = h.Settings.GetAllUnmasked()
	} else {
		out.Body.Settings = h.Settings.GetAll()
	}
	return out, nil
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
