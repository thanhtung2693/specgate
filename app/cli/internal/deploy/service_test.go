package deploy_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/deploy"
)

// fakeRunner records Run/Output calls so tests can assert which commands were executed.
type fakeRunner struct {
	Commands []string
	Err      error
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) error {
	f.record(name, args...)
	return f.Err
}

func (f *fakeRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	f.record(name, args...)
	return []byte("[]"), f.Err
}

func (f *fakeRunner) record(name string, args ...string) {
	all := append([]string{name}, args...)
	f.Commands = append(f.Commands, strings.Join(all, " "))
}

// Contains reports whether any recorded command contains sub as a word-subsequence.
// This tolerates flags like "-f /path/to/compose.yml" inserted between command words.
func (f *fakeRunner) Contains(sub string) bool {
	needle := strings.Fields(sub)
	for _, cmd := range f.Commands {
		if wordSubsequence(strings.Fields(cmd), needle) {
			return true
		}
	}
	return false
}

// wordSubsequence reports whether every word in needle appears in haystack in order.
func wordSubsequence(haystack, needle []string) bool {
	j := 0
	for _, w := range haystack {
		if j < len(needle) && w == needle[j] {
			j++
		}
	}
	return j == len(needle)
}

// newDeployTestServiceWithRunner creates a Service in dir with a minimal bundle
// (compose.yml + env examples) already in place, so Init does not attempt a download.
func newDeployTestServiceWithRunner(dir string) (*deploy.Service, *fakeRunner) {
	os.WriteFile(filepath.Join(dir, "compose.yml"), []byte("# placeholder\n"), 0644)
	os.WriteFile(filepath.Join(dir, "doc-registry.env.example"), []byte("SETTINGS_ENCRYPTION_KEY=\n"), 0644)
	os.WriteFile(filepath.Join(dir, "agents.env.example"), []byte("LANGSMITH_API_KEY=\n"), 0644)
	runner := &fakeRunner{}
	return deploy.New(dir, runner), runner
}

func newDeployTestService(dir string) *deploy.Service {
	svc, _ := newDeployTestServiceWithRunner(dir)
	return svc
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestInitPreservesExistingSecret verifies that Init does not overwrite an
// existing SETTINGS_ENCRYPTION_KEY when the env file is already present.
func TestInitPreservesExistingSecret(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "doc-registry.env"), "SETTINGS_ENCRYPTION_KEY=keep-me\n")
	svc := newDeployTestService(dir)
	if err := svc.Init(context.Background(), deploy.InitOptions{Seed: deploy.SeedNo}); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, filepath.Join(dir, "doc-registry.env"))
	if !strings.Contains(got, "SETTINGS_ENCRYPTION_KEY=keep-me") {
		t.Fatalf("secret overwritten: %s", got)
	}
}

// TestInitSeedsOnlyWhenRequested verifies that the seed command is executed
// exactly when SeedYes is requested, and not otherwise.
func TestInitSeedsOnlyWhenRequested(t *testing.T) {
	t.Parallel()
	svc, runner := newDeployTestServiceWithRunner(t.TempDir())
	if err := svc.Init(context.Background(), deploy.InitOptions{Seed: deploy.SeedYes}); err != nil {
		t.Fatal(err)
	}
	if !runner.Contains("docker compose run --rm doc-registry --seed-demo") {
		t.Fatalf("commands = %#v", runner.Commands)
	}
}

// TestInitNoSeedSkipsSeedCommand verifies that no seed command is issued for SeedNo.
func TestInitNoSeedSkipsSeedCommand(t *testing.T) {
	t.Parallel()
	svc, runner := newDeployTestServiceWithRunner(t.TempDir())
	if err := svc.Init(context.Background(), deploy.InitOptions{Seed: deploy.SeedNo}); err != nil {
		t.Fatal(err)
	}
	if runner.Contains("--seed-demo") {
		t.Fatalf("unexpected seed command: %#v", runner.Commands)
	}
}

func TestSeedDemoPassesAttributionFlags(t *testing.T) {
	t.Parallel()
	svc, runner := newDeployTestServiceWithRunner(t.TempDir())
	if err := svc.SeedDemo(context.Background(), deploy.SeedDemoOptions{
		WorkspaceID: "ws-1",
		CreatedBy:   "thanhtung2693",
	}); err != nil {
		t.Fatal(err)
	}
	if !runner.Contains("docker compose run --rm doc-registry --seed-demo --seed-demo-workspace-id ws-1 --seed-demo-created-by thanhtung2693") {
		t.Fatalf("commands = %#v", runner.Commands)
	}
}

// TestInitGeneratesEncryptionKeyWhenBlank verifies that a new random key is
// written when SETTINGS_ENCRYPTION_KEY is blank.
func TestInitGeneratesEncryptionKeyWhenBlank(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	svc := newDeployTestService(dir)
	if err := svc.Init(context.Background(), deploy.InitOptions{Seed: deploy.SeedNo}); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, filepath.Join(dir, "doc-registry.env"))
	const prefix = "SETTINGS_ENCRYPTION_KEY="
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, prefix) {
			val := strings.TrimPrefix(line, prefix)
			if val == "" {
				t.Fatal("SETTINGS_ENCRYPTION_KEY was not generated")
			}
			return
		}
	}
	t.Fatal("SETTINGS_ENCRYPTION_KEY not found in doc-registry.env")
}
