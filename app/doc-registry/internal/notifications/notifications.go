// Package notifications defines the small in-process event surface used by
// domain services when a durable row should prompt a UI refresh or automation.
//
// The package intentionally has no network transport. Self-managed deployments
// read notification state from persisted API resources; a push adapter can
// implement Publisher later without changing producers.
package notifications

// Event is a lightweight invalidation signal. Type names what changed; Data
// carries compact identifiers for consumers that want to refetch a narrow view.
type Event struct {
	Type string `json:"type"`
	Data any    `json:"data,omitempty"`
}

// Publisher is the producer-facing surface. Services hold this rather than a
// concrete transport so polling, logging, or push adapters can be swapped in
// without changing business logic.
type Publisher interface {
	Publish(Event)
}

// PublisherFunc adapts a function into a Publisher. A nil function is a no-op.
type PublisherFunc func(Event)

// Publish calls f when it is configured.
func (f PublisherFunc) Publish(evt Event) {
	if f != nil {
		f(evt)
	}
}
