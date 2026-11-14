package command_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestLegacyFullConfigIsPreservedBeforeAnyLocalOrRemoteSetup(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusTeapot)
	}))
	defer server.Close()
	for _, legacy := range []struct{ name, fields string }{
		{"explicit-full", `"mode":"full",`},
		{"implicit-server", `"server":"` + server.URL + `",`},
		{"implicit-deployment", `"deployment_dir":"legacy-appliance",`},
		{"implicit-credentials", `"credentials":{"` + server.URL + `":{"username":"fixture","secret":"fake"}},`},
		{"implicit-workspace", `"workspace":{"id":"remote-id"},`},
		{"implicit-project-binding", ``},
	} {
		t.Run(legacy.name, func(t *testing.T) {
			root := t.TempDir()
			store := filepath.Join(root, "unrelated-store")
			if err := os.Mkdir(store, 0o700); err != nil {
				t.Fatal(err)
			}
			state := filepath.Join(store, "state.db")
			if err := os.WriteFile(state, []byte("legacy sentinel"), 0o644); err != nil {
				t.Fatal(err)
			}
			t.Setenv("SPECGATE_LOCAL_DIR", store)
			t.Setenv("SPECGATE_SERVER", "")
			cfg := filepath.Join(root, "config.json")
			original := "{\n" + legacy.fields + `"projects":{"/legacy":{"workspace":{"id":"record"}}}}` + "\n"
			if err := os.WriteFile(cfg, []byte(original), 0o640); err != nil {
				t.Fatal(err)
			}
			for _, args := range [][]string{{"doctor"}, {"init", "--mode", "local", "--workspace-name", "Demo", "--display-name", "Test User", "--username", "tester"}, {"update"}, {"uninstall", "--purge-data", "--yes"}} {
				deps, out := newTestDeps(t)
				deps.ConfigPath, deps.WorkingDir = cfg, root
				deps.CommandRunner = &fakeCommandRunner{OnCommand: func() { t.Fatal("legacy configuration started an external command") }}
				if code := command.ExecuteForCode(command.NewRootCommand(deps), append([]string{"--json", "--timeout", "1s"}, args...)...); code != output.ExitIncompatible {
					t.Fatalf("%v exit=%d output=%s", args, code, out)
				}
				var envelope struct{ Error output.ErrorPayload }
				if err := json.Unmarshal(out.Bytes(), &envelope); err != nil || !strings.Contains(envelope.Error.Message, "SPECGATE_CONFIG_PATH") {
					t.Fatalf("%v recovery response: %s", args, out)
				}
			}
			for path, want := range map[string]struct {
				data string
				mode os.FileMode
			}{cfg: {original, 0o640}, state: {"legacy sentinel", 0o644}} {
				data, err := os.ReadFile(path)
				info, statErr := os.Stat(path)
				if err != nil || statErr != nil || string(data) != want.data || info.Mode().Perm() != want.mode {
					t.Fatalf("legacy file changed: %s", path)
				}
			}
		})
	}
	if requests.Load() != 0 {
		t.Fatalf("legacy configuration contacted server %d times", requests.Load())
	}
}

func TestFreshInitDefaultsToLocalAndRetiredSyntaxDoesNotCreateState(t *testing.T) {
	deps, out := newTestDeps(t)
	store := filepath.Join(t.TempDir(), "never-created")
	t.Setenv("SPECGATE_LOCAL_DIR", store)
	t.Setenv("SPECGATE_SERVER", "")
	for _, c := range []struct {
		args []string
		exit int
	}{
		{[]string{"init", "--mode", "full"}, output.ExitIncompatible},
		{[]string{"doctor", "--fix"}, output.ExitUsage},
		{[]string{"cleanup", "--backups", "--yes"}, output.ExitUsage},
		{[]string{"--server", "http://localhost:1", "doctor"}, output.ExitUsage},
	} {
		out.Reset()
		if code := command.ExecuteForCode(command.NewRootCommand(deps), append([]string{"--json"}, c.args...)...); code != c.exit {
			t.Fatalf("%v exit=%d output=%s", c.args, code, out)
		}
	}
	if _, err := os.Stat(store); !os.IsNotExist(err) {
		t.Fatalf("retired syntax opened a Local store: %v", err)
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "init", "--workspace-name", "Fresh", "--display-name", "Local tester", "--username", "tester"); code != output.ExitOK || !strings.Contains(out.String(), `"mode":"local"`) {
		t.Fatalf("fresh Local init exit=%d output=%s", code, out)
	}
}

