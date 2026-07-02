package command

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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
