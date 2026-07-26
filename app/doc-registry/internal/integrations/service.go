package integrations

import (
	"context"
	"fmt"
	"strings"

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

// NewService builds a Service with no work-board reader. Delivery-review
// lookups that need one are unavailable; production wiring uses
// NewServiceWithWorkBoard.
func NewService(store Store) *Service {
	return NewServiceWithWorkBoard(store, nil)
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
