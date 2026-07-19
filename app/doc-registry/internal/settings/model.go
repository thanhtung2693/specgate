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
	KeyGovernanceModelProvider = "governance.model_provider"
	KeyGovernanceModel         = "governance.model"
	KeyGovernanceModelEnabled  = "governance.model_enabled"
	KeyOpenAIAPIKey            = "openai.api_key"
	KeyGoogleAPIKey            = "google.api_key"
	KeyAnthropicAPIKey         = "anthropic.api_key"
	KeyOpenRouterAPIKey        = "openrouter.api_key"
	// Knowledge embedding model (Settings → Models). Empty provider/model means
	// embeddings are disabled and the server boots without an embedding key; the
	// API key reuses the per-provider *.api_key settings above.
	KeyEmbeddingModelProvider = "embedding.model_provider"
	KeyEmbeddingModel         = "embedding.model"
	// Policy thresholds surfaced in the UI "General" tab. The agent reads the
	// confidence thresholds; the UI reads the day-count thresholds.
	KeyGateConfidenceThreshold = "governance.gate_confidence_threshold"
	KeyFeatureFreshnessSLADays = "governance.feature_freshness_sla_days"
	KeyArtifactStaleDays       = "governance.artifact_stale_days"
	// KeyGovernanceFilesTTLDays controls how long idle ready governance_files rows are
	// kept before the in-process purger deletes them and their backing objects.
	KeyGovernanceFilesTTLDays = "governancefiles.ttl_days"
	// KeyArtifactRetentionSweepEnabled turns the artifact retention sweeper
	// (spec §9) on or off. Read on every sweep tick, so a change via the
	// Settings UI takes effect without restart.
	KeyArtifactRetentionSweepEnabled = "retention.artifact_sweep_enabled"
	// Advanced operational knob (agent-side) with Settings as the config path.
	KeyRegistryTimeoutSeconds = "governance.registry_timeout_seconds"
	// KeyGovernanceAutoArchiveOnDeliveryPass controls whether an explicit human
	// delivery approval archives the ChangeRequest automatically.
	KeyGovernanceAutoArchiveOnDeliveryPass = "governance.auto_archive_on_delivery_pass"
	// KeyGovernanceDefaultThinkingLevel sets the default reasoning budget for the
	// server-side governance model: "low" | "medium" | "high".
	KeyGovernanceDefaultThinkingLevel = "governance.default_thinking_level"
)

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

// AllKeys is the ordered list of valid setting keys.
var AllKeys = []string{
	KeyGovernanceModelProvider,
	KeyGovernanceModel,
	KeyGovernanceModelEnabled,
	KeyOpenAIAPIKey,
	KeyGoogleAPIKey,
	KeyAnthropicAPIKey,
	KeyOpenRouterAPIKey,
	KeyEmbeddingModelProvider,
	KeyEmbeddingModel,
	KeyGateConfidenceThreshold,
	KeyFeatureFreshnessSLADays,
	KeyArtifactStaleDays,
	KeyRegistryTimeoutSeconds,
	KeyGovernanceAutoArchiveOnDeliveryPass,
	KeyGovernanceDefaultThinkingLevel,
	KeyGovernanceFilesTTLDays,
	KeyArtifactRetentionSweepEnabled,
}

var Defaults = map[string]string{
	KeyGovernanceModelProvider: "openai",
	KeyGovernanceModel:         "gpt-5.4-mini",
	KeyGovernanceModelEnabled:  "true",
	KeyOpenAIAPIKey:            "",
	KeyGoogleAPIKey:            "",
	KeyAnthropicAPIKey:         "",
	KeyOpenRouterAPIKey:        "",
	// Embeddings are opt-in: blank provider/model means knowledge embeddings are
	// disabled and the server boots without an embedding key. Set both (plus the
	// provider's api_key) in Settings → Models to enable knowledge search/upload.
	KeyEmbeddingModelProvider: "",
	KeyEmbeddingModel:         "",
	// Defaults mirror the in-code constants these settings replace.
	KeyGateConfidenceThreshold:             "0.7",
	KeyFeatureFreshnessSLADays:             "7",
	KeyArtifactStaleDays:                   "5",
	KeyRegistryTimeoutSeconds:              "30",
	KeyGovernanceAutoArchiveOnDeliveryPass: "false",
	KeyGovernanceDefaultThinkingLevel:      "low",
	KeyGovernanceFilesTTLDays:              "90",
	KeyArtifactRetentionSweepEnabled:       "false",
}

var sensitiveKeys = map[string]bool{
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
	case KeyGovernanceAutoArchiveOnDeliveryPass, KeyArtifactRetentionSweepEnabled, KeyGovernanceModelEnabled:
		if value != "true" && value != "false" {
			return fmt.Errorf("must be true or false")
		}
	case KeyGovernanceModelProvider:
		if _, ok := governanceSupportedProviders[value]; !ok {
			return fmt.Errorf("must be one of [openai, google_genai, anthropic, openrouter]")
		}
	case KeyGovernanceModel:
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("must not be empty")
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
	case KeyGateConfidenceThreshold:
		f, err := strconv.ParseFloat(value, 64)
		if err != nil || f < 0 || f > 1 {
			return fmt.Errorf("must be a number between 0 and 1")
		}
	case KeyFeatureFreshnessSLADays, KeyArtifactStaleDays, KeyGovernanceFilesTTLDays:
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > 3650 {
			return fmt.Errorf("must be an integer number of days between 1 and 3650")
		}
	case KeyRegistryTimeoutSeconds:
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