func TestLegacyFullRecoveryWithSeparateConfigAndCleanEnvironment(t *testing.T) {
	deps, out := newTestDeps(t)
	legacyRepo, cleanDir := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(legacyRepo, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	const server = "http://127.0.0.1:1"
	repoConfig := filepath.Join(legacyRepo, ".specgate", "config")
	if err := os.Mkdir(filepath.Dir(repoConfig), 0o700); err != nil {
		t.Fatal(err)
	}
	const original = "{\"server\":\"" + server + "\"}\n"
	if err := os.WriteFile(repoConfig, []byte(original), 0o640); err != nil {
		t.Fatal(err)
	}
	deps.ConfigPath = filepath.Join(t.TempDir(), "fresh.json")
	stateDir := filepath.Join(t.TempDir(), "fresh-store")
	t.Setenv("SPECGATE_LOCAL_DIR", stateDir)
	args := []string{"--json", "init", "--workspace-name", "Fresh", "--display-name", "Local tester", "--username", "tester"}
	checkInit := func(dir, serverEnv string, want int) {
		t.Helper()
		deps.WorkingDir = dir
		t.Setenv("SPECGATE_SERVER", serverEnv)
		out.Reset()
		if code := command.ExecuteForCode(command.NewRootCommand(deps), args...); code != want {
			t.Fatalf("init exit=%d want=%d output=%s", code, want, out)
		}
		if want == output.ExitIncompatible {
			if !strings.Contains(out.String(), "unset SPECGATE_SERVER") || !strings.Contains(out.String(), "outside the legacy repository") {
				t.Errorf("incomplete recovery guidance: %s", out)
			}
		}
	}
	checkInit(legacyRepo, "", output.ExitIncompatible)
	checkInit(cleanDir, server, output.ExitIncompatible)
	for _, path := range []string{deps.ConfigPath, stateDir} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("refused init created %s: %v", path, err)
		}
	}
	checkInit(cleanDir, "", output.ExitOK)
	checkInit(legacyRepo, server, output.ExitOK)
	if data, err := os.ReadFile(repoConfig); err != nil || string(data) != original {
		t.Fatalf("legacy repository configuration changed: %v", err)
	}
}

func TestLocalStartupUsesBoundStoreWithLegacyFields(t *testing.T) {
	requests := atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusTeapot)
	}))
	defer server.Close()
	deps, out := newTestDeps(t)
	storeDir := t.TempDir()
	t.Setenv("SPECGATE_SERVER", server.URL)
	t.Setenv("SPECGATE_LOCAL_DIR", "")
	t.Setenv("SPECGATE_WORKSPACE", "")
	if err := os.Mkdir(filepath.Join(deps.WorkingDir, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	root, _ := config.FindProjectRoot(deps.WorkingDir)
	store, err := local.Open(filepath.Join(storeDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	sel, err := store.Initialize(t.Context(), local.InitInput{WorkspaceName: "global", DisplayName: "Local tester", Username: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	w, err := store.CreateWorkspace(t.Context(), "bound")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	global := filepath.Join(t.TempDir(), "unused-global")
	cfg := config.Config{Mode: config.ModeLocal, Server: server.URL, DeploymentDir: "/retired", Local: config.LocalStore{Path: global}, Projects: map[string]config.ProjectConfig{
		root: {Local: config.LocalStore{Path: storeDir, ID: sel.StoreID}, Workspace: config.CurrentWorkspace{ID: w.ID, Slug: w.Slug, Name: w.Name}},
	}}
	if err := cfg.SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(deps.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"workspace", "current"}, {"--workspace", sel.Workspace.ID, "workspace", "current"}} {
		out.Reset()
		if code := command.ExecuteForCode(command.NewRootCommand(deps), append([]string{"--json"}, args...)...); code != output.ExitOK {
			t.Fatalf("%v exit=%d output=%s", args, code, out)
		}
		slug := "bound"
		if len(args) > 2 {
			slug = "global"
		}
		if !strings.Contains(out.String(), `"slug":"`+slug+`"`) {
			t.Fatalf("%v wrong workspace: %s", args, out)
		}
	}
	after, err := os.ReadFile(deps.ConfigPath)
	if err != nil || string(before) != string(after) || requests.Load() != 0 {
		t.Fatalf("read mutated config or contacted legacy server. err=%v requests=%d", err, requests.Load())
	}
	if _, err := os.Stat(global); !os.IsNotExist(err) {
		t.Fatalf("unused global store opened: %v", err)
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "init", "--workspace-name", w.Name, "--display-name", "Local tester", "--username", "tester"); code != output.ExitOK {
		t.Fatalf("init exit=%d output=%s", code, out)
	}
	updated, err := config.LoadFrom(deps.ConfigPath)
	if err != nil || updated.Local != cfg.Local || updated.Projects[root].Local != cfg.Projects[root].Local || updated.Server != cfg.Server {
		t.Fatalf("re-init replaced an unrelated store or lost legacy settings: %v", err)
	}
	if _, err := os.Stat(global); !os.IsNotExist(err) {
		t.Fatalf("re-init created an unrelated store: %v", err)
	}
}
