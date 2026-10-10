package local

import "testing"

func TestAcceptanceFreshnessKeepsCheckoutScopeAndUnavailableDistinct(t *testing.T) {
	for _, tc := range []struct{ scope, storedID, currentID, storedRepo, currentRepo, fingerprint, want string }{
		{"local_checkout", "one", "one", "", "", "available:head:sha256:digest", "matching_endpoints"},
		{"local_checkout", "one", "two", "", "", "available:head:sha256:digest", "noncomparable"},
		{"local_checkout", "one", "one", "", "", "available:head:sha256:changed", "stale"},
		{"shared_repository", "one", "two", "repo", "repo", "available:head:sha256:digest", "matching_endpoints"},
		{"shared_repository", "one", "one", "repo", "other", "available:head:sha256:digest", "noncomparable"},
		{"shared_repository", "one", "one", "repo", "", "available:head:sha256:digest", "unavailable"},
		{"local_checkout", "one", "one", "", "", "unavailable::", "unavailable"},
	} {
		report := map[string]any{"git_receipt": map[string]any{"availability": "available", "freshness_scope": tc.scope, "checkout_id": tc.storedID, "repository": tc.storedRepo, "head_revision": "head", "diff_digest": "sha256:digest"}}
		got := acceptanceCheckoutFreshness(report, AcceptanceOptions{CheckoutID: tc.currentID, CheckoutRepository: tc.currentRepo, CheckoutFingerprint: tc.fingerprint})
		if got != tc.want {
			t.Fatalf("%+v: got %s", tc, got)
		}
	}
}
