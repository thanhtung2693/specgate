package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/governanceops"
	"github.com/specgate/doc-registry/internal/governanceprofile"
	"github.com/specgate/doc-registry/internal/workboard"
)

// publishTestWriter is a minimal ArtifactWriter for thin wiring tests.
type publishTestWriter struct {
	published []artifact.PublishInput
	autoID    string
}

func (w *publishTestWriter) LatestArtifact(_ context.Context, _ string) (*artifact.Artifact, error) {
	return nil, nil
}
func (w *publishTestWriter) NextVersion(_ context.Context, _ string) (string, error) {
	return "v0.1", nil
}
func (w *publishTestWriter) ResolveNextVersion(_ context.Context, _ string, base string) (string, error) {
	return base + ".1", nil
}
func (w *publishTestWriter) Publish(_ context.Context, in artifact.PublishInput) (*artifact.Artifact, error) {
	w.published = append(w.published, in)
	id := w.autoID
	if id == "" {
		id = "art-1"
	}
	return &artifact.Artifact{ID: id, FeatureID: in.FeatureID, Version: in.Version, Status: in.Status}, nil
}
func (w *publishTestWriter) UpdateStatus(_ context.Context, id string, in artifact.StatusUpdate) (*artifact.Artifact, error) {
	return &artifact.Artifact{ID: id, Status: in.Status}, nil
}

// publishTestUpserter is a minimal FeatureUpserter for thin wiring tests.
type publishTestUpserter struct{}

func (u *publishTestUpserter) UpsertFeatureByKey(_ context.Context, key, name string) (*workboard.Feature, error) {
	n := name
	if n == "" {
		n = key
	}
	return &workboard.Feature{ID: "feat-1", Key: key, Name: n, Status: workboard.FeatureStatusCandidate}, nil
}

// publishTestProfileResolver is a minimal ProfileResolver for thin wiring tests.
type publishTestProfileResolver struct{}

func (p *publishTestProfileResolver) ResolveProfile(_ context.Context, _ governanceprofile.ResolveInput) (*governanceprofile.ResolvedProfile, error) {
	return &governanceprofile.ResolvedProfile{
		Key:     "generic_change",
		FullKey: "generic_change",
		Version: "1",
		Digest:  "sha256:generic",
	}, nil
}

func buildPublishSvc(w governanceops.ArtifactWriter) *governanceops.Service {
	return &governanceops.Service{
		ArtifactWriter:  w,
		FeatureUpserter: &publishTestUpserter{},
		ProfileResolver: &publishTestProfileResolver{},
		AppBaseURL:      "https://app.example.com",
	}
}

// TestSpecgatePublishHandler_JSONOutput verifies the thin adapter marshals the
// service result into valid JSON with the expected top-level fields.
func TestSpecgatePublishHandler_JSONOutput(t *testing.T) {
	t.Parallel()
	writer := &publishTestWriter{autoID: "art-abc"}
	svc := buildPublishSvc(writer)
	handler := NewSpecgatePublishHandler(svc)

	out, err := handler(context.Background(), SpecgatePublishInput{
		FeatureKey: "checkout-loyalty",
		Documents: []DocumentInputDTO{
			{Path: "spec.md", Role: "spec", Content: "# Spec"},
		},
		SourceKind: "cursor",
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	var result SpecgatePublishResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.ArtifactID == "" {
		t.Error("artifact_id is empty")
	}
	if result.Version == "" {
		t.Error("version is empty")
	}
	if result.Status == "" {
		t.Error("status is empty")
	}
	if !strings.HasSuffix(result.ReviewURL, "/artifacts/"+result.ArtifactID) {
		t.Errorf("review_url = %q, want suffix /artifacts/%s", result.ReviewURL, result.ArtifactID)
	}
	if len(writer.published) != 1 {
		t.Fatalf("Publish called %d times, want 1", len(writer.published))
	}
}

// TestSpecgatePublishHandler_PropagatesError verifies errors from the service
// are returned without modification.
func TestSpecgatePublishHandler_PropagatesError(t *testing.T) {
	t.Parallel()
	// nil ArtifactWriter → service returns error
	svc := &governanceops.Service{
		FeatureUpserter: &publishTestUpserter{},
		ProfileResolver: &publishTestProfileResolver{},
	}
	handler := NewSpecgatePublishHandler(svc)
	_, err := handler(context.Background(), SpecgatePublishInput{
		FeatureKey: "checkout-loyalty",
		Documents:  []DocumentInputDTO{{Path: "spec.md", Role: "spec", Content: "# S"}},
	})
	if err == nil {
		t.Fatal("expected error when ArtifactWriter is nil")
	}
}

// TestSpecgateWhoami verifies the health/identity handler returns a valid result.
func TestSpecgateWhoami(t *testing.T) {
	t.Parallel()

	handler := NewSpecgateWhoamiHandler(5, 3, "")

	result, err := handler(context.Background(), struct{}{})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	var out SpecgateWhoamiResult
	if err := json.Unmarshal([]byte(result), &out); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if !out.OK {
		t.Error("ok is false, want true")
	}
	if out.Service != "specgate" {
		t.Errorf("service = %q, want specgate", out.Service)
	}
	if out.Tools <= 0 {
		t.Errorf("tools = %d, want > 0", out.Tools)
	}
}
