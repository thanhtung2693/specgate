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

	if svc.Get(KeyGovernanceDefaultThinkingLevel) != "low" {
		t.Fatalf("expected default thinking level low, got %q", svc.Get(KeyGovernanceDefaultThinkingLevel))
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

	err = svc.Update(map[string]string{KeyGovernanceModel: "gpt-5.4"})
	if err != nil {
		t.Fatal(err)
	}
	if svc.Get(KeyGovernanceModel) != "gpt-5.4" {
		t.Fatalf("expected gpt-5.4, got %q", svc.Get(KeyGovernanceModel))
	}
}

func TestService_UpdateAcceptsProviderOwnedGovernanceModelID(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	svc, err := NewServiceWithTTL(repo, testCrypto(t), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Stop()

	err = svc.Update(map[string]string{
		KeyGovernanceModelProvider: "anthropic",
		KeyGovernanceModel:         "provider-owned-model-id",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := svc.Get(KeyGovernanceModelProvider); got != "anthropic" {
		t.Fatalf("provider = %q, want anthropic", got)
	}
	if got := svc.Get(KeyGovernanceModel); got != "provider-owned-model-id" {
		t.Fatalf("model = %q, want provider-owned-model-id", got)
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

	err = svc.Update(map[string]string{KeyOpenAIAPIKey: "secret-api-key"})
	if err != nil {
		t.Fatal(err)
	}

	raw, _ := repo.Get(context.Background(), KeyOpenAIAPIKey)
	if raw == nil {
		t.Fatal("expected stored setting")
	}
	if raw.Value == "secret-api-key" {
		t.Fatal("value should be encrypted, not plaintext")
	}
	if !raw.Encrypted {
		t.Fatal("encrypted flag should be true")
	}

	if svc.Get(KeyOpenAIAPIKey) != "secret-api-key" {
		t.Fatalf("expected decrypted value, got %q", svc.Get(KeyOpenAIAPIKey))
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

	_ = svc.Update(map[string]string{KeyOpenAIAPIKey: "secret-api-key", KeyGovernanceModel: "gpt-5.4"})

	all := svc.GetAll()
	if all[KeyOpenAIAPIKey] != MaskedValue {
		t.Fatalf("expected masked, got %q", all[KeyOpenAIAPIKey])
	}
	if all[KeyGovernanceModel] != "gpt-5.4" {
		t.Fatalf("expected gpt-5.4, got %q", all[KeyGovernanceModel])
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

	_ = svc.Update(map[string]string{KeyOpenAIAPIKey: "real-key"})
	err = svc.Update(map[string]string{
		KeyOpenAIAPIKey:    MaskedValue,
		KeyGovernanceModel: "gpt-5.4",
	})
	if err != nil {
		t.Fatal(err)
	}
	if svc.Get(KeyOpenAIAPIKey) != "real-key" {
		t.Fatalf("expected unchanged secret, got %q", svc.Get(KeyOpenAIAPIKey))
	}
	if svc.Get(KeyGovernanceModel) != "gpt-5.4" {
		t.Fatalf("expected gpt-5.4, got %q", svc.Get(KeyGovernanceModel))
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

func TestService_UpdateSensitiveKeyEncryptsAtRest(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	crypto := testCrypto(t)
	svc, err := NewServiceWithTTL(repo, crypto, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Stop()

	value := "openai-test-key"
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
