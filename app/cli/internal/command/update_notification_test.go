package command_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/specgate/specgate/app/cli/internal/buildinfo"
	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestServerCommandWarnsWhenCLIBehindRecommendedVersion(t *testing.T) {
	oldVersion := buildinfo.Version
	buildinfo.Version = "v9.9.0-rc.1"
	t.Cleanup(func() { buildinfo.Version = oldVersion })

	var stderr bytes.Buffer
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/meta", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"body": map[string]any{
				"api_version":             "specgate.api/v1",
				"version":                 "v9.9.0-rc.2",
				"recommended_cli_version": "v9.9.0-rc.2",
				"capabilities":            map[string]bool{"agents": true},
			},
		})
	})
	mux.HandleFunc("/api/v1/status", jsonStatus(1, 0))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	deps, _ := newTestDeps(t, srv.URL)
	deps.Stderr = &stderr
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--server", srv.URL, "status", "--all-workspaces")
	if code != output.ExitOK {
		t.Fatalf("exit = %d", code)
	}
	got := stderr.String()
	if !strings.Contains(got, "specgate CLI v9.9.0-rc.1") ||
		!strings.Contains(got, "v9.9.0-rc.2") ||
		!strings.Contains(got, "specgate update") {
		t.Fatalf("missing update warning, got %q", got)
	}
}

func TestCommandWarnsWhenGitHubReleaseIsNewer(t *testing.T) {
	t.Setenv("CI", "")
	oldVersion := buildinfo.Version
	buildinfo.Version = "v9.9.0-rc.1"
	t.Cleanup(func() { buildinfo.Version = oldVersion })

	var stderr bytes.Buffer
	srv := (&fakeServer{
		metaHandler:   jsonMeta("specgate.api/v1", map[string]bool{"agents": true}),
		statusHandler: jsonStatus(1, 0),
	}).build(t)

	deps, _ := newTestDeps(t, srv.URL)
	deps.Stderr = &stderr
	deps.CheckLatestRelease = func(context.Context, time.Duration, string) (string, error) {
		return "v9.9.0-rc.2", nil
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--server", srv.URL, "status", "--all-workspaces")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := stderr.String()
	if !strings.Contains(got, "latest GitHub release v9.9.0-rc.2") ||
		!strings.Contains(got, "scripts/install-cli.sh") {
		t.Fatalf("missing GitHub release warning, got %q", got)
	}
}

func TestGitHubReleaseWarningCanBeDisabled(t *testing.T) {
	t.Setenv("CI", "")
	oldVersion := buildinfo.Version
	buildinfo.Version = "v9.9.0-rc.1"
	t.Cleanup(func() { buildinfo.Version = oldVersion })
	t.Setenv("SPECGATE_NO_UPDATE_CHECK", "1")

	var stderr bytes.Buffer
	called := false
	srv := (&fakeServer{
		metaHandler:   jsonMeta("specgate.api/v1", map[string]bool{"agents": true}),
		statusHandler: jsonStatus(1, 0),
	}).build(t)

	deps, _ := newTestDeps(t, srv.URL)
	deps.Stderr = &stderr
	deps.CheckLatestRelease = func(context.Context, time.Duration, string) (string, error) {
		called = true
		return "v9.9.0-rc.2", nil
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--server", srv.URL, "status", "--all-workspaces")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, want 0", code)
	}
	if called {
		t.Fatal("GitHub release check should be skipped when SPECGATE_NO_UPDATE_CHECK=1")
	}
	if got := stderr.String(); got != "" {
		t.Fatalf("disabled update check should be silent, got %q", got)
	}
}
