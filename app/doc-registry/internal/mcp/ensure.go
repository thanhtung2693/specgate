package mcp

import (
	"os"
	"strings"

	"github.com/specgate/doc-registry/internal/settings"
)

// SettingsStore is the subset of the settings service EnsureAPIKey needs: read
// the current value and persist a generated one.
type SettingsStore interface {
	Get(key string) string
	Update(values map[string]string) error
}

// EnvOverridesAPIKey reports whether MCP_API_KEY is set in the environment. When
// true, the env value is the effective token (ResolveAPIKey prefers it) and
// rotating the settings value has no effect.
func EnvOverridesAPIKey() bool {
	_, ok := os.LookupEnv(EnvMCPAPIKey)
	return ok
}

// EnsureAPIKey mints and persists an mcp.api_key into settings when none is
// configured, so the streamable MCP endpoint is gated by a real token without
// any manual setup. It is idempotent and a no-op when:
//   - MCP_API_KEY is set in the environment (env drives the key; 12-factor), or
//   - a mcp.api_key setting already exists.
//
// Returns generated=true only when it minted and stored a new token.
func EnsureAPIKey(store SettingsStore) (generated bool, err error) {
	env, envSet := os.LookupEnv(EnvMCPAPIKey)
	return ensureAPIKey(store, env, envSet)
}

// ensureAPIKey is the env-free core so the env precedence is unit-testable.
func ensureAPIKey(store SettingsStore, _ string, envSet bool) (bool, error) {
	if envSet {
		return false, nil
	}
	if store == nil {
		return false, nil
	}
	if strings.TrimSpace(store.Get(settings.KeyMCPAPIKey)) != "" {
		return false, nil
	}
	token, err := settings.GenerateToken()
	if err != nil {
		return false, err
	}
	if err := store.Update(map[string]string{settings.KeyMCPAPIKey: token}); err != nil {
		return false, err
	}
	return true, nil
}
