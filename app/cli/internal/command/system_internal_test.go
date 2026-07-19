package command

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/client"
)

type timeoutStubErr struct{}

func (timeoutStubErr) Error() string { return "i/o timeout" }
func (timeoutStubErr) Timeout() bool { return true }

func TestMapAPIError_DistinguishesTimeoutFromUnreachable(t *testing.T) {
	t.Parallel()
	const u = "http://localhost:18080/api/v1/gates"

	timeout := mapAPIError(&url.Error{Op: "Post", URL: u, Err: timeoutStubErr{}})
	if !strings.Contains(timeout.Message, "timed out") {
		t.Fatalf("timeout message = %q, want it to mention a timeout", timeout.Message)
	}
	if !timeout.Transient {
		t.Fatal("timeout must be marked transient")
	}
	if strings.Contains(timeout.Message, "is the stack running") {
		t.Fatalf("a timeout must not misdirect to stack-down: %q", timeout.Message)
	}

	refused := mapAPIError(&url.Error{Op: "Post", URL: u, Err: errors.New("connection refused")})
	if !strings.Contains(refused.Message, "rerun it with local-network access") {
		t.Fatalf("a connection failure should explain the sandbox recovery path: %q", refused.Message)
	}
	if strings.Contains(refused.Message, "`specgate up`") {
		t.Fatalf("a connection failure must not suggest starting the stack: %q", refused.Message)
	}
	if !refused.Transient {
		t.Fatal("connection failure must be marked transient")
	}
}

func TestMapAPIErrorMarksServiceUnavailableTransient(t *testing.T) {
	t.Parallel()
	payload := mapAPIError(&client.APIError{
		Kind:    client.ErrorUnavailable,
		Status:  http.StatusServiceUnavailable,
		Message: "service unavailable",
	})
	if !payload.Transient {
		t.Fatal("service unavailable must be marked transient")
	}
}

func TestUpdateInstallerArgsIncludesInstallDir(t *testing.T) {
	t.Parallel()

	args := updateInstallerArgs()
	if len(args) != 2 {
		t.Fatalf("len(args) = %d, want 2 (%v)", len(args), args)
	}
	if args[0] != "--install-dir" {
		t.Fatalf("args[0] = %q, want --install-dir", args[0])
	}
	if strings.TrimSpace(args[1]) == "" {
		t.Fatalf("args[1] empty, want install dir")
	}
}

func TestFetchAndRunScriptPassesAllArgs(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("#!/usr/bin/env sh\nprintf '%s\\n' \"$@\"\n"))
	}))
	defer srv.Close()

	var out bytes.Buffer
	deps := &Deps{Stdout: &out, Stderr: &out}
	if err := fetchAndRunScript(context.Background(), deps, srv.URL, "--one", "two words", "--three"); err != nil {
		t.Fatalf("fetchAndRunScript: %v", err)
	}

	got := strings.TrimSpace(out.String())
	want := "--one\ntwo words\n--three"
	if got != want {
		t.Fatalf("args output = %q, want %q", got, want)
	}
}
