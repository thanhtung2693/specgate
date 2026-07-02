package knowledge

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	targetChunkTokens = 650
	maxChunkTokens    = 850
)

func ChunkText(src string) []string {
	blocks := semanticBlocks(src)
	if len(blocks) == 0 {
		return nil
	}
	var out []string
	var cur []string
	curTokens := 0
	flush := func() {
		if len(cur) == 0 {
			return
		}
		out = append(out, strings.TrimSpace(strings.Join(cur, "\n\n")))
		cur = nil
		curTokens = 0
	}
	for _, block := range blocks {
		tokens := tokenCount(block)
		if tokens > maxChunkTokens {
			flush()
			out = append(out, splitLargeBlock(block)...)
			continue
		}
		if curTokens > 0 && curTokens+tokens > targetChunkTokens {
			flush()
		}
		cur = append(cur, block)
		curTokens += tokens
	}
	flush()
	return out
}

func semanticBlocks(src string) []string {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	parts := strings.Split(src, "\n\n")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) > 0 {
		return out
	}
	src = strings.TrimSpace(src)
	if src == "" {
		return nil
	}
	return []string{src}
}

func splitLargeBlock(block string) []string {
	sentences := splitSentences(block)
	var out []string
	var cur []string
	curTokens := 0
	for _, s := range sentences {
		t := tokenCount(s)
		if curTokens > 0 && curTokens+t > targetChunkTokens {
			out = append(out, strings.TrimSpace(strings.Join(cur, " ")))
			cur = nil
			curTokens = 0
		}
		cur = append(cur, s)
		curTokens += t
	}
	if len(cur) > 0 {
		out = append(out, strings.TrimSpace(strings.Join(cur, " ")))
	}
	return out
}

func splitSentences(src string) []string {
	var out []string
	start := 0
	for i, r := range src {
		if r != '.' && r != '!' && r != '?' && r != '\n' {
			continue
		}
		rlen := utf8.RuneLen(r)
		if rlen < 1 {
			continue
		}
		end := i + rlen
		if end > len(src) {
			end = len(src)
		}
		// Defensive: avoid panic if indices ever get out of sync (e.g. odd UTF-8 edge cases).
		if start > i {
			start = i
		}
		if start >= end {
			continue
		}
		part := strings.TrimSpace(src[start:end])
		if part != "" {
			out = append(out, part)
		}
		start = end
	}
	if start < len(src) {
		if tail := strings.TrimSpace(src[start:]); tail != "" {
			out = append(out, tail)
		}
	}
	return out
}

func tokenCount(src string) int {
	return len(strings.FieldsFunc(src, func(r rune) bool {
		return unicode.IsSpace(r)
	}))
}
