package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/artifactedit"
	"github.com/specgate/doc-registry/internal/governanceops"
)

// artifactUpdateTestReader implements ContextPackArtifactReader for wiring tests.
type artifactUpdateTestReader struct {
	art   *artifact.Artifact
	files map[string]string
}

func (a *artifactUpdateTestReader) Get(_ context.Context, _ string) (*artifact.Artifact, error) {
	if a.art == nil {
		return nil, artifact.ErrNotFound
	}
	return a.art, nil
}
func (a *artifactUpdateTestReader) FileContent(_ context.Context, _ string, path string) ([]byte, error) {
	content, ok := a.files[path]
	if !ok {
		return nil, artifact.ErrFileNotFound
	}
	return []byte(content), nil
}

// artifactUpdateTestEditStore implements the minimal ArtifactEditStore interface.
type artifactUpdateTestEditStore struct {
	sessions []artifactedit.Session
}

func (s *artifactUpdateTestEditStore) CreateSession(_ context.Context, session artifactedit.Session, _, _ map[string]string) error {
	s.sessions = append(s.sessions, session)
	return nil
}
func (s *artifactUpdateTestEditStore) ListProposals(_ context.Context) ([]artifactedit.Session, error) {
	return s.sessions, nil
}

// Unused methods needed to satisfy the broader artifactedit.Store interface (not used by governanceops).
func (s *artifactUpdateTestEditStore) LoadSession(context.Context, string) (*artifactedit.SessionState, error) {
	return nil, artifactedit.ErrNotFound
}
func (s *artifactUpdateTestEditStore) UpdateWorkingFile(context.Context, string, string, string, time.Time) error {
	return artifactedit.ErrNotFound
}
func (s *artifactUpdateTestEditStore) SetSessionMeta(context.Context, string, artifactedit.SessionMeta) error {
	return artifactedit.ErrNotFound
}
func (s *artifactUpdateTestEditStore) AppendHunkDecision(context.Context, artifactedit.HunkDecision) error {
	return artifactedit.ErrNotFound
}
func (s *artifactUpdateTestEditStore) CreateRevision(context.Context, artifactedit.Revision) error {
	return nil
}
func (s *artifactUpdateTestEditStore) GetRevision(context.Context, string) (*artifactedit.Revision, error) {
	return nil, artifactedit.ErrNotFound
}
func (s *artifactUpdateTestEditStore) ListRevisions(context.Context, string) ([]artifactedit.Revision, error) {
	return nil, nil
}

// TestDraftArtifactUpdateHandler_JSONOutput verifies the thin adapter marshals
// the service result into valid JSON with the expected shape.
func TestDraftArtifactUpdateHandler_JSONOutput(t *testing.T) {
	t.Parallel()
	reader := &artifactUpdateTestReader{
		art: &artifact.Artifact{
			ID:      "art-1",
			Version: "1",
			Files:   []artifact.File{{Path: "spec.md"}},
		},
		files: map[string]string{"spec.md": "# Old Spec"},
	}
	editStore := &artifactUpdateTestEditStore{}
	svc := &governanceops.Service{DraftArtifacts: reader, EditStore: editStore}
	handler := NewDraftArtifactUpdateHandler(svc)

	out, err := handler(context.Background(), DraftArtifactUpdateInput{
		ArtifactID: "art-1",
		Summary:    "Update spec",
		Files:      map[string]string{"spec.md": "# New Spec"},
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	for _, want := range []string{`"drafted"`, `"session_id"`, `"source_kind"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %s: %s", want, out)
		}
	}
	if len(editStore.sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(editStore.sessions))
	}
}

// TestDraftArtifactUpdateHandler_PropagatesError verifies errors from the
// service are returned without modification.
func TestDraftArtifactUpdateHandler_PropagatesError(t *testing.T) {
	t.Parallel()
	// nil stores → service returns an error
	svc := &governanceops.Service{}
	handler := NewDraftArtifactUpdateHandler(svc)

	_, err := handler(context.Background(), DraftArtifactUpdateInput{
		ArtifactID: "art-1",
		Summary:    "Update spec",
		Files:      map[string]string{"spec.md": "# New"},
	})
	if err == nil {
		t.Fatal("expected error when stores are nil")
	}
}
