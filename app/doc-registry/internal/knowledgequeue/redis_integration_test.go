//go:build integration

package knowledgequeue_test

import (
	"context"
	"net"
	"reflect"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/specgate/doc-registry/internal/integrations/coretypes"
	"github.com/specgate/doc-registry/internal/knowledgequeue"
	"github.com/specgate/doc-registry/internal/webhookqueue"
)

type knowledgeReceiver chan knowledgequeue.Task

func (r knowledgeReceiver) ProcessKnowledgeIngest(_ context.Context, task knowledgequeue.Task) error {
	r <- task
	return nil
}

type webhookReceiver chan webhookqueue.Task

func (r webhookReceiver) ProcessWebhookDelivery(_ context.Context, task webhookqueue.Task) error {
	r <- task
	return nil
}

func TestRedisQueueRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image: "redis:8-alpine", ExposedPorts: []string{"6379/tcp"},
			Tmpfs:      map[string]string{"/data": ""},
			WaitingFor: wait.ForLog("Ready to accept connections"),
		}, Started: true,
	})
	if err != nil {
		t.Fatal(err) // Explicit integration runs must not silently skip Redis.
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := container.Terminate(cleanup); err != nil {
			t.Errorf("terminate fixture Redis: %v", err)
		}
	})
	host, err := container.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	port, err := container.MappedPort(ctx, "6379/tcp")
	if err != nil {
		t.Fatal(err)
	}
	opt := asynq.RedisClientOpt{Addr: net.JoinHostPort(host, port.Port())}
	knowledge := knowledgequeue.NewClient(opt, 1)
	t.Cleanup(func() { _ = knowledge.Close() })
	webhooks := webhookqueue.NewClient(opt, 1)
	t.Cleanup(func() { _ = webhooks.Close() })
	gotKnowledge := make(knowledgeReceiver, 1)
	gotWebhook := make(webhookReceiver, 1)
	mux := asynq.NewServeMux()
	mux.Handle(knowledgequeue.TaskTypeKnowledgeIngest, knowledgequeue.Handler(gotKnowledge))
	mux.Handle(webhookqueue.TaskTypeWebhookDeliver, webhookqueue.Handler(gotWebhook))
	worker := asynq.NewServer(opt, asynq.Config{Concurrency: 1, ShutdownTimeout: time.Second,
		Queues: map[string]int{knowledgequeue.QueueName: 1, webhookqueue.QueueName: 1}})
	if err := worker.Start(mux); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(worker.Shutdown)
	wantKnowledge := knowledgequeue.Task{WorkspaceID: "fixture-knowledge", DocumentID: "fixture-doc", Version: "v1", Content: []byte("fixture bytes")}
	wantWebhook := webhookqueue.Task{WorkspaceID: "fixture-webhook", Kind: webhookqueue.KindResource, Provider: "fixture", IntegrationID: "fixture-integration", ResourceID: "fixture-resource",
		Inbound: coretypes.InboundWebhook{EventHeader: "fixture-event", EventUUID: "fixture-delivery", PayloadJSON: `{"fixture":true}`}}
	if err := knowledge.EnqueueKnowledgeIngest(ctx, wantKnowledge); err != nil {
		t.Fatal(err)
	}
	if err := webhooks.EnqueueWebhookDelivery(ctx, wantWebhook); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-gotKnowledge:
		if !reflect.DeepEqual(got, wantKnowledge) {
			t.Fatal("knowledge payload/workspace changed")
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case got := <-gotWebhook:
		if !reflect.DeepEqual(got, wantWebhook) {
			t.Fatal("webhook payload/workspace changed")
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	inspector := asynq.NewInspector(opt)
	t.Cleanup(func() { _ = inspector.Close() })
	for _, name := range []string{knowledgequeue.QueueName, webhookqueue.QueueName} {
		for {
			info, err := inspector.GetQueueInfo(name)
			if err != nil {
				t.Fatal(err)
			}
			if info.ProcessedTotal == 1 && info.FailedTotal == 0 && info.Size == 0 {
				break
			}
			select {
			case <-time.After(50 * time.Millisecond):
			case <-ctx.Done():
				t.Fatalf("queue %s was not acknowledged: %v", name, ctx.Err())
			}
		}
	}
}
