package integrations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/specgate/doc-registry/internal/integrations/gitlab"
)

// HandleGitLabWebhook verifies the inbound webhook token against the
// integration's stored secret hash, parses + normalizes the payload via the
// gitlab subpackage, and routes on the normalized event type: an Issue Hook
// (tracker_issue) goes to the augment-only handleGitLabIssueWebhook; a merge
// request resolves the project resource and runs the shared commitDelivery
// pipeline; anything else is ignored. Project identity is read off the returned
// NormalizedDelivery.
func (s *Service) HandleGitLabWebhook(ctx context.Context, integrationID string, in GitLabWebhookInput) (*GitLabWebhookResult, error) {
	integrationID = strings.TrimSpace(integrationID)
	if integrationID == "" {
		return nil, fmt.Errorf("%w: integration_id is required", ErrValidation)
	}
	integration, err := s.integrations.GetIntegration(ctx, integrationID)
	if err != nil {
		return nil, err
	}
	if integration.Provider != ProviderGitLab {
		return nil, fmt.Errorf("%w: integration is not a gitlab provider", ErrValidation)
	}
	// GitLab signs each delivery with the per-integration signing token (the
	// whsec_ value the user pasted from GitLab) per the Standard Webhooks spec:
	// HMAC-SHA256 over {webhook-id}.{webhook-timestamp}.{body}, sent in
	// webhook-signature. An unset token refuses the call rather than acting as an
	// open relay. The timestamp recency check defeats replay of a captured body.
	signingToken, err := s.resolveWebhookSecret(integration)
	if err != nil {
		return nil, fmt.Errorf("%w: cannot decrypt gitlab signing token", ErrUnauthorized)
	}
	if signingToken == "" {
		return nil, fmt.Errorf("%w: no gitlab signing token configured", ErrUnauthorized)
	}
	if !webhookTimestampWithinTolerance(in.WebhookTimestamp, webhookReplayTolerance) {
		return nil, fmt.Errorf("%w: gitlab webhook timestamp outside tolerance", ErrUnauthorized)
	}
	if !VerifyGitLabSigningToken(signingToken, in.WebhookID, in.WebhookTimestamp, []byte(in.PayloadJSON), in.WebhookSignature) {
		return nil, fmt.Errorf("%w: gitlab webhook signature mismatch", ErrUnauthorized)
	}

	externalEventID := strings.TrimSpace(in.EventUUID)
	if externalEventID == "" {
		// GitLab versions before 16.x and self-hosted runners sometimes omit
		// X-Gitlab-Event-UUID. Without dedup, the same physical webhook
		// retried by GitLab would create duplicate governance feedback rows.
		// SHA256 the raw payload as a stable replay key — the "sha256:" prefix
		// keeps it disjoint from real GitLab UUIDs.
		sum := sha256.Sum256([]byte(in.PayloadJSON))
		externalEventID = "sha256:" + hex.EncodeToString(sum[:])
	}
	nd, err := gitlab.ParseAndNormalize(in.PayloadJSON, externalEventID)
	if err != nil {
		return nil, err
	}
	if strings.ToLower(strings.TrimSpace(in.EventHeader)) == "note hook" {
		comment, err := gitlab.ParseCommentScopeDrift(in.PayloadJSON, externalEventID)
		if err != nil {
			return nil, err
		}
		projectID := strconv.Itoa(comment.ProjectID)
		matched, resource, err := s.resources.FindResourceByProvider(ctx, ProviderGitLab, ResourceTypeProject, projectID, comment.ProjectKey)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return &GitLabWebhookResult{Status: WebhookStatusIgnored, IgnoredReason: "unmatched_gitlab_project"}, nil
			}
			return nil, err
		}
		if matched == nil || matched.ID != integration.ID {
			return &GitLabWebhookResult{Status: WebhookStatusIgnored, IgnoredReason: "unmatched_gitlab_project"}, nil
		}
		return s.handleCommentScopeDrift(ctx, integration, resource, comment)
	}
	// An Issue Hook is the tracker peer of the Linear issue webhook (optional
	// upgrade on the MR delivery floor): emit delivery.tracker_status_changed —
	// no project resource is required (the signal augments, never gates).
	if nd.EventType == WebhookEventTrackerIssue {
		return s.handleGitLabIssueWebhook(ctx, integration, nd)
	}
	if nd.EventType != WebhookEventMergeRequest {
		return &GitLabWebhookResult{Status: WebhookStatusIgnored, IgnoredReason: "unsupported_gitlab_event"}, nil
	}

	projectID := strconv.Itoa(nd.ProjectID)
	matched, resource, err := s.resources.FindResourceByProvider(ctx, ProviderGitLab, ResourceTypeProject, projectID, nd.ProjectKey)
	if err != nil {
		// Distinguish "no resource matches this project" (200 ignored, GitLab
		// will not retry) from a real DB outage (return error so GitLab retries
		// and we don't lose the event silently).
		if errors.Is(err, ErrNotFound) {
			return &GitLabWebhookResult{Status: WebhookStatusIgnored, IgnoredReason: "unmatched_gitlab_project"}, nil
		}
		return nil, err
	}
	// Re-anchor on the integration we authenticated, not whatever
	// FindResourceByProvider happened to return — defense in depth in case
	// resource-to-integration mappings get crossed.
	if matched == nil || matched.ID != integration.ID {
		return &GitLabWebhookResult{Status: WebhookStatusIgnored, IgnoredReason: "unmatched_gitlab_project"}, nil
	}

	return s.commitDelivery(ctx, integration, resource, nd)
}

