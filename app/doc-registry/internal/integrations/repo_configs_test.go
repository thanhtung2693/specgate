package integrations

import (
	"context"
	"testing"
)

// repoConfigFakeStore embeds the Store interface (nil) and overrides only the
// methods ListGitLabRepoConfigs / RepoConfigHash touch. GetIntegration resolves
// by id (ResolveAPIToken re-fetches per integration), so the fake keys by id
// rather than returning one fixed row.
type repoConfigFakeStore struct {
	Store
	integrations []Integration
	resources    map[string][]Resource
}

func (f repoConfigFakeStore) ListIntegrations(context.Context) ([]Integration, error) {
	return f.integrations, nil
}

func (f repoConfigFakeStore) GetIntegration(_ context.Context, id string) (*Integration, error) {
	for i := range f.integrations {
		if f.integrations[i].ID == id {
			row := f.integrations[i]
			return &row, nil
		}
	}
	return nil, ErrNotFound
}

func (f repoConfigFakeStore) ListResources(_ context.Context, integrationID string) ([]Resource, error) {
	return f.resources[integrationID], nil
}

func gitlabIntegrationRow(t *testing.T, id, baseURL string, withToken bool) Integration {
	t.Helper()
	row := Integration{
		ID:       id,
		Provider: ProviderGitLab,
		Status:   StatusConnected,
		BaseURL:  baseURL,
	}
	if withToken {
		enc, err := EncryptSecret("gl_real_token")
		if err != nil {
			t.Fatal(err)
		}
		row.APITokenEncrypted = enc
		row.HasAPIToken = true
	}
	return row
}

func gitlabOAuthIntegrationRow(t *testing.T, id, baseURL string) Integration {
	t.Helper()
	enc, err := EncryptSecret("gl_oauth_token")
	if err != nil {
		t.Fatal(err)
	}
	return Integration{
		ID:                        id,
		Provider:                  ProviderGitLab,
		Status:                    StatusConnected,
		BaseURL:                   baseURL,
		AuthMethod:                AuthMethodOAuth,
		OAuthAccessTokenEncrypted: enc,
		HasOAuthToken:             true,
	}
}

func TestListGitLabRepoConfigs_MapsConnectedGitLabIntegrationResources(t *testing.T) {
	t.Setenv(SecretKeyEnvVar, "0000000000000000000000000000000000000000000000000000000000000001")
	disabled := gitlabIntegrationRow(t, "gl-3", "https://gitlab.example.com", true)
	disabled.Status = StatusDisabled
	store := repoConfigFakeStore{
		integrations: []Integration{
			// base_url is the full repo URL (as users often paste it); APIURL must
			// derive to scheme://host, not keep the path.
			gitlabIntegrationRow(t, "gl-1", "https://gitlab.example.com/group/sub/project", true),
			// No token → skipped.
			gitlabIntegrationRow(t, "gl-2", "https://gitlab.example.com", false),
			// OAuth token → included through ResolveAPIToken.
			gitlabOAuthIntegrationRow(t, "gl-4", "https://gitlab-oauth.example.com/group/project"),
			// Disabled (with a valid token) → skipped by ResolveAPIToken.
			disabled,
			// Non-gitlab provider → ignored.
			{ID: "lin-1", Provider: ProviderLinear, Status: StatusConnected, HasAPIToken: true},
		},
		resources: map[string][]Resource{
			"gl-1": {
				{ResourceType: ResourceTypeProject, ExternalKey: "group/sub/project", DefaultRef: "main"},
				// Non-project resource → filtered out (matches webhook convention).
				{ResourceType: ResourceTypeTeam, ExternalKey: "team-x"},
			},
			"gl-2": {{ResourceType: ResourceTypeProject, ExternalKey: "group/other", DefaultRef: "dev"}},
			"gl-3": {{ResourceType: ResourceTypeProject, ExternalKey: "group/disabled", DefaultRef: "main"}},
			"gl-4": {{ResourceType: ResourceTypeProject, ExternalKey: "group/oauth-project", DefaultRef: "release"}},
		},
	}
	svc := NewService(store)

	configs, err := svc.ListGitLabRepoConfigs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(configs) != 2 {
		t.Fatalf("configs = %+v, want exactly 2 usable gitlab repo configs", configs)
	}
	got := configs[0]
	if got.ProjectID != "group/sub/project" {
		t.Errorf("ProjectID = %q, want group/sub/project (from ExternalKey)", got.ProjectID)
	}
	if got.APIURL != "https://gitlab.example.com" {
		t.Errorf("APIURL = %q, want scheme://host derived from the base URL", got.APIURL)
	}
	if got.DefaultRef != "main" {
		t.Errorf("DefaultRef = %q, want main", got.DefaultRef)
	}
	if got.Token != "gl_real_token" {
		t.Errorf("Token = %q, want the resolved (decrypted) token", got.Token)
	}
	if configs[1].ProjectID != "group/oauth-project" || configs[1].Token != "gl_oauth_token" {
		t.Fatalf("expected oauth-backed repo config, got %+v", configs[1])
	}
}

func TestRepoConfigHash_ChangesWithResourcesAndStable(t *testing.T) {
	t.Setenv(SecretKeyEnvVar, "0000000000000000000000000000000000000000000000000000000000000001")
	base := repoConfigFakeStore{
		integrations: []Integration{gitlabIntegrationRow(t, "gl-1", "https://gitlab.example.com", true)},
		resources: map[string][]Resource{
			"gl-1": {{ResourceType: ResourceTypeProject, ExternalKey: "group/project", DefaultRef: "main"}},
		},
	}
	h1 := NewService(base).RepoConfigHash()
	if h1 == "" {
		t.Fatal("hash should be non-empty for a connected gitlab integration with a project resource")
	}
	if again := NewService(base).RepoConfigHash(); again != h1 {
		t.Fatalf("hash not stable: %q != %q", again, h1)
	}

	// Changing a resource DefaultRef must change the hash (rebuild trigger).
	changed := base
	changed.resources = map[string][]Resource{
		"gl-1": {{ResourceType: ResourceTypeProject, ExternalKey: "group/project", DefaultRef: "develop"}},
	}
	if h2 := NewService(changed).RepoConfigHash(); h2 == h1 {
		t.Fatalf("hash unchanged after DefaultRef change: %q", h2)
	}
}
