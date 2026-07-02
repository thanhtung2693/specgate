package settings

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Setting is the GORM model for the settings table.
type Setting struct {
	Key       string    `gorm:"column:key;primaryKey"`
	Value     string    `gorm:"column:value"`
	Encrypted bool      `gorm:"column:encrypted"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

func (Setting) TableName() string { return "settings" }

// Note: GitLab repo-reading providers are derived from connected GitLab
// integrations (see internal/integrations/), not from settings keys.
const (
	KeyMCPEnabled              = "mcp.enabled"
	KeyMCPAddr                 = "mcp.addr"
	KeyMCPAPIKey               = "mcp.api_key"
	KeyBudgetMaxRepoCalls      = "mcp.budget_max_repo_calls"
	KeyBudgetMaxBytesReturned  = "mcp.budget_max_bytes_returned"
	KeyGovernanceModelProvider = "governance.model_provider"
	KeyGovernanceModel         = "governance.model"
	KeySpeechToTextProvider    = "speech_to_text.provider"
	KeySpeechToTextModel       = "speech_to_text.model"
	KeyOpenAIAPIKey            = "openai.api_key"
	KeyGoogleAPIKey            = "google.api_key"
	KeyAnthropicAPIKey         = "anthropic.api_key"
	KeyOpenRouterAPIKey        = "openrouter.api_key"
	// Knowledge embedding model (Settings → Model). Empty provider/model means
	// embeddings are disabled and the server boots without an embedding key; the
	// API key reuses the per-provider *.api_key settings above.
	KeyEmbeddingModelProvider = "embedding.model_provider"
	KeyEmbeddingModel         = "embedding.model"
	// Policy thresholds surfaced in the UI "General" tab. The agent reads the
	// confidence thresholds; the UI reads the day-count thresholds.
	KeyGateConfidenceThreshold      = "governance.gate_confidence_threshold"
	KeyLifecycleConfidenceThreshold = "governance.lifecycle_confidence_threshold"
	KeyFeatureFreshnessSLADays      = "governance.feature_freshness_sla_days"
	KeyArtifactStaleDays            = "governance.artifact_stale_days"
	// KeyGovernanceFilesTTLDays controls how long idle ready governance_files rows are
	// kept before the in-process purger deletes them and their S3 objects.
	KeyGovernanceFilesTTLDays = "governancefiles.ttl_days"
	// Advanced operational knobs (agent-side) — no other config path today.
	KeyPublishRetryAttempts    = "governance.publish_retry_attempts"
	KeyPublishRetryBaseSeconds = "governance.publish_retry_base_seconds"
	KeyRegistryTimeoutSeconds  = "governance.registry_timeout_seconds"
	// KeyGovernanceAutoFeatureSummary controls whether the agent automatically
	// regenerates the persisted feature Overview summary when the canonical
	// artifact changes. "true" / "false".
	KeyGovernanceAutoFeatureSummary = "governance.auto_feature_summary"
	// KeyGovernanceAutoArchiveOnDeliveryPass controls whether a passing
	// delivery_review gate run archives the ChangeRequest automatically.
	KeyGovernanceAutoArchiveOnDeliveryPass = "governance.auto_archive_on_delivery_pass"
	// KeyGovernanceDefaultThinkingLevel sets the default reasoning budget for the
	// main governance model: "low" | "medium" | "high".
	KeyGovernanceDefaultThinkingLevel = "governance.default_thinking_level"
)

var supportedSpeechToTextProviders = map[string]struct{}{
	"openai": {},
	"google": {},
}

var supportedSpeechToTextModels = map[string]struct{}{
	"gpt-4o-transcribe": {},
	"chirp_3":           {},
}

var governanceSupportedProviders = map[string]struct{}{
	"openai":       {},
	"google_genai": {},
	"anthropic":    {},
	"openrouter":   {},
}

// embeddingSupportedProviders are the providers that expose an embeddings API.
// Anthropic has no embeddings endpoint, so it is intentionally excluded.
var embeddingSupportedProviders = map[string]struct{}{
	"openai":       {},
	"google_genai": {},
	"openrouter":   {},
}

var governanceSupportedModels = map[string]struct{}{
	"gpt-5.4-nano":          {},
	"gpt-5.4-mini":          {},
	"gpt-5.4":               {},
	"gpt-5.5":               {},
	"gemini-3.1-flash-lite": {},
	"gemini-3-flash":        {},
	"gemini-3.1-pro":        {},
	"claude-opus-4-7":       {},
	"claude-sonnet-4-6":     {},
}

func governanceProviderSupportsModel(provider string, model string) bool {
	switch provider {
	case "openai":
		return strings.HasPrefix(model, "gpt-")
	case "google_genai":
		return strings.HasPrefix(model, "gemini-")
	case "anthropic":
		return strings.HasPrefix(model, "claude-")
	case "openrouter":
		// OpenRouter routes a large catalog addressed as vendor/model slugs
		// (e.g. deepseek/deepseek-v4-flash), so it has no single family prefix.
		return strings.Contains(model, "/")
	default:
		return false
	}
}

// AllKeys is the ordered list of valid setting keys.
var AllKeys = []string{
	KeyMCPEnabled,
	KeyMCPAddr,
	KeyMCPAPIKey,
	KeyBudgetMaxRepoCalls,
	KeyBudgetMaxBytesReturned,
	KeyGovernanceModelProvider,
	KeyGovernanceModel,
	KeySpeechToTextProvider,
	KeySpeechToTextModel,
	KeyOpenAIAPIKey,
	KeyGoogleAPIKey,
	KeyAnthropicAPIKey,
	KeyOpenRouterAPIKey,
	KeyEmbeddingModelProvider,
	KeyEmbeddingModel,
	KeyGateConfidenceThreshold,
	KeyLifecycleConfidenceThreshold,
	KeyFeatureFreshnessSLADays,
	KeyArtifactStaleDays,
	KeyPublishRetryAttempts,
	KeyPublishRetryBaseSeconds,
	KeyRegistryTimeoutSeconds,
	KeyGovernanceAutoFeatureSummary,
	KeyGovernanceAutoArchiveOnDeliveryPass,
	KeyGovernanceDefaultThinkingLevel,
	KeyGovernanceFilesTTLDays,
}

var Defaults = map[string]string{
	KeyMCPEnabled:              "false",
	KeyMCPAddr:                 ":8081",
	KeyMCPAPIKey:               "",
	KeyBudgetMaxRepoCalls:      "50",
	KeyBudgetMaxBytesReturned:  "524288",
	KeyGovernanceModelProvider: "google_genai",
	KeyGovernanceModel:         "gemini-3.1-flash-lite",
	KeySpeechToTextProvider:    "openai",
	KeySpeechToTextModel:       "gpt-4o-transcribe",
	KeyOpenAIAPIKey:            "",
	KeyGoogleAPIKey:            "",
	KeyAnthropicAPIKey:         "",
	KeyOpenRouterAPIKey:        "",
	// Embeddings are opt-in: blank provider/model means knowledge embeddings are
	// disabled and the server boots without an embedding key. Set both (plus the
	// provider's api_key) in Settings → Model to enable knowledge search/upload.
	KeyEmbeddingModelProvider: "",
	KeyEmbeddingModel:         "",
	// Defaults mirror the in-code constants these settings replace.
	KeyGateConfidenceThreshold:             "0.7",
	KeyLifecycleConfidenceThreshold:        "0.7",
	KeyFeatureFreshnessSLADays:             "7",
	KeyArtifactStaleDays:                   "5",
	KeyPublishRetryAttempts:                "3",
	KeyPublishRetryBaseSeconds:             "0.5",
	KeyRegistryTimeoutSeconds:              "30",
	KeyGovernanceAutoFeatureSummary:        "true",
	KeyGovernanceAutoArchiveOnDeliveryPass: "false",
	KeyGovernanceDefaultThinkingLevel:      "low",
	KeyGovernanceFilesTTLDays:              "90",
}

var sensitiveKeys = map[string]bool{
	KeyMCPAPIKey:        true,
	KeyOpenAIAPIKey:     true,
	KeyGoogleAPIKey:     true,
	KeyAnthropicAPIKey:  true,
	KeyOpenRouterAPIKey: true,
}

// IsSensitive returns true if the setting value should be encrypted at rest
// and masked in GET responses.
func IsSensitive(key string) bool {
	return sensitiveKeys[key]
}

func IsValidKey(key string) bool {
	for _, k := range AllKeys {
		if k == key {
			return true
		}
	}
	return false
}

// MaskedValue is the placeholder shown for sensitive settings in GET responses.
const MaskedValue = "***"

// validateValue checks type/range constraints per key.
func validateValue(key, value string) error {
	switch key {
	case KeyMCPEnabled:
		if _, err := strconv.ParseBool(value); err != nil {
			return fmt.Errorf("must be true or false")
		}
	case KeyGovernanceAutoFeatureSummary, KeyGovernanceAutoArchiveOnDeliveryPass:
		if value != "true" && value != "false" {
			return fmt.Errorf("must be true or false")
		}
	case KeyBudgetMaxRepoCalls, KeyBudgetMaxBytesReturned:
		n, err := strconv.Atoi(value)
		if err != nil || n <= 0 {
			return fmt.Errorf("must be a positive integer")
		}
	case KeyMCPAddr:
		if value != "" && !strings.Contains(value, ":") {
			return fmt.Errorf("must be host:port format (e.g. :8081)")
		}
	case KeyGovernanceModelProvider:
		if _, ok := governanceSupportedProviders[value]; !ok {
			return fmt.Errorf("must be one of [openai, google_genai, anthropic, openrouter]")
		}
	case KeyGovernanceModel:
		// This field validator is provider-blind (it never sees the sibling
		// *_provider value), so it accepts either a curated first-party model
		// or an OpenRouter vendor/model slug. The provider+model pairing is
		// the real gate (governanceProviderSupportsModel).
		_, curated := governanceSupportedModels[value]
		if !curated && !strings.Contains(value, "/") {
			return fmt.Errorf("must be a supported governance model")
		}
	case KeyEmbeddingModelProvider:
		// Blank is allowed (embeddings disabled); otherwise it must be an
		// embeddings-capable provider. Pairs with KeyEmbeddingModel + the
		// provider's api_key.
		if value != "" {
			if _, ok := embeddingSupportedProviders[value]; !ok {
				return fmt.Errorf("must be one of [openai, google_genai, openrouter]")
			}
		}
	case KeyEmbeddingModel:
		// Provider-blind and free-form: blank means disabled, otherwise the UI
		// constrains the choice to embedding-capable models per provider.
	case KeySpeechToTextProvider:
		if _, ok := supportedSpeechToTextProviders[value]; !ok {
			return fmt.Errorf("must be one of [openai, google]")
		}
	case KeySpeechToTextModel:
		if _, ok := supportedSpeechToTextModels[value]; !ok {
			return fmt.Errorf("must be a supported speech-to-text model")
		}
	case KeyGateConfidenceThreshold, KeyLifecycleConfidenceThreshold:
		f, err := strconv.ParseFloat(value, 64)
		if err != nil || f < 0 || f > 1 {
			return fmt.Errorf("must be a number between 0 and 1")
		}
	case KeyFeatureFreshnessSLADays, KeyArtifactStaleDays, KeyGovernanceFilesTTLDays:
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > 3650 {
			return fmt.Errorf("must be an integer number of days between 1 and 3650")
		}
	case KeyPublishRetryAttempts:
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > 10 {
			return fmt.Errorf("must be an integer between 1 and 10")
		}
	case KeyPublishRetryBaseSeconds, KeyRegistryTimeoutSeconds:
		f, err := strconv.ParseFloat(value, 64)
		if err != nil || f <= 0 || f > 600 {
			return fmt.Errorf("must be a positive number of seconds (max 600)")
		}
	case KeyGovernanceDefaultThinkingLevel:
		if value != "low" && value != "medium" && value != "high" {
			return fmt.Errorf("must be one of [low, medium, high]")
		}
	}
	return nil
}
