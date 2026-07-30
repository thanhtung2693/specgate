package command_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestConfigCredentialStoresPerServerWithoutPrintingTheSecret(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	deps.ServerURL = "http://appliance.internal:3000"
	if err := (config.Config{Server: deps.ServerURL}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

	root := command.NewRootCommand(deps)
	root.SetIn(strings.NewReader("s3cret-value\n"))
	code := command.ExecuteForCode(root, "--json", "config", "credential", "tung")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	// The secret is the one thing that must never surface in output.
	if strings.Contains(out.String(), "s3cret-value") {
		t.Fatalf("credential secret leaked into output: %s", out.String())
	}

	cfg, err := config.LoadFrom(deps.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	credential, ok := cfg.CredentialFor("http://appliance.internal:3000/")
	if !ok || credential.Username != "tung" || credential.Secret != "s3cret-value" {
		t.Fatalf("credential = %#v, ok = %v; want it stored under the normalized server URL", credential, ok)
	}
	// A credential issued by one appliance must not resolve for another.
	if _, ok := cfg.CredentialFor("http://other.internal:3000"); ok {
		t.Fatal("a credential resolved for a server it was never issued for")
	}

	info, err := os.Stat(deps.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("config mode = %#o, want 0600 for a file holding a credential", mode)
	}
}

func TestConfigCredentialClearRemovesOnlyTheSelectedServer(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	deps.ServerURL = "http://a.internal:3000"
	seed := config.Config{Server: deps.ServerURL, Credentials: map[string]config.ServerCredential{
		"http://a.internal:3000": {Username: "tung", Secret: "one"},
		"http://b.internal:3000": {Username: "mai", Secret: "two"},
	}}
	if err := seed.SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "config", "credential", "--clear")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	cfg, err := config.LoadFrom(deps.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.CredentialFor("http://a.internal:3000"); ok {
		t.Fatal("selected server's credential survived --clear")
	}
	if _, ok := cfg.CredentialFor("http://b.internal:3000"); !ok {
		t.Fatal("--clear removed another server's credential")
	}
}

// The credential is useless unless requests actually carry it, and dangerous if
// requests to a different appliance carry it too.
func TestClientSendsStoredCredentialOnlyToItsOwnServer(t *testing.T) {
	t.Parallel()
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer server.Close()

	deps, _, _, out := newFakeDeps(t)
	deps.Client = nil
	deps.ServerURL = server.URL
	seed := config.Config{Server: server.URL, Credentials: map[string]config.ServerCredential{
		config.NormalizeServerURL(server.URL): {Username: "tung", Secret: "s3cret"},
	}}
	if err := seed.SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "workspace", "list"); code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	username, password, ok := basicAuthOf(gotAuth)
	if !ok || username != "tung" || password != "s3cret" {
		t.Fatalf("Authorization = %q; want Basic tung:s3cret", gotAuth)
	}

	// Same stored credential, a different selected server: nothing may be sent.
	// Uses its own capture variable so the assertion cannot read the first
	// server's request by accident.
	var otherAuth string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		otherAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer other.Close()
	deps2, _, _, out2 := newFakeDeps(t)
	deps2.Client = nil
	deps2.ServerURL = other.URL
	// The selected server is the other appliance; the stored credential still
	// belongs to the first one.
	elsewhere := config.Config{Server: other.URL, Credentials: seed.Credentials}
	if err := elsewhere.SaveTo(deps2.ConfigPath); err != nil {
		t.Fatal(err)
	}
	if code := command.ExecuteForCode(command.NewRootCommand(deps2), "--json", "workspace", "list"); code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out2.String())
	}
	if otherAuth != "" {
		t.Fatalf("credential sent to a server it was not issued for: %q", otherAuth)
	}
}

func basicAuthOf(header string) (username, password string, ok bool) {
	req := &http.Request{Header: http.Header{}}
	if header == "" {
		return "", "", false
	}
	req.Header.Set("Authorization", header)
	return req.BasicAuth()
}

