package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/integrations"
	"github.com/specgate/doc-registry/internal/knowledge"
	"github.com/specgate/doc-registry/internal/mcp/repo"
)

type stubRepoProvider struct{}

func (s *stubRepoProvider) Search(_ context.Context, _ string, _ repo.SearchOpts) (*repo.SearchResult, error) {
	return &repo.SearchResult{}, nil
}
func (s *stubRepoProvider) ListFiles(_ context.Context, _, _ string, _ repo.ListOpts) (*repo.ListResult, error) {
	return &repo.ListResult{}, nil
}
func (s *stubRepoProvider) GetFileContent(_ context.Context, _, _ string, _ int64) (*repo.FileContent, error) {
	return &repo.FileContent{}, nil
}
func (s *stubRepoProvider) GetFileMeta(_ context.Context, _, _ string) (*repo.FileMeta, error) {
	return &repo.FileMeta{}, nil
}

type stubKnowledge struct{}

func (s *stubKnowledge) Search(_ context.Context, _ knowledge.SearchInput) ([]knowledge.SearchResult, error) {
	return nil, nil
}

type stubFeedbackStore struct{}

func (s *stubFeedbackStore) CreateGovernanceFeedbackEvent(_ context.Context, in integrations.GovernanceFeedbackEvent) (*integrations.GovernanceFeedbackEvent, error) {
	in.ID = "feedback-1"
	return &in, nil
}

func (s *stubFeedbackStore) ListGovernanceFeedbackEvents(_ context.Context, _ integrations.GovernanceFeedbackFilter) ([]integrations.GovernanceFeedbackEvent, error) {
	return nil, nil
}

type stubArtifacts struct{}

func (s *stubArtifacts) List(_ context.Context, _ artifact.ListFilter) ([]artifact.Artifact, error) {
	return nil, nil
}

func (s *stubArtifacts) Count(_ context.Context, _ artifact.ListFilter) (int64, error) {
	return 0, nil
}

func (s *stubArtifacts) Publish(_ context.Context, _ artifact.PublishInput) (*artifact.Artifact, error) {
	return nil, errors.New("not implemented")
}

func (s *stubArtifacts) Get(_ context.Context, _ string) (*artifact.Artifact, error) {
	return nil, artifact.ErrNotFound
}

func (s *stubArtifacts) LatestArtifact(_ context.Context, _ string) (*artifact.Artifact, error) {
	return nil, nil
}

func (s *stubArtifacts) NextVersion(_ context.Context, _ string) (string, error) {
	return "v0.1", nil
}

func (s *stubArtifacts) ResolveNextVersion(_ context.Context, _ string, _ string) (string, error) {
	return "v0.1", nil
}

func (s *stubArtifacts) UpdateStatus(_ context.Context, _ string, _ artifact.StatusUpdate) (*artifact.Artifact, error) {
	return nil, errors.New("not implemented")
}

func (s *stubArtifacts) SignedFileURL(_ context.Context, _ string, _ string) (*artifact.SignedFile, error) {
	return nil, errors.New("not implemented")
}

func (s *stubArtifacts) FileContent(_ context.Context, _ string, _ string) ([]byte, error) {
	return nil, errors.New("not implemented")
}

func (s *stubArtifacts) CheckConflicts(_ context.Context, _ []string) (*artifact.ConflictReport, error) {
	return nil, nil
}

func (s *stubArtifacts) ListEvents(_ context.Context, _ artifact.EventFilter) ([]artifact.Event, error) {
	return nil, nil
}
func (s *stubArtifacts) RefreshReadinessRuns(_ context.Context, _ string, _ []artifact.ReadinessEvaluation) ([]artifact.ReadinessRun, error) {
	return nil, nil
}
func (s *stubArtifacts) ListReadinessRuns(_ context.Context, _ string, _ int) ([]artifact.ReadinessRun, error) {
	return nil, nil
}

func (s *stubArtifacts) Delete(_ context.Context, _ string) error {
	return nil
}

func TestNewMCPServer_Creates(t *testing.T) {
	t.Parallel()
	srv := NewMCPServer(MCPServerDeps{
		RepoProviders:   map[string]repo.RepoProvider{"default": &stubRepoProvider{}},
		RepoDefaultRefs: map[string]string{"default": "main"},
		Knowledge:       &stubKnowledge{},
		Artifacts:       &stubArtifacts{},
		Feedback:        &stubFeedbackStore{},
		APIKey:          "test-key",
		MaxRepoCalls:    50,
		MaxRepoBytes:    524288,
	})
	if srv == nil {
		t.Fatal("expected non-nil server")
	}
}

func TestInfoToolCatalog_IncludesImplementationFeedbackTool(t *testing.T) {
	t.Parallel()
	tools := InfoToolCatalog(false)
	for _, tool := range tools {
		if tool.Name == "report_implementation_feedback" {
			return
		}
	}
	t.Fatal("report_implementation_feedback not found in catalog")
}

func TestInfoToolCatalog_IncludesArtifactCreateAndDraftUpdateTools(t *testing.T) {
	t.Parallel()
	tools := InfoToolCatalog(false)
	var foundCreate bool
	var foundDraftUpdate bool
	for _, tool := range tools {
		switch tool.Name {
		case "artifact_create":
			foundCreate = true
		case "draft_artifact_update":
			foundDraftUpdate = true
		}
	}
	if !foundCreate {
		t.Fatal("artifact_create not found in catalog")
	}
	if !foundDraftUpdate {
		t.Fatal("draft_artifact_update not found in catalog")
	}
}

func TestBuildGitLabRepoProviders_NilSourceYieldsEmpty(t *testing.T) {
	t.Parallel()
	providers, defaults := buildGitLabRepoProviders(context.Background(), nil)
	if len(providers) != 0 || len(defaults) != 0 {
		t.Fatalf("nil source must produce empty maps, got providers=%+v defaults=%+v", providers, defaults)
	}
}

type fakeIntegrationRepoSource struct {
	configs []GitLabRepoConfig
	hash    string
}

func (f *fakeIntegrationRepoSource) GitLabRepoConfigs(_ context.Context) ([]GitLabRepoConfig, error) {
	return f.configs, nil
}

func (f *fakeIntegrationRepoSource) IntegrationsHash() string { return f.hash }

// repo providers are now sourced solely from GitLab integrations.
func TestBuildGitLabRepoProviders_IntegrationOnly(t *testing.T) {
	t.Parallel()
	configs := []GitLabRepoConfig{
		{ProjectID: "g/r", APIURL: "https://gitlab.example.com", Token: "itk", DefaultRef: "main"},
		// Skipped: missing token.
		{ProjectID: "g/r2", APIURL: "https://gitlab.example.com", Token: "", DefaultRef: "main"},
		// Skipped: missing project id.
		{ProjectID: "", APIURL: "https://gitlab.example.com", Token: "itk", DefaultRef: "main"},
	}
	providers, defaults := buildGitLabRepoProviders(
		context.Background(),
		&fakeIntegrationRepoSource{configs: configs},
	)
	if len(providers) != 1 {
		t.Fatalf("providers = %+v, want only g/r", providers)
	}
	if defaults["g/r"] != "main" {
		t.Fatalf("g/r default = %q, want main", defaults["g/r"])
	}
}
