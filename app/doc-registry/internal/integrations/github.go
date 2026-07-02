package integrations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/specgate/doc-registry/internal/integrations/github"
)

// HandleGitHubWebhook is the GitHub peer of HandleGitLabWebhook. It verifies
// the HMAC-SHA256 `X-Hub-Signature-256` header against the integration's
// recoverable webhook secret, filters to pull_request events, resolves the
// repository resource, and runs the shared commitDelivery pipeline. The
// provider-specific parse + normalize is delegated to the github subpackage;
// repo identity is read off the returned NormalizedDelivery.
func (s *Service) HandleGitHubWebhook(ctx context.Context, integrationID string, in GitHubWebhookInput) (*GitLabWebhookResult, error) {
	integrationID = strings.TrimSpace(integrationID)
	if integrationID == "" {
		return nil, fmt.Errorf("%w: integration_id is required", ErrValidation)
	}
	integration, err := s.integrations.GetIntegration(ctx, integrationID)
	if err != nil {
		return nil, err
	}
	if integration.Provider != ProviderGitHub {
		return nil, fmt.Errorf("%w: integration is not a github provider", ErrValidation)
	}
	// GitHub signs the body with HMAC-SHA256 using the shared secret. Verify
	// against the per-integration secret (self-served + rotatable); an unset
	// secret refuses the call rather than acting as an open relay.
	secret, err := s.resolveWebhookSecret(integration)
	if err != nil {
		return nil, fmt.Errorf("%w: cannot decrypt github webhook secret", ErrUnauthorized)
	}
	if secret == "" {
		return nil, fmt.Errorf("%w: no github webhook secret configured", ErrUnauthorized)
	}
	if !VerifyGitHubSignature(secret, []byte(in.PayloadJSON), in.Signature) {
		return nil, fmt.Errorf("%w: github signature mismatch", ErrUnauthorized)
	}

	externalEventID := strings.TrimSpace(in.DeliveryID)
	if externalEventID == "" {
		sum := sha256.Sum256([]byte(in.PayloadJSON))
		externalEventID = "sha256:" + hex.EncodeToString(sum[:])
	}

	switch strings.ToLower(strings.TrimSpace(in.EventHeader)) {
	case "issue_comment":
		return s.handleGitHubCommentWebhook(ctx, integration, in.PayloadJSON, externalEventID)
	case "workflow_run":
		return s.handleGitHubWorkflowRunWebhook(ctx, integration, in.PayloadJSON, externalEventID)
	case "pull_request":
		// handled below
	default:
		return &GitLabWebhookResult{Status: WebhookStatusIgnored, IgnoredReason: "unsupported_github_event"}, nil
	}
	nd, err := github.ParseAndNormalize(in.PayloadJSON, externalEventID)
	if err != nil {
		return nil, err
	}

	repoID := strconv.Itoa(nd.ProjectID)
	matched, resource, err := s.resources.FindResourceByProvider(ctx, ProviderGitHub, ResourceTypeRepo, repoID, nd.ProjectKey)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return &GitLabWebhookResult{Status: WebhookStatusIgnored, IgnoredReason: "unmatched_github_repository"}, nil
		}
		return nil, err
	}
	if matched == nil || matched.ID != integration.ID {
		return &GitLabWebhookResult{Status: WebhookStatusIgnored, IgnoredReason: "unmatched_github_repository"}, nil
	}

	return s.commitDelivery(ctx, integration, resource, nd)
}

func (s *Service) handleGitHubCommentWebhook(ctx context.Context, integration *Integration, payloadJSON string, externalEventID string) (*GitLabWebhookResult, error) {
	comment, err := github.ParseCommentScopeDrift(payloadJSON, externalEventID)
	if err != nil {
		return nil, err
	}
	repoID := strconv.Itoa(comment.ProjectID)
	matched, resource, err := s.resources.FindResourceByProvider(ctx, ProviderGitHub, ResourceTypeRepo, repoID, comment.ProjectKey)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return &GitLabWebhookResult{Status: WebhookStatusIgnored, IgnoredReason: "unmatched_github_repository"}, nil
		}
		return nil, err
	}
	if matched == nil || matched.ID != integration.ID {
		return &GitLabWebhookResult{Status: WebhookStatusIgnored, IgnoredReason: "unmatched_github_repository"}, nil
	}
	return s.handleCommentScopeDrift(ctx, integration, resource, comment)
}

func (s *Service) handleGitHubWorkflowRunWebhook(ctx context.Context, integration *Integration, payloadJSON string, externalEventID string) (*GitLabWebhookResult, error) {
	nd, err := github.ParseWorkflowRun(payloadJSON, externalEventID)
	if err != nil {
		// Non-success conclusion is ErrValidation — silently ignore.
		if errors.Is(err, ErrValidation) {
			return &GitLabWebhookResult{Status: WebhookStatusIgnored, IgnoredReason: "workflow_run_not_success"}, nil
		}
		return nil, err
	}
	repoID := strconv.Itoa(nd.ProjectID)
	matched, resource, err := s.resources.FindResourceByProvider(ctx, ProviderGitHub, ResourceTypeRepo, repoID, nd.ProjectKey)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return &GitLabWebhookResult{Status: WebhookStatusIgnored, IgnoredReason: "unmatched_github_repository"}, nil
		}
		return nil, err
	}
	if matched == nil || matched.ID != integration.ID {
		return &GitLabWebhookResult{Status: WebhookStatusIgnored, IgnoredReason: "unmatched_github_repository"}, nil
	}
	return s.commitCIRun(ctx, integration, resource, nd)
}
