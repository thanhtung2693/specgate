package integrations

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/specgate/doc-registry/internal/webhookqueue"
	"github.com/specgate/doc-registry/internal/workboard"
)

func enqueueGitLabSigningToken() string {
	return "wh" + "sec_" + base64.StdEncoding.EncodeToString(make([]byte, 32))
}

func signedEnqueueWebhook(payload string) InboundWebhook {
	const webhookID = "msg-enqueue"
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	key, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(enqueueGitLabSigningToken(), "whsec_"))
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(webhookID + "." + timestamp + "." + payload))
	return InboundWebhook{
		EventHeader:      "Merge Request Hook",
		WebhookID:        webhookID,
		WebhookTimestamp: timestamp,
		WebhookSignature: "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil)),
		PayloadJSON:      payload,
	}
}

// enqueueFakeStore implements only the two reads the verify-then-enqueue path
// needs. The commit-pipeline methods are left nil (embedded Store), so any inline
// processing would panic — proving the enqueue path never touches the DB pipeline.
type enqueueFakeStore struct {
	Store
	integration *Integration
	resource    *Resource
}

func (f enqueueFakeStore) GetIntegration(context.Context, string) (*Integration, error) {
	return f.integration, nil
}

func (f enqueueFakeStore) GetResource(context.Context, string, string) (*Resource, error) {
	return f.resource, nil
}

type fakeEnqueuer struct{ tasks []webhookqueue.Task }

func (f *fakeEnqueuer) EnqueueWebhookDelivery(_ context.Context, t webhookqueue.Task) error {
	f.tasks = append(f.tasks, t)
	return nil
}

func newEnqueueTestService(t *testing.T) (*Service, *fakeEnqueuer) {
	t.Helper()
	t.Setenv(SecretKeyEnvVar, "0000000000000000000000000000000000000000000000000000000000000001")
	enc, err := EncryptSecret(enqueueGitLabSigningToken())
	if err != nil {
		t.Fatal(err)
	}
	store := enqueueFakeStore{
		integration: &Integration{
			ID:          "int-1",
			WorkspaceID: "ws-test",
			Provider:    ProviderGitLab,
			Status:      StatusConnected,
		},
		resource: &Resource{
			ID: "res-1", IntegrationID: "int-1", ResourceType: ResourceTypeProject,
			ExternalID: "321", ExternalKey: "acme/web", WebhookSecretEncrypted: enc,
		},
	}
	enq := &fakeEnqueuer{}
	return NewService(store).WithWebhookEnqueuer(enq), enq
}

// A delivery that authenticates is enqueued (not committed inline): the result is
// "queued" and the captured task carries the routing fields + raw inbound.
func TestHandleResourceWebhook_EnqueuesAuthenticatedDelivery(t *testing.T) {
	svc, enq := newEnqueueTestService(t)
	in := signedEnqueueWebhook(`{"object_kind":"merge_request"}`)

	res, err := svc.HandleResourceWebhook(context.Background(), "int-1", "res-1", ProviderGitLab, in)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != WebhookStatusPending || res.IgnoredReason != "queued" {
		t.Fatalf("result = %#v, want queued/pending", res)
	}
	if len(enq.tasks) != 1 {
		t.Fatalf("enqueued %d tasks, want 1", len(enq.tasks))
	}
	got := enq.tasks[0]
	if got.Kind != webhookqueue.KindResource || got.Provider != ProviderGitLab ||
		got.IntegrationID != "int-1" || got.ResourceID != "res-1" ||
		got.Inbound.WebhookSignature == "" {
		t.Fatalf("enqueued task = %#v", got)
	}
}

// An unauthenticated delivery is rejected synchronously and never enqueued.
func TestHandleResourceWebhook_DoesNotEnqueueOnBadAuth(t *testing.T) {
	svc, enq := newEnqueueTestService(t)
	in := InboundWebhook{EventHeader: "Merge Request Hook", PayloadJSON: `{}`}

	_, err := svc.HandleResourceWebhook(context.Background(), "int-1", "res-1", ProviderGitLab, in)
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
	if len(enq.tasks) != 0 {
		t.Fatalf("enqueued %d tasks, want 0 (unauthenticated must not queue)", len(enq.tasks))
	}
}

