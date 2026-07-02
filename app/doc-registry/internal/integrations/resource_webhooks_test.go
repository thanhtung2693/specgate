package integrations

import (
	"strings"
	"testing"
)

func TestExistingProviderHookID(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		`{"provider_webhook_id":"42"}`:                             "42",
		`{"webhook_status":"connected","provider_webhook_id":"7"}`: "7",
		`{}`:                          "",
		``:                            "",
		`not json`:                    "",
		`{"provider_webhook_id":123}`: "", // non-string is ignored
	}
	for cfg, want := range cases {
		if got := existingProviderHookID(cfg); got != want {
			t.Fatalf("existingProviderHookID(%q) = %q, want %q", cfg, got, want)
		}
	}
}

func TestMergeResourceWebhookConfig_ClearsErrorOnConnected(t *testing.T) {
	t.Parallel()
	// A failed reprovision records the error.
	withErr := mergeResourceWebhookConfig(`{}`, managedWebhookConfig{
		Status: "error", LastError: "Url is blocked: localhost",
	})
	if !strings.Contains(withErr, "Url is blocked") || !strings.Contains(withErr, `"webhook_status":"error"`) {
		t.Fatalf("expected error recorded, got %s", withErr)
	}

	// A successful (re)provision clears the prior error and records the hook.
	connected := mergeResourceWebhookConfig(withErr, managedWebhookConfig{
		URL: "https://pub.example/webhook", ProviderHookID: "9", Status: "connected",
	})
	if strings.Contains(connected, "webhook_last_error") || strings.Contains(connected, "Url is blocked") {
		t.Fatalf("connected config still carries a stale error: %s", connected)
	}
	if !strings.Contains(connected, `"webhook_status":"connected"`) ||
		!strings.Contains(connected, `"provider_webhook_id":"9"`) ||
		!strings.Contains(connected, `"webhook_url":"https://pub.example/webhook"`) {
		t.Fatalf("connected config missing fields: %s", connected)
	}
}
