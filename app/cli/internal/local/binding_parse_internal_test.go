package local

import "testing"

// `@check:<name>` is the one place a human hand-writes a machine contract, so
// the parse has to survive the shapes people actually type. The original parser
// required the binding to be the final whitespace-separated field in exact lower
// case with no trailing punctuation. Every other shape returned no binding at
// all, which is the dangerous direction: the criterion reads as enforced to its
// author while delivery review quietly judges it on the agent's own claim.
func TestBindingSurvivesTheShapesPeopleWrite(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, raw, text, binding string }{
		{"plain", "Retries stop @check:unit", "Retries stop", "unit"},
		{"trailing period", "Retries stop @check:unit.", "Retries stop", "unit"},
		{"trailing comma", "Retries stop @check:unit,", "Retries stop", "unit"},
		{"upper case", "Retries stop @CHECK:unit", "Retries stop", "unit"},
		{"mixed case", "Retries stop @Check:Unit", "Retries stop", "Unit"},
		{"binding first", "@check:unit Retries stop", "Retries stop", "unit"},
		{"binding mid-sentence", "Retries @check:unit stop cleanly", "Retries stop cleanly", "unit"},
		{"no binding", "Retries stop cleanly", "Retries stop cleanly", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			text, binding := ParseAcceptanceCriterionBinding(tc.raw)
			if binding != tc.binding {
				t.Fatalf("binding = %q, want %q", binding, tc.binding)
			}
			if text != tc.text {
				t.Fatalf("text = %q, want %q", text, tc.text)
			}
		})
	}
}

// Where the intent is unresolvable, guessing is worse than refusing: the author
// gets to fix it while the criterion is still being written.
func TestUnresolvableBindingsAreReportedNotGuessed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, raw, want string }{
		{"space after colon", "Retries stop @check: unit", "no check name"},
		{"two bindings", "Retries stop @check:unit and @check:lint", "more than one check"},
		{"missing colon", "Retries stop @check unit", "missing the colon"},
		{"binding only", "@check:unit", "no criterion text"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			problem := AcceptanceCriterionBindingProblem(tc.raw)
			if problem == "" {
				t.Fatalf("%q was accepted silently", tc.raw)
			}
			if !contains(problem, tc.want) {
				t.Fatalf("problem = %q, want it to mention %q", problem, tc.want)
			}
		})
	}
}

func TestWellFormedCriteriaReportNoProblem(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		"Retries stop @check:unit",
		"Retries stop @check:unit.",
		"@check:unit Retries stop",
		"Retries stop cleanly",
		"Email addresses are validated",
	} {
		if problem := AcceptanceCriterionBindingProblem(raw); problem != "" {
			t.Fatalf("%q reported %q", raw, problem)
		}
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
