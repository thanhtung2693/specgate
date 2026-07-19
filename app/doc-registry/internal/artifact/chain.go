package artifact

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

// Per-artifact tamper-evident event chain (spec §8): every artifact_events row
// commits to its predecessor via prev_hash, so a direct UPDATE or mid-chain
// DELETE breaks recomputation. Honest limits (spec'd, not solved here): tail
// truncation leaves a valid shorter chain, and verification runs server-side —
// it defends against database edits, not a compromised server binary.

// ChainState is the verification verdict for one artifact's event history.
type ChainState string

const (
	// ChainIntact — every chained row recomputes and links.
	ChainIntact ChainState = "intact"
	// ChainTampered — a row's hash mismatches or a prev_hash link breaks.
	ChainTampered ChainState = "tampered"
)

// ChainReport is the result of verifying one artifact's event chain.
type ChainReport struct {
	State           ChainState `json:"state"`
	ArtifactID      string     `json:"artifact_id,omitempty"`
	FirstBadEventID string     `json:"first_bad_event_id,omitempty"`
	ChainedEvents   int        `json:"chained_events"`
}

// EventHash computes the chain hash for an event: hex SHA-256 over the
// canonical string prev_hash, id, artifact_id, event_type, payload, and
// created_at (RFC3339Nano, UTC, truncated to microseconds — Postgres
// timestamptz precision, so the stored row re-verifies bit-for-bit), joined by
// newlines. Payload is hashed verbatim; it is stored verbatim.
func EventHash(prevHash string, e Event) string {
	canonical := strings.Join([]string{
		prevHash,
		e.ID,
		e.ArtifactID,
		e.EventType,
		e.Payload,
		e.CreatedAt.UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano),
	}, "\n")
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}

// VerifyEventChain walks one artifact's events oldest→newest and recomputes
// the chain. Every persisted event must be chained.
func VerifyEventChain(events []Event) ChainReport {
	report := ChainReport{State: ChainIntact}
	if len(events) > 0 {
		report.ArtifactID = events[0].ArtifactID
	}
	prev := ""
	for _, e := range events {
		if e.Hash == "" || e.PrevHash != prev || EventHash(e.PrevHash, e) != e.Hash {
			report.State = ChainTampered
			report.FirstBadEventID = e.ID
			return report
		}
		prev = e.Hash
		report.ChainedEvents++
	}
	return report
}
