package knowledge

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	targetChunkTokens = 650
	maxChunkTokens    = 850
)

// ChunkCandidate is one chunk of text plus the Markdown section metadata it came
// from. Multiple candidates can share a section (heading/heading_path/section
// index) when a section body is large enough to split.
type ChunkCandidate struct {
	Text         string
	Heading      string
	HeadingPath  []string
	SectionIndex int
}

var headingRE = regexp.MustCompile(`^(#{1,6})\s+(.*\S)\s*$`)

// ChunkDocument splits Markdown/text into heading-bounded sections, keeping each
// heading attached to its section body, then splits oversized sections with the
// existing token target/max. Every chunk carries its section's heading,
// heading_path (the heading stack from top level down), and section_index.
func ChunkDocument(src string) []ChunkCandidate {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	lines := strings.Split(src, "\n")

	type section struct {
		heading     string
		headingPath []string
		lines       []string
	}
	type frame struct {
		level int
		title string
	}
	var sections []section
	var stack []frame
	var cur section
	started := false
	for _, line := range lines {
		if m := headingRE.FindStringSubmatch(strings.TrimRight(line, " \t")); m != nil {
			if started {
				sections = append(sections, cur)
			}
			level := len(m[1])
			title := strings.TrimSpace(m[2])
			for len(stack) > 0 && stack[len(stack)-1].level >= level {
				stack = stack[:len(stack)-1]
			}
			stack = append(stack, frame{level: level, title: title})
			path := make([]string, len(stack))
			for i, f := range stack {
				path[i] = f.title
			}
			cur = section{heading: title, headingPath: path, lines: []string{line}}
			started = true
			continue
		}
		if !started {
			cur = section{}
			started = true
		}
		cur.lines = append(cur.lines, line)
	}
	if started {
		sections = append(sections, cur)
	}

	var out []ChunkCandidate
	sectionIndex := 0
	for _, s := range sections {
		body := strings.TrimSpace(strings.Join(s.lines, "\n"))
		if body == "" {
			continue
		}
		for _, text := range groupBlocksIntoChunks(semanticBlocks(body)) {
			out = append(out, ChunkCandidate{
				Text:         text,
				Heading:      s.heading,
				HeadingPath:  s.headingPath,
				SectionIndex: sectionIndex,
			})
		}
		sectionIndex++
	}
	return out
}

// groupBlocksIntoChunks packs semantic blocks up to the target token size,
// flushing oversized blocks through sentence splitting. Shared by ChunkDocument
// across each section's body.
func groupBlocksIntoChunks(blocks []string) []string {
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
