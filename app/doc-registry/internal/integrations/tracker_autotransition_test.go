package integrations

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/specgate/doc-registry/internal/integrations/coretypes"
)

// atFakeStore implements only the methods AutoTransitionIssueOnDeliveryPass
// exercises; all other Store calls panic to keep the fake honest.
type atFakeStore struct {
	Store
	integration *Integration
	resources   []Resource
	links       []TrackerLink
}

func (f *atFakeStore) GetIntegration(_ context.Context, _ string) (*Integration, error) {
	return f.integration, nil
}

func (f *atFakeStore) ListResources(_ context.Context, _ string) ([]Resource, error) {
	return f.resources, nil
}

func (f *atFakeStore) ListTrackerLinksByChangeRequest(_ context.Context, _ string) ([]TrackerLink, error) {
	return f.links, nil
}

func encryptedToken(t *testing.T, raw string) string {
	t.Helper()
	enc, err := EncryptSecret(raw)
	if err != nil {
		t.Fatalf("EncryptSecret: %v", err)
	}
	return enc
}

// TestParseIssueNumber covers the pure helper directly.
func TestParseIssueNumber(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in      string
		want    int
		wantErr bool
	}{
		{"#45", 45, false},
		{"45", 45, false},
		{"#1", 1, false},
		{"  #99  ", 99, false},
		{"", 0, true},
		{"#0", 0, true},
		{"#abc", 0, true},
		{"abc", 0, true},
	}
	for _, tc := range cases {
		got, err := parseIssueNumber(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseIssueNumber(%q) = %d, nil error; want error", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseIssueNumber(%q) error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseIssueNumber(%q) = %d; want %d", tc.in, got, tc.want)
		}
	}
}

// TestAutoTransition_GitHubIssueClosedOnDeliveryPass verifies that a GitHub
// tracker link results in a PATCH /repos/{repo}/issues/{n} call with
// state=closed and state_reason=completed.
func TestAutoTransition_GitHubIssueClosedOnDeliveryPass(t *testing.T) {
	t.Setenv(SecretKeyEnvVar, "0000000000000000000000000000000000000000000000000000000000000001")

	var gotMethod, gotPath string
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"number":45,"state":"closed"}`))
	}))
	defer srv.Close()

	store := &atFakeStore{
		integration: &Integration{
			ID:                "int-gh",
			Provider:          ProviderGitHub,
			Status:            StatusConnected,
			BaseURL:           srv.URL, // routes gitHubAPIURL to the test server
			APITokenEncrypted: encryptedToken(t, "ghp_test_token"),
		},
		resources: []Resource{
			{
				IntegrationID: "int-gh",
				ResourceType:  coretypes.ResourceTypeProject,
				ExternalKey:   "owner/myrepo",
			},
		},
		links: []TrackerLink{
			{
				IntegrationID: "int-gh",
				ExternalKey:   "#45",
				State:         TrackerStateOpened,
			},
		},
	}

	svc := NewService(store)
	svc.AutoTransitionIssueOnDeliveryPass(context.Background(), "cr-1")

	if gotMethod != http.MethodPatch {
		t.Errorf("method = %q; want PATCH", gotMethod)
	}
	if gotPath != "/api/v3/repos/owner/myrepo/issues/45" {
		t.Errorf("path = %q; want /api/v3/repos/owner/myrepo/issues/45", gotPath)
	}
	if gotBody["state"] != "closed" {
		t.Errorf("state = %q; want closed", gotBody["state"])
	}
	if gotBody["state_reason"] != "completed" {
		t.Errorf("state_reason = %q; want completed", gotBody["state_reason"])
	}
}

// TestAutoTransition_GitLabIssueClosedOnDeliveryPass verifies that a GitLab
// tracker link results in a PUT /projects/{id}/issues/{iid} call with
// state_event=close.
func TestAutoTransition_GitLabIssueClosedOnDeliveryPass(t *testing.T) {
	t.Setenv(SecretKeyEnvVar, "0000000000000000000000000000000000000000000000000000000000000001")

	var gotMethod, gotPath string
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"iid":42,"state":"closed"}`))
	}))
	defer srv.Close()

	store := &atFakeStore{
		integration: &Integration{
			ID:                "int-gl",
			Provider:          ProviderGitLab,
			Status:            StatusConnected,
			BaseURL:           srv.URL,
			APITokenEncrypted: encryptedToken(t, "glpat_test_token"),
		},
		resources: []Resource{
			{
				IntegrationID: "int-gl",
				ResourceType:  coretypes.ResourceTypeProject,
				ExternalID:    "123",
			},
		},
		links: []TrackerLink{
			{
				IntegrationID: "int-gl",
				ExternalKey:   "#42",
				State:         TrackerStateOpened,
			},
		},
	}

	svc := NewService(store)
	svc.AutoTransitionIssueOnDeliveryPass(context.Background(), "cr-1")

	if gotMethod != http.MethodPut {
		t.Errorf("method = %q; want PUT", gotMethod)
	}
	if gotPath != "/api/v4/projects/123/issues/42" {
		t.Errorf("path = %q; want /api/v4/projects/123/issues/42", gotPath)
	}
	if gotBody["state_event"] != "close" {
		t.Errorf("state_event = %q; want close", gotBody["state_event"])
	}
}

