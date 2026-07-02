package api

import (
	"context"
	"testing"

	"github.com/specgate/doc-registry/internal/mcp"
	"github.com/specgate/doc-registry/internal/settings"
)

func TestGetMcpApiKey_returnsEffectiveToken(t *testing.T) {
	t.Parallel()
	if mcp.EnvOverridesAPIKey() {
		t.Skip("MCP_API_KEY env is set")
	}
	h, cleanup := testHandlersSettings(t)
	defer cleanup()

	if err := h.Settings.Update(map[string]string{settings.KeyMCPAPIKey: "tok-123"}); err != nil {
		t.Fatal(err)
	}

	out, err := h.GetMcpApiKey(context.Background(), &struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Body.APIKey != "tok-123" {
		t.Errorf("api_key = %q, want tok-123", out.Body.APIKey)
	}
	if out.Body.Path != "/mcp/stream" {
		t.Errorf("path = %q, want /mcp/stream", out.Body.Path)
	}
	if out.Body.EnvOverridden {
		t.Error("env_overridden should be false when env is unset")
	}
}

func TestRotateMcpApiKey_changesAndPersists(t *testing.T) {
	t.Parallel()
	if mcp.EnvOverridesAPIKey() {
		t.Skip("MCP_API_KEY env is set")
	}
	h, cleanup := testHandlersSettings(t)
	defer cleanup()

	if err := h.Settings.Update(map[string]string{settings.KeyMCPAPIKey: "old-token"}); err != nil {
		t.Fatal(err)
	}

	out, err := h.RotateMcpApiKey(context.Background(), &struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Body.APIKey == "" || out.Body.APIKey == "old-token" {
		t.Errorf("rotated key = %q, expected a new token", out.Body.APIKey)
	}
	if len(out.Body.APIKey) != 64 {
		t.Errorf("rotated key len = %d, want 64", len(out.Body.APIKey))
	}
	if got := h.Settings.Get(settings.KeyMCPAPIKey); got != out.Body.APIKey {
		t.Errorf("persisted key %q != returned %q", got, out.Body.APIKey)
	}
}
