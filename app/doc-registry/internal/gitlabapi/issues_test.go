package gitlabapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateIssue_PostsToProjectIssuesWithPrivateToken(t *testing.T) {
	var gotMethod, gotPath, gotToken string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.EscapedPath()
		gotToken = r.Header.Get("PRIVATE-TOKEN")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = io.WriteString(w, `{"iid":42,"web_url":"https://gitlab.example/g/p/-/issues/42"}`)
	}))
	defer srv.Close()

	client := NewClient(ClientConfig{APIURL: srv.URL, Token: "glpat-x", ProjectID: "group/sub/repo"})
	issue, err := client.CreateIssue(context.Background(), CreateIssueRequest{
		Title:       "Add refund flow",
		Description: "body\n\nfixes SPECGATE-CR-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if issue.IID != 42 || issue.URL != "https://gitlab.example/g/p/-/issues/42" {
		t.Fatalf("issue = %#v", issue)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("method = %s", gotMethod)
	}
	// Project path must be URL-encoded into the :id segment.
	if gotPath != "/projects/group%2Fsub%2Frepo/issues" {
		t.Fatalf("path = %s", gotPath)
	}
	if gotToken != "glpat-x" {
		t.Fatalf("PRIVATE-TOKEN = %q", gotToken)
	}
	if gotBody["title"] != "Add refund flow" || !strings.Contains(gotBody["description"].(string), "fixes SPECGATE-CR-1") {
		t.Fatalf("body = %#v", gotBody)
	}
}

func TestCreateIssue_NumericProjectIDNotDoubleEncoded(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, `{"iid":7,"web_url":"u"}`)
	}))
	defer srv.Close()

	client := NewClient(ClientConfig{APIURL: srv.URL, Token: "t", ProjectID: "654"})
	if _, err := client.CreateIssue(context.Background(), CreateIssueRequest{Title: "T"}); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/projects/654/issues" {
		t.Fatalf("path = %s", gotPath)
	}
}

func TestCreateIssue_Non2xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"message":"403 Forbidden"}`)
	}))
	defer srv.Close()

	client := NewClient(ClientConfig{APIURL: srv.URL, Token: "t", ProjectID: "1"})
	if _, err := client.CreateIssue(context.Background(), CreateIssueRequest{Title: "T"}); err == nil {
		t.Fatal("expected error on 403")
	}
}

func TestCreateIssue_RequiresTokenProjectTitle(t *testing.T) {
	base := ClientConfig{APIURL: "http://x", Token: "t", ProjectID: "1"}
	cases := []ClientConfig{
		{APIURL: "", Token: "t", ProjectID: "1"},
		{APIURL: "http://x", Token: "", ProjectID: "1"},
		{APIURL: "http://x", Token: "t", ProjectID: ""},
	}
	for i, cfg := range cases {
		if _, err := NewClient(cfg).CreateIssue(context.Background(), CreateIssueRequest{Title: "T"}); err == nil {
			t.Fatalf("case %d: expected error for missing config", i)
		}
	}
	// Missing title also errors.
	if _, err := NewClient(base).CreateIssue(context.Background(), CreateIssueRequest{Title: "  "}); err == nil {
		t.Fatal("expected error for blank title")
	}
}
