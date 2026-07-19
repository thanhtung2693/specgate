package integrations

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/specgate/doc-registry/internal/integrations/coretypes"
	"github.com/specgate/doc-registry/internal/webhookqueue"
	"github.com/specgate/doc-registry/internal/workboard"
)

type Store interface {
	IntegrationCRUDStore
	ResourceStore
	OAuthStore
	WebhookEventStore
	TrackerLinkStore
	FeedbackEventStore
	// WithTx runs fn against a transaction-scoped Store. All writes done via
	// the inner Store either commit together or roll back together.
	WithTx(ctx context.Context, fn func(Store) error) error
}

// IntegrationCRUDStore covers integration lifecycle and credential management.
type IntegrationCRUDStore interface {
	ListIntegrations(ctx context.Context) ([]Integration, error)
	GetIntegration(ctx context.Context, id string) (*Integration, error)
	CreateIntegration(ctx context.Context, in Integration) (*Integration, error)
	UpdateIntegration(ctx context.Context, in Integration) (*Integration, error)
	DeleteIntegration(ctx context.Context, id string) error
	UpdateApiTokenEncrypted(ctx context.Context, id string, encrypted string) error
}

// ResourceStore covers integration-resource CRUD and lookup.
type ResourceStore interface {
	GetResource(ctx context.Context, integrationID string, resourceID string) (*Resource, error)
	UpdateResourceWebhookSecretEncrypted(ctx context.Context, integrationID string, resourceID string, encrypted string) error
	UpdateResourceConfigJSON(ctx context.Context, integrationID string, resourceID string, configJSON string) error
	DeleteResource(ctx context.Context, integrationID string, resourceID string) error
	ListResources(ctx context.Context, integrationID string) ([]Resource, error)
	CreateResource(ctx context.Context, in Resource) (*Resource, error)
	FindResourceByProvider(ctx context.Context, provider string, resourceType string, externalID string, externalKey string) (*Integration, *Resource, error)
}

// OAuthStore covers OAuth grant and state management.
type OAuthStore interface {
	UpdateOAuthGrant(ctx context.Context, in Integration) error
	ClearOAuthGrant(ctx context.Context, id string) error
	CreateOAuthState(ctx context.Context, in OAuthState) (*OAuthState, error)
	GetOAuthState(ctx context.Context, state string) (*OAuthState, error)
	ConsumeOAuthState(ctx context.Context, state string) (*OAuthState, error)
}

// WebhookEventStore covers inbound webhook recording and delivery links.
type WebhookEventStore interface {
	RecordWebhookEvent(ctx context.Context, in WebhookEvent) (created bool, event *WebhookEvent, err error)
	ClaimFailedWebhookEvent(ctx context.Context, id string) (claimed bool, event *WebhookEvent, err error)
	UpdateWebhookEventStatus(ctx context.Context, id string, status string, errorMessage string) (*WebhookEvent, error)
	ListWebhookEvents(ctx context.Context, filter WebhookEventFilter) ([]WebhookEvent, error)
	UpsertDeliveryLink(ctx context.Context, in DeliveryLink) (*DeliveryLink, error)
	ListDeliveryLinksByChangeRequest(ctx context.Context, changeRequestID string) ([]DeliveryLink, error)
}

// TrackerLinkStore covers issue tracker ↔ work-item correlation.
type TrackerLinkStore interface {
	UpsertTrackerLink(ctx context.Context, in TrackerLink) (*TrackerLink, error)
	TrackerLinkByExternal(ctx context.Context, integrationID, externalID, externalKey string) (*TrackerLink, error)
	ListTrackerLinksByChangeRequest(ctx context.Context, changeRequestID string) ([]TrackerLink, error)
}

// ChangeRequestHandoffLocker serializes one Linear handoff without persisting a
// reservation. Production uses a PostgreSQL transaction-scoped advisory lock;
// narrow unit-test stores may omit this optional seam.
type ChangeRequestHandoffLocker interface {
	WithChangeRequestHandoffLock(context.Context, string, func(TrackerLinkStore) error) error
}

// FeedbackEventStore covers governance feedback event lifecycle.
type FeedbackEventStore interface {
	CreateGovernanceFeedbackEvent(ctx context.Context, in GovernanceFeedbackEvent) (*GovernanceFeedbackEvent, error)
	ListGovernanceFeedbackEvents(ctx context.Context, filter GovernanceFeedbackFilter) ([]GovernanceFeedbackEvent, error)
	UpdateGovernanceFeedbackEventStatus(ctx context.Context, id string, status string, reason string) (*GovernanceFeedbackEvent, error)
}

