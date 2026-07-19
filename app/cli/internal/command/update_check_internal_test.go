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
			{"tag_name": "v9.9.0-rc.2", "draft": false, "prerelease": true},
		})
	}))
	t.Cleanup(srv.Close)

	cachePath := filepath.Join(t.TempDir(), "update-check.json")
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)

	got, err := latestGitHubReleaseWithCacheFrom(context.Background(), time.Second, cachePath, srv.URL, func() time.Time { return now })
	if err != nil {
		t.Fatalf("fetch latest release: %v", err)
	}
	if got != "v9.9.0-rc.2" {
		t.Fatalf("latest = %q, want v9.9.0-rc.2", got)
	}

	got, err = latestGitHubReleaseWithCacheFrom(context.Background(), time.Second, cachePath, srv.URL, func() time.Time { return now.Add(time.Hour) })
	if err != nil {
		t.Fatalf("cached latest release: %v", err)
	}
	if got != "v9.9.0-rc.2" {
		t.Fatalf("cached latest = %q, want v9.9.0-rc.2", got)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
}

func TestLatestGitHubReleasePrefersStableOverNewerPrerelease(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"tag_name": "v9.10.0-rc.1", "draft": false, "prerelease": true},
			{"tag_name": "v9.9.0", "draft": false, "prerelease": false},
		})
	}))
	t.Cleanup(srv.Close)

	got, err := fetchLatestGitHubRelease(context.Background(), time.Second, srv.URL)
	if err != nil {
		t.Fatalf("fetch latest release: %v", err)
	}
	if got != "v9.9.0" {
		t.Fatalf("latest = %q, want stable v9.9.0", got)
	}
}

func TestLatestGitHubReleaseWithCacheRefreshesStaleCache(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"tag_name": "v9.9.0-rc.3", "draft": false, "prerelease": true},
		})
	}))
	t.Cleanup(srv.Close)

	cachePath := filepath.Join(t.TempDir(), "update-check.json")
	old := updateCheckCache{
		CheckedAt:     time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		LatestVersion: "v9.9.0-rc.2",
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
	if got != "v9.9.0-rc.3" {
		t.Fatalf("latest = %q, want v9.9.0-rc.3", got)
	}
}

func TestWriteUpdateCheckCacheDoesNotFollowPredictableTempSymlink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "update-check.json")
	external := filepath.Join(dir, "external")
	if err := os.WriteFile(external, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, path+".tmp"); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	if err := writeUpdateCheckCache(path, updateCheckCache{
		CheckedAt:     time.Now(),
		LatestVersion: "v1.2.3",
	}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(external)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "keep" {
		t.Fatalf("predictable temp symlink target changed: %q", body)
	}
}

func TestIsVersionNewerHandlesPrereleases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		latest  string
		current string
		want    bool
	}{
		{latest: "v9.9.0-rc.2", current: "v9.9.0-rc.1", want: true},
		{latest: "v9.9.0", current: "v9.9.0-rc.2", want: true},
		{latest: "v9.9.0-rc.2", current: "v9.9.0", want: false},
		{latest: "v0.1.0", current: "v0.1.1", want: false},
		{latest: "v0.2.0", current: "v0.1.9", want: true},
	}
	for _, tt := range tests {
		if got := isVersionNewer(tt.latest, tt.current); got != tt.want {
			t.Fatalf("isVersionNewer(%q, %q) = %v, want %v", tt.latest, tt.current, got, tt.want)
		}
	}
}
