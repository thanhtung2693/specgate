package command

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestSelfUpdateWindowsCLIVerifiesArchiveBeforeReplacing(t *testing.T) {
	var archive bytes.Buffer
	zipWriter := zip.NewWriter(&archive)
	binary, err := zipWriter.Create("specgate.exe")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := binary.Write([]byte("new-windows-binary")); err != nil {
		t.Fatal(err)
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(archive.Bytes())

	for _, testCase := range []struct {
		name         string
		checksum     string
		wantErr      string
		wantContents string
	}{
		{
			name:         "valid checksum",
			checksum:     fmt.Sprintf("%x  specgate_9.9.9_windows_amd64.zip\n", sum),
			wantContents: "new-windows-binary",
		},
		{
			name:         "invalid checksum",
			checksum:     strings.Repeat("0", sha256.Size*2) + "  specgate_9.9.9_windows_amd64.zip\n",
			wantErr:      "checksum",
			wantContents: "old-windows-binary",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/v9.9.9/specgate_9.9.9_windows_amd64.zip", func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write(archive.Bytes())
			})
			mux.HandleFunc("/v9.9.9/specgate_9.9.9_checksums.txt", func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(testCase.checksum))
			})
			server := httptest.NewServer(mux)
			defer server.Close()

			target := filepath.Join(t.TempDir(), "specgate.exe")
			if err := os.WriteFile(target, []byte("old-windows-binary"), 0o755); err != nil {
				t.Fatal(err)
			}
			deps := &Deps{
				Timeout:           time.Second,
				CLIReleaseBaseURL: server.URL,
			}
			err := selfUpdateWindowsCLI(context.Background(), deps, "v9.9.9", target, "amd64")
			if testCase.wantErr == "" && err != nil {
				t.Fatalf("selfUpdateWindowsCLI: %v", err)
			}
			if testCase.wantErr != "" && (err == nil || !strings.Contains(strings.ToLower(err.Error()), testCase.wantErr)) {
				t.Fatalf("error = %v, want %q", err, testCase.wantErr)
			}
			got, readErr := os.ReadFile(target)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if string(got) != testCase.wantContents {
				t.Fatalf("target contents = %q, want %q", got, testCase.wantContents)
			}
		})
	}
}
