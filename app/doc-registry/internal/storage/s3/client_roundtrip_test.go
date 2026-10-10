package s3

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/specgate/doc-registry/internal/config"
)

func TestClientRoundTripThroughConfiguredEndpoint(t *testing.T) {
	// Keep the SDK from consulting a contributor's shared AWS configuration.
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(t.TempDir(), "config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "credentials"))
	const key = "workspaces/fixture/artifacts/example/v1/spec.md"
	want := []byte("fixture specification")
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		if r.URL.Path != "/fixture-bucket/"+key {
			t.Errorf("unexpected object path %q", r.URL.Path)
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=fixture/") {
			t.Error("request was not signed with the fixture credential")
		}
		switch r.Method {
		case http.MethodPut:
			body, err := io.ReadAll(r.Body)
			if err != nil || !bytes.Equal(body, want) || r.Header.Get("Content-Type") != "text/markdown" {
				t.Error("PUT did not preserve fixture bytes/content type")
			}
		case http.MethodGet:
			w.Header().Set("Content-Type", "text/markdown")
			_, _ = w.Write(want)
		case http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected method %q", r.Method)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer server.Close()
	client, err := New(t.Context(), config.S3Config{
		Endpoint: server.URL, Region: "us-east-1", Bucket: "fixture-bucket",
		AccessKey: "fixture", SecretKey: "fixture-secret", UsePathStyle: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.PutObjectWithContentType(t.Context(), key, want, "text/markdown"); err != nil {
		t.Fatal(err)
	}
	got, err := client.GetObject(t.Context(), key, int64(len(want)))
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("GET bytes differ or failed: %v", err)
	}
	if err := client.DeleteObject(t.Context(), key); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(methods, []string{"PUT", "GET", "DELETE"}) {
		t.Fatalf("methods = %v", methods)
	}
}
