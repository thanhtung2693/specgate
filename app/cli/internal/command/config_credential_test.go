package command_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

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
