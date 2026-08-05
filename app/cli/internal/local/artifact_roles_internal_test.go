package local

import (
	"strings"
	"testing"
)

// A role is a routing label. One source often carries several — a single
// specification is commonly both the spec and the plan — and the snapshot digest
// has always hashed path and role together, as Full mode does. Keying uniqueness
// on the path alone rejected manifests `--preview` had already accepted and left
// an author whose spec is one document unable to satisfy a multi-role policy at
// all: readiness failed on required_roles_present and approval refused.
func TestOneSourceCanCarrySeveralRoles(t *testing.T) {
	t.Parallel()
	documents, digest, err := validateArtifactDocuments([]ArtifactDocumentInput{
		{Path: "docs/spec.md", Role: "spec", Content: []byte("# Spec\n")},
		{Path: "docs/spec.md", Role: "plan", Content: []byte("# Spec\n")},
	})
	if err != nil {
		t.Fatalf("one source under two roles was rejected: %v", err)
	}
	if len(documents) != 2 {
		t.Fatalf("documents = %d, want 2", len(documents))
	}
	if digest == "" {
		t.Fatal("no snapshot digest computed")
	}
	roles := []string{documents[0].Role, documents[1].Role}
	if roles[0] != "plan" || roles[1] != "spec" {
		t.Fatalf("roles = %v, want a stable path-then-role order", roles)
	}
}

func TestContextPackRendersSharedRoleSourceOnce(t *testing.T) {
	t.Parallel()
	const content = "# Shared contract\n\nOne source, two roles."
	markdown := contextMarkdown("LOCAL-1", "Implement shared contract", Artifact{
		ID: "artifact-1", Version: 1, SnapshotDigest: "sha256:snapshot",
		Documents: []ArtifactDocument{
			{Path: "docs/spec.md", Role: "plan", Content: []byte(content), Digest: "sha256:same"},
			{Path: "docs/spec.md", Role: "spec", Content: []byte(content), Digest: "sha256:same"},
		},
	}, []string{"The contract is implemented"})

	if got := strings.Count(markdown, content); got != 1 {
		t.Fatalf("shared source rendered %d times:\n%s", got, markdown)
	}
}

// Duplicate paths make the order between rows ambiguous unless role breaks the
// tie, and an unstable order changes the snapshot digest for identical input.
func TestSnapshotDigestIsStableAcrossInputOrder(t *testing.T) {
	t.Parallel()
	spec := ArtifactDocumentInput{Path: "docs/spec.md", Role: "spec", Content: []byte("# Spec\n")}
	plan := ArtifactDocumentInput{Path: "docs/spec.md", Role: "plan", Content: []byte("# Spec\n")}

	_, first, err := validateArtifactDocuments([]ArtifactDocumentInput{spec, plan})
	if err != nil {
		t.Fatal(err)
	}
	_, second, err := validateArtifactDocuments([]ArtifactDocumentInput{plan, spec})
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("digest depends on input order: %s vs %s", first, second)
	}
}

// The same path under the same role twice is still a mistake, and the error must
// say which document and role rather than blaming the path.
func TestSamePathAndRoleTwiceIsRejected(t *testing.T) {
	t.Parallel()
	_, _, err := validateArtifactDocuments([]ArtifactDocumentInput{
		{Path: "docs/spec.md", Role: "spec", Content: []byte("a")},
		{Path: "docs/spec.md", Role: "spec", Content: []byte("b")},
	})
	if err == nil {
		t.Fatal("expected a rejection")
	}
	for _, want := range []string{"docs/spec.md", "spec", "twice"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention %q", err, want)
		}
	}
}

// The approval blocker used to say "resolve gate tasks" for every failure. When
// a deterministic gate was the blocker and every IDE task had been submitted,
// that sent the author to a command reporting nothing to do.
func TestUnresolvedReadinessRemedyNamesTheBlockingGates(t *testing.T) {
	t.Parallel()
	remedy := unresolvedReadinessRemedy(map[string]any{
		"has_documents":          map[string]any{"gate": "has_documents", "state": "pass"},
		"required_roles_present": map[string]any{"gate": "required_roles_present", "state": "fail", "hint": "missing role(s) plan"},
	}, "art-1")

	for _, want := range []string{"required_roles_present", "fail", "missing role(s) plan"} {
		if !strings.Contains(remedy, want) {
			t.Fatalf("remedy %q does not mention %q", remedy, want)
		}
	}
	if strings.Contains(remedy, "gates tasks list") {
		t.Fatalf("remedy points at an empty task list when no task is pending: %q", remedy)
	}
	if strings.Contains(remedy, "has_documents") {
		t.Fatalf("remedy names a passing gate: %q", remedy)
	}
}

func TestUnresolvedReadinessRemedyStillPointsAtPendingTasks(t *testing.T) {
	t.Parallel()
	remedy := unresolvedReadinessRemedy(map[string]any{
		"scope_clear": map[string]any{"gate": "scope_clear", "state": "not_run", "hint": "IDE-agent result required"},
	}, "art-1")

	if !strings.Contains(remedy, "gates tasks list art-1") {
		t.Fatalf("a not_run gate must still route to its task: %q", remedy)
	}
}
