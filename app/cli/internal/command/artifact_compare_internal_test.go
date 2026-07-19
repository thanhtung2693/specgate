package command

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/client"
)

func TestBuildArtifactComparisonClassifiesExplicitDocuments(t *testing.T) {
	t.Parallel()

	body := map[string]any{"documents": []any{
		map[string]any{"path": "docs/spec.md", "role": "spec", "content": "new spec"},
		map[string]any{"path": "docs/plan.md", "role": "plan", "content": "same plan"},
		map[string]any{"path": "docs/new.md", "role": "research", "content": "new"},
		map[string]any{"path": "docs/legacy.md", "role": "reference", "content": "legacy"},
	}}
	base := &client.Artifact{ID: "art-base", Version: "v0.2", SnapshotDigest: "sha256:package"}
	baseFiles := []client.ArtifactFile{
		{Path: "docs/spec.md", Role: "design", ContentSHA256: digestArtifactContent("old spec")},
		{Path: "docs/plan.md", Role: "plan", ContentSHA256: digestArtifactContent("same plan")},
		{Path: "docs/removed.md", Role: "verification", ContentSHA256: digestArtifactContent("gone")},
	}

	got, err := buildArtifactComparison(body, base, baseFiles)
	if err != nil {
		t.Fatal(err)
	}
	if got.BaseArtifactID != "art-base" || got.BaseVersion != "v0.2" || got.BaseSnapshotDigest != "sha256:package" {
		t.Fatalf("base identity = %#v", got)
	}
	wantCounts := artifactComparisonCounts{Added: 2, Removed: 1, Changed: 1, Unchanged: 1}
	if got.Counts != wantCounts {
		t.Fatalf("counts = %#v, want %#v", got.Counts, wantCounts)
	}
	wantPaths := []string{"docs/legacy.md", "docs/new.md", "docs/plan.md", "docs/spec.md"}
	paths := make([]string, 0, len(got.Files))
	states := map[string]string{}
	changes := map[string][]string{}
	for _, file := range got.Files {
		paths = append(paths, file.Path)
		states[file.Path] = file.State
		changes[file.Path] = file.Changes
	}
	if !reflect.DeepEqual(paths, wantPaths) {
		t.Fatalf("paths = %#v, want %#v", paths, wantPaths)
	}
	if states["docs/new.md"] != "added" || states["docs/plan.md"] != "unchanged" || states["docs/spec.md"] != "changed" || states["docs/legacy.md"] != "added" {
		t.Fatalf("states = %#v", states)
	}
	if !reflect.DeepEqual(changes["docs/spec.md"], []string{"content", "role"}) {
		t.Fatalf("spec changes = %#v", changes["docs/spec.md"])
	}
	if len(got.Removed) != 1 || got.Removed[0].Path != "docs/removed.md" || got.Removed[0].State != "removed" {
		t.Fatalf("removed = %#v", got.Removed)
	}
}

func TestBuildArtifactComparisonRejectsMissingBaseHash(t *testing.T) {
	t.Parallel()

	body := map[string]any{"documents": []any{
		map[string]any{"path": "docs/spec.md", "role": "spec", "content": "spec"},
	}}
	_, err := buildArtifactComparison(body, &client.Artifact{ID: "base"}, []client.ArtifactFile{
		{Path: "docs/spec.md", Role: "spec"},
	})
	if err == nil || !strings.Contains(err.Error(), "missing content_sha256") {
		t.Fatalf("err = %v, want missing content_sha256", err)
	}
}

func TestBuildArtifactComparisonRejectsDuplicateNormalizedPaths(t *testing.T) {
	t.Parallel()

	body := map[string]any{"documents": []any{
		map[string]any{"path": "docs/./spec.md", "role": "spec", "content": "one"},
		map[string]any{"path": "docs/spec.md", "role": "spec", "content": "two"},
	}}
	_, err := buildArtifactComparison(body, &client.Artifact{ID: "base"}, nil)
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("err = %v, want duplicate path error", err)
	}
}

func TestBuildArtifactComparisonNormalizesRolesLikeServer(t *testing.T) {
	t.Parallel()

	body := map[string]any{"documents": []any{
		map[string]any{"path": "spec.md", "role": " Spec ", "content": "same"},
		map[string]any{"path": "notes.md", "role": "invented", "content": "same"},
	}}
	baseFiles := []client.ArtifactFile{
		{Path: "spec.md", Role: "spec", ContentSHA256: digestArtifactContent("same")},
		{Path: "notes.md", Role: "unspecified", ContentSHA256: digestArtifactContent("same")},
	}

	got, err := buildArtifactComparison(body, &client.Artifact{ID: "base"}, baseFiles)
	if err != nil {
		t.Fatal(err)
	}
	if got.Counts.Unchanged != 2 || got.Counts.Changed != 0 {
		t.Fatalf("counts = %#v, want two unchanged canonical roles", got.Counts)
	}
	if got.Files[0].Role != "unspecified" || got.Files[1].Role != "spec" {
		t.Fatalf("roles = %#v", []string{got.Files[0].Role, got.Files[1].Role})
	}
}

func TestBuildArtifactComparisonRejectsUnsafePaths(t *testing.T) {
	t.Parallel()

	for _, unsafe := range []string{"", "/absolute.md", "docs/../escape.md", `docs\spec.md`, "docs/\x00spec.md"} {
		unsafe := unsafe
		t.Run(unsafe, func(t *testing.T) {
			t.Parallel()
			body := map[string]any{"documents": []any{
				map[string]any{"path": unsafe, "role": "spec", "content": "x"},
			}}
			_, err := buildArtifactComparison(body, &client.Artifact{ID: "base"}, nil)
			if err == nil || !strings.Contains(err.Error(), "unsafe") {
				t.Fatalf("path %q err = %v, want unsafe path error", unsafe, err)
			}
		})
	}
}

func TestWriteArtifactComparisonUsesStableReadableStates(t *testing.T) {
	t.Parallel()

	comparison := artifactComparison{
		BaseArtifactID: "art-base",
		BaseVersion:    "v0.2",
		Files: []artifactComparisonFile{
			{Path: "a.md", Role: "spec", State: "changed", Changes: []string{"content"}},
			{Path: "b.md", Role: "plan", State: "unchanged"},
		},
		Removed: []artifactComparisonFile{{Path: "z.md", Role: "verification", State: "removed"}},
	}
	var out bytes.Buffer
	writeArtifactComparison(&out, comparison)
	got := out.String()
	for _, want := range []string{"art-base", "v0.2", "changed", "a.md", "content", "unchanged", "b.md", "removed", "z.md"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
	if strings.Index(got, "a.md") > strings.Index(got, "b.md") || strings.Index(got, "b.md") > strings.Index(got, "z.md") {
		t.Fatalf("output not stably ordered:\n%s", got)
	}
}
