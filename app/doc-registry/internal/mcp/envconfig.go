package mcp

import (
	"os"
	"strconv"
	"strings"

	"github.com/specgate/doc-registry/internal/settings"
)

// Env var names that, when set, override the corresponding settings-service
// values. Env takes precedence so 12-factor deployments can drive MCP config
// entirely from secrets/config managers without bootstrapping the settings DB.
const (
	EnvMCPAPIKey  = "MCP_API_KEY"
	EnvMCPEnabled = "MCP_ENABLED"
)

// ResolveAPIKey returns the effective MCP API key. MCP_API_KEY in the
// environment wins over settings when explicitly set (including explicit
// empty string, which disables MCP). Falls back to the settings value when
// the env var is unset so existing deployments keep working.
func ResolveAPIKey(sp SettingsProvider) string {
	if v, ok := os.LookupEnv(EnvMCPAPIKey); ok {
		return strings.TrimSpace(v)
	}
	if sp == nil {
		return ""
	}
	return sp.Get(settings.KeyMCPAPIKey)
}

// ResolveEnabled mirrors ResolveAPIKey for mcp.enabled. Any value parseable
// as a bool by strconv.ParseBool (1/true/on/yes … 0/false/off/no) overrides
// the settings value; otherwise the settings value wins.
func ResolveEnabled(sp SettingsProvider) bool {
	if v, ok := os.LookupEnv(EnvMCPEnabled); ok {
		if b, err := strconv.ParseBool(strings.TrimSpace(v)); err == nil {
			return b
		}
	}
	if sp == nil {
		return false
	}
	return sp.GetBool(settings.KeyMCPEnabled)
}