type WorkBoardStore interface {
	ListChangeRequests(context.Context, bool) ([]workboard.ChangeRequest, error)
	GetChangeRequest(context.Context, string) (*workboard.ChangeRequest, error)
	GetFeature(context.Context, string) (*workboard.Feature, error)
	ListAcceptanceCriteria(context.Context, string) ([]workboard.AcceptanceCriterion, error)
}

type OAuthAppLookup func(context.Context, string, string) (*OAuthAppConfig, error)

type Service struct {
	integrations  IntegrationCRUDStore
	resources     ResourceStore
	oauth         OAuthStore
	webhookEvents WebhookEventStore
	trackerLinks  TrackerLinkStore
	feedback      FeedbackEventStore
	txStore       Store
	handoffLocker ChangeRequestHandoffLocker
	workBoard     WorkBoardStore
	oauthApps     OAuthAppLookup
	// enqueuer offloads authenticated inbound webhooks to an async worker. Nil ⇒
	// deliveries are processed inline (synchronous fallback, e.g. when no Redis is
	// configured or in tests).
	enqueuer webhookqueue.Enqueuer
}

func NewService(store Store) *Service {
	s := &Service{
		integrations:  store,
		resources:     store,
		oauth:         store,
		webhookEvents: store,
		trackerLinks:  store,
		feedback:      store,
		txStore:       store,
	}
	if locker, ok := store.(ChangeRequestHandoffLocker); ok {
		s.handoffLocker = locker
	}
	return s
}

func NewServiceWithWorkBoard(store Store, workBoard WorkBoardStore) *Service {
	s := &Service{
		integrations:  store,
		resources:     store,
		oauth:         store,
		webhookEvents: store,
		trackerLinks:  store,
		feedback:      store,
		txStore:       store,
		workBoard:     workBoard,
	}
	if locker, ok := store.(ChangeRequestHandoffLocker); ok {
		s.handoffLocker = locker
	}
	return s
}

func (s *Service) WithOAuthAppLookup(lookup OAuthAppLookup) *Service {
	s.oauthApps = lookup
	return s
}

// WithWebhookEnqueuer enables async webhook processing: authenticated deliveries
// are enqueued and processed by a worker instead of inline. A nil enqueuer keeps
// the synchronous path.
func (s *Service) WithWebhookEnqueuer(e webhookqueue.Enqueuer) *Service {
	s.enqueuer = e
	return s
}

func (s *Service) List(ctx context.Context) ([]Integration, error) {
	return s.integrations.ListIntegrations(ctx)
}

func (s *Service) Get(ctx context.Context, id string) (*Integration, error) {
	return s.integrations.GetIntegration(ctx, strings.TrimSpace(id))
}

// CreateInput is the public surface for `POST /integrations`.
type CreateInput struct {
	WorkspaceID string
	Provider    string
	Name        string
	Status      string
	BaseURL     string
	ConfigJSON  string
}

// Create persists a new integration.
func (s *Service) Create(ctx context.Context, in CreateInput) (*Integration, error) {
	workspaceID := WorkspaceID(ctx)
	if workspaceID == "" {
		return nil, fmt.Errorf("%w: workspace_id is required", ErrValidation)
	}
	if requested := strings.TrimSpace(in.WorkspaceID); requested != "" && workspaceID != "" && requested != workspaceID {
		return nil, fmt.Errorf("%w: workspace_id", ErrValidation)
	}
	row := Integration{
		WorkspaceID: workspaceID,
		Provider:    in.Provider,
		Name:        in.Name,
		Status:      in.Status,
		BaseURL:     in.BaseURL,
		ConfigJSON:  in.ConfigJSON,
	}
	if err := normalizeIntegration(&row); err != nil {
		return nil, err
	}
	return s.integrations.CreateIntegration(ctx, row)
}

func (s *Service) Update(ctx context.Context, id string, in Integration) (*Integration, error) {
	in.ID = strings.TrimSpace(id)
	if in.ID == "" {
		return nil, fmt.Errorf("%w: id is required", ErrValidation)
	}
	if requested, workspaceID := strings.TrimSpace(in.WorkspaceID), WorkspaceID(ctx); requested != "" && workspaceID != "" && requested != workspaceID {
		return nil, fmt.Errorf("%w: workspace_id", ErrValidation)
	}
	in.WorkspaceID = WorkspaceID(ctx)
	if err := normalizeIntegration(&in); err != nil {
		return nil, err
	}
	return s.integrations.UpdateIntegration(ctx, in)
}

func (s *Service) ListResources(ctx context.Context, integrationID string) ([]Resource, error) {
	integrationID = strings.TrimSpace(integrationID)
	if integrationID == "" {
		return nil, fmt.Errorf("%w: integration_id is required", ErrValidation)
	}
	if _, err := s.integrations.GetIntegration(ctx, integrationID); err != nil {
		return nil, err
	}
	return s.resources.ListResources(ctx, integrationID)
}

