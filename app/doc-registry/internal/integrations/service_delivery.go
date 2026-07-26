package integrations

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/specgate/doc-registry/internal/integrations/coretypes"
	"github.com/specgate/doc-registry/internal/workboard"
)

func (s *Service) CreateGovernanceFeedbackEvent(ctx context.Context, in GovernanceFeedbackEvent) (*GovernanceFeedbackEvent, error) {
	in.ChangeRequestID = strings.TrimSpace(in.ChangeRequestID)
	in.ArtifactID = strings.TrimSpace(in.ArtifactID)
	in.EventType = strings.TrimSpace(in.EventType)
	in.PayloadJSON = strings.TrimSpace(in.PayloadJSON)
	in.Reason = strings.TrimSpace(in.Reason)
	if in.ChangeRequestID == "" {
		return nil, fmt.Errorf("%w: change_request_id is required", ErrValidation)
	}
	if in.EventType == "" {
		return nil, fmt.Errorf("%w: event_type is required", ErrValidation)
	}
	if in.Status == "" {
		in.Status = FeedbackStatusReceived
	}
	return s.feedback.CreateGovernanceFeedbackEvent(ctx, in)
}

// ReconcileFeedbackEvent records a human resolution of a feedback signal. A
// resolved event is accepted; a dismissed one is rejected. Status must be one
// of the terminal feedback statuses; the originating signal remains the source
// of truth for whether the feedback has been acted on.
func (s *Service) ReconcileFeedbackEvent(ctx context.Context, id string, status string, reason string) (*GovernanceFeedbackEvent, error) {
	if status != FeedbackStatusAccepted && status != FeedbackStatusRejected {
		return nil, fmt.Errorf("invalid feedback status %q", status)
	}
	return s.feedback.UpdateGovernanceFeedbackEventStatus(ctx, id, status, reason)
}

func (s *Service) handleCommentScopeDrift(ctx context.Context, integration *Integration, resource *Resource, comment coretypes.NormalizedComment) (*GitLabWebhookResult, error) {
	correlationID := strings.TrimSpace(comment.CorrelationID)
	if correlationID == "" {
		if refs := parseWorkRefMarkers(comment.Body, comment.Title); len(refs) > 0 {
			correlationID = refs[0]
		}
	}
	resourceID := ""
	if resource != nil {
		resourceID = resource.ID
	}

	var result *GitLabWebhookResult
	txErr := s.txStore.WithTx(ctx, func(tx Store) error {
		created, event, err := tx.RecordWebhookEvent(ctx, WebhookEvent{
			IntegrationID:   integration.ID,
			ResourceID:      resourceID,
			Provider:        comment.Provider,
			EventType:       comment.EventType,
			ExternalEventID: comment.ExternalEventID,
			CorrelationID:   correlationID,
			PayloadJSON:     comment.RawPayload,
			Status:          WebhookStatusPending,
		})
		if err != nil {
			return err
		}
		event, process, err := claimWebhookEventForProcessing(ctx, tx, created, event)
		if err != nil {
			return err
		}
		if !process {
			result = &GitLabWebhookResult{
				WebhookEventID:  event.ID,
				IntegrationID:   integration.ID,
				ResourceID:      resourceID,
				ChangeRequestID: correlationID,
				Status:          event.Status,
				IgnoredReason:   "duplicate_webhook_event",
			}
			return nil
		}
		feedback, err := s.createCommentScopeDriftFeedback(ctx, tx, integration.ID, resourceID, event.ID, correlationID, comment)
		if err != nil {
			return err
		}
		updated, err := tx.UpdateWebhookEventStatus(ctx, event.ID, WebhookStatusProcessed, "")
		if err != nil {
			return err
		}
		result = &GitLabWebhookResult{
			WebhookEventID:   updated.ID,
			FeedbackEventIDs: []string{feedback.ID},
			IntegrationID:    integration.ID,
			ResourceID:       resourceID,
			ChangeRequestID:  correlationID,
			Status:           updated.Status,
		}
		return nil
	})
	if txErr != nil {
		_, _, _ = s.webhookEvents.RecordWebhookEvent(ctx, WebhookEvent{
			IntegrationID:   integration.ID,
			ResourceID:      resourceID,
			Provider:        comment.Provider,
			EventType:       comment.EventType,
			ExternalEventID: comment.ExternalEventID,
			CorrelationID:   correlationID,
			PayloadJSON:     comment.RawPayload,
			Status:          WebhookStatusFailed,
			Error:           txErr.Error(),
		})
		return nil, txErr
	}
	return result, nil
}

