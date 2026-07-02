package webhookqueue

import (
	"context"
	"errors"
	"testing"

	"github.com/hibiken/asynq"

	"github.com/specgate/doc-registry/internal/integrations/coretypes"
)

type fakeProcessor struct {
	got    Task
	called int
}

func (f *fakeProcessor) ProcessWebhookDelivery(_ context.Context, t Task) error {
	f.got = t
	f.called++
	return nil
}

func TestTaskRoundTripsThroughHandler(t *testing.T) {
	want := Task{
		Kind:          KindResource,
		Provider:      "gitlab",
		IntegrationID: "int-1",
		ResourceID:    "res-1",
		Inbound: coretypes.InboundWebhook{
			EventHeader:      "Merge Request Hook",
			WebhookSignature: "v1,abc",
			PayloadJSON:      `{"object_kind":"merge_request"}`,
		},
	}
	at, err := newTask(want)
	if err != nil {
		t.Fatal(err)
	}
	if at.Type() != TaskTypeWebhookDeliver {
		t.Fatalf("task type = %q, want %q", at.Type(), TaskTypeWebhookDeliver)
	}

	fp := &fakeProcessor{}
	if err := Handler(fp)(context.Background(), at); err != nil {
		t.Fatal(err)
	}
	if fp.called != 1 {
		t.Fatalf("processor called %d times, want 1", fp.called)
	}
	if fp.got.Provider != "gitlab" || fp.got.ResourceID != "res-1" ||
		fp.got.Inbound.PayloadJSON != `{"object_kind":"merge_request"}` {
		t.Fatalf("decoded task = %#v", fp.got)
	}
}

func TestHandler_SkipsRetryOnUndecodablePayload(t *testing.T) {
	at := asynq.NewTask(TaskTypeWebhookDeliver, []byte("not json"))
	err := Handler(&fakeProcessor{})(context.Background(), at)
	if err == nil {
		t.Fatal("want an error for an undecodable payload")
	}
	if !errors.Is(err, asynq.SkipRetry) {
		t.Fatalf("want asynq.SkipRetry, got %v", err)
	}
}
