package command_test

import (
	"context"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/interactive"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestRootHelpIncludesModelCommand(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	root := command.NewRootCommand(deps)
	root.SetOut(out)
	root.SetErr(out)
	code := command.ExecuteForCode(root, "--help")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, out = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "\n  model") {
		t.Fatalf("root help should include model command, got:\n%s", out.String())
	}
}

func TestModelOffDisablesServerModelWithoutClearingConfiguration(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "model", "off")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, out = %s", code, out.String())
	}
	if fc.lastUpdateSettings["governance.model_enabled"] != "false" {
		t.Fatalf("model off settings = %#v", fc.lastUpdateSettings)
	}
}

func TestRootHelpIncludesReleaseCommandFamilies(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	root := command.NewRootCommand(deps)
	root.SetOut(out)
	root.SetErr(out)
	code := command.ExecuteForCode(root, "--help")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, out = %s", code, out.String())
	}
	help := out.String()
	for _, name := range []string{
		"artifact",
		"config",
		"delivery",
		"doctor",
		"down",
		"gates",
		"init",
		"local-status",
		"model",
		"open",
		"policy",
		"skill",
		"status",
		"up",
		"update",
		"work",
	} {
		if !strings.Contains(help, "\n  "+name) {
			t.Fatalf("root help missing %q, got:\n%s", name, help)
		}
	}
	for _, name := range []string{"corpus", "evidence", "outcome"} {
		if strings.Contains(help, "\n  "+name) {
			t.Fatalf("root help should not list retired %s command, got:\n%s", name, help)
		}
	}
	if strings.Contains(help, "\n  gate ") {
		t.Fatalf("root help should not list retired singular gate command, got:\n%s", help)
	}
}

func TestRootHelpGroupsCommandsByAudience(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	root := command.NewRootCommand(deps)
	root.SetOut(out)
	root.SetErr(out)
	code := command.ExecuteForCode(root, "--help")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, out = %s", code, out.String())
	}
	help := out.String()
	for _, heading := range []string{
		"Core workflow commands",
		"Setup and identity commands",
		"Full appliance commands",
	} {
		if !strings.Contains(help, heading) {
			t.Fatalf("root help missing heading %q, got:\n%s", heading, help)
		}
	}
	for _, retired := range []string{"Advanced governance commands", "Local stack commands"} {
		if strings.Contains(help, retired) {
			t.Fatalf("root help still contains misleading heading %q, got:\n%s", retired, help)
		}
	}
	core := strings.Index(help, "Core workflow commands")
	setup := strings.Index(help, "Setup and identity commands")
	full := strings.Index(help, "Full appliance commands")
	additional := strings.Index(help, "Additional Commands:")
	if !(core < setup && setup < full && full < additional) {
		t.Fatalf("root help groups are out of order, got:\n%s", help)
	}
	coreHelp := help[core:setup]
	if !strings.Contains(coreHelp, "\n  feature ") {
		t.Fatalf("feature should be a core command because Local supports list/show, got:\n%s", help)
	}
	setupHelp := help[setup:full]
	for _, name := range []string{"cleanup", "init"} {
		if !strings.Contains(setupHelp, "\n  "+name+" ") {
			t.Fatalf("%s should be a setup command, got:\n%s", name, help)
		}
	}
	if strings.Contains(setupHelp, "CLI-to-server connectivity") ||
		!strings.Contains(setupHelp, "doctor       Check Local or Full setup health") {
		t.Fatalf("doctor should describe both Local and Full setup health, got:\n%s", help)
	}
	fullHelp := help[full:additional]
	for _, name := range []string{"demo", "down", "knowledge", "local-status", "model", "open", "policy", "skill", "up"} {
		if !strings.Contains(fullHelp, "\n  "+name) {
			t.Fatalf("Full appliance help missing %q, got:\n%s", name, help)
		}
	}
	if strings.Contains(fullHelp, "local SpecGate stack") {
		t.Fatalf("Full appliance commands still use misleading stack terminology, got:\n%s", help)
	}
}

func TestModelSetCmd_WritesProviderAndModel(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain",
		"model", "set", "--provider", "google", "--model", "gemini-3.1-flash-lite")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, out = %s", code, out.String())
	}
	// friendly "google" maps to provider value google_genai
	if fc.lastUpdateSettings["governance.model_provider"] != "google_genai" {
		t.Errorf("provider value = %q, want google_genai", fc.lastUpdateSettings["governance.model_provider"])
	}
	if fc.lastUpdateSettings["governance.model"] != "gemini-3.1-flash-lite" {
		t.Errorf("model = %q", fc.lastUpdateSettings["governance.model"])
	}
}

