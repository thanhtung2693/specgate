package command_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/local"
)

func TestLocalDoctorReportsTheStoreItOpened(t *testing.T) {
	for _, source := range []string{"global", "project", "environment"} {
		t.Run(source, func(t *testing.T) {
			t.Setenv("SPECGATE_LOCAL_DIR", "")
			deps, out := newTestDeps(t)
			if err := os.Mkdir(filepath.Join(deps.WorkingDir, ".git"), 0755); err != nil {
				t.Fatal(err)
			}
			globalDir, activeDir := t.TempDir(), t.TempDir()
			if source == "global" {
				activeDir = globalDir
			}
			store, err := local.Open(filepath.Join(activeDir, "state.db"))
			if err != nil {
				t.Fatal(err)
			}
			selection, err := store.Initialize(context.Background(), local.InitInput{
				WorkspaceName: "Active", DisplayName: "Human", Username: "human",
			})
			if closeErr := store.Close(); err != nil || closeErr != nil {
				t.Fatalf("initialize store: %v; close: %v", err, closeErr)
			}
			cfg := config.Config{Mode: config.ModeLocal, Local: config.LocalStore{Path: globalDir}}
			if source != "global" {
				projectDir := activeDir
				if source == "environment" {
					projectDir = t.TempDir()
					t.Setenv("SPECGATE_LOCAL_DIR", "  "+activeDir+"  ")
				}
				root, found := config.FindProjectRoot(deps.WorkingDir)
				if !found {
					t.Fatal("fixture repository not found")
				}
				cfg.Projects = map[string]config.ProjectConfig{
					root: {Local: config.LocalStore{Path: projectDir}},
				}
			}
			if err := cfg.SaveTo(deps.ConfigPath); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(deps.ConfigPath)
			if err != nil {
				t.Fatal(err)
			}
			for _, format := range []string{"--json", "--plain"} {
				out.Reset()
				if code := command.ExecuteForCode(command.NewRootCommand(deps), format, "doctor"); code != 0 {
					t.Fatalf("doctor exit=%d: %s", code, out.String())
				}
				if format == "--json" {
					var got struct {
						Data struct{ Store struct{ Path, ID string } }
					}
					if err := json.Unmarshal(out.Bytes(), &got); err != nil {
						t.Fatal(err)
					}
					if got.Data.Store.Path != activeDir || got.Data.Store.ID != selection.StoreID {
						t.Fatalf("path and ID must describe the same active store: %s", out.String())
					}
				} else if !strings.Contains(out.String(), activeDir) || (activeDir != globalDir && strings.Contains(out.String(), globalDir)) {
					t.Fatalf("plain diagnosis names the wrong store: %s", out.String())
				}
			}
			after, err := os.ReadFile(deps.ConfigPath)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("doctor persisted its override: %v", err)
			}
			if deps.Client != nil {
				t.Fatal("Local doctor created a remote client")
			}
		})
	}
}