func TestLoadWebhookResourceRejectsDisabledIntegration(t *testing.T) {
	store := enqueueFakeStore{
		integration: &Integration{ID: "int-1", Provider: ProviderGitLab, Status: StatusDisabled},
		resource:    &Resource{ID: "res-1", IntegrationID: "int-1", ResourceType: ResourceTypeProject},
	}
	_, _, err := NewService(store).loadWebhookResource(context.Background(), "int-1", "res-1", ProviderGitLab, ResourceTypeProject)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want ErrValidation for disabled integration", err)
	}
}

func TestProcessWebhookDelivery_RejectsUnknownKind(t *testing.T) {
	svc := NewService(enqueueFakeStore{})
	err := svc.ProcessWebhookDelivery(context.Background(), webhookqueue.Task{Kind: "bogus"})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
}

type failedWebhookRetryStore struct {
	Store
	event         WebhookEvent
	recordedEvent WebhookEvent
	claimCount    int
	deliveryLinks int
	feedback      int
}

func (s *failedWebhookRetryStore) WithTx(_ context.Context, fn func(Store) error) error {
	return fn(s)
}

func (s *failedWebhookRetryStore) RecordWebhookEvent(_ context.Context, in WebhookEvent) (bool, *WebhookEvent, error) {
	s.recordedEvent = in
	event := s.event
	return false, &event, nil
}

func (s *failedWebhookRetryStore) ClaimFailedWebhookEvent(_ context.Context, id string) (bool, *WebhookEvent, error) {
	if id != s.event.ID || s.event.Status != WebhookStatusFailed {
		event := s.event
		return false, &event, nil
	}
	s.claimCount++
	s.event.Status = WebhookStatusPending
	s.event.Error = ""
	s.event.ProcessedAt = nil
	event := s.event
	return true, &event, nil
}

func (s *failedWebhookRetryStore) UpdateWebhookEventStatus(_ context.Context, _ string, status, reason string) (*WebhookEvent, error) {
	s.event.Status = status
	s.event.Error = reason
	event := s.event
	return &event, nil
}

func (s *failedWebhookRetryStore) UpsertDeliveryLink(_ context.Context, in DeliveryLink) (*DeliveryLink, error) {
	s.deliveryLinks++
	in.ID = "delivery-link-1"
	return &in, nil
}

func (s *failedWebhookRetryStore) CreateGovernanceFeedbackEvent(_ context.Context, in GovernanceFeedbackEvent) (*GovernanceFeedbackEvent, error) {
	s.feedback++
	in.ID = "feedback-1"
	return &in, nil
}

func (*failedWebhookRetryStore) TrackerLinkByExternal(context.Context, string, string, string) (*TrackerLink, error) {
	return nil, ErrNotFound
}

func TestCommitDeliveryRetriesPreviouslyFailedWebhookEvent(t *testing.T) {
	store := &failedWebhookRetryStore{event: WebhookEvent{ID: "event-1", Status: WebhookStatusFailed, Error: "temporary database failure"}}
	cr := workboard.ChangeRequest{ID: "cr-id", Key: "CR-RETRY", FeatureID: "feature-1"}
	svc := NewServiceWithWorkBoard(store, &fakeWorkBoard{items: []workboard.ChangeRequest{cr}})

	result, err := svc.commitDelivery(context.Background(),
		&Integration{ID: "integration-1", Provider: ProviderGitHub},
		&Resource{ID: "resource-1", IntegrationID: "integration-1", ResourceType: ResourceTypeRepo},
		normalizedDelivery{
			Provider: ProviderGitHub, EventType: WebhookEventMergeRequest, ExternalEventID: "delivery-1",
			ExternalID: "42", IID: 42, ExternalKey: "owner/repo#42", Title: "Retry delivery",
			Description: "<!-- specgate-work-ref: CR-RETRY -->", RawPayload: `{}`,
			DeliveryState: DeliveryStateMerged,
		},
	)
	if err != nil {
		t.Fatalf("commitDelivery: %v", err)
	}
	if result.Status != WebhookStatusProcessed || result.IgnoredReason != "" {
		t.Fatalf("result = %#v, want processed retry", result)
	}
	if store.claimCount != 1 || store.deliveryLinks != 1 || store.feedback != 1 {
		t.Fatalf("claims=%d links=%d feedback=%d, want 1/1/1", store.claimCount, store.deliveryLinks, store.feedback)
	}
}

