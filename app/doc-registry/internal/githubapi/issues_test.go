package githubapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateIssue_PostsToRepoIssuesWithBearerToken(t *testing.T) {
	var gotMethod, gotPath, gotAuth, gotAccept string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.EscapedPath()
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = io.WriteString(w, `{"number":42,"html_url":"https://github.com/acme/widgets/issues/42"}`)
	}))
	defer srv.Close()

	client := NewClient(ClientConfig{APIURL: srv.URL, Token: "ghp-x", Repo: "acme/widgets"})
	issue, err := client.CreateIssue(context.Background(), CreateIssueRequest{
		Title: "Add refund flow",
		Body:  "body\n\nfixes SPECGATE-CR-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if issue.Number != 42 || issue.URL != "https://github.com/acme/widgets/issues/42" {
		t.Fatalf("issue = %#v", issue)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("method = %s", gotMethod)
	}
	if gotPath != "/repos/acme/widgets/issues" {
		t.Fatalf("path = %s", gotPath)
	}
	if gotAuth != "Bearer ghp-x" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if gotAccept != "application/vnd.github+json" {
		t.Fatalf("Accept = %q", gotAccept)
	}
	if gotBody["title"] != "Add refund flow" || !strings.Contains(gotBody["body"].(string), "fixes SPECGATE-CR-1") {
		t.Fatalf("body = %#v", gotBody)
	}
}

func TestCreateIssue_Non2xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"message":"Forbidden"}`)
	}))
	defer srv.Close()

	client := NewClient(ClientConfig{APIURL: srv.URL, Token: "t", Repo: "a/b"})
	if _, err := client.CreateIssue(context.Background(), CreateIssueRequest{Title: "T"}); err == nil {
		t.Fatal("expected error on 403")
	}
}

func TestCreateIssue_RequiresTokenRepoTitle(t *testing.T) {
	base := ClientConfig{APIURL: "http://x", Token: "t", Repo: "a/b"}
	cases := []ClientConfig{
		{APIURL: "", Token: "t", Repo: "a/b"},
		{APIURL: "http://x", Token: "", Repo: "a/b"},
		{APIURL: "http://x", Token: "t", Repo: ""},
	}
	for i, cfg := range cases {
		if _, err := NewClient(cfg).CreateIssue(context.Background(), CreateIssueRequest{Title: "T"}); err == nil {
			t.Fatalf("case %d: expected error for missing config", i)
		}
	}
	if _, err := NewClient(base).CreateIssue(context.Background(), CreateIssueRequest{Title: "  "}); err == nil {
		t.Fatal("expected error for blank title")
	}
}
