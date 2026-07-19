package knowledge

import (
	"strings"
)

const defaultContextMaxChars = 4000

func normalizeContextMode(mode ContextMode) ContextMode {
	switch mode {
	case ContextModeSection, ContextModeDocument:
		return mode
	default:
		return ContextModeChunk
	}
}

func normalizeContextMaxChars(n int) int {
	if n <= 0 {
		return defaultContextMaxChars
	}
	if n > 12000 {
		return 12000
	}
	return n
}

// expandSectionContext returns the containing section's chunks (siblings that
// share the hit chunk's section_index) joined and bounded by maxChars, plus the
// covered chunk-index range. It never crosses into another section. The hit
// chunk is always included even if it alone exceeds the cap.
func expandSectionContext(chunks []Chunk, hitChunkIndex int, maxChars int) (string, int, int) {
	maxChars = normalizeContextMaxChars(maxChars)
	hit := -1
	for i := range chunks {
		if chunks[i].ChunkIndex == hitChunkIndex {
			hit = i
			break
		}
	}
	if hit < 0 {
		return "", hitChunkIndex, hitChunkIndex
	}
	section := chunks[hit].SectionIndex
	var parts []string
	start := chunks[hit].ChunkIndex
	end := chunks[hit].ChunkIndex
	for _, chunk := range chunks {
		if chunk.SectionIndex != section {
			continue
		}
		candidate := strings.TrimSpace(strings.Join(append(parts, chunk.ChunkText), "\n\n"))
		if len(candidate) > maxChars && len(parts) > 0 {
			break
		}
		parts = append(parts, chunk.ChunkText)
		if chunk.ChunkIndex < start {
			start = chunk.ChunkIndex
		}
		if chunk.ChunkIndex > end {
			end = chunk.ChunkIndex
		}
	}
	return truncate(strings.TrimSpace(strings.Join(parts, "\n\n")), maxChars), start, end
}

// joinChunksCapped joins the ordered chunks with blank-line separators, stopping
// before exceeding maxChars, and reports whether anything was dropped or the
// result was truncated. The first chunk is always included.
func joinChunksCapped(chunks []Chunk, maxChars int) (string, bool) {
	maxChars = normalizeContextMaxChars(maxChars)
	var parts []string
	truncated := false
	for _, chunk := range chunks {
		candidate := strings.TrimSpace(strings.Join(append(parts, chunk.ChunkText), "\n\n"))
		if len(candidate) > maxChars && len(parts) > 0 {
			truncated = true
			break
		}
		parts = append(parts, chunk.ChunkText)
	}
	joined := strings.TrimSpace(strings.Join(parts, "\n\n"))
	if len(joined) > maxChars {
		joined = truncate(joined, maxChars)
		truncated = true
	}
	return joined, truncated
}