func TestModelSetCmd_ReadsProviderKeyFromEnvironment(t *testing.T) {
	t.Setenv("GOOGLE_API_KEY", "env-key")
	deps, fc, _, out := newFakeDeps(t)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain",
		"model", "set", "--provider", "google", "--model", "gemini-3.1-flash-lite")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, out = %s", code, out.String())
	}
	if fc.lastUpdateSettings["google.api_key"] != "env-key" {
		t.Fatalf("api key not read from GOOGLE_API_KEY: %v", fc.lastUpdateSettings)
	}
}

func TestModelSetCmd_RejectsUnknownProvider(t *testing.T) {
	t.Parallel()
	deps, _, _, _ := newFakeDeps(t)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain",
		"model", "set", "--provider", "bedrock")
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want ExitUsage", code)
	}
}

func TestModelSetCmd_PlainDoesNotPromptForProvider(t *testing.T) {
	t.Parallel()
	deps, fc, fp, out := newFakeDeps(t)

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "model", "set")
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want usage; output = %s", code, out.String())
	}
	if fc.lastUpdateSettings != nil {
		t.Fatalf("settings updated despite validation error: %v", fc.lastUpdateSettings)
	}
	if len(fp.selectOptions) != 0 || fp.inputCalls != 0 || fp.secretCalls != 0 {
		t.Fatalf("plain model set prompted: select=%d input=%d secret=%d", len(fp.selectOptions), fp.inputCalls, fp.secretCalls)
	}
}

func TestModelShowCmd_MasksKey(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.settings = map[string]string{
		"governance.model_provider": "openai",
		"governance.model":          "gpt-5.4-mini",
		"openai.api_key":            "***",
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "model", "show")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, out = %s", code, out.String())
	}
	s := out.String()
	if !strings.Contains(s, "openai") || !strings.Contains(s, "gpt-5.4-mini") {
		t.Errorf("missing provider/model: %s", s)
	}
	if !strings.Contains(s, "set") || strings.Contains(s, "k-") {
		t.Errorf("api key should show as set, never the value: %s", s)
	}
}

func TestModelTestReportsConfiguredProvider(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.settings = map[string]string{
		"governance.model_provider": "google_genai",
		"governance.model":          "gemini-3.1-flash-lite",
		"google.api_key":            "***",
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "model", "test")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "Model settings are configured") {
		t.Fatalf("unexpected output: %s", out.String())
	}
	if !strings.Contains(out.String(), "no live model call") {
		t.Fatalf("unexpected output: %s", out.String())
	}
}

func TestModelTestFailsWithoutAPIKey(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.settings = map[string]string{
		"governance.model_provider": "google_genai",
		"governance.model":          "gemini-3.1-flash-lite",
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "model", "test")
	if code != output.ExitUnavailable {
		t.Fatalf("exit = %d, want unavailable; output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "API key is not set") {
		t.Fatalf("output missing API key hint: %s", out.String())
	}
}

func TestModelSetCmd_InteractiveWizard(t *testing.T) {
	t.Parallel()
	deps, fc, fp, out := newFakeDeps(t)
	fp.selectedValue = "google"             // provider pick
	fp.inputValue = "gemini-3.1-flash-lite" // model id
	fp.secretValue = "k-xyz"                // masked API key
	code := command.ExecuteForCode(command.NewRootCommand(deps), "model", "set")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, out = %s", code, out.String())
	}
	if fc.lastUpdateSettings["governance.model_provider"] != "google_genai" {
		t.Errorf("provider = %q, want google_genai", fc.lastUpdateSettings["governance.model_provider"])
	}
	if fc.lastUpdateSettings["google.api_key"] != "k-xyz" {
		t.Errorf("api key from secret prompt not written: %v", fc.lastUpdateSettings)
	}
	if fc.lastUpdateSettings["governance.model"] != "gemini-3.1-flash-lite" {
		t.Errorf("model from input prompt not written: %v", fc.lastUpdateSettings)
	}
	for _, opt := range fp.selectOptions {
		if strings.Contains(strings.ToLower(opt.Label), "free tier") {
			t.Fatalf("provider label should not mention free tier: %#v", fp.selectOptions)
		}
	}
	if !strings.Contains(fp.secretTitle, "GOOGLE_API_KEY") {
		t.Fatalf("secret prompt should name GOOGLE_API_KEY, got %q", fp.secretTitle)
	}
	if len(fp.suggestions) == 0 || fp.suggestions[0] != "gemini-3.1-flash-lite" {
		t.Fatalf("missing Google model suggestions: %#v", fp.suggestions)
	}
}

