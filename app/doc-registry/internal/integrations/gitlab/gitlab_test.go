package gitlab

import (
	"testing"

	"github.com/specgate/doc-registry/internal/integrations/coretypes"
)

func TestGitLabMergeRequestNormalizationCapturesHeadSHA(t *testing.T) {
	t.Parallel()
	nd, err := ParseAndNormalize(`{
		"object_kind":"merge_request",
		"project":{"id":1,"path_with_namespace":"acme/repo"},
		"object_attributes":{"id":12,"iid":7,"state":"opened","source_branch":"feature/SG-123","target_branch":"main","merge_commit_sha":"provider-merge","last_commit":{"id":"submitted-head"}}
	}`, "event-1")
	if err != nil {
		t.Fatal(err)
	}
	if nd.EventType != coretypes.WebhookEventMergeRequest {
		t.Fatalf("event type = %q", nd.EventType)
	}
	if nd.HeadSHA != "submitted-head" || nd.MergeCommitSHA != "provider-merge" {
		t.Fatalf("head/merge = %q/%q", nd.HeadSHA, nd.MergeCommitSHA)
	}
}

func TestGitLabIssueHookIsUnsupported(t *testing.T) {
	t.Parallel()
	nd, err := ParseAndNormalize(`{"object_kind":"issue","project":{"id":1},"object_attributes":{"id":12,"iid":7,"state":"opened"}}`, "event-1")
	if err != nil {
		t.Fatal(err)
	}
	if nd.EventType != "" {
		t.Fatalf("event type = %q, want unsupported", nd.EventType)
	}
}
