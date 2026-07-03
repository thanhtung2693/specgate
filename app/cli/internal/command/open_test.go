package command_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/output"
)

// newOpenDeps returns fake deps that record the URL passed to the opener.
// --server is passed explicitly in each test so the resolved web URL is stable.
func newOpenDeps(t *testing.T) (*command.Deps, *fakeClient, *string, *bytes.Buffer) {
	t.Helper()
	deps, fc, _, out := newFakeDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	opened := new(string)
	deps.Opener = func(u string) error {
		*opened = u
		return nil
	}
	return deps, fc, opened, out
}

func TestOpenBaseURLUnchanged(t *testing.T) {
	t.Parallel()
	deps, _, opened, out := newOpenDeps(t)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--server", "http://web.test", "open")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if *opened != "http://web.test" {
		t.Fatalf("opened = %q, want http://web.test", *opened)
	}
}

func TestOpenSectionPage(t *testing.T) {
	t.Parallel()
	for _, section := range []string{"reviews", "artifacts", "work"} {
		deps, fc, opened, out := newOpenDeps(t)
		code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--server", "http://web.test", "open", section)
		if code != output.ExitOK {
			t.Fatalf("exit = %d, output = %s", code, out.String())
		}
		if want := "http://web.test/" + section; *opened != want {
			t.Fatalf("opened = %q, want %q", *opened, want)
		}
		if fc.lastWorkRef != "" {
			t.Fatalf("section name %q must not be resolved as a work ref", section)
		}
	}
}

func TestOpenWorkRefDeepLink(t *testing.T) {
	t.Parallel()
	deps, fc, opened, out := newOpenDeps(t)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--server", "http://web.test", "open", "CR-101")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastWorkRef != "CR-101" {
		t.Fatalf("lastWorkRef = %q, want CR-101", fc.lastWorkRef)
	}
	if *opened != "http://web.test/work/CR-101" {
		t.Fatalf("opened = %q, want http://web.test/work/CR-101", *opened)
	}
}

func TestOpenArtifactFlagDeepLink(t *testing.T) {
	t.Parallel()
	deps, _, opened, out := newOpenDeps(t)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--server", "http://web.test", "open", "--artifact", "art-1")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if *opened != "http://web.test/artifacts?artifact=art-1" {
		t.Fatalf("opened = %q, want http://web.test/artifacts?artifact=art-1", *opened)
	}
}

func TestOpenArtifactFlagConflictsWithArg(t *testing.T) {
	t.Parallel()
	deps, _, opened, out := newOpenDeps(t)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--server", "http://web.test", "open", "CR-101", "--artifact", "art-1")
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want %d, output = %s", code, output.ExitUsage, out.String())
	}
	if *opened != "" {
		t.Fatalf("opened = %q, want nothing opened", *opened)
	}
}

func TestOpenJSONEnvelope(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newOpenDeps(t)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--server", "http://web.test", "open", "artifacts")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v, output: %s", err, out.String())
	}
	if !env.OK || env.Data.URL != "http://web.test/artifacts" {
		t.Fatalf("unexpected envelope: %s", out.String())
	}
}

// --- error language ---

func TestWorkShowNotFoundSuggestsWorkList(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.resolveErr = &client.APIError{Kind: client.ErrorNotFound, Status: 404, Message: "resolve_work_ref: no match"}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "work", "show", "CR-404")
	if code != output.ExitNotFound {
		t.Fatalf("exit = %d, want %d, output = %s", code, output.ExitNotFound, out.String())
	}
	want := "work item \"CR-404\" not found — try `specgate work list`"
	if !strings.Contains(out.String(), want) {
		t.Fatalf("output = %q, want %q", out.String(), want)
	}
	if strings.Contains(out.String(), "resolve_work_ref") {
		t.Fatalf("output must not leak the internal op name:\n%s", out.String())
	}
}

func TestOpenWorkRefNotFoundSuggestsWorkList(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newOpenDeps(t)
	fc.resolveErr = &client.APIError{Kind: client.ErrorNotFound, Status: 404, Message: "resolve_work_ref: no match"}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--server", "http://web.test", "open", "CR-404")
	if code != output.ExitNotFound {
		t.Fatalf("exit = %d, want %d, output = %s", code, output.ExitNotFound, out.String())
	}
	if !strings.Contains(out.String(), "work item \"CR-404\" not found — try `specgate work list`") {
		t.Fatalf("output = %q, want friendly not-found message", out.String())
	}
}

func TestUnreachableServerErrorMessage(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	serverURL := srv.URL
	srv.Close() // guarantee connection refused

	var out bytes.Buffer
	deps := &command.Deps{
		Stdout:     &out,
		Stderr:     &out,
		Stdin:      strings.NewReader(""),
		Opener:     func(string) error { return nil },
		ConfigPath: filepath.Join(t.TempDir(), "config.json"),
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--server", serverURL, "status")
	if code != output.ExitUnavailable {
		t.Fatalf("exit = %d, want %d, output = %s", code, output.ExitUnavailable, out.String())
	}
	want := "SpecGate server unreachable at " + serverURL + " — is the stack running? Try `specgate doctor` or `specgate up`."
	if !strings.Contains(out.String(), want) {
		t.Fatalf("output = %q, want %q", out.String(), want)
	}
}
