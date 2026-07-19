package knowledge

import (
	"strings"
	"testing"
)

func TestChunkDocumentStable(t *testing.T) {
	t.Parallel()
	src := "# Heading\n\n" + "First paragraph has a few words.\n\n" + "Second paragraph stays together."
	chunks := ChunkDocument(src)
	if len(chunks) != 1 {
		t.Fatalf("len=%d chunks=%#v", len(chunks), chunks)
	}
	if chunks[0].Text != src {
		t.Fatalf("chunk=%q", chunks[0].Text)
	}
}

func TestChunkDocumentKeepsMarkdownHeadingWithChunk(t *testing.T) {
	t.Parallel()
	input := "# Refunds\n\nRefunds require reviewer approval.\n\n# Loyalty\n\nPlatinum tier changes need manual review."
	chunks := ChunkDocument(input)
	if len(chunks) != 2 {
		t.Fatalf("chunks=%d want 2: %#v", len(chunks), chunks)
	}
	if !strings.Contains(chunks[0].Text, "# Refunds") || !strings.Contains(chunks[0].Text, "reviewer approval") {
		t.Fatalf("refund chunk missing heading/body: %#v", chunks[0].Text)
	}
	if !strings.Contains(chunks[1].Text, "# Loyalty") || !strings.Contains(chunks[1].Text, "manual review") {
		t.Fatalf("loyalty chunk missing heading/body: %#v", chunks[1].Text)
	}
}

func TestChunkDocumentStoresSectionMetadata(t *testing.T) {
	t.Parallel()
	input := "# Refunds\n\nRefunds require reviewer approval.\n\n" +
		"## Approvals\n\nApprovers record a note.\n\n" +
		"# Loyalty\n\nPlatinum tier changes need manual review."
	chunks := ChunkDocument(input)
	if len(chunks) != 3 {
		t.Fatalf("chunks=%d want 3: %#v", len(chunks), chunks)
	}
	if chunks[0].Heading != "Refunds" || chunks[0].SectionIndex != 0 ||
		len(chunks[0].HeadingPath) != 1 || chunks[0].HeadingPath[0] != "Refunds" {
		t.Fatalf("refund section meta: %#v", chunks[0])
	}
	// Nested heading: path includes the parent.
	if chunks[1].Heading != "Approvals" || chunks[1].SectionIndex != 1 ||
		len(chunks[1].HeadingPath) != 2 || chunks[1].HeadingPath[0] != "Refunds" || chunks[1].HeadingPath[1] != "Approvals" {
		t.Fatalf("approvals section meta: %#v", chunks[1])
	}
	// Returning to a top-level heading resets the path.
	if chunks[2].Heading != "Loyalty" || chunks[2].SectionIndex != 2 ||
		len(chunks[2].HeadingPath) != 1 || chunks[2].HeadingPath[0] != "Loyalty" {
		t.Fatalf("loyalty section meta: %#v", chunks[2])
	}
}
