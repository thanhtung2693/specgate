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

func TestAcceptanceHistoricalSharedReceiptRequiresFullEndpointMatch(t *testing.T) {
	report := map[string]any{"git_receipt": map[string]any{
		"availability": "available", "freshness_scope": "shared_repository",
		"checkout_generation": "source", "repository": "repo",
		"branch": "main", "base_revision": "base", "head_revision": "head", "diff_digest": "source-dirty",
	}}
	opts := AcceptanceOptions{CheckoutGeneration: "receiving", CheckoutRepository: "repo", CheckoutBranch: "main", CheckoutBaseRevision: "base", CheckoutFingerprint: "available:head:receiver-dirty"}
	if got := acceptanceCheckoutFreshness(report, opts); got != "noncomparable" {
		t.Fatalf("working-tree mismatch comparison = %s", got)
	}
	opts.CheckoutFingerprint = "available:head:source-dirty"
	if got := acceptanceCheckoutFreshness(report, opts); got != "matching_endpoints" {
		t.Fatalf("full endpoint comparison = %s", got)
	}
	stored := report["git_receipt"].(map[string]any)
	stored["base_revision"] = ""
	if got := acceptanceCheckoutFreshness(report, opts); got != "unavailable" {
		t.Fatalf("missing source base comparison = %s", got)
	}
	stored["base_revision"] = "base"
	stored["branch"] = ""
	if got := acceptanceCheckoutFreshness(report, opts); got != "stale" {
		t.Fatalf("named versus detached branch comparison = %s", got)
	}
	stored["branch"] = "main"
	stored["checkout_generation"] = ""
	opts.CheckoutDirty = true
	if got := acceptanceCheckoutFreshness(report, opts); got != "noncomparable" {
		t.Fatalf("pre-generation dirty endpoint comparison = %s", got)
	}
	opts.CheckoutDirty = false
	stored["checkout_generation"] = "source"
	opts.CheckoutFingerprint = "available:different-head:receiver-dirty"
	if got := acceptanceCheckoutFreshness(report, opts); got != "stale" {
		t.Fatalf("different HEAD comparison = %s", got)
	}
}
