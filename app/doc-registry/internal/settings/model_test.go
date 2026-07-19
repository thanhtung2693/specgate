package settings

import (
	"testing"
)

func TestIsSensitive(t *testing.T) {
	t.Parallel()
	if !IsSensitive(KeyOpenAIAPIKey) {
		t.Fatal("openai.api_key should be sensitive")
	}
}

func TestIsValidKey(t *testing.T) {
	t.Parallel()
	if !IsValidKey(KeyGovernanceModelProvider) {
		t.Fatal("governance.model_provider should be valid")
	}
	if !IsValidKey(KeyGovernanceModel) {
		t.Fatal("governance.model should be valid")
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
	if KeyArtifactRetentionSweepEnabled != "retention.artifact_sweep_enabled" {
		t.Fatalf("KeyArtifactRetentionSweepEnabled = %q, want retention.artifact_sweep_enabled", KeyArtifactRetentionSweepEnabled)
	}
	if err := validateValue(KeyArtifactRetentionSweepEnabled, "true"); err != nil {
		t.Fatalf("validateValue(true): %v", err)
	}
	if err := validateValue(KeyArtifactRetentionSweepEnabled, "sometimes"); err == nil {
		t.Fatal("validateValue should reject non-boolean values")
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

func TestGovernanceDefaultThinkingLevel_DefaultAndValidation(t *testing.T) {
	t.Parallel()
	if Defaults[KeyGovernanceDefaultThinkingLevel] != "low" {
		t.Fatalf("default = %q, want low", Defaults[KeyGovernanceDefaultThinkingLevel])
	}
	for _, ok := range []string{"low", "medium", "high"} {
		if err := validateValue(KeyGovernanceDefaultThinkingLevel, ok); err != nil {
			t.Fatalf("%q should be valid: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "max", "LOW"} {
		if err := validateValue(KeyGovernanceDefaultThinkingLevel, bad); err == nil {
			t.Fatalf("%q should be rejected", bad)
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
	if err := validateValue(KeyGovernanceModel, "provider-owned-model-id"); err != nil {
		t.Fatalf("provider-owned model id should be valid: %v", err)
	}
	if err := validateValue(KeyGovernanceModel, "claude-sonnet-4-6"); err != nil {
		t.Fatalf("Anthropic model should be valid: %v", err)
	}
}

func TestValidateGovernanceModelEnabled(t *testing.T) {
	t.Parallel()
	if err := validateValue(KeyGovernanceModelEnabled, "false"); err != nil {
		t.Fatalf("false should disable the governance model: %v", err)
	}
	if err := validateValue(KeyGovernanceModelEnabled, "sometimes"); err == nil {
		t.Fatal("invalid model_enabled value should be rejected")
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
	// Model IDs belong to the provider and are not classified by SpecGate.
	if err := validateValue(KeyGovernanceModel, "deepseek/deepseek-v4-flash"); err != nil {
		t.Fatalf("openrouter slug model should be valid: %v", err)
	}
	if err := validateValue(KeyGovernanceModel, "noslughere"); err != nil {
		t.Fatalf("provider-owned model id should be valid: %v", err)
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

	// Positive-float registry timeout.
	for _, ok := range []string{"0.5", "30"} {
		if err := validateValue(KeyRegistryTimeoutSeconds, ok); err != nil {
			t.Fatalf("%s=%q should be valid: %v", KeyRegistryTimeoutSeconds, ok, err)
		}
	}
	for _, bad := range []string{"0", "-1", "abc"} {
		if err := validateValue(KeyRegistryTimeoutSeconds, bad); err == nil {
			t.Fatalf("%s=%q should be rejected", KeyRegistryTimeoutSeconds, bad)
		}
	}
}
