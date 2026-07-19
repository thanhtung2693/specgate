package github

import (
	"testing"

	"github.com/specgate/doc-registry/internal/githubapi"
	"github.com/specgate/doc-registry/internal/integrations/coretypes"
)

func TestGitHubNormalizedDelivery_MapsFieldsAndState(t *testing.T) {
	t.Parallel()
	raw := `{"action":"closed","number":7,"pull_request":{"id":4242,"number":7,` +
		`"title":"Add X","body":"<!-- specgate-work-ref: CR-1 -->","html_url":"https://github.com/acme/repo/pull/7",` +
		`"state":"closed","merged":true,"merge_commit_sha":"provider-merge",` +
		`"head":{"ref":"feature/SG-123","sha":"submitted-head"},"base":{"ref":"main"}},` +
		`"repository":{"id":99,"full_name":"acme/repo"}}`
	nd, err := ParseAndNormalize(raw, "evt-1")
	if err != nil {
		t.Fatal(err)
	}
	if nd.Provider != coretypes.ProviderGitHub || nd.EventType != coretypes.WebhookEventMergeRequest {
		t.Fatalf("provider/eventType = %q/%q", nd.Provider, nd.EventType)
	}
	if nd.ProjectID != 99 || nd.ProjectKey != "acme/repo" {
		t.Fatalf("project = %d/%q", nd.ProjectID, nd.ProjectKey)
	}
	if nd.ExternalID != "4242" || nd.IID != 7 || nd.ExternalKey != "acme/repo#7" {
		t.Fatalf("external = %q/%d/%q", nd.ExternalID, nd.IID, nd.ExternalKey)
	}
	if nd.SourceBranch != "feature/SG-123" || nd.TargetBranch != "main" || nd.HeadSHA != "submitted-head" || nd.MergeCommitSHA != "provider-merge" {
		t.Fatalf("branches/head/merge = %q/%q/%q/%q", nd.SourceBranch, nd.TargetBranch, nd.HeadSHA, nd.MergeCommitSHA)
	}
	if nd.Description != "<!-- specgate-work-ref: CR-1 -->" || nd.URL != "https://github.com/acme/repo/pull/7" {
		t.Fatalf("desc/url = %q/%q", nd.Description, nd.URL)
	}
	if nd.DeliveryState != coretypes.DeliveryStateMerged {
		t.Fatalf("deliveryState = %q, want merged (closed+merged)", nd.DeliveryState)
	}
}

func TestParseAndNormalize_RejectsEmptyRepository(t *testing.T) {
	t.Parallel()
	raw := `{"action":"opened","pull_request":{"id":1,"number":1},"repository":{}}`
	if _, err := ParseAndNormalize(raw, "evt-1"); err == nil {
		t.Fatal("expected validation error for empty repository, got nil")
	}
}

func TestGitHubPRDeliveryState(t *testing.T) {
	t.Parallel()
	cases := []struct {
		action string
		merged bool
		want   string
	}{
		{"opened", false, coretypes.DeliveryStateOpened},
		{"reopened", false, coretypes.DeliveryStateOpened},
		{"synchronize", false, coretypes.DeliveryStateOpened},
		{"closed", true, coretypes.DeliveryStateMerged},
		{"closed", false, coretypes.DeliveryStateClosed},
	}
	for _, c := range cases {
		if got := gitHubPRDeliveryState(c.action, c.merged); got != c.want {
			t.Fatalf("gitHubPRDeliveryState(%q, %v) = %q, want %q", c.action, c.merged, got, c.want)
		}
	}
}

func TestGitHubAPIURL_HostedAndEnterprise(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"":                        "https://api.github.com",
		"https://github.com":      "https://api.github.com",
		"https://github.com/":     "https://api.github.com",
		"https://ghe.example.com": "https://ghe.example.com/api/v3",
	}
	for in, want := range cases {
		if got := githubapi.APIURL(in); got != want {
			t.Errorf("githubapi.APIURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWebhookDriver_RejectsWorkflowRun(t *testing.T) {
	t.Parallel()
	_, _, ignored, err := webhookDriver{}.Normalize(coretypes.InboundWebhook{
		EventHeader: "workflow" + "_run",
		PayloadJSON: `{"action":"completed","workflow` + `_run":{"conclusion":"success"},"repository":{"id":99,"full_name":"acme/repo"}}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if ignored != "unsupported_github_event" {
		t.Fatalf("ignored = %q, want unsupported_github_event", ignored)
	}
}