// handleGitLabIssueWebhook is the GitLab tracker peer of HandleLinearWebhook: it
// reads the `fixes SPECGATE-{key}` correlation footer off the normalized issue
// delivery and emits a delivery.tracker_status_changed feedback event inside one
// transaction (record → feedback → processed), deduping on the GitLab event
// UUID. It does not require a matched project resource — trackers augment, never
// gate.
func (s *Service) handleGitLabIssueWebhook(ctx context.Context, integration *Integration, nd normalizedDelivery) (*GitLabWebhookResult, error) {
	correlationID := ""
	if refs := parseFixesRefs(nd.Description, nd.Title); len(refs) > 0 {
		correlationID = refs[0]
	}

	var result *GitLabWebhookResult
	txErr := s.txStore.WithTx(ctx, func(tx Store) error {
		created, event, err := tx.RecordWebhookEvent(ctx, WebhookEvent{
			IntegrationID:   integration.ID,
			Provider:        ProviderGitLab,
			EventType:       WebhookEventTrackerIssue,
			ExternalEventID: nd.ExternalEventID,
			CorrelationID:   correlationID,
			PayloadJSON:     nd.RawPayload,
			Status:          WebhookStatusPending,
		})
		if err != nil {
			return err
		}
		if !created {
			result = &GitLabWebhookResult{
				WebhookEventID: event.ID,
				IntegrationID:  integration.ID,
				Status:         event.Status,
				IgnoredReason:  "duplicate_webhook_event",
			}
			return nil
		}
		feedbackID, changed, err := s.recordTrackerStatusChange(ctx, tx, integration.ID, event.ID, correlationID, nd)
		if err != nil {
			return err
		}
		updated, err := tx.UpdateWebhookEventStatus(ctx, event.ID, WebhookStatusProcessed, "")
		if err != nil {
			return err
		}
		result = &GitLabWebhookResult{
			WebhookEventID:  updated.ID,
			IntegrationID:   integration.ID,
			ChangeRequestID: correlationID,
			Status:          updated.Status,
		}
		if changed {
			result.FeedbackEventIDs = []string{feedbackID}
		} else {
			result.IgnoredReason = "tracker_status_unchanged"
		}
		return nil
	})
	if txErr != nil {
		_, _, _ = s.webhookEvents.RecordWebhookEvent(ctx, WebhookEvent{
			IntegrationID:   integration.ID,
			Provider:        ProviderGitLab,
			EventType:       WebhookEventTrackerIssue,
			ExternalEventID: nd.ExternalEventID,
			CorrelationID:   correlationID,
			PayloadJSON:     nd.RawPayload,
			Status:          WebhookStatusFailed,
			Error:           txErr.Error(),
		})
		return nil, txErr
	}
	return result, nil
}
