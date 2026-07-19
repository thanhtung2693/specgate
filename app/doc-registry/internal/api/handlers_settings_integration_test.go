package api

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/specgate/doc-registry/internal/config"
	"github.com/specgate/doc-registry/internal/settings"
	storagedb "github.com/specgate/doc-registry/internal/storage/db"
)

func settingsTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	db := newTestGormDB(t)

	hexKey := hex.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	crypto, err := settings.NewCrypto(hexKey)
	if err != nil {
		t.Fatal(err)
	}
	repo := storagedb.NewSettingsRepository(db)
	svc, err := settings.NewServiceWithTTL(repo, crypto, time.Hour)
	if err != nil {
		t.Logf("settings init warning: %v", err)
	}
	t.Cleanup(svc.Stop)

	handlers := &Handlers{Settings: svc}
	rt := &Router{
		Handlers: handlers,
		Config: &config.Config{
			OpenAPI: config.OpenAPIConfig{Enabled: false},
		},
	}
	router := rt.Build()
	return httptest.NewServer(DevCORS(router))
}

func decodeSettingsBody(t *testing.T, raw []byte) map[string]string {
	t.Helper()
	var outer map[string]json.RawMessage
	if err := json.Unmarshal(raw, &outer); err != nil {
		t.Fatalf("unmarshal outer: %v", err)
	}
	rawSettings, ok := outer["settings"]
	if !ok {
		t.Fatalf("no settings key in %s", string(raw))
	}
	var settings map[string]string
	if err := json.Unmarshal(rawSettings, &settings); err != nil {
		t.Fatalf("unmarshal settings: %v", err)
	}
	return settings
}

func TestGetSettings_ReturnsDefaults(t *testing.T) {
	t.Parallel()
	srv := settingsTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/settings")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	settingsMap := decodeSettingsBody(t, raw)
	if settingsMap[settings.KeyGovernanceModel] != "gpt-5.4-mini" {
		t.Fatalf("expected default governance model, got %q", settingsMap[settings.KeyGovernanceModel])
	}
}

func TestPutSettings_UpdatesAndMasks(t *testing.T) {
	t.Parallel()
	srv := settingsTestServer(t)
	defer srv.Close()

	payload := map[string]any{
		"settings": map[string]string{
			settings.KeyOpenAIAPIKey:    "some-api-key",
			settings.KeyGovernanceModel: "gpt-5.4",
		},
	}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPut, srv.URL+"/settings", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d body=%s", resp.StatusCode, readSettingsBody(resp))
	}
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	body := decodeSettingsBody(t, buf.Bytes())
	if body[settings.KeyOpenAIAPIKey] != settings.MaskedValue {
		t.Fatalf("token should be masked, got %q", body[settings.KeyOpenAIAPIKey])
	}
	if body[settings.KeyGovernanceModel] != "gpt-5.4" {
		t.Fatalf("expected updated model, got %q", body[settings.KeyGovernanceModel])
	}
}

func readSettingsBody(resp *http.Response) string {
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	return buf.String()
}

func TestPutSettings_InvalidKey(t *testing.T) {
	t.Parallel()
	srv := settingsTestServer(t)
	defer srv.Close()

	payload := map[string]any{
		"settings": map[string]string{"bad.key": "value"},
	}
	b, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/settings", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", resp.StatusCode, readSettingsBody(resp))
	}
}

func TestGetSettings_InternalGovernanceReturnsUnmaskedSecrets(t *testing.T) {
	t.Parallel()
	srv := settingsTestServer(t)
	defer srv.Close()

	seed := map[string]any{
		"settings": map[string]string{
			settings.KeyOpenAIAPIKey: "openai-token-raw",
		},
	}
	b, err := json.Marshal(seed)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPut, srv.URL+"/settings", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("seed PUT status %d: %s", resp.StatusCode, readSettingsBody(resp))
	}

	resp, err = http.Get(srv.URL + "/settings")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	masked := decodeSettingsBody(t, raw)
	if masked[settings.KeyOpenAIAPIKey] != settings.MaskedValue {
		t.Fatalf("anonymous GET should mask sensitive key, got %q", masked[settings.KeyOpenAIAPIKey])
	}

	req, err = http.NewRequest(http.MethodGet, srv.URL+"/settings", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-SpecGate-Internal-Agent", "governance")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("governance GET status %d", resp.StatusCode)
	}
	raw, _ = io.ReadAll(resp.Body)
	full := decodeSettingsBody(t, raw)
	if full[settings.KeyOpenAIAPIKey] != "openai-token-raw" {
		t.Fatalf("governance GET should return raw sensitive key, got %q", full[settings.KeyOpenAIAPIKey])
	}
}
