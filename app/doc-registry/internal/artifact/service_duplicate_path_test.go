package artifact

import (
	"context"
	"strings"
	"testing"
)

// A role is a routing label, so one source may carry several — a single
// specification is commonly both the spec and the plan. `artifact_files` keys on
// (artifact_id, path, role) to match, and every role of one path addresses the
// same stored object because the object key derives from the path. Keying on the
// path alone silently kept one row and dropped the other declared role from an
// approved, immutable snapshot.
func TestPublishAcceptsOneSourceUnderSeveralRoles(t *testing.T) {
	t.Parallel()
	svc, ctx := newDuplicatePathService(t)

	published, err := svc.Publish(ctx, PublishInput{
		WorkspaceID: "ws-1",
		FeatureID:   "feat-1",
		Version:     "v1",
		Documents: []DocumentInput{
			{Path: "docs/spec.md", Role: "spec", Content: []byte("# Spec\n")},
			{Path: "docs/spec.md", Role: "plan", Content: []byte("# Spec\n")},
		},
	})
	if err != nil {
		t.Fatalf("one source under two roles was rejected: %v", err)
	}
	if len(published.Files) != 2 {
		t.Fatalf("stored %d files, want both roles retained: %+v", len(published.Files), published.Files)
	}
	roles := map[Role]bool{}
	for _, f := range published.Files {
		if f.Path != "docs/spec.md" {
			t.Fatalf("unexpected path %q", f.Path)
		}
		roles[f.Role] = true
	}
	if !roles["spec"] || !roles["plan"] {
		t.Fatalf("roles = %v, want both spec and plan", roles)
	}
}

// The same path under the same role is still a mistake, refused rather than
// silently collapsed.
func TestPublishRefusesTheSamePathAndRoleTwice(t *testing.T) {
	t.Parallel()
	svc, ctx := newDuplicatePathService(t)

	_, err := svc.Publish(ctx, PublishInput{
		WorkspaceID: "ws-1",
		FeatureID:   "feat-1",
		Version:     "v1",
		Documents: []DocumentInput{
			{Path: "docs/spec.md", Role: "spec", Content: []byte("a")},
			{Path: "docs/spec.md", Role: "spec", Content: []byte("b")},
		},
	})
	if err == nil {
		t.Fatal("the same path and role twice was accepted")
	}
	for _, want := range []string{"docs/spec.md", "same role", "spec"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention %q", err, want)
		}
	}
}

// Distinct paths are the supported way to cover two roles and must keep working.
func TestPublishAcceptsOneRolePerPath(t *testing.T) {
	t.Parallel()
	svc, ctx := newDuplicatePathService(t)

	published, err := svc.Publish(ctx, PublishInput{
		WorkspaceID: "ws-1",
		FeatureID:   "feat-1",
		Version:     "v1",
		Documents: []DocumentInput{
			{Path: "docs/spec.md", Role: "spec", Content: []byte("# Spec\n")},
			{Path: "docs/plan.md", Role: "plan", Content: []byte("# Plan\n")},
		},
	})
	if err != nil {
		t.Fatalf("distinct paths were rejected: %v", err)
	}
	if published == nil {
		t.Fatal("no artifact returned")
	}
}

func newDuplicatePathService(t *testing.T) (*RegistryService, context.Context) {
	t.Helper()
	svc, _, _ := newTestService(t)
	return svc, WithWorkspace(context.Background(), "ws-1")
}
