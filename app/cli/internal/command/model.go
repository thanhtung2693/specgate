package command

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/specgate/specgate/app/cli/internal/interactive"
	"github.com/specgate/specgate/app/cli/internal/output"
	"github.com/spf13/cobra"
)

// modelProviderSpec maps a friendly provider name to its settings provider value
// and API-key setting name. The provider VALUE (e.g. google_genai) and the
// api-key KEY (e.g. google.api_key) deliberately differ — callers use the
// friendly name and never see this asymmetry.
type modelProviderSpec struct {
	providerValue string
	apiKeyKey     string
	envKey        string
	label         string
	defaultModel  string
	modelHint     string
	suggestions   []string
}

var modelProviders = map[string]modelProviderSpec{
	"openai": {
		providerValue: "openai",
		apiKeyKey:     "openai.api_key",
		envKey:        "OPENAI_API_KEY",
		label:         "OpenAI",
		defaultModel:  "gpt-5.4-mini",
		modelHint:     "gpt-5.4-mini",
		suggestions:   []string{"gpt-5.4-mini", "gpt-5.4", "gpt-5.4-nano", "gpt-5.5"},
	},
	"google": {
		providerValue: "google_genai",
		apiKeyKey:     "google.api_key",
		envKey:        "GOOGLE_API_KEY",
		label:         "Google",
		defaultModel:  "gemini-3.1-flash-lite",
		modelHint:     "gemini-3.1-flash-lite",
		suggestions:   []string{"gemini-3.1-flash-lite", "gemini-3.1-pro", "gemini-3.1-flash"},
	},
	"anthropic": {
		providerValue: "anthropic",
		apiKeyKey:     "anthropic.api_key",
		envKey:        "ANTHROPIC_API_KEY",
		label:         "Anthropic",
		defaultModel:  "claude-sonnet-4-6",
		modelHint:     "claude-sonnet-4-6",
		suggestions:   []string{"claude-sonnet-4-6", "claude-opus-4-7"},
	},
	"openrouter": {
		providerValue: "openrouter",
		apiKeyKey:     "openrouter.api_key",
		envKey:        "OPENROUTER_API_KEY",
		label:         "OpenRouter",
		defaultModel:  "",
		modelHint:     "openai/gpt-5.4-mini or google/gemini-3.1-flash-lite",
		suggestions: []string{
			"openai/gpt-5.4-mini",
			"openai/gpt-5.4",
			"google/gemini-3.1-flash-lite",
			"anthropic/claude-sonnet-4-6",
		},
	},
}

type openRouterModel struct {
	ID           string
	Name         string
	Architecture struct {
		Modality         string   `json:"modality"`
		InputModalities  []string `json:"input_modalities"`
		OutputModalities []string `json:"output_modalities"`
	} `json:"architecture"`
}