func TestLinearWebhookRetriesPreviouslyFailedWebhookEvent(t *testing.T) {
	store := &failedWebhookRetryStore{event: WebhookEvent{ID: "event-1", Status: WebhookStatusFailed, Error: "temporary database failure"}}
	svc := NewService(store)
	payload := `{"type":"Issue","webhookId":"linear-event-1","data":{"id":"issue-1","identifier":"ENG-1","title":"Retry work","url":"https://linear.app/acme/issue/ENG-1","state":{"id":"state-1","name":"In Progress","type":"started"}}}`

	result, err := svc.processLinearWebhookPayloadWithDeliveryID(context.Background(),
		&Integration{ID: "integration-1", Provider: ProviderLinear},
		&Resource{ID: "resource-1", IntegrationID: "integration-1", ResourceType: ResourceTypeTeam},
		payload,
		"linear-delivery-2",
	)
	if err != nil {
		t.Fatalf("processLinearWebhookPayload: %v", err)
	}
	if result.Status != WebhookStatusProcessed || result.IgnoredReason != "" {
		t.Fatalf("result = %#v, want processed retry", result)
	}
	if store.claimCount != 1 || store.feedback != 1 {
		t.Fatalf("claims=%d feedback=%d, want 1/1", store.claimCount, store.feedback)
	}
	if store.recordedEvent.ExternalEventID != "linear-delivery-2" {
		t.Fatalf("external event id = %q, want Linear-Delivery", store.recordedEvent.ExternalEventID)
	}
}

func TestHandleLinearResourceWebhook_EnqueuesAuthenticatedDelivery(t *testing.T) {
	t.Setenv(SecretKeyEnvVar, "0000000000000000000000000000000000000000000000000000000000000001")
	const secret = "linear-resource-secret"
	enc, err := EncryptSecret(secret)
	if err != nil {
		t.Fatal(err)
	}
	store := enqueueFakeStore{
		integration: &Integration{ID: "int-1", WorkspaceID: "ws-test", Provider: ProviderLinear, Status: StatusConnected},
		resource:    &Resource{ID: "res-1", IntegrationID: "int-1", ResourceType: ResourceTypeTeam, WebhookSecretEncrypted: enc},
	}
	enq := &fakeEnqueuer{}
	svc := NewService(store).WithWebhookEnqueuer(enq)
	payload := fmt.Sprintf(`{"type":"Issue","webhookId":"installation-1","webhookTimestamp":%d}`, time.Now().UnixMilli())
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(payload))
	result, err := svc.HandleLinearResourceWebhook(context.Background(), "int-1", "res-1", LinearWebhookInput{
		Signature: hex.EncodeToString(mac.Sum(nil)), DeliveryID: "delivery-1", PayloadJSON: payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != WebhookStatusPending || result.IgnoredReason != "queued" {
		t.Fatalf("result = %#v, want queued/pending", result)
	}
	if len(enq.tasks) != 1 || enq.tasks[0].Provider != ProviderLinear || enq.tasks[0].Inbound.EventUUID != "delivery-1" {
		t.Fatalf("tasks = %#v, want one Linear delivery task", enq.tasks)
	}
}
