package artifact

import (
	"context"
	"strings"
	"testing"
)

// `artifact_files` is keyed on (artifact_id, path) and FileContent reads by path,
// so a stored document holds exactly one role. A manifest naming the same path
// twice used to publish successfully and keep one row — the other declared role
// disappeared from an approved, immutable snapshot with nothing reporting it.
func TestPublishRefusesTheSamePathTwice(t *testing.T) {
	t.Parallel()
	svc, ctx := newDuplicatePathService(t)

	_, err := svc.Publish(ctx, PublishInput{
		WorkspaceID: "ws-1",
		FeatureID:   "feat-1",
		Version:     "v1",
		Documents: []DocumentInput{
			{Path: "docs/spec.md", Role: "spec", Content: []byte("# Spec\n")},
			{Path: "docs/spec.md", Role: "plan", Content: []byte("# Spec\n")},
		},
	})

	if err == nil {
		t.Fatal("publishing one path under two roles was accepted; a role would be lost silently")
	}
	for _, want := range []string{"docs/spec.md", "mapped twice", "spec", "plan"} {
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
