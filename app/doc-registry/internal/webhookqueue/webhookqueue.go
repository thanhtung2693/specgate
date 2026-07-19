// Package webhookqueue is the asynq-backed async pipeline for inbound provider
// webhooks: the HTTP receiver authenticates a delivery synchronously and enqueues
// a (secret-free) task; a worker re-runs the normalize + commit back-half. It
// depends only on coretypes + asynq, never the parent integrations package, so it
// stays import-cycle-free and unit-testable (the Service depends on the Enqueuer
// interface, the worker on the Processor interface).
package webhookqueue

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hibiken/asynq"

	"github.com/specgate/doc-registry/internal/integrations/coretypes"
)

const TaskTypeWebhookDeliver = "webhook:deliver"

// QueueName is the asynq queue these tasks land on.
const QueueName = "webhooks"

// Kind discriminates which receiver back-half the worker runs.
type Kind string

const (
	// KindResource = POST /integrations/{id}/resources/{rid}/{provider}/webhook.
	KindResource Kind = "resource"
)

// Task is the secret-free payload enqueued after a delivery authenticates. It
// carries everything the worker needs to re-run normalize + commit. The raw body
// and headers are the same data the provider sent; no decrypted secret is ever
// placed on the queue.
type Task struct {
	WorkspaceID   string                   `json:"workspace_id"`
	Kind          Kind                     `json:"kind"`
	Provider      string                   `json:"provider"`
	IntegrationID string                   `json:"integration_id"`
	ResourceID    string                   `json:"resource_id,omitempty"`
	Inbound       coretypes.InboundWebhook `json:"inbound"`
}

// Enqueuer enqueues an authenticated webhook delivery for async processing. The
// integrations Service holds one of these (nil ⇒ synchronous fallback).
type Enqueuer interface {
	EnqueueWebhookDelivery(ctx context.Context, t Task) error
}

// Processor runs an enqueued delivery (re-normalize + commit). Implemented by the
// integrations Service; the worker handler calls it.
type Processor interface {
	ProcessWebhookDelivery(ctx context.Context, t Task) error
}

// Handler returns the asynq handler for TaskTypeWebhookDeliver: decode → process.
// A processing error propagates so asynq retries; an undecodable payload skips
// retry (it can never succeed).
func Handler(p Processor) asynq.HandlerFunc {
	return func(ctx context.Context, at *asynq.Task) error {
		var t Task
		if err := json.Unmarshal(at.Payload(), &t); err != nil {
			return fmt.Errorf("%w: decode webhook task: %v", asynq.SkipRetry, err)
		}
		return p.ProcessWebhookDelivery(ctx, t)
	}
}

func newTask(t Task) (*asynq.Task, error) {
	payload, err := json.Marshal(t)
	if err != nil {
		return nil, err
	}
	return asynq.NewTask(TaskTypeWebhookDeliver, payload, asynq.Queue(QueueName)), nil
}

// Client is an asynq-backed Enqueuer over Redis.
type Client struct {
	client   *asynq.Client
	maxRetry int
}

// NewClient builds an asynq enqueuer. maxRetry caps automatic retries per task.
func NewClient(opt asynq.RedisConnOpt, maxRetry int) *Client {
	return &Client{client: asynq.NewClient(opt), maxRetry: maxRetry}
}

func (c *Client) EnqueueWebhookDelivery(ctx context.Context, t Task) error {
	at, err := newTask(t)
	if err != nil {
		return err
	}
	_, err = c.client.EnqueueContext(ctx, at, asynq.MaxRetry(c.maxRetry))
	return err
}

// Close releases the underlying Redis connections.
func (c *Client) Close() error { return c.client.Close() }
