package integrations

import (
	"github.com/specgate/doc-registry/internal/integrations/coretypes"
)

// providerSupportsAPIToken reports whether a provider stores a recoverable
// outbound API token. It derives from the tracker-adapter registry — the single
// source of truth for outbound-capable providers — so registering a new tracker
// adapter automatically enables its token endpoint. SetApiToken / ResolveAPIToken
// gate on it; both stay on *Service, so this predicate lives in the parent. Each
// provider registers its adapter from its subpackage's init() (the github,
// gitlab, and linear packages the parent imports) — no edits are needed to add a
// provider.
func providerSupportsAPIToken(provider string) bool {
	_, ok := coretypes.LookupTracker(provider)
	return ok
}

// integrationConfigString is a thin in-package wrapper over coretypes so the
// existing call sites read unchanged.
func integrationConfigString(integration *Integration, key string) string {
	return coretypes.IntegrationConfigString(integration, key)
}
