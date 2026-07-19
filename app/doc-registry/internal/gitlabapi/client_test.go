package gitlabapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateProjectWebhook_ExcludesIssueHooks(t *testing.T) {
	t.Parallel()
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"id":7,"signing_token_present":true}`))
	}))
	defer srv.Close()

	_, err := NewClient(ClientConfig{APIURL: srv.URL, Token: "token", ProjectID: "7"}).CreateProjectWebhook(context.Background(), CreateProjectWebhookRequest{URL: "https://specgate.example/hook", SigningToken: "whsec_secret"})
	if err != nil {
		t.Fatal(err)
	}
	if body["merge_requests_events"] != true || body["note_events"] != true {
		t.Fatalf("events = %#v", body)
	}
	if _, ok := body["issues_events"]; ok {
		t.Fatalf("issues_events must be omitted: %#v", body)
	}
}