var fetchOpenRouterModels = func(ctx context.Context) ([]openRouterModel, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://openrouter.ai/api/v1/models", nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("openrouter models returned HTTP %d", resp.StatusCode)
	}
	var payload struct {
		Data []openRouterModel `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return payload.Data, nil
}

func registerModelCommands(root *cobra.Command, deps *Deps) {
	model := &cobra.Command{
		Use:   "model",
		Short: "Configure the local model provider, model, and API key — no UI required",
	}

	var provider, modelID, apiKey, thinkingLevel string
	setCmd := &cobra.Command{
		Use:   "set",
		Short: "Set the local model provider, model, and API key",
		Long: strings.TrimSpace(`Set the local model provider, model, and API key.

Run without flags for an interactive setup:

  specgate model set

The model id is free-form so providers can use any supported model id. Leave it
blank to use the provider default when SpecGate knows one. For OpenRouter, the
interactive flow searches the public model catalog and shows only text-output
models; choose Manual entry if you want to paste an id directly.`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			canPrompt := !deps.NoInput
			providerFlag := cmd.Flags().Changed("provider")
			modelFlag := cmd.Flags().Changed("model")
			apiKeyFlag := cmd.Flags().Changed("api-key")
			fullWizard := strings.TrimSpace(provider) == "" && canPrompt
			if fullWizard {
				picked, err := deps.Prompter.Select("Model provider", []interactive.Option{
					{Label: "OpenAI", Value: "openai"},
					{Label: "Google", Value: "google"},
					{Label: "Anthropic", Value: "anthropic"},
					{Label: "OpenRouter", Value: "openrouter"},
				})
				if err != nil {
					return err
				}
				provider = picked
			}

			spec, ok := modelProviders[strings.ToLower(strings.TrimSpace(provider))]
			if !ok {
				deps.Printer.Error("model.set", output.ErrorPayload{
					Code:    "invalid_argument",
					Message: fmt.Sprintf("invalid --provider %q; allowed: openai, google, anthropic, openrouter", provider),
				})
				return &output.ExitError{Code: output.ExitUsage, Err: fmt.Errorf("invalid provider")}
			}
			promptPartial := providerFlag && !modelFlag && !apiKeyFlag
			if canPrompt && strings.TrimSpace(modelID) == "" && (fullWizard || promptPartial) {
				var err error
				modelID, err = promptModelID(cmd.Context(), deps, spec)
				if err != nil {
					return err
				}
			}
			if canPrompt && apiKey == "" && (fullWizard || promptPartial || (providerFlag && modelFlag && !apiKeyFlag)) {
				var err error
				apiKey, err = deps.Prompter.Secret(fmt.Sprintf("%s API key (blank to use %s from agents.env)", spec.label, spec.envKey))
				if err != nil {
					return err
				}
			}

			settings := map[string]string{"governance.model_provider": spec.providerValue}
			if strings.TrimSpace(modelID) != "" {
				settings["governance.model"] = strings.TrimSpace(modelID)
			} else if spec.defaultModel != "" {
				settings["governance.model"] = spec.defaultModel
			}
			if apiKey != "" {
				settings[spec.apiKeyKey] = apiKey
			}
			if thinkingLevel != "" {
				lvl := strings.ToLower(strings.TrimSpace(thinkingLevel))
				if lvl != "low" && lvl != "medium" && lvl != "high" {
					deps.Printer.Error("model.set", output.ErrorPayload{
						Code:    "invalid_argument",
						Message: fmt.Sprintf("invalid --thinking-level %q; allowed: low, medium, high", thinkingLevel),
					})
					return &output.ExitError{Code: output.ExitUsage, Err: fmt.Errorf("invalid thinking level")}
				}
				settings["governance.default_thinking_level"] = lvl
			}
			result, err := deps.Client.UpdateSettings(cmd.Context(), settings)
			if err != nil {
				code := deps.Printer.Error("model.set", mapAPIError("model.set", err))
				return &output.ExitError{Code: code, Err: err}
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("model.set", modelView(result))
				return nil
			}
			fmt.Fprintf(deps.Stdout, "Platform model configured: provider=%s model=%s api_key=%s\n",
				result["governance.model_provider"], result["governance.model"], modelView(result)["api_key"])
			return nil
		},
	}
	setCmd.Flags().StringVar(&provider, "provider", "", "Model provider: openai | google | anthropic | openrouter (prompts interactively if omitted)")
	setCmd.Flags().StringVar(&modelID, "model", "", "Model id (e.g. gpt-5.4-mini); provider default applies when omitted")
	setCmd.Flags().StringVar(&apiKey, "api-key", "", "Provider API key (stored encrypted)")
	setCmd.Flags().StringVar(&thinkingLevel, "thinking-level", "", "Reasoning effort: low | medium | high")

	showCmd := &cobra.Command{
		Use:   "show",
		Short: "Show the configured local model (API key reported as set/not set)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			settings, err := deps.Client.GetSettings(cmd.Context())
			if err != nil {
				code := deps.Printer.Error("model.show", mapAPIError("model.show", err))
				return &output.ExitError{Code: code, Err: err}
			}
			view := modelView(settings)
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("model.show", view)
				return nil
			}
			fmt.Fprintf(deps.Stdout, "provider:       %s\n", view["provider"])
			fmt.Fprintf(deps.Stdout, "model:          %s\n", view["model"])
			fmt.Fprintf(deps.Stdout, "thinking_level: %s\n", view["thinking_level"])
			fmt.Fprintf(deps.Stdout, "api_key:        %s\n", view["api_key"])
			return nil
		},
	}

	model.AddCommand(setCmd, showCmd)
	root.AddCommand(model)
}

func promptModelID(ctx context.Context, deps *Deps, spec modelProviderSpec) (string, error) {
	if spec.providerValue == "openrouter" {
		search := deps.OpenRouterModelOptions
		if search == nil {
			search = defaultOpenRouterModelOptions
		}
		options, err := search(ctx)
		if err == nil {
			if len(options) > 0 {
				selected, selectErr := deps.Prompter.SearchSelect(
					"OpenRouter model",
					"Type to search text-output models by name or id. Choose Manual entry to paste a model id.",
					options,
				)
				if selectErr != nil {
					return "", selectErr
				}
				if strings.TrimSpace(selected) != "" {
					return selected, nil
				}
			}
		} else if deps.Printer != nil && deps.Printer.Mode() != output.ModeJSON {
			fmt.Fprintf(deps.Stderr, "OpenRouter model search unavailable (%v); paste a model id manually.\n", err)
		}
	}

	prompt := fmt.Sprintf("Model id for %s", spec.label)
	placeholder := spec.modelHint
	if spec.defaultModel != "" {
		prompt = fmt.Sprintf("%s (blank for %s)", prompt, spec.defaultModel)
	} else {
		prompt = prompt + " (paste an OpenRouter model id)"
	}
	return deps.Prompter.InputWithSuggestions(prompt, placeholder, spec.suggestions, nil)
}

func defaultOpenRouterModelOptions(ctx context.Context) ([]interactive.Option, error) {
	models, err := fetchOpenRouterModels(ctx)
	if err != nil {
		return nil, err
	}
	return openRouterModelOptions(models), nil
}

func openRouterModelOptions(models []openRouterModel) []interactive.Option {
	options := make([]interactive.Option, 0, len(models)+1)
	seen := make(map[string]struct{}, len(models))
	for _, model := range models {
		id := strings.TrimSpace(model.ID)
		if id == "" || !isOpenRouterTextModel(model) {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		label := id
		if name := strings.TrimSpace(model.Name); name != "" && name != id {
			label = fmt.Sprintf("%s — %s", name, id)
		}
		options = append(options, interactive.Option{Label: label, Value: id})
	}
	sort.SliceStable(options, func(i, j int) bool {
		return strings.ToLower(options[i].Label) < strings.ToLower(options[j].Label)
	})
	options = append(options, interactive.Option{Label: "Manual entry / paste model id", Value: ""})
	return options
}

func isOpenRouterTextModel(model openRouterModel) bool {
	outputs := model.Architecture.OutputModalities
	if len(outputs) > 0 {
		hasText := false
		for _, modality := range outputs {
			switch strings.ToLower(strings.TrimSpace(modality)) {
			case "text":
				hasText = true
			default:
				return false
			}
		}
		return hasText
	}
	modality := strings.ToLower(strings.TrimSpace(model.Architecture.Modality))
	if modality != "" {
		return strings.HasSuffix(modality, "->text") && !strings.Contains(modality, "->text+")
	}
	haystack := strings.ToLower(model.ID + " " + model.Name)
	for _, blocked := range []string{"image", "audio", "embedding", "moderation", "tts", "whisper"} {
		if strings.Contains(haystack, blocked) {
			return false
		}
	}
	return true
}

// modelView projects the model-related settings into a compact view. The API key
// is never echoed — only reported as "set" or "not set" for the active provider.
func modelView(s map[string]string) map[string]any {
	provider := s["governance.model_provider"]
	apiKeyState := "not set"
	for _, spec := range modelProviders {
		if spec.providerValue == provider && strings.TrimSpace(s[spec.apiKeyKey]) != "" {
			apiKeyState = "set"
		}
	}
	return map[string]any{
		"provider":       provider,
		"model":          s["governance.model"],
		"thinking_level": s["governance.default_thinking_level"],
		"api_key":        apiKeyState,
	}
}
