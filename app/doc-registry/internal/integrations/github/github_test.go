package github

import (
	"testing"

	"github.com/specgate/doc-registry/internal/integrations/coretypes"
)

func TestGitHubNormalizedDelivery_MapsFieldsAndState(t *testing.T) {
	t.Parallel()
	raw := `{"action":"closed","number":7,"pull_request":{"id":4242,"number":7,` +
		`"title":"Add X","body":"fixes SPECGATE-CR-1","html_url":"https://github.com/acme/repo/pull/7",` +
		`"state":"closed","merged":true,"merge_commit_sha":"abc123",` +
		`"head":{"ref":"feat/x"},"base":{"ref":"main"}},` +
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
	if nd.SourceBranch != "feat/x" || nd.TargetBranch != "main" || nd.MergeCommitSHA != "abc123" {
		t.Fatalf("branches/sha = %q/%q/%q", nd.SourceBranch, nd.TargetBranch, nd.MergeCommitSHA)
	}
	if nd.Description != "fixes SPECGATE-CR-1" || nd.URL != "https://github.com/acme/repo/pull/7" {
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
		if got := gitHubAPIURL(in); got != want {
			t.Errorf("gitHubAPIURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseWorkflowRun_SuccessConclusion(t *testing.T) {
	t.Parallel()
	raw := `{"action":"completed","workflow_run":{"id":9900,"name":"CI","conclusion":"success",` +
		`"head_branch":"feat/ci","head_sha":"deadbeef","html_url":"https://github.com/acme/repo/actions/runs/9900"},` +
		`"repository":{"id":99,"full_name":"acme/repo"}}`
	nd, err := ParseWorkflowRun(raw, "evt-wr-1")
	if err != nil {
		t.Fatal(err)
	}
	if nd.Provider != coretypes.ProviderGitHub {
		t.Errorf("Provider = %q, want %q", nd.Provider, coretypes.ProviderGitHub)
	}
	if nd.EventType != coretypes.WebhookEventWorkflowRun {
		t.Errorf("EventType = %q, want %q", nd.EventType, coretypes.WebhookEventWorkflowRun)
	}
	if nd.DeliveryState != coretypes.DeliveryStateCIPassed {
		t.Errorf("DeliveryState = %q, want %q", nd.DeliveryState, coretypes.DeliveryStateCIPassed)
	}
	if nd.SourceBranch != "feat/ci" {
		t.Errorf("SourceBranch = %q, want feat/ci", nd.SourceBranch)
	}
	if nd.MergeCommitSHA != "deadbeef" {
		t.Errorf("MergeCommitSHA = %q, want deadbeef", nd.MergeCommitSHA)
	}
	if nd.ExternalID != "9900" {
		t.Errorf("ExternalID = %q, want 9900", nd.ExternalID)
	}
	if nd.ProjectID != 99 || nd.ProjectKey != "acme/repo" {
		t.Errorf("project = %d/%q", nd.ProjectID, nd.ProjectKey)
	}
}

func TestParseWorkflowRun_NonSuccessIgnored(t *testing.T) {
	t.Parallel()
	for _, conclusion := range []string{"failure", "cancelled", "skipped", ""} {
		raw := `{"action":"completed","workflow_run":{"id":1,"conclusion":"` + conclusion + `",` +
			`"head_branch":"main","head_sha":"abc"},` +
			`"repository":{"id":1,"full_name":"acme/repo"}}`
		if _, err := ParseWorkflowRun(raw, "e1"); err == nil {
			t.Errorf("conclusion=%q: expected validation error, got nil", conclusion)
		}
	}
}

func TestParseWorkflowRun_RejectsEmptyRepository(t *testing.T) {
	t.Parallel()
	raw := `{"action":"completed","workflow_run":{"id":1,"conclusion":"success","head_branch":"x","head_sha":"y"},` +
		`"repository":{}}`
	if _, err := ParseWorkflowRun(raw, "e1"); err == nil {
		t.Fatal("expected validation error for empty repository, got nil")
	}
}
