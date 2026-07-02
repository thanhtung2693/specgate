package integrations

import (
	"context"
	"errors"
	"testing"

	"github.com/specgate/doc-registry/internal/webhookqueue"
)

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
	enc, err := EncryptSecret("plain-secret-token")
	if err != nil {
		t.Fatal(err)
	}
	store := enqueueFakeStore{
		integration: &Integration{ID: "int-1", Provider: ProviderGitLab, Status: StatusConnected},
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
	in := InboundWebhook{
		Token:       "plain-secret-token", // matches the stored secret token (verbatim path)
		EventHeader: "Merge Request Hook",
		PayloadJSON: `{"object_kind":"merge_request"}`,
	}

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
		got.Inbound.Token != "plain-secret-token" {
		t.Fatalf("enqueued task = %#v", got)
	}
}

// An unauthenticated delivery is rejected synchronously and never enqueued.
func TestHandleResourceWebhook_DoesNotEnqueueOnBadAuth(t *testing.T) {
	svc, enq := newEnqueueTestService(t)
	in := InboundWebhook{Token: "wrong-token", EventHeader: "Merge Request Hook", PayloadJSON: `{}`}

	_, err := svc.HandleResourceWebhook(context.Background(), "int-1", "res-1", ProviderGitLab, in)
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
	if len(enq.tasks) != 0 {
		t.Fatalf("enqueued %d tasks, want 0 (unauthenticated must not queue)", len(enq.tasks))
	}
}

func TestProcessWebhookDelivery_RejectsUnknownKind(t *testing.T) {
	svc := NewService(enqueueFakeStore{})
	err := svc.ProcessWebhookDelivery(context.Background(), webhookqueue.Task{Kind: "bogus"})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
}
