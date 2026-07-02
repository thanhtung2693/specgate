package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/specgate/doc-registry/internal/artifact"
)

type mockArtifactService struct {
	listFn        func(ctx context.Context, f artifact.ListFilter) ([]artifact.Artifact, error)
	getFn         func(ctx context.Context, id string) (*artifact.Artifact, error)
	fileContentFn func(ctx context.Context, id string, path string) ([]byte, error)
}

func (m *mockArtifactService) List(ctx context.Context, f artifact.ListFilter) ([]artifact.Artifact, error) {
	return m.listFn(ctx, f)
}

func (m *mockArtifactService) Get(ctx context.Context, id string) (*artifact.Artifact, error) {
	return m.getFn(ctx, id)
}

func (m *mockArtifactService) FileContent(ctx context.Context, id string, path string) ([]byte, error) {
	return m.fileContentFn(ctx, id, path)
}

func (m *mockArtifactService) RefreshReadinessRuns(context.Context, string, []artifact.ReadinessEvaluation) ([]artifact.ReadinessRun, error) {
	return nil, nil
}
func (m *mockArtifactService) ListReadinessRuns(context.Context, string, int) ([]artifact.ReadinessRun, error) {
	return nil, nil
}

func TestArtifactSearch_Basic(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	svc := &mockArtifactService{
		listFn: func(_ context.Context, f artifact.ListFilter) ([]artifact.Artifact, error) {
			_ = f
			return []artifact.Artifact{
				{
					ID:          "art-1",
					FeatureID:   "feat-1",
					Status:      artifact.StatusDraft,
					ImpactLevel: artifact.ImpactLevelHigh,
					CreatedAt:   now,
					Services: []artifact.ServiceRef{
						{Name: "svc-a", Kind: "service"},
					},
				},
			}, nil
		},
	}

	handler := NewArtifactSearchHandler(svc)
	result, err := handler(context.Background(), ArtifactSearchInput{
		FeatureID: "feat-1",
		Limit:     10,
	})
	if err != nil {
		t.Fatal(err)
	}

	var output struct {
		Items []struct {
			ID        string   `json:"id"`
			FeatureID string   `json:"feature_id"`
			Status    string   `json:"status"`
			Services  []string `json:"services"`
		} `json:"items"`
		Truncated bool `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(result), &output); err != nil {
		t.Fatal(err)
	}
	if len(output.Items) != 1 {
		t.Fatalf("got %d items", len(output.Items))
	}
	if output.Items[0].FeatureID != "feat-1" {
		t.Errorf("feature_id = %q", output.Items[0].FeatureID)
	}
}

func TestArtifactSearch_Truncated(t *testing.T) {
	t.Parallel()
	svc := &mockArtifactService{
		listFn: func(_ context.Context, f artifact.ListFilter) ([]artifact.Artifact, error) {
			results := make([]artifact.Artifact, f.Limit)
			for i := range results {
				results[i] = artifact.Artifact{ID: "art", FeatureID: "f", Status: artifact.StatusDraft, ImpactLevel: artifact.ImpactLevelLow, CreatedAt: time.Now().UTC()}
			}
			return results, nil
		},
	}

	handler := NewArtifactSearchHandler(svc)
	result, err := handler(context.Background(), ArtifactSearchInput{
		Limit: 3,
	})
	if err != nil {
		t.Fatal(err)
	}

	var output struct {
		Items     []json.RawMessage `json:"items"`
		Truncated bool              `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(result), &output); err != nil {
		t.Fatal(err)
	}
	if !output.Truncated {
		t.Error("expected truncated=true")
	}
	if len(output.Items) != 3 {
		t.Errorf("got %d items, want 3", len(output.Items))
	}
}

