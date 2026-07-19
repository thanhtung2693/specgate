package artifact

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"
)

// The canonical hash input is spec'd (doc-registry spec §8): prev_hash, id,
// artifact_id, event_type, payload, created_at(RFC3339Nano, UTC, µs-truncated),
// joined by newlines. This test recomputes it independently so the format is
// pinned by contract, not by implementation detail.
func TestEventHashCanonicalFormat(t *testing.T) {
	t.Parallel()
	created := time.Date(2026, 7, 10, 8, 30, 0, 123456789, time.UTC) // ns beyond µs must not affect the hash
	e := Event{
		ID:         "ev-1",
		ArtifactID: "art-1",
		EventType:  EventPublished,
		Payload:    `{"status":"draft"}`,
		CreatedAt:  created,
	}
	canonical := "prev-abc\nev-1\nart-1\nartifact.published\n{\"status\":\"draft\"}\n" +
		created.Truncate(time.Microsecond).Format(time.RFC3339Nano)
	sum := sha256.Sum256([]byte(canonical))
	want := hex.EncodeToString(sum[:])

	if got := EventHash("prev-abc", e); got != want {
		t.Fatalf("EventHash = %q, want %q", got, want)
	}
	// Microsecond truncation: nanosecond jitter below µs yields the same hash.
	e2 := e
	e2.CreatedAt = created.Truncate(time.Microsecond)
	if EventHash("prev-abc", e2) != want {
		t.Fatal("hash must be stable across sub-microsecond precision loss (Postgres stores µs)")
	}
}

func chainEvents(t *testing.T, base time.Time) []Event {
	t.Helper()
	events := []Event{
		{ID: "ev-1", ArtifactID: "art-1", EventType: EventPublished, Payload: `{"v":"1"}`, CreatedAt: base},
		{ID: "ev-2", ArtifactID: "art-1", EventType: EventSuperseded, Payload: `{"v":"2"}`, CreatedAt: base.Add(time.Minute)},
		{ID: "ev-3", ArtifactID: "art-1", EventType: EventNeedsChanges, Payload: `{"v":"3"}`, CreatedAt: base.Add(2 * time.Minute)},
	}
	prev := ""
	for i := range events {
		events[i].PrevHash = prev
		events[i].Hash = EventHash(prev, events[i])
		prev = events[i].Hash
	}
	return events
}

func TestVerifyEventChainIntact(t *testing.T) {
	t.Parallel()
	events := chainEvents(t, time.Date(2026, 7, 10, 9, 0, 0, 0, time.UTC))
	report := VerifyEventChain(events)
	if report.State != ChainIntact {
		t.Fatalf("state = %q, want %q", report.State, ChainIntact)
	}
}

func TestVerifyEventChainDetectsTamperedPayload(t *testing.T) {
	t.Parallel()
	events := chainEvents(t, time.Date(2026, 7, 10, 9, 0, 0, 0, time.UTC))
	events[1].Payload = `{"v":"2","approved_by":"attacker"}` // simulate a direct UPDATE
	report := VerifyEventChain(events)
	if report.State != ChainTampered {
		t.Fatalf("state = %q, want %q", report.State, ChainTampered)
	}
	if report.FirstBadEventID != "ev-2" {
		t.Fatalf("FirstBadEventID = %q, want ev-2", report.FirstBadEventID)
	}
}

func TestVerifyEventChainDetectsBrokenLink(t *testing.T) {
	t.Parallel()
	events := chainEvents(t, time.Date(2026, 7, 10, 9, 0, 0, 0, time.UTC))
	// Simulate deleting ev-2: ev-3's prev no longer matches ev-1's hash.
	report := VerifyEventChain([]Event{events[0], events[2]})
	if report.State != ChainTampered || report.FirstBadEventID != "ev-3" {
		t.Fatalf("report = %+v, want tampered at ev-3", report)
	}
}

func TestVerifyEventChainRejectsUnhashedEvent(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 7, 10, 9, 0, 0, 0, time.UTC)
	unchained := Event{ID: "ev-old", ArtifactID: "art-1", EventType: EventPublished, Payload: `{}`, CreatedAt: base}
	report := VerifyEventChain([]Event{unchained})
	if report.State != ChainTampered || report.FirstBadEventID != "ev-old" {
		t.Fatalf("report = %+v, want tampered at ev-old", report)
	}
}

func TestVerifyEventChainEmpty(t *testing.T) {
	t.Parallel()
	if got := VerifyEventChain(nil).State; got != ChainIntact {
		t.Fatalf("empty history = %q, want intact", got)
	}
}
