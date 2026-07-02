package mcp

import (
	"testing"

	"github.com/specgate/doc-registry/internal/settings"
)

type fakeStore struct {
	values map[string]string
}

func (f *fakeStore) Get(key string) string { return f.values[key] }
func (f *fakeStore) Update(values map[string]string) error {
	for k, v := range values {
		f.values[k] = v
	}
	return nil
}

func TestEnsureAPIKey_generatesWhenUnset(t *testing.T) {
	t.Parallel()
	store := &fakeStore{values: map[string]string{}}

	gen, err := ensureAPIKey(store, "", false)
	if err != nil {
		t.Fatalf("ensureAPIKey: %v", err)
	}
	if !gen {
		t.Fatal("expected generated=true on empty store with no env override")
	}
	got := store.Get(settings.KeyMCPAPIKey)
	if len(got) != 64 {
		t.Fatalf("stored key = %q (len %d), want 64-char token", got, len(got))
	}

	// Idempotent: a second call is a no-op and keeps the same token.
	gen2, err := ensureAPIKey(store, "", false)
	if err != nil {
		t.Fatalf("ensureAPIKey (2nd): %v", err)
	}
	if gen2 {
		t.Fatal("expected generated=false when a key already exists")
	}
	if store.Get(settings.KeyMCPAPIKey) != got {
		t.Fatal("existing key was overwritten")
	}
}

func TestEnsureAPIKey_envOverrideIsNoop(t *testing.T) {
	t.Parallel()
	store := &fakeStore{values: map[string]string{}}

	gen, err := ensureAPIKey(store, "env-key", true)
	if err != nil {
		t.Fatalf("ensureAPIKey: %v", err)
	}
	if gen {
		t.Fatal("expected no generation when MCP_API_KEY env is set")
	}
	if store.Get(settings.KeyMCPAPIKey) != "" {
		t.Fatal("settings should be untouched when env drives the key")
	}
}
