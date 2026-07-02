package mcp

import "testing"

type stubSettings struct {
	apiKey  string
	enabled bool
}

func (s stubSettings) Get(key string) string  { return s.apiKey }
func (s stubSettings) GetBool(string) bool    { return s.enabled }
func (s stubSettings) GetInt(string, int) int { return 0 }
func (s stubSettings) ConfigHash() string     { return "stub" }

func TestResolveAPIKey_EnvWinsOverSettings(t *testing.T) {
	t.Setenv(EnvMCPAPIKey, "from-env")
	got := ResolveAPIKey(stubSettings{apiKey: "from-settings"})
	if got != "from-env" {
		t.Fatalf("want from-env, got %q", got)
	}
}

func TestResolveAPIKey_EmptyEnvDisablesMCP(t *testing.T) {
	// Explicit empty env is a valid "I want MCP off" signal and must beat settings.
	t.Setenv(EnvMCPAPIKey, "")
	got := ResolveAPIKey(stubSettings{apiKey: "from-settings"})
	if got != "" {
		t.Fatalf("want empty (env override), got %q", got)
	}
}

func TestResolveAPIKey_FallsBackToSettings(t *testing.T) {
	// With no env var set, the settings value wins. Can't use t.Setenv to
	// unset, so rely on Lookup returning ok=false for a name we never set.
	got := ResolveAPIKey(stubSettings{apiKey: "from-settings"})
	if got != "from-settings" {
		t.Fatalf("want from-settings, got %q", got)
	}
}

func TestResolveEnabled_EnvWins(t *testing.T) {
	t.Setenv(EnvMCPEnabled, "true")
	if !ResolveEnabled(stubSettings{enabled: false}) {
		t.Fatal("env=true should win over settings=false")
	}
}

func TestResolveEnabled_InvalidEnvFallsBack(t *testing.T) {
	t.Setenv(EnvMCPEnabled, "not-a-bool")
	if !ResolveEnabled(stubSettings{enabled: true}) {
		t.Fatal("unparseable env must fall back to settings value")
	}
}

func TestResolveEnabled_NilProvider(t *testing.T) {
	if ResolveEnabled(nil) {
		t.Fatal("nil provider + no env should return false")
	}
	if ResolveAPIKey(nil) != "" {
		t.Fatal("nil provider + no env should return empty string")
	}
}
