package deploy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// noopRunner satisfies CommandRunner without executing anything.
type noopRunner struct{}

func (noopRunner) Run(context.Context, string, ...string) error { return nil }
func (noopRunner) Output(context.Context, string, ...string) ([]byte, error) {
	return []byte("[]"), nil
}

// TestInitDownloadsBundleWhenAbsent verifies Init fetches the compose bundle
// when the deployment dir has no compose.yml, and pins SPECGATE_VERSION.
func TestInitDownloadsBundleWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	svc := New(dir, noopRunner{})

	var gotBaseURL, gotVersion string
	svc.fetchBundle = func(_ context.Context, baseURL, version, destDir string) error {
		gotBaseURL, gotVersion = baseURL, version
		// Simulate extraction of the bundle into destDir.
		_ = os.WriteFile(filepath.Join(destDir, "compose.yml"), []byte("# bundle\n"), 0644)
		_ = os.WriteFile(filepath.Join(destDir, "doc-registry.env.example"), []byte("SETTINGS_ENCRYPTION_KEY=\n"), 0644)
		_ = os.WriteFile(filepath.Join(destDir, "agents.env.example"), []byte("\n"), 0644)
		return nil
	}

	if err := svc.Init(context.Background(), InitOptions{Seed: SeedNo, BundleVersion: "v9.9.9"}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if gotVersion != "v9.9.9" {
		t.Fatalf("fetchBundle version = %q, want v9.9.9", gotVersion)
	}
	if !strings.Contains(gotBaseURL, "thanhtung2693/specgate/releases/download/v9.9.9") {
		t.Fatalf("fetchBundle baseURL = %q", gotBaseURL)
	}
	env, _ := os.ReadFile(filepath.Join(dir, ".env"))
	if !strings.Contains(string(env), "SPECGATE_VERSION=v9.9.9") {
		t.Fatalf(".env missing version pin: %q", env)
	}
}

// TestInitErrorsWhenNoBundleAndDevVersion verifies a dev build with no staged
// bundle fails with guidance instead of silently starting nothing.
func TestInitErrorsWhenNoBundleAndDevVersion(t *testing.T) {
	dir := t.TempDir()
	svc := New(dir, noopRunner{})
	svc.fetchBundle = func(context.Context, string, string, string) error {
		t.Fatal("fetchBundle must not be called for a dev build")
		return nil
	}
	err := svc.Init(context.Background(), InitOptions{Seed: SeedNo, BundleVersion: "dev"})
	if err == nil || !strings.Contains(err.Error(), "no compose bundle") {
		t.Fatalf("expected a no-bundle error, got %v", err)
	}
}

// TestInitSkipsDownloadWhenBundlePresent verifies an existing compose.yml is
// left in place and no download is attempted.
func TestInitSkipsDownloadWhenBundlePresent(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "compose.yml"), []byte("# already here\n"), 0644)
	svc := New(dir, noopRunner{})
	svc.fetchBundle = func(context.Context, string, string, string) error {
		t.Fatal("fetchBundle must not be called when compose.yml exists")
		return nil
	}
	if err := svc.Init(context.Background(), InitOptions{Seed: SeedNo, BundleVersion: "v1.0.0"}); err != nil {
		t.Fatalf("Init: %v", err)
	}
}
