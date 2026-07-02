package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/config"
)

func TestServerPrecedence(t *testing.T) {
	t.Setenv("SPECGATE_SERVER", "https://env.example")
	got := config.ResolveServer("https://flag.example", config.Config{Server: "https://file.example"})
	if got != "https://flag.example" {
		t.Fatalf("server = %q, want flag value", got)
	}
}

func TestEnvOverridesFile(t *testing.T) {
	t.Setenv("SPECGATE_SERVER", "https://env.example")
	got := config.ResolveServer("", config.Config{Server: "https://file.example"})
	if got != "https://env.example" {
		t.Fatalf("server = %q, want env value", got)
	}
}

func TestFileOverridesDefault(t *testing.T) {
	t.Setenv("SPECGATE_SERVER", "")
	got := config.ResolveServer("", config.Config{Server: "https://file.example"})
	if got != "https://file.example" {
		t.Fatalf("server = %q, want file value", got)
	}
}

func TestDefaultFallback(t *testing.T) {
	t.Setenv("SPECGATE_SERVER", "")
	got := config.ResolveServer("", config.Config{})
	if got != "http://localhost:8080" {
		t.Fatalf("server = %q, want http://localhost:8080", got)
	}
}

func TestSaveAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	original := config.Config{Server: "https://saved.example", DeploymentDir: "/tmp/deploy"}
	if err := original.SaveTo(path); err != nil {
		t.Fatal(err)
	}
	got, err := config.LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Server != original.Server {
		t.Fatalf("Server = %q, want %q", got.Server, original.Server)
	}
	if got.DeploymentDir != original.DeploymentDir {
		t.Fatalf("DeploymentDir = %q, want %q", got.DeploymentDir, original.DeploymentDir)
	}
}

func TestSaveIsAtomicAndRestrictedMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	c := config.Config{Server: "https://atomic.example"}
	if err := c.SaveTo(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestLoadMissingFileReturnsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent.json")
	got, err := config.LoadFrom(path)
	if err != nil {
		t.Fatalf("unexpected error for missing file: %v", err)
	}
	if got.Server != "" {
		t.Fatalf("Server = %q, want empty", got.Server)
	}
}
