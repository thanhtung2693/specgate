package knowledge

import (
	"strings"
	"testing"
)

func TestExpandSectionContextUsesSiblingChunks(t *testing.T) {
	chunks := []Chunk{
		{ChunkIndex: 0, SectionIndex: 1, Heading: "Refunds", ChunkText: "# Refunds\nRefunds require reviewer approval."},
		{ChunkIndex: 1, SectionIndex: 1, Heading: "Refunds", ChunkText: "Approvers must record a note."},
		{ChunkIndex: 2, SectionIndex: 2, Heading: "Loyalty", ChunkText: "# Loyalty\nPlatinum changes need review."},
	}
	text, start, end := expandSectionContext(chunks, 1, 500)
	if start != 0 || end != 1 {
		t.Fatalf("range = %d..%d, want 0..1", start, end)
	}
	if !strings.Contains(text, "Refunds require reviewer approval") || !strings.Contains(text, "Approvers must record a note") {
		t.Fatalf("context missing section siblings:\n%s", text)
	}
	if strings.Contains(text, "Platinum changes") {
		t.Fatalf("context leaked another section:\n%s", text)
	}
}

func TestExpandSectionContextCapsByMaxChars(t *testing.T) {
	chunks := []Chunk{
		{ChunkIndex: 0, SectionIndex: 1, ChunkText: strings.Repeat("a", 40)},
		{ChunkIndex: 1, SectionIndex: 1, ChunkText: strings.Repeat("b", 40)},
		{ChunkIndex: 2, SectionIndex: 1, ChunkText: strings.Repeat("c", 40)},
	}
	// Cap allows the hit chunk but not all siblings.
	text, start, end := expandSectionContext(chunks, 0, 50)
	if len(text) > 50 {
		t.Fatalf("context exceeds cap: len=%d", len(text))
	}
	if start != 0 || end != 0 {
		t.Fatalf("range = %d..%d, want 0..0 (cap stops after hit)", start, end)
	}
	if !strings.Contains(text, strings.Repeat("a", 40)) {
		t.Fatalf("context dropped the hit chunk:\n%s", text)
	}
	if strings.Contains(text, strings.Repeat("b", 40)) {
		t.Fatalf("context exceeded cap by adding a sibling:\n%s", text)
	}
}

func TestJoinChunksCappedFitsAndCaps(t *testing.T) {
	chunks := []Chunk{
		{ChunkIndex: 0, ChunkText: "alpha"},
		{ChunkIndex: 1, ChunkText: "beta"},
	}
	full, truncated := joinChunksCapped(chunks, 1000)
	if truncated {
		t.Fatalf("small doc should not be truncated: %q", full)
	}
	if !strings.Contains(full, "alpha") || !strings.Contains(full, "beta") {
		t.Fatalf("joined doc missing chunks: %q", full)
	}

	capped, wasTruncated := joinChunksCapped([]Chunk{
		{ChunkIndex: 0, ChunkText: strings.Repeat("x", 40)},
		{ChunkIndex: 1, ChunkText: strings.Repeat("y", 40)},
	}, 50)
	if !wasTruncated {
		t.Fatalf("oversized doc should report truncation: %q", capped)
	}
	if len(capped) > 50 {
		t.Fatalf("capped doc exceeds cap: len=%d", len(capped))
	}
}