func (s *Service) CreateResource(ctx context.Context, integrationID string, in Resource) (*Resource, error) {
	integrationID = strings.TrimSpace(integrationID)
	if integrationID == "" {
		return nil, fmt.Errorf("%w: integration_id is required", ErrValidation)
	}
	if _, err := s.integrations.GetIntegration(ctx, integrationID); err != nil {
		return nil, err
	}
	in.IntegrationID = integrationID
	if err := normalizeResource(&in); err != nil {
		return nil, err
	}
	return s.resources.CreateResource(ctx, in)
}

func (s *Service) DeleteResource(ctx context.Context, integrationID string, resourceID string) error {
	integrationID = strings.TrimSpace(integrationID)
	resourceID = strings.TrimSpace(resourceID)
	if integrationID == "" {
		return fmt.Errorf("%w: integration_id is required", ErrValidation)
	}
	if resourceID == "" {
		return fmt.Errorf("%w: resource_id is required", ErrValidation)
	}
	integration, err := s.integrations.GetIntegration(ctx, integrationID)
	if err != nil {
		return err
	}
	resource, err := s.resources.GetResource(ctx, integrationID, resourceID)
	if err != nil {
		return err
	}
	if err := s.deleteManagedProviderWebhook(ctx, integration, resource); err != nil {
		return err
	}
	return s.resources.DeleteResource(ctx, integrationID, resourceID)
}

func (s *Service) RecordWebhookEvent(ctx context.Context, integrationID string, in WebhookEvent) (*WebhookEvent, error) {
	integrationID = strings.TrimSpace(integrationID)
	if integrationID == "" {
		return nil, fmt.Errorf("%w: integration_id is required", ErrValidation)
	}
	integration, err := s.integrations.GetIntegration(ctx, integrationID)
	if err != nil {
		return nil, err
	}
	in.IntegrationID = integrationID
	if strings.TrimSpace(in.Provider) == "" {
		in.Provider = integration.Provider
	}
	if err := normalizeWebhookEvent(&in); err != nil {
		return nil, err
	}
	_, event, err := s.webhookEvents.RecordWebhookEvent(ctx, in)
	return event, err
}

// Delete permanently removes an integration. Managed provider webhooks are
// deleted first; a provider failure preserves the local integration so cleanup
// can be retried with its credentials and resource metadata intact. Once remote
// cleanup succeeds, database cascades remove the local child rows atomically.
func (s *Service) Delete(ctx context.Context, integrationID string) error {
	integrationID = strings.TrimSpace(integrationID)
	if integrationID == "" {
		return fmt.Errorf("%w: integration_id is required", ErrValidation)
	}
	integration, err := s.integrations.GetIntegration(ctx, integrationID)
	if err != nil {
		return err
	}
	resources, err := s.resources.ListResources(ctx, integrationID)
	if err != nil {
		return err
	}
	for i := range resources {
		if err := s.deleteManagedProviderWebhook(ctx, integration, &resources[i]); err != nil {
			return err
		}
	}
	return s.integrations.DeleteIntegration(ctx, integrationID)
}

func (s *Service) ListWebhookEvents(ctx context.Context, integrationID string, filter WebhookEventFilter) ([]WebhookEvent, error) {
	filter.IntegrationID = strings.TrimSpace(integrationID)
	if filter.IntegrationID == "" {
		return nil, fmt.Errorf("%w: integration_id is required", ErrValidation)
	}
	if _, err := s.integrations.GetIntegration(ctx, filter.IntegrationID); err != nil {
		return nil, err
	}
	return s.webhookEvents.ListWebhookEvents(ctx, filter)
}

func (s *Service) ListGovernanceFeedbackEvents(ctx context.Context, filter GovernanceFeedbackFilter) ([]GovernanceFeedbackEvent, error) {
	return s.feedback.ListGovernanceFeedbackEvents(ctx, filter)
}

// ListTrackerLinks returns a work item's handed-off tracker issue links (all
// lanes, newest first) for the work-item "linked issues" surface.
func (s *Service) ListTrackerLinks(ctx context.Context, changeRequestID string) ([]TrackerLink, error) {
	return s.trackerLinks.ListTrackerLinksByChangeRequest(ctx, changeRequestID)
}

// ListDeliveryLinks returns the delivery links recorded for one work item,
// newest first. It is a persistence readback only: it does not poll providers
// or compute a delivery verdict.
func (s *Service) ListDeliveryLinks(ctx context.Context, changeRequestID string) ([]DeliveryLink, error) {
	return s.webhookEvents.ListDeliveryLinksByChangeRequest(ctx, changeRequestID)
}

