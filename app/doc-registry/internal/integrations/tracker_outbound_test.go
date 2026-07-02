package integrations

import (
	"testing"

	"github.com/specgate/doc-registry/internal/integrations/coretypes"
)

func TestProviderSupportsAPIToken_IncludesGitHub(t *testing.T) {
	t.Parallel()
	if !providerSupportsAPIToken(ProviderGitHub) {
		t.Fatal("github should support an outbound api token (it has a tracker adapter)")
	}
}

func TestTrackerAdaptersRegistryMembership(t *testing.T) {
	t.Parallel()
	want := []string{ProviderLinear, ProviderGitLab, ProviderGitHub}
	for _, provider := range want {
		adapter, ok := coretypes.LookupTracker(provider)
		if !ok {
			t.Errorf("tracker registry missing %q", provider)
			continue
		}
		if adapter == nil {
			t.Errorf("tracker registry[%q] is nil", provider)
		}
	}
}