// TestAutoTransition_SkipsAlreadyClosedLink verifies that a link with
// lifecycle state=closed is not sent to the provider API.
func TestAutoTransition_SkipsAlreadyClosedLink(t *testing.T) {
	t.Setenv(SecretKeyEnvVar, "0000000000000000000000000000000000000000000000000000000000000001")

	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	store := &atFakeStore{
		integration: &Integration{
			ID:                "int-gh",
			Provider:          ProviderGitHub,
			Status:            StatusConnected,
			BaseURL:           srv.URL,
			APITokenEncrypted: encryptedToken(t, "ghp_test"),
		},
		resources: []Resource{
			{ResourceType: coretypes.ResourceTypeProject, ExternalKey: "owner/repo"},
		},
		links: []TrackerLink{
			// Already closed — must be skipped.
			{IntegrationID: "int-gh", ExternalKey: "#7", State: TrackerStateClosed},
		},
	}

	svc := NewService(store)
	svc.AutoTransitionIssueOnDeliveryPass(context.Background(), "cr-1")

	if called {
		t.Error("expected no API call for already-closed link; got one")
	}
}

// TestAutoTransition_SkipsUnknownProvider verifies that a provider other than
// Linear/GitHub/GitLab is silently skipped without panicking.
func TestAutoTransition_SkipsUnknownProvider(t *testing.T) {
	t.Setenv(SecretKeyEnvVar, "0000000000000000000000000000000000000000000000000000000000000001")

	store := &atFakeStore{
		integration: &Integration{
			ID:                "int-jira",
			Provider:          "jira",
			Status:            StatusConnected,
			APITokenEncrypted: encryptedToken(t, "jira_token"),
		},
		links: []TrackerLink{
			{IntegrationID: "int-jira", ExternalKey: "#10", State: TrackerStateOpened},
		},
	}

	svc := NewService(store)
	// Must not panic.
	svc.AutoTransitionIssueOnDeliveryPass(context.Background(), "cr-1")
}

// TestAutoTransition_EmptyChangeRequestIDIsNoop verifies early return on blank input.
func TestAutoTransition_EmptyChangeRequestIDIsNoop(t *testing.T) {
	// No store fields set — any method call would panic via the embedded nil Store.
	svc := NewService(&atFakeStore{})
	svc.AutoTransitionIssueOnDeliveryPass(context.Background(), "")
	svc.AutoTransitionIssueOnDeliveryPass(context.Background(), "   ")
}
