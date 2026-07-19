package integrations

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/specgate/doc-registry/internal/integrations/coretypes"
	_ "github.com/specgate/doc-registry/internal/integrations/github"
	_ "github.com/specgate/doc-registry/internal/integrations/gitlab"
	"github.com/specgate/doc-registry/internal/webhookqueue"
)

// HandleResourceWebhook is the resource-scoped webhook entry: it authenticates
// the delivery synchronously (bad signature → 401, never queued), then either
// enqueues it for async processing or — when no enqueuer is wired — runs the
// commit pipeline inline.
func (s *Service) HandleResourceWebhook(ctx context.Context, integrationID, resourceID, provider string, in InboundWebhook) (*GitLabWebhookResult, error) {
	driver, integration, resource, err := s.loadAndVerifyResource(ctx, provider, integrationID, resourceID, in)
	if err != nil {
		return nil, err
	}
	ctx, err = bindIntegrationWorkspace(ctx, integration)
	if err != nil {
		return nil, err
	}
	if s.enqueuer != nil {
		workspaceID := WorkspaceID(ctx)
		if workspaceID == "" {
			workspaceID = strings.TrimSpace(integration.WorkspaceID)
		}
		if err := s.enqueuer.EnqueueWebhookDelivery(ctx, webhookqueue.Task{
			WorkspaceID:   workspaceID,
			Kind:          webhookqueue.KindResource,
			Provider:      provider,
			IntegrationID: integrationID,
			ResourceID:    resourceID,
			Inbound:       in,
		}); err != nil {
			return nil, fmt.Errorf("%w: enqueue webhook delivery: %v", ErrUpstream, err)
		}
		return &GitLabWebhookResult{
			IntegrationID: integration.ID,
			ResourceID:    resource.ID,
			Status:        WebhookStatusPending,
			IgnoredReason: "queued",
		}, nil
	}
	return s.processLoadedResourceDelivery(ctx, driver, integration, resource, provider, in)
}

// processResourceDelivery is the worker-side entry: it re-loads the integration +
// resource and runs the commit pipeline. It does NOT re-authenticate — the
// delivery was verified at receive time before enqueue.
func (s *Service) processResourceDelivery(ctx context.Context, provider, integrationID, resourceID string, in InboundWebhook) (*GitLabWebhookResult, error) {
	driver, ok := coretypes.LookupWebhookDriver(provider)
	if !ok {
		return nil, fmt.Errorf("%w: no webhook driver for provider %q", ErrValidation, provider)
	}
	resourceType := ResourceTypeRepo
	if provider == ProviderGitLab {
		resourceType = ResourceTypeProject
	}
	integration, resource, err := s.loadWebhookResource(ctx, integrationID, resourceID, provider, resourceType)
	if err != nil {
		return nil, err
	}
	return s.processLoadedResourceDelivery(ctx, driver, integration, resource, provider, in)
}

// processLoadedResourceDelivery normalizes the delivery, confirms it targets this
// resource, and runs the shared commit pipeline. Shared by the inline (sync) path
// and the async worker.
func (s *Service) processLoadedResourceDelivery(ctx context.Context, driver coretypes.WebhookDriver, integration *Integration, resource *Resource, provider string, in InboundWebhook) (*GitLabWebhookResult, error) {
	nd, comment, ignoreReason, err := driver.Normalize(in)
	if err != nil {
		return nil, err
	}
	if ignoreReason != "" {
		return &GitLabWebhookResult{Status: WebhookStatusIgnored, IgnoredReason: ignoreReason}, nil
	}
	mismatchReason := "resource_repository_mismatch"
	if provider == ProviderGitLab {
		mismatchReason = "resource_project_mismatch"
	}
	if comment != nil {
		if !resourceMatchesProviderTarget(resource, strconv.Itoa(comment.ProjectID), comment.ProjectKey) {
			return &GitLabWebhookResult{Status: WebhookStatusIgnored, IgnoredReason: mismatchReason}, nil
		}
		return s.handleCommentScopeDrift(ctx, integration, resource, *comment)
	}
	if !resourceMatchesProviderTarget(resource, strconv.Itoa(nd.ProjectID), nd.ProjectKey) {
		return &GitLabWebhookResult{Status: WebhookStatusIgnored, IgnoredReason: mismatchReason}, nil
	}
	return s.commitDelivery(ctx, integration, resource, *nd)
}

// loadAndVerifyResource loads the integration + resource and authenticates the
// delivery against the resource's stored secret via the provider's driver.
func (s *Service) loadAndVerifyResource(ctx context.Context, provider, integrationID, resourceID string, in InboundWebhook) (coretypes.WebhookDriver, *Integration, *Resource, error) {
	driver, ok := coretypes.LookupWebhookDriver(provider)
	if !ok {
		return nil, nil, nil, fmt.Errorf("%w: no webhook driver for provider %q", ErrValidation, provider)
	}
	resourceType := ResourceTypeRepo
	if provider == ProviderGitLab {
		resourceType = ResourceTypeProject
	}
	integration, resource, err := s.loadWebhookResource(ctx, integrationID, resourceID, provider, resourceType)
	if err != nil {
		return nil, nil, nil, err
	}
	secret, err := resolveResourceWebhookSecret(resource)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("%w: cannot decrypt webhook secret", ErrUnauthorized)
	}
	if err := driver.VerifyDelivery(secret, in); err != nil {
		return nil, nil, nil, err
	}
	return driver, integration, resource, nil
}

func (s *Service) loadWebhookResource(ctx context.Context, integrationID string, resourceID string, provider string, resourceType string) (*Integration, *Resource, error) {
	integrationID = strings.TrimSpace(integrationID)
	resourceID = strings.TrimSpace(resourceID)
	if integrationID == "" {
		return nil, nil, fmt.Errorf("%w: integration_id is required", ErrValidation)
	}
	if resourceID == "" {
		return nil, nil, fmt.Errorf("%w: resource_id is required", ErrValidation)
	}
	integration, err := s.integrations.GetIntegration(ctx, integrationID)
	if err != nil {
		return nil, nil, err
	}
	if integration.Provider != provider {
		return nil, nil, fmt.Errorf("%w: integration is not a %s provider", ErrValidation, provider)
	}
	if integration.Status != StatusConnected {
		return nil, nil, fmt.Errorf("%w: integration is not connected", ErrValidation)
	}
	resource, err := s.resources.GetResource(ctx, integrationID, resourceID)
	if err != nil {
		return nil, nil, err
	}
	if resource.ResourceType != resourceType {
		return nil, nil, fmt.Errorf("%w: resource is not a %s", ErrValidation, resourceType)
	}
	return integration, resource, nil
}

func resolveResourceWebhookSecret(resource *Resource) (string, error) {
	if resource == nil || strings.TrimSpace(resource.WebhookSecretEncrypted) == "" {
		return "", nil
	}
	return DecryptSecret(resource.WebhookSecretEncrypted)
}

func resourceMatchesProviderTarget(resource *Resource, externalID string, externalKey string) bool {
	if resource == nil {
		return false
	}
	if strings.TrimSpace(resource.ExternalID) != "" && strings.TrimSpace(externalID) != "" && strings.TrimSpace(resource.ExternalID) == strings.TrimSpace(externalID) {
		return true
	}
	if strings.TrimSpace(resource.ExternalKey) != "" && strings.TrimSpace(externalKey) != "" && strings.EqualFold(strings.TrimSpace(resource.ExternalKey), strings.TrimSpace(externalKey)) {
		return true
	}
	return false
}
