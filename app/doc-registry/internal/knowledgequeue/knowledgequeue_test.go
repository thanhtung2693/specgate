package knowledgequeue

import (
	"context"
	"errors"
	"testing"

	"github.com/hibiken/asynq"
)

type fakeProcessor struct {
	got    Task
	called int
	err    error
}

func (f *fakeProcessor) ProcessKnowledgeIngest(_ context.Context, t Task) error {
	f.got = t
	f.called++
	return f.err
}

func TestTaskRoundTripsThroughHandler(t *testing.T) {
	want := Task{WorkspaceID: "ws-1", DocumentID: "doc-1", Version: "v1", Content: []byte("# Refunds\n\nbody")}
	at, err := newTask(want)
	if err != nil {
		t.Fatal(err)
	}
	if at.Type() != TaskTypeKnowledgeIngest {
		t.Fatalf("task type = %q, want %q", at.Type(), TaskTypeKnowledgeIngest)
	}

	fp := &fakeProcessor{}
	if err := Handler(fp)(context.Background(), at); err != nil {
		t.Fatal(err)
	}
	if fp.called != 1 {
		t.Fatalf("processor called %d times, want 1", fp.called)
	}
	if fp.got.DocumentID != "doc-1" || fp.got.Version != "v1" || string(fp.got.Content) != "# Refunds\n\nbody" {
		t.Fatalf("decoded task = %#v", fp.got)
	}
}

func TestHandler_SkipsRetryOnUndecodablePayload(t *testing.T) {
	at := asynq.NewTask(TaskTypeKnowledgeIngest, []byte("not json"))
	err := Handler(&fakeProcessor{})(context.Background(), at)
	if err == nil {
		t.Fatal("want an error for an undecodable payload")
	}
	if !errors.Is(err, asynq.SkipRetry) {
		t.Fatalf("want asynq.SkipRetry, got %v", err)
	}
}

// TestHandler_PropagatesProcessorErrorForRetry: a transient processing error
// (e.g. embed-provider timeout) must propagate AS-IS — not wrapped in
// asynq.SkipRetry — so asynq retries the task rather than dropping it. This is
// the retry half of the queue contract; only the skip-retry half was covered.
func TestHandler_PropagatesProcessorErrorForRetry(t *testing.T) {
	boom := errors.New("embed provider timeout")
	at, err := newTask(Task{WorkspaceID: "ws-1", DocumentID: "doc-1", Version: "v1", Content: []byte("body")})
	if err != nil {
		t.Fatal(err)
	}
	err = Handler(&fakeProcessor{err: boom})(context.Background(), at)
	if !errors.Is(err, boom) {
		t.Fatalf("want the processor error propagated, got %v", err)
	}
	if errors.Is(err, asynq.SkipRetry) {
		t.Fatal("a transient processor error must NOT be wrapped in SkipRetry")
	}
}

func TestNewTask_RequiresWorkspace(t *testing.T) {
	if _, err := newTask(Task{DocumentID: "doc-1", Version: "v1"}); err == nil {
		t.Fatal("newTask accepted missing workspace")
	}
}
