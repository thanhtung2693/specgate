package integrations

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/specgate/doc-registry/internal/integrations/coretypes"
)

type managedWebhookConfig struct {
	URL            string `json:"webhook_url,omitempty"`
	ProviderHookID string `json:"provider_webhook_id,omitempty"`
	Status         string `json:"webhook_status,omitempty"`
	LastError      string `json:"webhook_last_error,omitempty"`
}

func resourceWebhookURL(baseURL, integrationID, resourceID, provider string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	slug := provider
	if slug != ProviderGitHub && slug != ProviderLinear {
		slug = ProviderGitLab
	}
	return fmt.Sprintf("%s/integrations/%s/resources/%s/%s/webhook", base, integrationID, resourceID, slug)
}

// CreateResourceAndProvisionWebhook creates the resource and, for a provider whose
// WebhookDriver supports managed webhooks (GitLab/GitHub) and that has outbound
// auth, registers the provider webhook via the driver and stores the per-resource
// secret. A provisioning failure rolls the resource back so no orphan is left.
func (s *Service) CreateResourceAndProvisionWebhook(ctx context.Context, integrationID string, in Resource, registryBaseURL string) (*Resource, error) {
	integrationID = strings.TrimSpace(integrationID)
	if integrationID == "" {
		return nil, fmt.Errorf("%w: integration_id is required", ErrValidation)
	}
	integration, err := s.integrations.GetIntegration(ctx, integrationID)
	if err != nil {
		return nil, err
	}
	in.IntegrationID = integrationID
	if err := normalizeResource(&in); err != nil {
		return nil, err
	}
	if !webhookableResourceType(integration.Provider, in.ResourceType) {
		return nil, fmt.Errorf("%w: resource type %q is not supported by %s integrations", ErrValidation, in.ResourceType, integration.Provider)
	}
	created, err := s.resources.CreateResource(ctx, in)
	if err != nil {
		return nil, err
	}

	driver, ok := coretypes.LookupWebhookDriver(integration.Provider)
	if !ok || !driver.SupportsManagedWebhook() {
		return created, nil
	}
	if !integration.HasAPIToken && !integration.HasOAuthToken {
		return created, nil
	}

	// provisionFailed rolls the just-created resource back so a failed webhook
	// registration never leaves an orphan resource (no secret → 401s every
	// inbound delivery). The original error wins.
	var providerHookID string
	var target ProviderTarget
	provisionFailed := func(err error) (*Resource, error) {
		if providerHookID != "" {
			_ = driver.DeleteWebhook(ctx, providerHookID, target)
		}
		if delErr := s.resources.DeleteResource(ctx, integrationID, created.ID); delErr != nil {
			return nil, fmt.Errorf("%w (and rollback failed: %v)", err, delErr)
		}
		return nil, err
	}

	token, err := s.ResolveAPIToken(ctx, integrationID)
	if err != nil {
		return provisionFailed(err)
	}
	webhookURL := resourceWebhookURL(registryBaseURL, integrationID, created.ID, integration.Provider)
	if !strings.HasPrefix(webhookURL, "http://") && !strings.HasPrefix(webhookURL, "https://") {
		return provisionFailed(fmt.Errorf("%w: public registry base url is required to provision provider webhooks", ErrValidation))
	}
	target = s.providerTarget(integration, created, token)
	result, err := driver.ProvisionWebhook(ctx, ProvisionInput{
		Target:     target,
		WebhookURL: webhookURL,
	})
	if err != nil {
		return provisionFailed(fmt.Errorf("%w: %v", ErrUpstream, err))
	}
	providerHookID = result.ProviderHookID
	enc, err := EncryptSecret(result.Secret)
	if err != nil {
		return provisionFailed(err)
	}
	if err := s.resources.UpdateResourceWebhookSecretEncrypted(ctx, integrationID, created.ID, enc); err != nil {
		return provisionFailed(err)
	}
	mergedConfigJSON := mergeResourceWebhookConfig(created.ConfigJSON, managedWebhookConfig{
		URL:            webhookURL,
		ProviderHookID: result.ProviderHookID,
		Status:         "connected",
	})
	if err := s.resources.UpdateResourceConfigJSON(ctx, integrationID, created.ID, mergedConfigJSON); err != nil {
		return provisionFailed(err)
	}
	created.ConfigJSON = mergedConfigJSON
	created.WebhookSecretEncrypted = enc
	created.HasWebhookSecret = true
	return created, nil
}

