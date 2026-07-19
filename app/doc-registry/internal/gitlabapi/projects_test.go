package gitlabapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListProjects_QueriesMembershipAndDecodes(t *testing.T) {
	var gotPath, gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		gotToken = r.Header.Get("PRIVATE-TOKEN")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":7,"path_with_namespace":"acme/web","name":"web","default_branch":"main"}]`))
	}))
	defer srv.Close()

	c := NewClient(ClientConfig{APIURL: srv.URL, Token: "gitlab-test-token"})
	projects, err := c.ListProjects(context.Background(), "we", 25)
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 || projects[0].ID != 7 || projects[0].PathWithNS != "acme/web" || projects[0].DefaultBranch != "main" {
		t.Fatalf("unexpected projects: %#v", projects)
	}
	if gotToken != "gitlab-test-token" {
		t.Fatalf("token header = %q", gotToken)
	}
	for _, want := range []string{"membership=true", "search=we", "per_page=25", "simple=true"} {
		if !strings.Contains(gotPath, want) {
			t.Fatalf("query %q missing %q", gotPath, want)
		}
	}
	// order_by is intentionally omitted — last_activity_at 500s on GitLab.com for
	// membership listings (statement timeout); the default created_at order is used.
	if strings.Contains(gotPath, "order_by") {
		t.Fatalf("query %q should not set order_by", gotPath)
	}
}

// An OAuth integration sets Bearer: the token must travel as
// `Authorization: Bearer`, not PRIVATE-TOKEN (GitLab rejects OAuth tokens on the
// PRIVATE-TOKEN header with 401).
func TestListProjects_OAuthUsesBearer(t *testing.T) {
	var gotAuth, gotPrivate string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPrivate = r.Header.Get("PRIVATE-TOKEN")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	if _, err := NewClient(ClientConfig{APIURL: srv.URL, Token: "oauth-x", Bearer: true}).
		ListProjects(context.Background(), "", 25); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer oauth-x" {
		t.Fatalf("Authorization = %q, want \"Bearer oauth-x\"", gotAuth)
	}
	if gotPrivate != "" {
		t.Fatalf("PRIVATE-TOKEN = %q, want empty for OAuth", gotPrivate)
	}
}

func TestListProjects_RequiresAPIAndToken(t *testing.T) {
	if _, err := NewClient(ClientConfig{Token: "t"}).ListProjects(context.Background(), "", 0); err == nil {
		t.Fatal("missing api url must error")
	}
	if _, err := NewClient(ClientConfig{APIURL: "https://x"}).ListProjects(context.Background(), "", 0); err == nil {
		t.Fatal("missing token must error")
	}
}

func TestListProjects_Non2xxErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"401 Unauthorized"}`))
	}))
	defer srv.Close()
	if _, err := NewClient(ClientConfig{APIURL: srv.URL, Token: "bad"}).ListProjects(context.Background(), "", 0); err == nil {
		t.Fatal("401 must surface as error")
	}
}

func TestListProjects_RejectsOversizedResponse(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", (4<<20)+1)))
	}))
	t.Cleanup(srv.Close)

	_, err := NewClient(ClientConfig{APIURL: srv.URL, Token: "token"}).ListProjects(context.Background(), "", 50)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("ListProjects() error = %v, want response limit", err)
	}
}
