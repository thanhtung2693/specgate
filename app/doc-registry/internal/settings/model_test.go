package settings

import (
	"testing"
)

func TestIsSensitive(t *testing.T) {
	t.Parallel()
	if !IsSensitive(KeyMCPAPIKey) {
		t.Fatal("mcp.api_key should be sensitive")
	}
	if !IsSensitive(KeyOpenAIAPIKey) {
		t.Fatal("openai.api_key should be sensitive")
	}
	if IsSensitive(KeyMCPAddr) {
		t.Fatal("mcp.addr should not be sensitive")
	}
}

func TestIsValidKey(t *testing.T) {
	t.Parallel()
	if !IsValidKey(KeyMCPEnabled) {
		t.Fatal("mcp.enabled should be valid")
	}
	if !IsValidKey(KeyGovernanceModelProvider) {
		t.Fatal("governance.model_provider should be valid")
	}
	if !IsValidKey(KeyGovernanceModel) {
		t.Fatal("governance.model should be valid")
	}
	if !IsValidKey(KeySpeechToTextProvider) {
		t.Fatal("speech_to_text.provider should be valid")
	}
	if !IsValidKey(KeySpeechToTextModel) {
		t.Fatal("speech_to_text.model should be valid")
	}
	if IsValidKey("unknown.key") {
		t.Fatal("unknown.key should be invalid")
	}
}

func TestGovernanceSettingKeyNames(t *testing.T) {
	t.Parallel()

	if KeyGovernanceModel != "governance.model" {
		t.Fatalf("KeyGovernanceModel = %q, want governance.model", KeyGovernanceModel)
	}
	if KeyGateConfidenceThreshold != "governance.gate_confidence_threshold" {
		t.Fatalf("KeyGateConfidenceThreshold = %q, want governance.gate_confidence_threshold", KeyGateConfidenceThreshold)
	}
	if KeyGovernanceFilesTTLDays != "governancefiles.ttl_days" {
		t.Fatalf("KeyGovernanceFilesTTLDays = %q, want governancefiles.ttl_days", KeyGovernanceFilesTTLDays)
	}

	if IsValidKey("governance.not_a_key") {
		t.Fatal("unknown key should be invalid")
	}
}

func TestDefaults_CoverAllKeys(t *testing.T) {
	t.Parallel()
	for _, k := range AllKeys {
		if _, ok := Defaults[k]; !ok {
			t.Fatalf("key %q missing from Defaults", k)
		}
	}
}

func TestGovernanceFilesTTLDays_DefaultAndValidation(t *testing.T) {
	t.Parallel()
	if Defaults[KeyGovernanceFilesTTLDays] != "90" {
		t.Fatalf("default = %q, want 90", Defaults[KeyGovernanceFilesTTLDays])
	}
	for _, ok := range []string{"1", "90", "3650"} {
		if err := validateValue(KeyGovernanceFilesTTLDays, ok); err != nil {
			t.Fatalf("%q should be valid: %v", ok, err)
		}
	}
	for _, bad := range []string{"0", "-1", "abc", "", "4000"} {
		if err := validateValue(KeyGovernanceFilesTTLDays, bad); err == nil {
			t.Fatalf("%q should be rejected", bad)
		}
	}
}

func TestValidateSpeechToTextSettings(t *testing.T) {
	t.Parallel()
	if err := validateValue(KeySpeechToTextProvider, "openai"); err != nil {
		t.Fatalf("openai provider should be valid: %v", err)
	}
	if err := validateValue(KeySpeechToTextProvider, "google"); err != nil {
		t.Fatalf("google provider should be valid: %v", err)
	}
	if err := validateValue(KeySpeechToTextProvider, "anthropic"); err == nil {
		t.Fatal("anthropic provider should be rejected until Anthropic exposes a speech-to-text model")
	}
	if err := validateValue(KeySpeechToTextModel, "gpt-4o-transcribe"); err != nil {
		t.Fatalf("OpenAI transcription model should be valid: %v", err)
	}
	if err := validateValue(KeySpeechToTextModel, "chirp_3"); err != nil {
		t.Fatalf("Google Chirp 3 model should be valid: %v", err)
	}
	if err := validateValue(KeySpeechToTextModel, "claude-sonnet-4-6"); err == nil {
		t.Fatal("Claude text model should not be accepted as speech-to-text")
	}
}

func TestValidateEmbeddingModelSettings(t *testing.T) {
	t.Parallel()
	// Empty provider/model = embeddings disabled; must be accepted.
	if err := validateValue(KeyEmbeddingModelProvider, ""); err != nil {
		t.Fatalf("empty embedding provider (disabled) should be valid: %v", err)
	}
	if err := validateValue(KeyEmbeddingModel, ""); err != nil {
		t.Fatalf("empty embedding model (disabled) should be valid: %v", err)
	}
	for _, p := range []string{"openai", "google_genai", "openrouter"} {
		if err := validateValue(KeyEmbeddingModelProvider, p); err != nil {
			t.Fatalf("embedding provider %q should be valid: %v", p, err)
		}
	}
	// Anthropic has no embeddings API.
	if err := validateValue(KeyEmbeddingModelProvider, "anthropic"); err == nil {
		t.Fatal("anthropic should be rejected as an embedding provider")
	}
	// Model is free-form (UI-constrained) — any non-empty value is accepted.
	if err := validateValue(KeyEmbeddingModel, "text-embedding-3-small"); err != nil {
		t.Fatalf("embedding model should be valid: %v", err)
	}
}

