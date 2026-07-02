package settings

import (
	"context"
	"sync"
	"testing"
	"time"
)

type fakeRepo struct {
	mu    sync.Mutex
	store map[string]Setting
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{store: make(map[string]Setting)}
}

func (r *fakeRepo) GetAll(_ context.Context) ([]Setting, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Setting, 0, len(r.store))
	for _, s := range r.store {
		out = append(out, s)
	}
	return out, nil
}

func (r *fakeRepo) Get(_ context.Context, key string) (*Setting, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.store[key]
	if !ok {
		return nil, nil
	}
	return &s, nil
}

func (r *fakeRepo) PutBatch(_ context.Context, items []Setting) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range items {
		s.UpdatedAt = time.Now()
		r.store[s.Key] = s
	}
	return nil
}

func (r *fakeRepo) Count(_ context.Context) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return int64(len(r.store)), nil
}

func testCrypto(t *testing.T) *Crypto {
	t.Helper()
	c, err := NewCrypto(testHexKey(t))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestService_GetDefaultsOnEmptyDB(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	svc, err := NewServiceWithTTL(repo, testCrypto(t), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Stop()

	if svc.Get(KeyMCPAddr) != ":8081" {
		t.Fatalf("expected default :8081, got %q", svc.Get(KeyMCPAddr))
	}
	if svc.GetBool(KeyMCPEnabled) {
		t.Fatal("default mcp.enabled should be false")
	}
	if svc.GetInt(KeyBudgetMaxRepoCalls, 0) != 50 {
		t.Fatalf("expected 50, got %d", svc.GetInt(KeyBudgetMaxRepoCalls, 0))
	}
}

func TestService_UpdateAndGet(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	svc, err := NewServiceWithTTL(repo, testCrypto(t), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Stop()

	err = svc.Update(map[string]string{
		KeyMCPEnabled: "true",
		KeyMCPAddr:    ":9090",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !svc.GetBool(KeyMCPEnabled) {
		t.Fatal("expected mcp.enabled=true after update")
	}
	if svc.Get(KeyMCPAddr) != ":9090" {
		t.Fatalf("expected :9090, got %q", svc.Get(KeyMCPAddr))
	}
}

func TestService_UpdateEncryptsSensitive(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	crypto := testCrypto(t)
	svc, err := NewServiceWithTTL(repo, crypto, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Stop()

	err = svc.Update(map[string]string{
		KeyMCPAPIKey: "secret-api-key",
	})
	if err != nil {
		t.Fatal(err)
	}

	raw, _ := repo.Get(context.Background(), KeyMCPAPIKey)
	if raw == nil {
		t.Fatal("expected stored setting")
	}
	if raw.Value == "secret-api-key" {
		t.Fatal("value should be encrypted, not plaintext")
	}
	if !raw.Encrypted {
		t.Fatal("encrypted flag should be true")
	}

	if svc.Get(KeyMCPAPIKey) != "secret-api-key" {
		t.Fatalf("expected decrypted value, got %q", svc.Get(KeyMCPAPIKey))
	}
}

func TestService_GetAllMasksSensitive(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	svc, err := NewServiceWithTTL(repo, testCrypto(t), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Stop()

	_ = svc.Update(map[string]string{
		KeyMCPAPIKey: "secret-api-key",
		KeyMCPAddr:   ":9090",
	})

	all := svc.GetAll()
	if all[KeyMCPAPIKey] != MaskedValue {
		t.Fatalf("expected masked, got %q", all[KeyMCPAPIKey])
	}
	if all[KeyMCPAddr] != ":9090" {
		t.Fatalf("expected :9090, got %q", all[KeyMCPAddr])
	}
}

func TestService_UpdateSkipsMaskedValues(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	svc, err := NewServiceWithTTL(repo, testCrypto(t), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Stop()

	_ = svc.Update(map[string]string{KeyMCPAPIKey: "real-key"})
	err = svc.Update(map[string]string{
		KeyMCPAPIKey: MaskedValue,
		KeyMCPAddr:   ":7777",
	})
	if err != nil {
		t.Fatal(err)
	}
	if svc.Get(KeyMCPAPIKey) != "real-key" {
		t.Fatalf("expected unchanged secret, got %q", svc.Get(KeyMCPAPIKey))
	}
	if svc.Get(KeyMCPAddr) != ":7777" {
		t.Fatalf("expected :7777, got %q", svc.Get(KeyMCPAddr))
	}
}

func TestService_UpdateRejectsUnknownKey(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	svc, err := NewServiceWithTTL(repo, testCrypto(t), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Stop()

	err = svc.Update(map[string]string{"bad.key": "value"})
	if err == nil {
		t.Fatal("expected error for unknown key")
	}
}

func TestService_IgnoresUnknownRowsFromStorage(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	repo.store["old.model"] = Setting{Key: "old.model", Value: "old-model"}
	repo.store["governance.model_mini"] = Setting{Key: "governance.model_mini", Value: "old-mini-model"}
	repo.store[KeyGovernanceModel] = Setting{Key: KeyGovernanceModel, Value: "gemini-3.1-flash-lite"}

	svc, err := NewServiceWithTTL(repo, testCrypto(t), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Stop()

	if got := svc.Get("old.model"); got != "" {
		t.Fatalf("unknown old.model should not resolve, got %q", got)
	}
	if got := svc.Get("governance.model_mini"); got != "" {
		t.Fatalf("node-tier governance.model_mini should not resolve, got %q", got)
	}
	all := svc.GetAll()
	if _, ok := all["old.model"]; ok {
		t.Fatal("unknown old.model should not be exposed by GetAll")
	}
	if _, ok := all["governance.model_mini"]; ok {
		t.Fatal("node-tier governance.model_mini should not be exposed by GetAll")
	}
	if all[KeyGovernanceModel] != "gemini-3.1-flash-lite" {
		t.Fatalf("canonical model missing from GetAll: %#v", all)
	}
}

func TestService_ConfigHashChanges(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	svc, err := NewServiceWithTTL(repo, testCrypto(t), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Stop()

	h1 := svc.ConfigHash()
	_ = svc.Update(map[string]string{KeyMCPAddr: ":6666"})
	h2 := svc.ConfigHash()
	if h1 == h2 {
		t.Fatal("hash should change after update")
	}
}

func TestService_UpdateSensitiveKeyEncryptsAtRest(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	crypto := testCrypto(t)
	svc, err := NewServiceWithTTL(repo, crypto, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Stop()

	value := "sk-secret-openai-key"
	if err := svc.Update(map[string]string{KeyOpenAIAPIKey: value}); err != nil {
		t.Fatal(err)
	}

	raw, _ := repo.Get(context.Background(), KeyOpenAIAPIKey)
	if raw == nil {
		t.Fatal("expected stored openai.api_key setting")
	}
	if raw.Value == value {
		t.Fatal("openai.api_key should be encrypted at rest")
	}
	if !raw.Encrypted {
		t.Fatal("openai.api_key encrypted flag should be true")
	}
	if svc.Get(KeyOpenAIAPIKey) != value {
		t.Fatalf("expected decrypted openai.api_key, got %q", svc.Get(KeyOpenAIAPIKey))
	}
}
