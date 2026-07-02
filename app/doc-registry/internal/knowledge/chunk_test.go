package knowledge

import "testing"

func TestChunkTextStable(t *testing.T) {
	t.Parallel()
	src := "# Heading\n\n" + "First paragraph has a few words.\n\n" + "Second paragraph stays together."
	chunks := ChunkText(src)
	if len(chunks) != 1 {
		t.Fatalf("len=%d chunks=%q", len(chunks), chunks)
	}
	if chunks[0] != src {
		t.Fatalf("chunk=%q", chunks[0])
	}
}
