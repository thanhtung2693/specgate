package command

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/specgate/specgate/app/cli/internal/fsutil"
)

const (
	githubReleasesAPI          = "https://api.github.com/repos/thanhtung2693/specgate/releases"
	updateCheckTTL             = 24 * time.Hour
	maxUpdateCheckResponseBody = 1 << 20
)

type updateCheckCache struct {
	CheckedAt     time.Time `json:"checked_at"`
	LatestVersion string    `json:"latest_version"`
}

type githubRelease struct {
	TagName    string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
}

func latestGitHubReleaseWithCache(ctx context.Context, timeout time.Duration, cachePath string) (string, error) {
	return latestGitHubReleaseWithCacheFrom(ctx, timeout, cachePath, githubReleasesAPI, time.Now)
}

func latestGitHubReleaseWithCacheFrom(ctx context.Context, timeout time.Duration, cachePath, apiURL string, now func() time.Time) (string, error) {
	if cachePath == "" {
		if p, err := defaultUpdateCheckCachePath(); err == nil {
			cachePath = p
		}
	}
	if latest, ok := readFreshUpdateCheckCache(cachePath, now()); ok {
		return latest, nil
	}

	latest, err := fetchLatestGitHubRelease(ctx, timeout, apiURL)
	if err != nil {
		return "", err
	}
	_ = writeUpdateCheckCache(cachePath, updateCheckCache{
		CheckedAt:     now(),
		LatestVersion: latest,
	})
	return latest, nil
}

func defaultUpdateCheckCachePath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "specgate", "update-check.json"), nil
}

func readFreshUpdateCheckCache(path string, now time.Time) (string, bool) {
	if path == "" {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	var cache updateCheckCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return "", false
	}
	if isDevVersion(cache.LatestVersion) || cache.CheckedAt.IsZero() {
		return "", false
	}
	if now.Sub(cache.CheckedAt) >= updateCheckTTL {
		return "", false
	}
	return strings.TrimSpace(cache.LatestVersion), true
}

func writeUpdateCheckCache(path string, cache updateCheckCache) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return fsutil.AtomicWriteFile(path, data, 0o600)
}

func fetchLatestGitHubRelease(ctx context.Context, timeout time.Duration, apiURL string) (string, error) {
	httpClient := &http.Client{Timeout: timeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "specgate-cli")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub returned %d", resp.StatusCode)
	}

	var releases []githubRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxUpdateCheckResponseBody)).Decode(&releases); err != nil {
		return "", err
	}
	fallbackPrerelease := ""
	for _, release := range releases {
		tag := strings.TrimSpace(release.TagName)
		if release.Draft || tag == "" {
			continue
		}
		if !release.Prerelease {
			return tag, nil
		}
		if fallbackPrerelease == "" {
			fallbackPrerelease = tag
		}
	}
	if fallbackPrerelease != "" {
		return fallbackPrerelease, nil
	}
	return "", fmt.Errorf("no published GitHub releases found")
}

func isVersionNewer(latest, current string) bool {
	cmp, ok := compareReleaseVersions(latest, current)
	return ok && cmp > 0
}

type releaseVersion struct {
	nums [3]int
	pre  string
}

func compareReleaseVersions(a, b string) (int, bool) {
	av, okA := parseReleaseVersion(a)
	bv, okB := parseReleaseVersion(b)
	if !okA || !okB {
		return 0, false
	}
	for i := range av.nums {
		if av.nums[i] > bv.nums[i] {
			return 1, true
		}
		if av.nums[i] < bv.nums[i] {
			return -1, true
		}
	}
	return comparePrerelease(av.pre, bv.pre), true
}

func parseReleaseVersion(v string) (releaseVersion, bool) {
	s := normalizeVersion(v)
	if s == "" {
		return releaseVersion{}, false
	}
	parts := strings.SplitN(s, "-", 2)
	numParts := strings.Split(parts[0], ".")
	if len(numParts) == 0 || len(numParts) > 3 {
		return releaseVersion{}, false
	}
	var out releaseVersion
	for i, part := range numParts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return releaseVersion{}, false
		}
		out.nums[i] = n
	}
	if len(parts) == 2 {
		out.pre = parts[1]
	}
	return out, true
}

func comparePrerelease(a, b string) int {
	if a == "" && b == "" {
		return 0
	}
	if a == "" {
		return 1
	}
	if b == "" {
		return -1
	}
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")
	for i := 0; i < len(aParts) && i < len(bParts); i++ {
		if aParts[i] == bParts[i] {
			continue
		}
		aNum, aErr := strconv.Atoi(aParts[i])
		bNum, bErr := strconv.Atoi(bParts[i])
		if aErr == nil && bErr == nil {
			if aNum > bNum {
				return 1
			}
			return -1
		}
		if aErr == nil {
			return -1
		}
		if bErr == nil {
			return 1
		}
		if aParts[i] > bParts[i] {
			return 1
		}
		return -1
	}
	if len(aParts) > len(bParts) {
		return 1
	}
	if len(aParts) < len(bParts) {
		return -1
	}
	return 0
}
