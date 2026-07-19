package integrations

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/specgate/doc-registry/internal/integrations/coretypes"
)

// deleteManagedProviderWebhook removes the provider-side managed webhook for a
// resource (via the provider's WebhookDriver) before the local row is deleted.
// A no-op when the resource has no managed hook or the provider has no driver.
func (s *Service) deleteManagedProviderWebhook(ctx context.Context, integration *Integration, resource *Resource) error {
	if integration == nil || resource == nil {
		return fmt.Errorf("%w: integration and resource are required", ErrValidation)
	}
	cfg := managedWebhookConfigFromJSON(resource.ConfigJSON)
	if strings.TrimSpace(cfg.ProviderHookID) == "" {
		return nil
	}
	driver, ok := coretypes.LookupWebhookDriver(integration.Provider)
	if !ok || !driver.SupportsManagedWebhook() {
		return nil
	}
	token, err := s.ResolveAPIToken(ctx, integration.ID)
	if err != nil {
		return err
	}
	if err := driver.DeleteWebhook(ctx, cfg.ProviderHookID, s.providerTarget(integration, resource, token)); err != nil {
		return fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	return nil
}

func managedWebhookConfigFromJSON(raw string) managedWebhookConfig {
	var cfg managedWebhookConfig
	if strings.TrimSpace(raw) == "" {
		return cfg
	}
	_ = json.Unmarshal([]byte(raw), &cfg)
	return cfg
}
