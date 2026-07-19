package githubapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateRepositoryWebhook_UsesRepositoryEvents(t *testing.T) {
	t.Parallel()
	var body struct {
		Events []string `json:"events"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/acme/repo/hooks" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"id":7}`))
	}))
	defer srv.Close()

	_, err := NewClient(ClientConfig{APIURL: srv.URL, Token: "token", Repo: "acme/repo"}).CreateRepositoryWebhook(context.Background(), CreateRepositoryWebhookRequest{URL: "https://specgate.example/hook", Secret: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if len(body.Events) != 2 || body.Events[0] != "pull_request" || body.Events[1] != "issue_comment" {
		t.Fatalf("events = %#v", body.Events)
	}
}
