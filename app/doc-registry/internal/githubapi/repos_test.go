package githubapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListRepos_DecodesAndClientSideFilters(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer ghp-x" {
			t.Errorf("auth header = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":1,"full_name":"acme/web","name":"web","default_branch":"main"},{"id":2,"full_name":"acme/api","name":"api","default_branch":"trunk"}]`))
	}))
	defer srv.Close()

	c := NewClient(ClientConfig{APIURL: srv.URL, Token: "ghp-x"})
	all, err := c.ListRepos(context.Background(), "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("no search: want 2, got %d (%#v)", len(all), all)
	}
	// GitHub's list endpoint has no server search, so a query filters client-side.
	filtered, err := c.ListRepos(context.Background(), "API", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 1 || filtered[0].FullName != "acme/api" || filtered[0].DefaultBranch != "trunk" {
		t.Fatalf("unexpected filtered: %#v", filtered)
	}
}

func TestListRepos_RequiresAPIAndToken(t *testing.T) {
	if _, err := NewClient(ClientConfig{Token: "t"}).ListRepos(context.Background(), "", 0); err == nil {
		t.Fatal("missing api url must error")
	}
	if _, err := NewClient(ClientConfig{APIURL: "https://x"}).ListRepos(context.Background(), "", 0); err == nil {
		t.Fatal("missing token must error")
	}
}