func TestModelSetCmd_InteractiveWizardUsesProviderDefaultWhenModelBlank(t *testing.T) {
	t.Parallel()
	deps, fc, fp, out := newFakeDeps(t)
	fp.selectedValue = "google"
	fp.inputValue = ""
	fp.secretValue = "k-xyz"
	code := command.ExecuteForCode(command.NewRootCommand(deps), "model", "set")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, out = %s", code, out.String())
	}
	if fc.lastUpdateSettings["governance.model"] != "gemini-3.1-flash-lite" {
		t.Errorf("blank model should write provider default: %v", fc.lastUpdateSettings)
	}
}

func TestModelSetCmd_WithProviderPromptsForModelAndKey(t *testing.T) {
	t.Parallel()
	deps, fc, fp, out := newFakeDeps(t)
	deps.OpenRouterModelOptions = func(context.Context) ([]interactive.Option, error) {
		return []interactive.Option{
			{Label: "OpenAI GPT 5.4 Mini — openai/gpt-5.4-mini", Value: "openai/gpt-5.4-mini"},
			{Label: "Google Gemini 3.1 Flash Lite — google/gemini-3.1-flash-lite", Value: "google/gemini-3.1-flash-lite"},
			{Label: "Manual entry / paste model id", Value: ""},
		}, nil
	}
	fp.searchValue = "openai/gpt-5.4-mini"
	fp.secretValue = "openrouter-test-key"
	code := command.ExecuteForCode(command.NewRootCommand(deps),
		"model", "set", "--provider", "openrouter")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, out = %s", code, out.String())
	}
	if fc.lastUpdateSettings["governance.model_provider"] != "openrouter" {
		t.Errorf("provider = %q, want openrouter", fc.lastUpdateSettings["governance.model_provider"])
	}
	if fc.lastUpdateSettings["governance.model"] != "openai/gpt-5.4-mini" {
		t.Errorf("model = %q", fc.lastUpdateSettings["governance.model"])
	}
	if fc.lastUpdateSettings["openrouter.api_key"] != "openrouter-test-key" {
		t.Errorf("api key not written under openrouter.api_key: %v", fc.lastUpdateSettings)
	}
	if !strings.Contains(fp.secretTitle, "OPENROUTER_API_KEY") {
		t.Fatalf("secret prompt should name OPENROUTER_API_KEY, got %q", fp.secretTitle)
	}
	if fp.searchTitle != "OpenRouter model" {
		t.Fatalf("OpenRouter should use searchable model picker, got title %q", fp.searchTitle)
	}
	if !containsOptionValue(fp.searchOptions, "openai/gpt-5.4-mini") ||
		!containsOptionValue(fp.searchOptions, "google/gemini-3.1-flash-lite") {
		t.Fatalf("missing OpenRouter search options: %#v", fp.searchOptions)
	}
}

func TestModelSetCmd_OpenRouterManualFallbackWhenSearchUnavailable(t *testing.T) {
	t.Parallel()
	deps, fc, fp, out := newFakeDeps(t)
	deps.OpenRouterModelOptions = func(context.Context) ([]interactive.Option, error) {
		return nil, context.Canceled
	}
	fp.inputValue = "custom/vendor-text-model"
	fp.secretValue = "openrouter-test-key"
	code := command.ExecuteForCode(command.NewRootCommand(deps),
		"model", "set", "--provider", "openrouter")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, out = %s", code, out.String())
	}
	if fc.lastUpdateSettings["governance.model"] != "custom/vendor-text-model" {
		t.Errorf("manual fallback model = %q", fc.lastUpdateSettings["governance.model"])
	}
	if fp.inputCalls != 1 {
		t.Fatalf("manual fallback should prompt for model id once, inputCalls=%d", fp.inputCalls)
	}
}

func TestModelSetCmd_WithProviderAndKeyUsesDefaultWithoutPrompt(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-123")
	deps, fc, fp, out := newFakeDeps(t)
	code := command.ExecuteForCode(command.NewRootCommand(deps),
		"--plain", "model", "set", "--provider", "openai")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, out = %s", code, out.String())
	}
	if fp.inputCalls != 0 || fp.secretCalls != 0 {
		t.Fatalf("provider+key should not prompt, inputCalls=%d secretCalls=%d", fp.inputCalls, fp.secretCalls)
	}
	if fc.lastUpdateSettings["governance.model"] != "gpt-5.4-mini" {
		t.Errorf("provider default model not written: %v", fc.lastUpdateSettings)
	}
}

func containsOptionValue(values []interactive.Option, target string) bool {
	for _, value := range values {
		if value.Value == target {
			return true
		}
	}
	return false
}