func TestValidateGovernanceModelSettings(t *testing.T) {
	t.Parallel()
	if err := validateValue(KeyGovernanceModelProvider, "google_genai"); err != nil {
		t.Fatalf("provider should be valid: %v", err)
	}
	if err := validateValue(KeyGovernanceModel, "gemini-3.1-flash-lite"); err != nil {
		t.Fatalf("model should be valid: %v", err)
	}
	if err := validateValue(KeyGovernanceModel, "gpt-5-nano"); err == nil {
		t.Fatal("unsupported model should be rejected")
	}
	if err := validateValue(KeyGovernanceModel, "claude-sonnet-4-6"); err != nil {
		t.Fatalf("Anthropic model should be valid: %v", err)
	}
}

func TestOpenRouterProviderSupport(t *testing.T) {
	t.Parallel()
	if !IsSensitive(KeyOpenRouterAPIKey) {
		t.Fatal("openrouter.api_key should be sensitive")
	}
	if err := validateValue(KeyGovernanceModelProvider, "openrouter"); err != nil {
		t.Fatalf("openrouter provider should be valid: %v", err)
	}
	// OpenRouter models are vendor/model slugs, not gpt-*/gemini-*/claude-*.
	if err := validateValue(KeyGovernanceModel, "deepseek/deepseek-v4-flash"); err != nil {
		t.Fatalf("openrouter slug model should be valid: %v", err)
	}
	if !governanceProviderSupportsModel("openrouter", "z-ai/glm-5.1") {
		t.Fatal("openrouter should accept a vendor/model slug")
	}
	if governanceProviderSupportsModel("openrouter", "noslughere") {
		t.Fatal("openrouter should reject a model without a vendor/ prefix")
	}
}

func TestValidatePolicyAndOperationalSettings(t *testing.T) {
	t.Parallel()

	for _, ok := range []string{"true", "false"} {
		if err := validateValue(KeyGovernanceAutoArchiveOnDeliveryPass, ok); err != nil {
			t.Fatalf("auto archive=%q should be valid: %v", ok, err)
		}
	}
	for _, bad := range []string{"yes", "0", ""} {
		if err := validateValue(KeyGovernanceAutoArchiveOnDeliveryPass, bad); err == nil {
			t.Fatalf("auto archive=%q should be rejected", bad)
		}
	}

	// Confidence thresholds: float in [0,1].
	for _, key := range []string{KeyGateConfidenceThreshold} {
		for _, ok := range []string{"0", "0.7", "1"} {
			if err := validateValue(key, ok); err != nil {
				t.Fatalf("%s=%q should be valid: %v", key, ok, err)
			}
		}
		for _, bad := range []string{"-0.1", "1.5", "abc", ""} {
			if err := validateValue(key, bad); err == nil {
				t.Fatalf("%s=%q should be rejected", key, bad)
			}
		}
	}

	// Day-count thresholds: positive int within a sane range.
	for _, key := range []string{KeyFeatureFreshnessSLADays, KeyArtifactStaleDays, KeyGovernanceFilesTTLDays} {
		for _, ok := range []string{"1", "7", "365"} {
			if err := validateValue(key, ok); err != nil {
				t.Fatalf("%s=%q should be valid: %v", key, ok, err)
			}
		}
		for _, bad := range []string{"0", "-1", "abc", "4000"} {
			if err := validateValue(key, bad); err == nil {
				t.Fatalf("%s=%q should be rejected", key, bad)
			}
		}
	}

	// Retry attempts: int 1..10.
	for _, ok := range []string{"1", "3", "10"} {
		if err := validateValue(KeyPublishRetryAttempts, ok); err != nil {
			t.Fatalf("attempts=%q should be valid: %v", ok, err)
		}
	}
	for _, bad := range []string{"0", "-1", "11", "abc"} {
		if err := validateValue(KeyPublishRetryAttempts, bad); err == nil {
			t.Fatalf("attempts=%q should be rejected", bad)
		}
	}

	// Positive-float seconds (base backoff + registry timeout).
	for _, key := range []string{KeyPublishRetryBaseSeconds, KeyRegistryTimeoutSeconds} {
		for _, ok := range []string{"0.5", "30"} {
			if err := validateValue(key, ok); err != nil {
				t.Fatalf("%s=%q should be valid: %v", key, ok, err)
			}
		}
		for _, bad := range []string{"0", "-1", "abc"} {
			if err := validateValue(key, bad); err == nil {
				t.Fatalf("%s=%q should be rejected", key, bad)
			}
		}
	}
}