// CreateGovernanceFeedbackEvent is the narrow write surface shared by webhook
// normalization and CLI feedback reports. It centralizes basic normalization so
// newer producers do not write malformed rows directly to the store.
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
var workRefMarkerPattern = regexp.MustCompile(`<!-- specgate-work-ref: ([A-Za-z0-9_-]+) -->`)

func parseWorkRefMarkers(texts ...string) []string {
	var refs []string
	seen := map[string]struct{}{}
	for _, text := range texts {
		for _, m := range workRefMarkerPattern.FindAllStringSubmatch(text, -1) {
			ref := strings.TrimSpace(m[1])
			if ref == "" {
				continue
			}
			key := strings.ToLower(ref)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			refs = append(refs, ref)
		}
	}
	return refs
}

func equalsRef(ref string, value string) bool {
	return strings.TrimSpace(value) != "" && strings.EqualFold(strings.TrimSpace(ref), strings.TrimSpace(value))
}

var providerPattern = regexp.MustCompile(`^[a-z0-9_-]+$`)

func normalizeIntegration(in *Integration) error {
	in.ID = strings.TrimSpace(in.ID)
	in.Provider = strings.ToLower(strings.TrimSpace(in.Provider))
	in.Name = strings.TrimSpace(in.Name)
	in.Status = strings.TrimSpace(in.Status)
	in.BaseURL = strings.TrimSpace(in.BaseURL)
	in.ConfigJSON = strings.TrimSpace(in.ConfigJSON)
	in.LastError = strings.TrimSpace(in.LastError)
	if in.Provider == "" {
		return fmt.Errorf("%w: provider is required", ErrValidation)
	}
	if !providerPattern.MatchString(in.Provider) {
		return fmt.Errorf("%w: provider must use lowercase letters, numbers, dash, or underscore", ErrValidation)
	}
	if in.Name == "" {
		return fmt.Errorf("%w: name is required", ErrValidation)
	}
	if in.Status == "" {
		in.Status = StatusConnected
	}
	if !validStatus(in.Status) {
		return fmt.Errorf("%w: unsupported status %q", ErrValidation, in.Status)
	}
	return normalizeJSONField(&in.ConfigJSON, "config_json")
}

func normalizeResource(in *Resource) error {
	in.ResourceType = strings.ToLower(strings.TrimSpace(in.ResourceType))
	in.ExternalID = strings.TrimSpace(in.ExternalID)
	in.ExternalKey = strings.TrimSpace(in.ExternalKey)
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	in.DefaultRef = strings.TrimSpace(in.DefaultRef)
	in.ConfigJSON = strings.TrimSpace(in.ConfigJSON)
	if in.ResourceType == "" {
		return fmt.Errorf("%w: resource_type is required", ErrValidation)
	}
	if in.ExternalKey == "" {
		return fmt.Errorf("%w: external_key is required", ErrValidation)
	}
	return normalizeJSONField(&in.ConfigJSON, "config_json")
}

func normalizeWebhookEvent(in *WebhookEvent) error {
	in.Provider = strings.ToLower(strings.TrimSpace(in.Provider))
	in.EventType = strings.ToLower(strings.TrimSpace(in.EventType))
	in.ExternalEventID = strings.TrimSpace(in.ExternalEventID)
	in.PayloadJSON = strings.TrimSpace(in.PayloadJSON)
	in.Status = strings.TrimSpace(in.Status)
	in.Error = strings.TrimSpace(in.Error)
	if in.Provider == "" {
		return fmt.Errorf("%w: provider is required", ErrValidation)
	}
	if in.EventType == "" {
		return fmt.Errorf("%w: event_type is required", ErrValidation)
	}
	if in.Status == "" {
		in.Status = WebhookStatusPending
	}
	if !validWebhookStatus(in.Status) {
		return fmt.Errorf("%w: unsupported webhook status %q", ErrValidation, in.Status)
	}
	return normalizeJSONField(&in.PayloadJSON, "payload_json")
}

func normalizeJSONField(value *string, field string) error {
	if strings.TrimSpace(*value) == "" {
		*value = "{}"
		return nil
	}
	var raw json.RawMessage
	if err := json.Unmarshal([]byte(*value), &raw); err != nil {
		return fmt.Errorf("%w: %s must be valid JSON", ErrValidation, field)
	}
	return nil
}

func validStatus(status string) bool {
	switch status {
	case StatusConnected, StatusDisabled, StatusError:
		return true
	default:
		return false
	}
}

func validWebhookStatus(status string) bool {
	switch status {
	case WebhookStatusPending, WebhookStatusProcessed, WebhookStatusFailed, WebhookStatusIgnored:
		return true
	default:
		return false
	}
}
