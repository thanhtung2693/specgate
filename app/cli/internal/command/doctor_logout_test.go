package command_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
)

func TestLocalDoctorAfterLogoutDiagnosesWithoutSelectingIdentity(t *testing.T) {
	deps, out := newTestDeps(t)
	initArgs := []string{"--json", "init", "--mode", "local", "--local-dir", t.TempDir(), "--workspace-name", "Alpha", "--display-name", "Human", "--username", "human"}
	if code := command.ExecuteForCode(command.NewRootCommand(deps), initArgs...); code != 0 {
		t.Fatalf("init exit=%d: %s", code, out.String())
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "doctor"); code != 0 {
		t.Fatalf("initial doctor exit=%d: %s", code, out.String())
	}
	var before struct {
		Data struct{ Store struct{ ID string } }
	}
	if err := json.Unmarshal(out.Bytes(), &before); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "user", "logout"); code != 0 {
		t.Fatalf("logout exit=%d: %s", code, out.String())
	}
	configBefore, err := os.ReadFile(deps.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"--json", "--plain"} {
		out.Reset()
		if code := command.ExecuteForCode(command.NewRootCommand(deps), format, "doctor"); code != 0 {
			t.Fatalf("doctor after logout exit=%d: %s", code, out.String())
		}
		if format == "--json" {
			var got struct {
				OK   bool
				Data struct {
					Mode                string
					Store               struct{ ID, Status string }
					Identity, Workspace struct{ Status, Command string }
					Network             struct{ Status string }
				}
			}
			if err := json.Unmarshal(out.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if !got.OK || got.Data.Mode != "local" || got.Data.Store.Status != "ok" || before.Data.Store.ID == "" || got.Data.Store.ID != before.Data.Store.ID || got.Data.Identity.Status != "missing" || got.Data.Workspace.Status != "missing" || got.Data.Identity.Command != "specgate user login" || got.Data.Network.Status != "not_required" {
				t.Fatalf("wrong diagnosis: %s", out.String())
			}
		} else if bytes.Contains(out.Bytes(), []byte("ready")) || !bytes.Contains(out.Bytes(), []byte("specgate user login")) {
			t.Fatalf("logged-out plain diagnosis must not claim ready: %s", out.String())
		}
	}
	configAfter, err := os.ReadFile(deps.ConfigPath)
	if err != nil || !bytes.Equal(configBefore, configAfter) {
		t.Fatalf("doctor changed config: %v", err)
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "user", "current"); code != 3 {
		t.Fatalf("doctor silently restored selection: exit=%d %s", code, out.String())
	}
	if deps.Client != nil {
		t.Fatal("Local doctor created a remote client")
	}
}

func TestLocalDoctorDoesNotHideUnreadableStore(t *testing.T) {
	deps, out := newTestDeps(t)
	stateDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(stateDir, "state.db"), []byte("not a SQLite database"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := (config.Config{Mode: config.ModeLocal, Local: config.LocalStore{Path: stateDir}}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "doctor")
	var got struct {
		OK    bool
		Error struct{ Code string }
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if code != 5 || got.OK || got.Error.Code != "unavailable" {
		t.Fatalf("unreadable store was treated as missing selection: exit=%d %s", code, out.String())
	}
}
