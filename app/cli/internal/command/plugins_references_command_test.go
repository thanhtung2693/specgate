package command_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestLocalPluginReferenceInstallRefreshAndDoctor(t *testing.T) {
	for _, projectLocal := range []bool{false, true} {
		t.Run(fmt.Sprintf("project=%v", projectLocal), func(t *testing.T) {
			project := t.TempDir()
			t.Chdir(project)
			home := t.TempDir()
			deps, out := newPluginDeps(home)
			if err := (config.Config{Mode: config.ModeLocal, Local: config.LocalStore{Path: t.TempDir()}}).SaveTo(deps.ConfigPath); err != nil {
				t.Fatal(err)
			}
			install := []string{"--json", "plugins", "install", "--agent", "all"}
			doctor := []string{"--json", "plugins", "doctor", "--agent", "all"}
			dirs := []string{
				filepath.Join(home, ".cursor", "skills", "specgate-work-preparation"),
				filepath.Join(home, ".codex", "plugins", "specgate", "skills", "specgate-work-preparation"),
				filepath.Join(home, ".claude", "skills", "specgate", "skills", "specgate-work-preparation"),
			}
			if projectLocal {
				install = append(install, "--project-local")
				doctor = append(doctor, "--project-local")
				dirs = []string{
					filepath.Join(project, ".cursor", "skills", "specgate-work-preparation"),
					filepath.Join(project, ".agents", "skills", "specgate-work-preparation"),
					filepath.Join(project, ".claude", "skills", "specgate-work-preparation"),
				}
			}
			for range 2 {
				out.Reset()
				if code := command.ExecuteForCode(command.NewRootCommand(deps), install...); code != output.ExitOK {
					t.Fatalf("install/refresh exit=%d output=%s", code, out.String())
				}
			}
			for _, dir := range dirs {
				file := filepath.Join(dir, "references", "preservation.md")
				if body, err := os.ReadFile(file); err != nil || !strings.Contains(string(body), "# Preservation criteria") {
					t.Fatalf("reference missing at %s: err=%v", file, err)
				}
			}
			out.Reset()
			if code := command.ExecuteForCode(command.NewRootCommand(deps), doctor...); code != output.ExitOK {
				t.Fatalf("complete install unhealthy: exit=%d output=%s", code, out.String())
			}
			missing := filepath.Join(dirs[0], "references", "preservation.md")
			if err := os.Remove(missing); err != nil {
				t.Fatal(err)
			}
			out.Reset()
			if code := command.ExecuteForCode(command.NewRootCommand(deps), doctor...); code == output.ExitOK || !strings.Contains(out.String(), "preservation.md") {
				t.Fatalf("doctor accepted missing reference: exit=%d output=%s", code, out.String())
			}
			out.Reset()
			if code := command.ExecuteForCode(command.NewRootCommand(deps), install...); code != output.ExitOK {
				t.Fatalf("repair failed: exit=%d output=%s", code, out.String())
			}
			if !projectLocal {
				out.Reset()
				if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "uninstall"); code != output.ExitOK {
					t.Fatalf("uninstall failed: exit=%d output=%s", code, out.String())
				}
				for _, dir := range dirs {
					if _, err := os.Stat(filepath.Join(dir, "references", "preservation.md")); !os.IsNotExist(err) {
						t.Fatalf("uninstall retained managed reference: %s err=%v", dir, err)
					}
				}
			}
		})
	}
}