// Machine callers must be able to see whether a credential is configured without
// the value ever appearing.
func TestConfigCredentialJSONReportsSetStateOnly(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	deps.ServerURL = "http://appliance.internal:3000"
	if err := (config.Config{Server: deps.ServerURL}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	root := command.NewRootCommand(deps)
	root.SetIn(strings.NewReader("s3cret-value\n"))
	if code := command.ExecuteForCode(root, "--json", "config", "credential", "tung"); code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var envelope struct {
		Data struct {
			Username string `json:"username"`
			Set      bool   `json:"credential_set"`
			Secret   string `json:"secret"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.Data.Set || envelope.Data.Username != "tung" {
		t.Fatalf("envelope = %#v, want credential_set for tung", envelope.Data)
	}
	if envelope.Data.Secret != "" {
		t.Fatal("envelope carried the secret")
	}
}

// Issuing is a Full-mode appliance concern. Local has no gateway, so the command
// must say that instead of failing obscurely against a server that isn't there.
func TestWorkspaceCredentialRefusedInLocalMode(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	stateDir := t.TempDir()
	if err := (config.Config{Mode: config.ModeLocal, Local: config.LocalStore{Path: stateDir}}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "workspace", "credential", "mai")
	if code != output.ExitIncompatible {
		t.Fatalf("exit = %d, want ExitIncompatible; output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "requires Full mode") {
		t.Fatalf("output = %q, want it to name where credentials apply", out.String())
	}
}

// The secret is shown once, so the human output has to carry it and say so, and
// point at the command that stores it on the other machine.
func TestWorkspaceCredentialShowsTheSecretOnceWithItsNextStep(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "workspace", "credential", "mai")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastCredentialUser != "mai" || fc.lastCredentialRevoke {
		t.Fatalf("issued for %q revoke=%v", fc.lastCredentialUser, fc.lastCredentialRevoke)
	}
	got := out.String()
	for _, want := range []string{"generated-secret", "only time the secret is shown", "specgate config credential mai"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output = %q, want %q", got, want)
		}
	}
}

// The member list is where an operator asks who can reach a shared appliance, so
// it reports credential state and who last changed it. On an appliance with no
// gateway credentials it stays silent rather than adding a column of "not set".
func TestWorkspaceMembersReportsAccessStateOnlyWhenCredentialsExist(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		members []client.WorkspaceMember
		want    []string
		absent  []string
	}{
		{
			name: "ungated appliance says nothing about credentials",
			members: []client.WorkspaceMember{
				{Username: "tung", DisplayName: "Tung", Role: "owner"},
			},
			absent: []string{"credential"},
		},
		{
			name: "a member who can authenticate, and who granted it",
			members: []client.WorkspaceMember{
				{Username: "mai", DisplayName: "Mai", Role: "member", CredentialSet: true,
					CredentialChangedBy: "tung", CredentialChangedAt: "2026-07-30T09:00:00Z"},
			},
			want: []string{"credential: set", "last changed by tung"},
		},
		{
			name: "a revoked member still shows who revoked it",
			members: []client.WorkspaceMember{
				{Username: "mai", DisplayName: "Mai", Role: "member",
					CredentialChangedBy: "tung", CredentialChangedAt: "2026-07-30T10:00:00Z"},
			},
			want: []string{"credential: revoked", "last changed by tung"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			deps, fc, _, out := newFakeDeps(t)
			deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
			if err := (config.Config{
				Workspace: config.CurrentWorkspace{ID: "ws-1", Slug: "platform", Name: "Platform"},
			}).SaveTo(deps.ConfigPath); err != nil {
				t.Fatal(err)
			}
			fc.workspaceMembers = &client.WorkspaceMembersResult{
				Workspace: client.IdentityWorkspace{ID: "ws-1", Slug: "platform", Name: "Platform"},
				Members:   tc.members,
			}
			if code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "workspace", "members"); code != output.ExitOK {
				t.Fatalf("exit = %d, output = %s", code, out.String())
			}
			got := out.String()
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Fatalf("output = %q, want %q", got, want)
				}
			}
			for _, absent := range tc.absent {
				if strings.Contains(got, absent) {
					t.Fatalf("output = %q, want no mention of %q", got, absent)
				}
			}
		})
	}
}

// The access record names the workspace the operator was acting in, so it has to
// travel with the request. `workspace` commands are not workspace-scoped as a
// family, so this one attaches it explicitly and a regression would silently
// leave every access row unscoped.
func TestWorkspaceCredentialSendsTheSelectedWorkspace(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	if err := (config.Config{
		Workspace: config.CurrentWorkspace{ID: "ws-1", Slug: "platform", Name: "Platform"},
	}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "workspace", "credential", "mai"); code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastCredentialWorkspace != "ws-1" {
		t.Fatalf("request workspace = %q, want ws-1", fc.lastCredentialWorkspace)
	}
}