// ReprovisionResourceWebhook (re)registers the provider webhook for an EXISTING
// resource — for resources created before auto-provisioning existed, ones where
// provisioning was skipped (e.g. no token at the time), or to re-register after
// the public webhook base URL changed. Unlike create, it never deletes the
// resource on failure: it records webhook_status="error" + webhook_last_error in
// the resource config so the UI can surface it, and returns the error.
//
// If the resource already carries a managed provider_webhook_id, that hook is
// best-effort deleted first so re-running does not leave a duplicate in the
// provider.
func (s *Service) ReprovisionResourceWebhook(ctx context.Context, integrationID, resourceID, registryBaseURL string) (*Resource, error) {
	integrationID = strings.TrimSpace(integrationID)
	resourceID = strings.TrimSpace(resourceID)
	if integrationID == "" || resourceID == "" {
		return nil, fmt.Errorf("%w: integration_id and resource_id are required", ErrValidation)
	}
	integration, err := s.integrations.GetIntegration(ctx, integrationID)
	if err != nil {
		return nil, err
	}
	resource, err := s.resources.GetResource(ctx, integrationID, resourceID)
	if err != nil {
		return nil, err
	}

	driver, ok := coretypes.LookupWebhookDriver(integration.Provider)
	if !ok || !driver.SupportsManagedWebhook() {
		return nil, fmt.Errorf("%w: %s does not support managed webhooks", ErrValidation, integration.Provider)
	}
	if !webhookableResourceType(integration.Provider, resource.ResourceType) {
		return nil, fmt.Errorf("%w: resource type %q is not supported by %s integrations", ErrValidation, resource.ResourceType, integration.Provider)
	}
	if !integration.HasAPIToken && !integration.HasOAuthToken {
		return nil, fmt.Errorf("%w: integration has no API or OAuth token to register a webhook", ErrValidation)
	}

	token, err := s.ResolveAPIToken(ctx, integrationID)
	if err != nil {
		return nil, err
	}
	webhookURL := resourceWebhookURL(registryBaseURL, integrationID, resourceID, integration.Provider)
	if !strings.HasPrefix(webhookURL, "http://") && !strings.HasPrefix(webhookURL, "https://") {
		return nil, fmt.Errorf("%w: a public registry base url is required to register provider webhooks", ErrValidation)
	}

	target := s.providerTarget(integration, resource, token)
	// Best-effort delete of a prior managed hook so re-running doesn't duplicate.
	if oldHookID := existingProviderHookID(resource.ConfigJSON); oldHookID != "" {
		_ = driver.DeleteWebhook(ctx, oldHookID, target)
	}

	result, err := driver.ProvisionWebhook(ctx, ProvisionInput{Target: target, WebhookURL: webhookURL})
	if err != nil {
		// Persist the failure on the resource (kept) so the UI can show why.
		failCfg := mergeResourceWebhookConfig(resource.ConfigJSON, managedWebhookConfig{
			Status:    "error",
			LastError: err.Error(),
		})
		_ = s.resources.UpdateResourceConfigJSON(ctx, integrationID, resourceID, failCfg)
		return nil, fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	postProvisionFailed := func(err error) (*Resource, error) {
		if result.ProviderHookID != "" {
			_ = driver.DeleteWebhook(ctx, result.ProviderHookID, target)
		}
		failCfg := mergeResourceWebhookConfig(resource.ConfigJSON, managedWebhookConfig{
			Status:    "error",
			LastError: err.Error(),
		})
		_ = s.resources.UpdateResourceConfigJSON(ctx, integrationID, resourceID, failCfg)
		return nil, err
	}

	enc, err := EncryptSecret(result.Secret)
	if err != nil {
		return postProvisionFailed(err)
	}
	if err := s.resources.UpdateResourceWebhookSecretEncrypted(ctx, integrationID, resourceID, enc); err != nil {
		return postProvisionFailed(err)
	}
	okCfg := mergeResourceWebhookConfig(resource.ConfigJSON, managedWebhookConfig{
		URL:            webhookURL,
		ProviderHookID: result.ProviderHookID,
		Status:         "connected",
	})
	if err := s.resources.UpdateResourceConfigJSON(ctx, integrationID, resourceID, okCfg); err != nil {
		return postProvisionFailed(err)
	}
	resource.ConfigJSON = okCfg
	resource.WebhookSecretEncrypted = enc
	resource.HasWebhookSecret = true
	return resource, nil
}

// existingProviderHookID extracts a previously-registered provider_webhook_id
// from a resource config blob, or "" when none.
func existingProviderHookID(configJSON string) string {
	trimmed := strings.TrimSpace(configJSON)
	if trimmed == "" {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(trimmed), &m); err != nil {
		return ""
	}
	if v, ok := m["provider_webhook_id"].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// providerTarget builds the provider-neutral ProviderTarget a webhook driver
// needs to manage a webhook (raw base_url + resource selectors + the decrypted
// outbound credential); each driver derives its own API root and project/repo.
func (s *Service) providerTarget(integration *Integration, resource *Resource, token string) ProviderTarget {
	return ProviderTarget{
		BaseURL:     integration.BaseURL,
		Token:       token,
		Bearer:      strings.TrimSpace(integration.AuthMethod) == AuthMethodOAuth,
		ExternalID:  resource.ExternalID,
		ExternalKey: resource.ExternalKey,
	}
}

// webhookableResourceType reports whether a resource matches its provider's
// webhook contract. GitHub uses repos, GitLab uses projects, and Linear uses
// teams (each webhook is team-scoped in the Linear data model). per spec §14.
func webhookableResourceType(provider, resourceType string) bool {
	switch provider {
	case ProviderGitHub:
		return resourceType == ResourceTypeRepo
	case ProviderGitLab:
		return resourceType == ResourceTypeProject
	case ProviderLinear:
		return resourceType == ResourceTypeTeam
	default:
		return false
	}
}

func mergeResourceWebhookConfig(existing string, cfg managedWebhookConfig) string {
	dst := map[string]any{}
	if trimmed := strings.TrimSpace(existing); trimmed != "" {
		_ = json.Unmarshal([]byte(trimmed), &dst)
	}
	if cfg.URL != "" {
		dst["webhook_url"] = cfg.URL
	}
	if cfg.ProviderHookID != "" {
		dst["provider_webhook_id"] = cfg.ProviderHookID
	}
	if cfg.Status != "" {
		dst["webhook_status"] = cfg.Status
	}
	if cfg.LastError != "" {
		dst["webhook_last_error"] = cfg.LastError
	} else if cfg.Status == "connected" {
		// A successful (re)provision clears any prior error.
		delete(dst, "webhook_last_error")
	}
	raw, err := json.Marshal(dst)
	if err != nil {
		return existing
	}
	return string(raw)
}

func removeManagedWebhookConfig(existing string) string {
	dst := map[string]any{}
	if trimmed := strings.TrimSpace(existing); trimmed != "" {
		_ = json.Unmarshal([]byte(trimmed), &dst)
	}
	delete(dst, "webhook_url")
	delete(dst, "provider_webhook_id")
	delete(dst, "webhook_status")
	delete(dst, "webhook_last_error")
	raw, err := json.Marshal(dst)
	if err != nil {
		return existing
	}
	return string(raw)
}