func TestReadArtifactBundleSelectedFiles(t *testing.T) {
	t.Parallel()
	svc := &mockArtifactService{
		getFn: func(_ context.Context, id string) (*artifact.Artifact, error) {
			if id != "art-1" {
				t.Errorf("artifact_id = %q", id)
			}
			return &artifact.Artifact{
				ID:          "art-1",
				FeatureID:   "checkout",
				Version:     "v0.1",
				Status:      artifact.StatusDraft,
				ImpactLevel: artifact.ImpactLevelLow,
			}, nil
		},
		fileContentFn: func(_ context.Context, id string, path string) ([]byte, error) {
			if id != "art-1" {
				t.Errorf("artifact_id = %q", id)
			}
			return []byte("content for " + path), nil
		},
	}

	handler := NewArtifactBundleReadHandler(svc)
	result, err := handler(context.Background(), ArtifactBundleReadInput{
		ArtifactID: "art-1",
		Files:      []string{"prd", "spec"},
		MaxChars:   100,
	})
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Artifact artifactBundleMeta        `json:"artifact"`
		Files    map[string]map[string]any `json:"files"`
	}
	if err := json.Unmarshal([]byte(result), &out); err != nil {
		t.Fatal(err)
	}
	if out.Artifact.FeatureID != "checkout" {
		t.Fatalf("artifact = %+v", out.Artifact)
	}
	if len(out.Files) != 2 || out.Files["prd"]["content"] != "content for prd" {
		t.Fatalf("files = %+v", out.Files)
	}
}

type mockPublishArtifactService struct {
	mockArtifactService
	publishFn func(ctx context.Context, in artifact.PublishInput) (*artifact.Artifact, error)
}

func (m *mockPublishArtifactService) Publish(ctx context.Context, in artifact.PublishInput) (*artifact.Artifact, error) {
	if m.publishFn != nil {
		return m.publishFn(ctx, in)
	}
	return nil, nil
}

func TestArtifactCreate_Basic(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	svc := &mockPublishArtifactService{
		publishFn: func(_ context.Context, in artifact.PublishInput) (*artifact.Artifact, error) {
			if in.FeatureID != "feat-x" {
				t.Errorf("feature_id = %q", in.FeatureID)
			}
			if len(in.Documents) != 1 {
				t.Fatalf("documents len = %d", len(in.Documents))
			}
			doc := in.Documents[0]
			if doc.Path != "prd.md" || string(doc.Content) != "hello" {
				t.Errorf("prd doc = path=%q content=%q", doc.Path, string(doc.Content))
			}
			if in.ArtifactPhase != "phase1" {
				t.Errorf("artifact_phase = %q", in.ArtifactPhase)
			}
			if in.ArtifactCompleteness != "partial" {
				t.Errorf("artifact_completeness = %q", in.ArtifactCompleteness)
			}
			return &artifact.Artifact{
				ID:        "id-1",
				FeatureID: "feat-x",
				Version:   "v1.0",
				Status:    artifact.StatusDraft,
				CreatedAt: now,
				UpdatedAt: now,
			}, nil
		},
	}

	handler := NewArtifactCreateHandler(svc)
	prdB64 := base64.StdEncoding.EncodeToString([]byte("hello"))
	result, err := handler(context.Background(), ArtifactCreateInput{
		FeatureID:            "feat-x",
		RequestType:          "new_feature",
		ImpactLevel:          "low",
		Version:              "v1.0",
		ArtifactPhase:        "phase1",
		ArtifactCompleteness: "partial",
		ImpactedServices:     []string{"svc-a"},
		Files:                map[string]string{"prd": prdB64},
	})
	if err != nil {
		t.Fatal(err)
	}
	var out artifactCreateOutput
	if err := json.Unmarshal([]byte(result), &out); err != nil {
		t.Fatal(err)
	}
	if out.ID != "id-1" || out.Version != "v1.0" || out.Status != "draft" {
		t.Fatalf("got %+v", out)
	}
}

func TestArtifactCreate_NilService(t *testing.T) {
	t.Parallel()
	handler := NewArtifactCreateHandler(nil)
	_, err := handler(context.Background(), ArtifactCreateInput{
		FeatureID:        "f",
		Version:          "v1.0",
		ImpactedServices: []string{"s"},
		Files:            map[string]string{"prd": base64.StdEncoding.EncodeToString([]byte("x"))},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}
