package command

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

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
		OutputModalities []string `json:"output_modalities"`
	} `json:"architecture"`
}

const (
	openRouterModelsURL              = "https://openrouter.ai/api/v1/models"
	maxOpenRouterModelsResponseBytes = 4 << 20
)

var fetchOpenRouterModels = func(ctx context.Context) ([]openRouterModel, error) {
	return fetchOpenRouterModelsFrom(ctx, &http.Client{Timeout: 30 * time.Second}, openRouterModelsURL)
}

func fetchOpenRouterModelsFrom(ctx context.Context, client *http.Client, url string) ([]openRouterModel, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("openrouter models returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxOpenRouterModelsResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxOpenRouterModelsResponseBytes {
		return nil, fmt.Errorf("openrouter models response exceeds %d bytes", maxOpenRouterModelsResponseBytes)
	}
	var payload struct {
		Data []openRouterModel `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	return payload.Data, nil
}

func registerModelCommands(root *cobra.Command, deps *Deps) {
	model := &cobra.Command{
		Use:   "model",
		Short: "Configure Full-mode models and provider keys",
		Example: strings.TrimSpace(`specgate model set
specgate model off
specgate model on
specgate model show
specgate model test`),
	}

	var provider, modelID, thinkingLevel string
	setCmd := &cobra.Command{
		Use:   "set",
		Short: "Set the server-side governance model provider, model, and API key",
		Long: strings.TrimSpace(`Set the server-side governance model provider, model, and API key.

Run without flags for an interactive setup:

  specgate model set

The model id is free-form so providers can use any supported model id. Leave it
blank to use the provider default when SpecGate knows one. For OpenRouter, the
interactive flow searches the public model catalog and shows only text-output
models; choose Manual entry if you want to paste an id directly.

Because this command may transmit an API key, it ignores server URLs committed
in the current repository. Use --server, SPECGATE_SERVER, or saved user
configuration to select the trusted destination.`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			promptAllowed := canPrompt(deps)
			providerFlag := cmd.Flags().Changed("provider")
			modelFlag := cmd.Flags().Changed("model")
			fullWizard := strings.TrimSpace(provider) == "" && promptAllowed
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
			apiKey := strings.TrimSpace(os.Getenv(spec.envKey))
			promptPartial := providerFlag && !modelFlag
			if promptAllowed && strings.TrimSpace(modelID) == "" && (fullWizard || promptPartial) {
				var err error
				modelID, err = promptModelID(cmd.Context(), deps, spec)
				if err != nil {
					return err
				}
			}
			if promptAllowed && apiKey == "" && (fullWizard || promptPartial || (providerFlag && modelFlag)) {
				var err error
				apiKey, err = deps.Prompter.Secret(fmt.Sprintf("%s API key (blank to use configured %s)", spec.label, spec.envKey))
				if err != nil {
					return err
				}
			}
			settings := map[string]string{"governance.model_provider": spec.providerValue, "governance.model_enabled": "true"}
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
				return apiExitError(deps, "model.set", err)
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("model.set", modelView(result))
				return nil
			}
			view := modelView(result)
			fmt.Fprintf(deps.Stdout, "%s %s %s %s %s %s\n",
				styled(deps, output.StyleSuccess, "Platform model configured:"),
				label(deps, "provider="), styled(deps, output.StyleAction, result["governance.model_provider"]),
				label(deps, "model="), styled(deps, output.StyleAction, result["governance.model"]),
				label(deps, "api_key=")+fmt.Sprint(view["api_key"]))
			return nil
		},
	}
	setCmd.Flags().StringVar(&provider, "provider", "", "Model provider: openai | google | anthropic | openrouter (prompts interactively if omitted)")
	setCmd.Flags().StringVar(&modelID, "model", "", "Model id (e.g. gpt-5.4-mini); provider default applies when omitted")
	setCmd.Flags().StringVar(&thinkingLevel, "thinking-level", "", "Reasoning effort: low | medium | high")

	showCmd := &cobra.Command{
		Use:   "show",
		Short: "Show the configured server-side governance model (API key reported as set/not set)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			settings, err := deps.Client.GetSettings(cmd.Context())
			if err != nil {
				return apiExitError(deps, "model.show", err)
			}
			view := modelView(settings)
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("model.show", view)
				return nil
			}
			fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "mode:"), styledStatus(deps, fmt.Sprint(view["mode"])))
			fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "provider:"), view["provider"])
			fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "model:"), styled(deps, output.StyleAction, fmt.Sprint(view["model"])))
			fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "thinking_level:"), view["thinking_level"])
			fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "api_key:"), styledStatus(deps, fmt.Sprint(view["api_key"])))
			return nil
		},
	}

	offCmd := &cobra.Command{
		Use: "off", Short: "Turn off the server-side model and use IDE-agent model-less workflows", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			result, err := deps.Client.UpdateSettings(cmd.Context(), map[string]string{"governance.model_enabled": "false"})
			if err != nil {
				return apiExitError(deps, "model.off", err)
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("model.off", modelView(result))
				return nil
			}
			fmt.Fprintln(deps.Stdout, styled(deps, output.StyleWarning, "Server-side model disabled.")+" IDE-agent checks remain available; agent-attested delivery requires peer or human review.")
			return nil
		},
	}
	onCmd := &cobra.Command{
		Use: "on", Short: "Turn on the saved server-side model configuration", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			result, err := deps.Client.UpdateSettings(cmd.Context(), map[string]string{"governance.model_enabled": "true"})
			if err != nil {
				return apiExitError(deps, "model.on", err)
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("model.on", modelView(result))
				return nil
			}
			fmt.Fprintln(deps.Stdout, styled(deps, output.StyleSuccess, "Server-side model enabled")+" with the saved configuration.")
			return nil
		},
	}

	testCmd := &cobra.Command{
		Use:   "test",
		Short: "Check governance model settings without making a live model call",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			settings, err := deps.Client.GetSettings(cmd.Context())
			if err != nil {
				return apiExitError(deps, "model.test", err)
			}
			view := modelView(settings)
			status := modelSettingsStatus(view)
			if status.NeedsSetup {
				code := deps.Printer.Error("model.test", output.ErrorPayload{Code: "unavailable", Message: status.Message})
				return &output.ExitError{Code: code}
			}
			if deps.Printer.Mode() == output.ModeJSON {
				payload := make(map[string]any, len(view)+1)
				for key, value := range view {
					payload[key] = value
				}
				payload["message"] = "settings are configured; no live model call was made"
				deps.Printer.Success("model.test", payload)
				return nil
			}
			fmt.Fprintf(deps.Stdout, "%s %s %s %s %s\n", styled(deps, output.StyleSuccess, "Model settings are configured:"), label(deps, "provider="), status.Provider, label(deps, "model="), styled(deps, output.StyleAction, status.Model)+" (no live model call)")
			return nil
		},
	}

	model.AddCommand(setCmd, offCmd, onCmd, showCmd, testCmd)
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
	return false
}

// modelView projects the model-related settings into a compact view. The API key
// is never echoed — only reported as "set" or "not set" for the active provider.
// providerAPIKeyIsSet reports whether the API key for the given provider VALUE
// (e.g. "google_genai") is present in raw settings. Raw settings carry the
// redacted key value ("" when unset, a masked placeholder when set); a non-empty
// raw value means the key is set. This is the single key-presence normalization
// shared by the model view and the doctor embedding check.
func providerAPIKeyIsSet(settings map[string]string, providerValue string) bool {
	for _, spec := range modelProviders {
		if spec.providerValue == providerValue {
			return strings.TrimSpace(settings[spec.apiKeyKey]) != ""
		}
	}
	return false
}

func modelView(s map[string]string) map[string]any {
	provider := s["governance.model_provider"]
	apiKeyState := "not set"
	if providerAPIKeyIsSet(s, provider) {
		apiKeyState = "set"
	}
	return map[string]any{
		"mode":           modelMode(s),
		"provider":       provider,
		"model":          s["governance.model"],
		"thinking_level": s["governance.default_thinking_level"],
		"api_key":        apiKeyState,
	}
}

func modelMode(s map[string]string) string {
	if strings.EqualFold(strings.TrimSpace(s["governance.model_enabled"]), "false") {
		return "model_less"
	}
	return "enabled"
}

type modelSettingsCheck struct {
	Provider   string
	Model      string
	NeedsSetup bool
	Message    string
}

func modelSettingsStatus(view map[string]any) modelSettingsCheck {
	provider, _ := view["provider"].(string)
	modelID, _ := view["model"].(string)
	apiKey, _ := view["api_key"].(string)
	status := modelSettingsCheck{Provider: provider, Model: modelID}
	switch {
	case view["mode"] == "model_less":
		status.NeedsSetup = true
		status.Message = "server-side model is disabled; use IDE-agent gates and peer or human delivery review, or run `specgate model set` to enable it"
	case strings.TrimSpace(provider) == "" || strings.TrimSpace(modelID) == "":
		status.NeedsSetup = true
		status.Message = "governance model provider or model is missing; run `specgate model set`"
	case apiKey != "set":
		status.NeedsSetup = true
		status.Message = "governance model API key is not set — delivery verdicts are self-attested (agent claims, unreviewed) and server-side model review cannot run; run `specgate model set`, or dispatch artifact gates to your IDE agent via `specgate gates tasks dispatch <artifact-id>`"
	}
	return status
}
