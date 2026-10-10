package command_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestOpenServerMetadataHTTPBoundary(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		target string
		code   int
	}{
		{"cmd.exe /c echo unsafe", output.ExitUnavailable},
		{`https://web.invalid/reviews?one=1&literal="quoted"&env=%PATH%`, output.ExitOK},
	} {
		t.Run(tc.target, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/meta" {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(client.Meta{APIVersion: "specgate.api/v1", WebURL: tc.target})
			}))
			defer server.Close()
			deps, _, opened, out := newOpenDeps(t)
			deps.Client = nil
			code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--server", server.URL, "open")
			if code != tc.code {
				t.Fatalf("HTTP boundary code=%d want=%d output=%s", code, tc.code, out.String())
			}
			want := ""
			if tc.code == output.ExitOK {
				want = tc.target
			}
			if *opened != want {
				t.Fatalf("HTTP metadata target=%q want=%q", *opened, want)
			}
		})
	}
}

func TestOpenRefusesNonBrowserMetadataBeforeLaunchOrPrint(t *testing.T) {
	t.Parallel()
	for _, target := range []string{
		"cmd.exe /c echo unsafe", `C:\Windows\System32\cmd.exe`,
		"file:///C:/Windows/System32/cmd.exe", "javascript:alert(1)", "ms-settings:display",
		"//web.invalid/reviews", "https:opaque", "https:///no-host", "https://",
		"https://web.invalid/\nunsafe", "https://web.invalid/\x00unsafe", "https://web.invalid/\tunsafe",
	} {
		for _, print := range []bool{false, true} {
			t.Run(target+"/print="+map[bool]string{false: "false", true: "true"}[print], func(t *testing.T) {
				deps, fc, opened, out := newOpenDeps(t)
				fc.metaResult = &client.Meta{APIVersion: "specgate.api/v1", WebURL: target}
				args := []string{"--json", "open"}
				if print {
					args = append(args, "--print")
				}
				code := command.ExecuteForCode(command.NewRootCommand(deps), args...)
				if code != output.ExitUnavailable || *opened != "" || !strings.Contains(out.String(), "HTTP(S)") {
					t.Fatalf("unsafe target accepted: code=%d opened=%q output=%s", code, *opened, out.String())
				}
			})
		}
	}
}

func TestOpenPreservesBrowserURLDataAndBasePaths(t *testing.T) {
	t.Parallel()
	for _, target := range []string{
		"http://localhost:4317/base", "https://[::1]:8443/base",
		"https://web.invalid/base?one=1&two=2#review",
		"https://web.invalid/%E2%9C%93?name=a%20b",
		"https://web.invalid/reviews?literal=%25PATH%25&next=%22quoted%22",
	} {
		t.Run(target, func(t *testing.T) {
			deps, fc, opened, out := newOpenDeps(t)
			fc.metaResult = &client.Meta{APIVersion: "specgate.api/v1", WebURL: target}
			code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "open")
			if code != output.ExitOK || *opened != target {
				t.Fatalf("legitimate URL changed: code=%d opened=%q output=%s", code, *opened, out.String())
			}
		})
	}
}

func TestOpenRefusesNonBrowserFallbackURL(t *testing.T) {
	t.Parallel()
	deps, fc, opened, out := newOpenDeps(t)
	fc.metaResult = &client.Meta{APIVersion: "specgate.api/v1"}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--server", "file:///C:/unsafe.exe", "open", "--print")
	if code != output.ExitUnavailable || *opened != "" || !strings.Contains(out.String(), "HTTP(S)") {
		t.Fatalf("unsafe fallback accepted: code=%d opened=%q output=%s", code, *opened, out.String())
	}
}
