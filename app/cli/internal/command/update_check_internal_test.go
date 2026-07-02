package command

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLatestGitHubReleaseWithCacheFetchesPrereleaseAndCaches(t *testing.T) {
	t.Parallel()

	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"tag_name": "v0.1.0-alpha.2", "draft": false, "prerelease": true},
		})
	}))
	t.Cleanup(srv.Close)

	cachePath := filepath.Join(t.TempDir(), "update-check.json")
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)

	got, err := latestGitHubReleaseWithCacheFrom(context.Background(), time.Second, cachePath, srv.URL, func() time.Time { return now })
	if err != nil {
		t.Fatalf("fetch latest release: %v", err)
	}
	if got != "v0.1.0-alpha.2" {
		t.Fatalf("latest = %q, want v0.1.0-alpha.2", got)
	}

	got, err = latestGitHubReleaseWithCacheFrom(context.Background(), time.Second, cachePath, srv.URL, func() time.Time { return now.Add(time.Hour) })
	if err != nil {
		t.Fatalf("cached latest release: %v", err)
	}
	if got != "v0.1.0-alpha.2" {
		t.Fatalf("cached latest = %q, want v0.1.0-alpha.2", got)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
}

func TestLatestGitHubReleaseWithCacheRefreshesStaleCache(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"tag_name": "v0.1.0-alpha.3", "draft": false, "prerelease": true},
		})
	}))
	t.Cleanup(srv.Close)

	cachePath := filepath.Join(t.TempDir(), "update-check.json")
	old := updateCheckCache{
		CheckedAt:     time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		LatestVersion: "v0.1.0-alpha.2",
	}
	data, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, data, 0600); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	got, err := latestGitHubReleaseWithCacheFrom(context.Background(), time.Second, cachePath, srv.URL, func() time.Time { return now })
	if err != nil {
		t.Fatalf("fetch latest release: %v", err)
	}
	if got != "v0.1.0-alpha.3" {
		t.Fatalf("latest = %q, want v0.1.0-alpha.3", got)
	}
}

func TestIsVersionNewerHandlesPrereleases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		latest  string
		current string
		want    bool
	}{
		{latest: "v0.1.0-alpha.2", current: "v0.1.0-alpha.1", want: true},
		{latest: "v0.1.0", current: "v0.1.0-alpha.2", want: true},
		{latest: "v0.1.0-alpha.2", current: "v0.1.0", want: false},
		{latest: "v0.1.0", current: "v0.1.1", want: false},
		{latest: "v0.2.0", current: "v0.1.9", want: true},
	}
	for _, tt := range tests {
		if got := isVersionNewer(tt.latest, tt.current); got != tt.want {
			t.Fatalf("isVersionNewer(%q, %q) = %v, want %v", tt.latest, tt.current, got, tt.want)
		}
	}
}
