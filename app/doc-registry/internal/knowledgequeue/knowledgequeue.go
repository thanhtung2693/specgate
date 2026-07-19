// Package knowledgequeue is the asynq-backed async pipeline for Governance
// Knowledge ingestion: create/upload authenticates and persists the version
// synchronously (status uploaded) and enqueues a (secret-free) task; a worker
// re-runs extract → chunk → embed → index. It depends only on asynq, never the
// parent knowledge package, so it stays import-cycle-free and unit-testable (the
// knowledge Service depends on the Enqueuer interface, the worker on the
// Processor interface). Mirrors internal/webhookqueue.
package knowledgequeue

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hibiken/asynq"
)

const TaskTypeKnowledgeIngest = "knowledge:ingest"

// QueueName is the asynq queue these tasks land on.
const QueueName = "knowledge"

// Task is the self-contained, secret-free payload enqueued after a Knowledge
// version is persisted. It carries the raw source content so the worker can
// re-run ingest without depending on the object store (which is a no-op under
// STORAGE_DRIVER=local). Document content is not a secret.
type Task struct {
	WorkspaceID string `json:"workspace_id"`
	DocumentID  string `json:"document_id"`
	Version     string `json:"version"`
	Content     []byte `json:"content"`
}

// Enqueuer enqueues a persisted Knowledge version for async ingestion. The
// knowledge Service holds one of these (nil ⇒ synchronous inline fallback).
type Enqueuer interface {
	EnqueueKnowledgeIngest(ctx context.Context, t Task) error
}

// Processor runs an enqueued ingestion (extract → chunk → embed → index).
// Implemented by the knowledge Service; the worker handler calls it.
type Processor interface {
	ProcessKnowledgeIngest(ctx context.Context, t Task) error
}

// Handler returns the asynq handler for TaskTypeKnowledgeIngest: decode →
// process. A processing error propagates so asynq retries; an undecodable
// payload skips retry (it can never succeed).
func Handler(p Processor) asynq.HandlerFunc {
	return func(ctx context.Context, at *asynq.Task) error {
		var t Task
		if err := json.Unmarshal(at.Payload(), &t); err != nil {
			return fmt.Errorf("%w: decode knowledge ingest task: %v", asynq.SkipRetry, err)
		}
		return p.ProcessKnowledgeIngest(ctx, t)
	}
}

func newTask(t Task) (*asynq.Task, error) {
	if strings.TrimSpace(t.WorkspaceID) == "" {
		return nil, fmt.Errorf("knowledge ingest task: workspace_id is required")
	}
	payload, err := json.Marshal(t)
	if err != nil {
		return nil, err
	}
	return asynq.NewTask(TaskTypeKnowledgeIngest, payload, asynq.Queue(QueueName)), nil
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

func (c *Client) EnqueueKnowledgeIngest(ctx context.Context, t Task) error {
	at, err := newTask(t)
	if err != nil {
		return err
	}
	_, err = c.client.EnqueueContext(ctx, at, asynq.MaxRetry(c.maxRetry))
	return err
}

// Close releases the underlying Redis connections.
func (c *Client) Close() error { return c.client.Close() }
