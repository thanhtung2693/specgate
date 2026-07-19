package integrations

import "testing"

func TestProviderSupportsAPIToken_UsesExplicitProviderBoundary(t *testing.T) {
	t.Parallel()
	for _, provider := range []string{ProviderGitHub, ProviderGitLab, ProviderLinear} {
		if !providerSupportsAPIToken(provider) {
			t.Errorf("providerSupportsAPIToken(%q) = false, want true", provider)
		}
	}
	if providerSupportsAPIToken("jira") {
		t.Fatal("unknown provider supports API token")
	}
}
