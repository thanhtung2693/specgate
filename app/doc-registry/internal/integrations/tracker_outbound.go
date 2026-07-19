package integrations

func providerSupportsAPIToken(provider string) bool {
	switch provider {
	case ProviderGitHub, ProviderGitLab, ProviderLinear:
		return true
	default:
		return false
	}
}
