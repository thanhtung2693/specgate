package notifications

import "testing"

func TestPublisherFuncPublishesEvent(t *testing.T) {
	t.Parallel()

	var got Event
	publisher := PublisherFunc(func(evt Event) {
		got = evt
	})

	publisher.Publish(Event{
		Type: "feedback.recorded",
		Data: map[string]any{"change_request_id": "cr-1"},
	})

	if got.Type != "feedback.recorded" {
		t.Fatalf("event type = %q, want feedback.recorded", got.Type)
	}
	if got.Data == nil {
		t.Fatal("event data was not forwarded")
	}
}

func TestPublisherFuncNilIsNoop(t *testing.T) {
	t.Parallel()

	var publisher PublisherFunc
	publisher.Publish(Event{Type: "feedback.recorded"})
}