// commitDelivery is the provider-neutral ingest pipeline shared by GitLab and
// GitHub. Resource-scoped receivers authenticate and match the provider target
// before calling it. It records the webhook event (deduping on the provider's
// replay key), links a matched work item, and emits governance feedback in one
// Store transaction so a mid-flight crash never half-ingests.
// On transaction failure it persists a best-effort `failed` audit row outside
// the rolled-back transaction.
func (s *Service) commitDelivery(ctx context.Context, integration *Integration, resource *Resource, nd normalizedDelivery) (*GitLabWebhookResult, error) {
	correlationID := ""
	if refs := parseWorkRefMarkers(nd.Description, nd.Title); len(refs) > 0 {
		correlationID = refs[0]
	}
	var result *GitLabWebhookResult
	txErr := s.txStore.WithTx(ctx, func(tx Store) error {
		created, event, err := tx.RecordWebhookEvent(ctx, WebhookEvent{
			IntegrationID:   integration.ID,
			ResourceID:      resource.ID,
			Provider:        nd.Provider,
			EventType:       nd.EventType,
			ExternalEventID: nd.ExternalEventID,
			CorrelationID:   correlationID,
			PayloadJSON:     nd.RawPayload,
			Status:          WebhookStatusPending,
		})
		if err != nil {
			return err
		}
		event, process, err := claimWebhookEventForProcessing(ctx, tx, created, event)
		if err != nil {
			return err
		}
		if !process {
			result = &GitLabWebhookResult{
				WebhookEventID: event.ID,
				IntegrationID:  integration.ID,
				ResourceID:     resource.ID,
				Status:         event.Status,
				IgnoredReason:  "duplicate_webhook_event",
			}
			return nil
		}

		cr, matchErr := s.matchChangeRequest(ctx, nd)
		if matchErr != nil {
			feedback, feedbackErr := s.createFeedback(ctx, tx, integration.ID, resource.ID, event.ID, "", "", "", FeedbackEventPRUnmatched, nd, matchErr.Error())
			if feedbackErr != nil {
				return feedbackErr
			}
			updated, err := tx.UpdateWebhookEventStatus(ctx, event.ID, WebhookStatusProcessed, "")
			if err != nil {
				return err
			}
			result = &GitLabWebhookResult{
				WebhookEventID:   updated.ID,
				FeedbackEventIDs: []string{feedback.ID},
				IntegrationID:    integration.ID,
				ResourceID:       resource.ID,
				Status:           updated.Status,
				IgnoredReason:    "merge_request_unlinked_to_work_item",
			}
			return nil
		}

		link, err := tx.UpsertDeliveryLink(ctx, DeliveryLink{
			IntegrationID:   integration.ID,
			ResourceID:      resource.ID,
			FeatureID:       cr.FeatureID,
			ChangeRequestID: cr.ID,
			ExternalType:    ExternalTypeMergeRequest,
			ExternalID:      nd.ExternalID,
			ExternalIID:     strconv.Itoa(nd.IID),
			ExternalKey:     nd.ExternalKey,
			URL:             nd.URL,
			Title:           nd.Title,
			State:           nd.DeliveryState,
			SourceBranch:    nd.SourceBranch,
			TargetBranch:    nd.TargetBranch,
			HeadSHA:         nd.HeadSHA,
			MergeCommitSHA:  nd.MergeCommitSHA,
			LastEventID:     event.ID,
		})
		if err != nil {
			return err
		}

		// The base delivery event fires on any non-merge state change (opened,
		// reopened, synchronize, and close); a merge overrides it to pr_merged.
		// A closed-without-merge delivery additionally raises the pr_closed
		// review warning below, so a close emits both pr_opened and pr_closed.
		feedbackType := FeedbackEventPROpened
		if nd.DeliveryState == DeliveryStateMerged {
			feedbackType = FeedbackEventPRMerged
		}
		feedback, err := s.createFeedback(ctx, tx, integration.ID, resource.ID, event.ID, link.ID, cr.FeatureID, cr.ID, feedbackType, nd, "")
		if err != nil {
			return err
		}
		feedbackIDs := []string{feedback.ID}
		// A PR/MR closed without merging is a review signal (possible
		// abandonment), not a state change to act on automatically: it raises a
		// warning, and the webhook never rolls back or rewrites governance state —
		// a human reviews the warning.
		if nd.DeliveryState == DeliveryStateClosed {
			warning, err := s.createFeedback(ctx, tx, integration.ID, resource.ID, event.ID, link.ID, cr.FeatureID, cr.ID, FeedbackEventPRClosed, nd, "delivery closed without merge — review for abandonment")
			if err != nil {
				return err
			}
			feedbackIDs = append(feedbackIDs, warning.ID)
		}
		updated, err := tx.UpdateWebhookEventStatus(ctx, event.ID, WebhookStatusProcessed, "")
		if err != nil {
			return err
		}
		result = &GitLabWebhookResult{
			WebhookEventID:   updated.ID,
			DeliveryLinkID:   link.ID,
			FeedbackEventIDs: feedbackIDs,
			IntegrationID:    integration.ID,
			ResourceID:       resource.ID,
			FeatureID:        cr.FeatureID,
			ChangeRequestID:  cr.ID,
			Status:           updated.Status,
		}
		return nil
	})
	if txErr != nil {
		// Best-effort: the transaction rolled back so no event row was
		// committed. Persist a `failed` audit row outside the tx so operators
		// can see what bounced; ignore failure of the audit itself.
		_, _, _ = s.webhookEvents.RecordWebhookEvent(ctx, WebhookEvent{
			IntegrationID:   integration.ID,
			ResourceID:      resource.ID,
			Provider:        nd.Provider,
			EventType:       nd.EventType,
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

// claimWebhookEventForProcessing keeps processed and in-flight deliveries
// idempotent while allowing the queue to retry a delivery whose prior database
// transaction failed. The store claims failed rows atomically, so concurrent
// redeliveries cannot both create links or feedback.
func claimWebhookEventForProcessing(ctx context.Context, tx Store, created bool, event *WebhookEvent) (*WebhookEvent, bool, error) {
	if event == nil {
		return nil, false, fmt.Errorf("%w: webhook event was not returned", ErrValidation)
	}
	if created {
		return event, true, nil
	}
	if event.Status != WebhookStatusFailed {
		return event, false, nil
	}
	claimed, current, err := tx.ClaimFailedWebhookEvent(ctx, event.ID)
	if err != nil {
		return nil, false, err
	}
	return current, claimed, nil
}

func (s *Service) createFeedback(ctx context.Context, store Store, integrationID, resourceID, eventID, linkID, featureID, crID, eventType string, nd normalizedDelivery, reason string) (*GovernanceFeedbackEvent, error) {
	body, err := json.Marshal(map[string]any{
		"provider":         nd.Provider,
		"repository_id":    nd.ProjectID,
		"repository":       nd.ProjectKey,
		"number":           nd.IID,
		"url":              nd.URL,
		"title":            nd.Title,
		"action":           nd.Action,
		"state":            nd.RawState,
		"source_branch":    nd.SourceBranch,
		"target_branch":    nd.TargetBranch,
		"head_sha":         nd.HeadSHA,
		"merge_commit_sha": nd.MergeCommitSHA,
	})
	if err != nil {
		return nil, err
	}
	return store.CreateGovernanceFeedbackEvent(ctx, GovernanceFeedbackEvent{
		IntegrationID:   integrationID,
		ResourceID:      resourceID,
		WebhookEventID:  eventID,
		DeliveryLinkID:  linkID,
		FeatureID:       featureID,
		ChangeRequestID: crID,
		EventType:       eventType,
		PayloadJSON:     string(body),
		Status:          FeedbackStatusReceived,
		Reason:          reason,
	})
}

func (s *Service) createCommentScopeDriftFeedback(ctx context.Context, store Store, integrationID, resourceID, eventID, correlationID string, comment coretypes.NormalizedComment) (*GovernanceFeedbackEvent, error) {
	body, err := json.Marshal(map[string]any{
		"provider":       comment.Provider,
		"url":            comment.URL,
		"author":         comment.Author,
		"body":           comment.Body,
		"external_id":    comment.ExternalID,
		"external_key":   comment.ExternalKey,
		"correlation_id": correlationID,
		"title":          comment.Title,
	})
	if err != nil {
		return nil, err
	}
	reason := strings.TrimSpace(comment.Body)
	if reason == "" {
		reason = "scope drift comment received"
	}
	feedback := GovernanceFeedbackEvent{
		IntegrationID:   integrationID,
		ResourceID:      resourceID,
		WebhookEventID:  eventID,
		ChangeRequestID: correlationID,
		EventType:       FeedbackEventCommentScopeDrift,
		PayloadJSON:     string(body),
		Status:          FeedbackStatusReceived,
		Reason:          reason,
	}
	// Resolve the exact SpecGate work marker to the work item's UUID + feature so the
	// drift comment surfaces on the work item (UI queries by change_request_id).
	// No match ⇒ fall back to the raw ref already set above.
	if cr := s.resolveWorkItemRef(ctx, correlationID); cr != nil {
		feedback.ChangeRequestID = cr.ID
		feedback.FeatureID = cr.FeatureID
	}
	return store.CreateGovernanceFeedbackEvent(ctx, feedback)
}

// resolveWorkItemRef resolves an explicit SpecGate work-reference marker
// to its change request, matching by id or key.
// Returns nil (no error) when the ref is empty or matches nothing, so tracker
// feedback is still emitted unlinked (the work item match is optional).
func (s *Service) resolveWorkItemRef(ctx context.Context, ref string) *workboard.ChangeRequest {
	ref = strings.TrimSpace(ref)
	if ref == "" || s.workBoard == nil {
		return nil
	}
	items, err := s.workBoard.ListChangeRequests(ctx, false)
	if err != nil {
		return nil
	}
	for i := range items {
		if equalsRef(ref, items[i].ID) || equalsRef(ref, items[i].Key) {
			return &items[i]
		}
	}
	return nil
}

// resolveTrackerWorkItem correlates an inbound tracker (issue) webhook to its
// work item: first by the persisted handoff DeliveryLink (matched on the issue's
// immutable id/key, so it survives description edits that drop the marker), then
// by the exact work-reference marker. Returns nil when nothing matches.
func (s *Service) resolveTrackerWorkItem(ctx context.Context, store Store, integrationID string, nd normalizedDelivery, correlationID string) *workboard.ChangeRequest {
	if s.workBoard != nil {
		if link, err := store.TrackerLinkByExternal(ctx, integrationID, nd.ExternalID, nd.ExternalKey); err == nil && link != nil {
			if crID := strings.TrimSpace(link.ChangeRequestID); crID != "" {
				if cr, err := s.workBoard.GetChangeRequest(ctx, crID); err == nil && cr != nil {
					return cr
				}
			}
		}
	}
	return s.resolveWorkItemRef(ctx, correlationID)
}

// persistedTrackerState is the value written to and deduped against
// TrackerLink.TrackerState. Linear carries the full workflow state name in
// Action (e.g. "In Review"). A removed issue bypasses Action entirely; the
// Linear handler sets RawState="removed" while Action retains the last state.
func persistedTrackerState(nd normalizedDelivery) string {
	if nd.RawState == TrackerStateRemoved {
		return TrackerStateRemoved
	}
	if nd.Provider == ProviderLinear && nd.Action != "" {
		return nd.Action
	}
	return nd.RawState
}

// recordTrackerStatusChange emits delivery.tracker_status_changed for an inbound
// tracker (issue) event only when the workflow state actually changed since the
// one last seen on the persisted link — a description/title edit (same state)
// emits nothing. It also advances the link's tracker_state + lifecycle state.
// Returns the feedback id and whether a change was recorded. Issues with no
// persisted link (e.g. pre-dating link persistence) always emit.
//
// TrackerLink.TrackerState is written as the full workflow state name for Linear
// (nd.Action, e.g. "In Review") so the UI badge shows the human-readable name,
// not the coarse type. Lifecycle is provider-normalized into
// nd.TrackerLifecycle; this service never interprets state labels or prose.
func (s *Service) recordTrackerStatusChange(ctx context.Context, store Store, integrationID, resourceID, eventID, correlationID string, nd normalizedDelivery) (string, bool, error) {
	link, _ := store.TrackerLinkByExternal(ctx, integrationID, nd.ExternalID, nd.ExternalKey)
	toRecord := persistedTrackerState(nd)
	if link != nil && strings.EqualFold(strings.TrimSpace(link.TrackerState), strings.TrimSpace(toRecord)) {
		return "", false, nil
	}
	feedback, err := s.createTrackerFeedback(ctx, store, integrationID, resourceID, eventID, correlationID, nd)
	if err != nil {
		return "", false, err
	}
	if link != nil {
		link.TrackerState = toRecord
		link.State = trackerLifecycleState(nd.TrackerLifecycle)
		if _, err := store.UpsertTrackerLink(ctx, *link); err != nil {
			return "", false, err
		}
	}
	return feedback.ID, true, nil
}

// trackerLifecycleState accepts only provider-normalized lifecycle values.
func trackerLifecycleState(lifecycle string) string {
	switch lifecycle {
	case TrackerStateClosed:
		return TrackerStateClosed
	case TrackerStateRemoved:
		return TrackerStateRemoved
	default:
		return TrackerStateOpened
	}
}

func (s *Service) matchChangeRequest(ctx context.Context, nd normalizedDelivery) (*workboard.ChangeRequest, error) {
	if s.workBoard == nil {
		return nil, fmt.Errorf("%w: workboard is required for webhook matching", ErrValidation)
	}
	items, err := s.workBoard.ListChangeRequests(ctx, false)
	if err != nil {
		return nil, err
	}
	refs := parseWorkRefMarkers(nd.Description, nd.Title)
	for _, ref := range refs {
		for _, item := range items {
			if equalsRef(ref, item.ID) || equalsRef(ref, item.Key) {
				return &item, nil
			}
		}
	}
	return nil, fmt.Errorf("%w: delivery is not linked to a known work item", ErrValidation)
}

// workRefMarkerPattern is a machine-readable correlation contract. Unlike a
// prose keyword, it is exact and never interprets surrounding natural language.
