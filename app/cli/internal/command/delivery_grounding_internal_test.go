package command

import (
	"os"
	"testing"
)

// A cited path that exists proves nothing about the criterion. These lock the
// distinction between an anchored citation and one that only looks anchored.
func TestEvidenceExcerptReportsHowItAnchored(t *testing.T) {
	t.Parallel()
	file := []byte("import unittest\n\nclass TagFilterTest(unittest.TestCase):\n    def test_tag_matching_ignores_case(self):\n        pass\n")

	for _, tc := range []struct {
		name    string
		line    int
		heading string
		status  string
		excerpt string
	}{
		{
			name:    "explicit line wins",
			line:    3,
			heading: "test_tag_matching_ignores_case",
			status:  "grounded",
			excerpt: "class TagFilterTest(unittest.TestCase):",
		},
		{
			name:    "heading anchors when no line is given",
			heading: "test_tag_matching_ignores_case",
			status:  "grounded",
			excerpt: "def test_tag_matching_ignores_case(self):",
		},
		{
			name:    "a heading absent from the file is not grounded",
			heading: "test_that_was_never_written",
			status:  "heading_not_found",
		},
		{
			name:   "a bare path is not evidence for one criterion",
			status: "unanchored",
		},
		{
			name:   "a line past the end is reported, not silently clamped",
			line:   99,
			status: "line_out_of_range",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			excerpt, status := evidenceExcerpt(file, tc.line, tc.heading)
			if status != tc.status {
				t.Fatalf("status = %q, want %q", status, tc.status)
			}
			if excerpt != tc.excerpt {
				t.Fatalf("excerpt = %q, want %q", excerpt, tc.excerpt)
			}
		})
	}
}

func TestGroundCompletionEvidenceMarksAFabricatedHeading(t *testing.T) {
	// No t.Parallel: grounding resolves evidence paths against the process
	// working directory, and t.Chdir is incompatible with parallel tests.
	t.Chdir(t.TempDir())
	if err := os.WriteFile("test_notes.py", []byte("def test_real(self):\n    pass\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	body := map[string]any{"criteria": []any{
		map[string]any{"criterion_id": "local-1", "evidence": map[string]any{
			"kind": "test", "path": "test_notes.py", "heading": "test_invented",
		}},
		map[string]any{"criterion_id": "local-2", "evidence": map[string]any{
			"kind": "test", "path": "test_notes.py", "heading": "test_real",
		}},
	}}

	groundCompletionEvidence(body)

	criteria, _ := body["criteria"].([]any)
	for index, wantStatus := range []string{"heading_not_found", "grounded"} {
		entry, _ := criteria[index].(map[string]any)
		evidence, _ := entry["evidence"].(map[string]any)
		grounding, _ := evidence["grounding"].(map[string]any)
		if grounding == nil {
			t.Fatalf("criterion %d has no grounding", index+1)
		}
		if got := grounding["status"]; got != wantStatus {
			t.Fatalf("criterion %d status = %v, want %v", index+1, got, wantStatus)
		}
		if _, hasExcerpt := grounding["excerpt"]; hasExcerpt != (wantStatus == "grounded") {
			t.Fatalf("criterion %d excerpt presence = %v for status %s", index+1, hasExcerpt, wantStatus)
		}
		if grounding["digest"] == nil {
			t.Fatalf("criterion %d lost its file digest", index+1)
		}
	}
}
