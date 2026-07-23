package deploy_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/specgate/specgate/app/cli/internal/deploy"
)

// fakeRunner records Run/Output calls so tests can assert which commands were executed.
type fakeRunner struct {
	Commands   []string
	Err        error
	OutputData []byte
}

func (f *fakeRunner) OutputToFile(_ context.Context, path, name string, args ...string) error {
	f.record(name, args...)
	if f.Err != nil {
		return f.Err
	}
	return os.WriteFile(path, f.OutputData, 0600)
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
// (compose.yml + env example) already in place, so Init does not attempt a download.
func newDeployTestServiceWithRunner(dir string) (*deploy.Service, *fakeRunner) {
	os.WriteFile(filepath.Join(dir, "compose.yml"), []byte("# placeholder\n"), 0644)
	os.WriteFile(filepath.Join(dir, "specgate.env.example"), []byte("SETTINGS_ENCRYPTION_KEY=\n"), 0644)
	if err := deploy.MarkManagedDirectory(dir); err != nil {
		panic(err)
	}
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

func TestUpRejectsUnmanagedComposeDirectoryWithoutMutation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "compose.yml"), "services: {}\n")
	runner := &fakeRunner{}
	svc := deploy.New(dir, runner)

	err := svc.Up(context.Background())
	if err == nil || !strings.Contains(err.Error(), "not a managed SpecGate deployment") {
		t.Fatalf("Up error = %v, want managed-directory refusal", err)
	}
	if len(runner.Commands) != 0 {
		t.Fatalf("Docker commands ran for unmanaged directory: %#v", runner.Commands)
	}
	if _, err := os.Stat(filepath.Join(dir, ".env")); !os.IsNotExist(err) {
		t.Fatalf("Up mutated unmanaged directory; .env stat err=%v", err)
	}
}

func TestExistingApplianceOperationsRejectUnmanagedDirectory(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		run  func(*deploy.Service) error
	}{
		{name: "pull", run: func(svc *deploy.Service) error { return svc.Pull(context.Background()) }},
		{name: "seed", run: func(svc *deploy.Service) error {
			return svc.SeedDemo(context.Background(), deploy.SeedDemoOptions{})
		}},
		{name: "remove labeled resources", run: func(svc *deploy.Service) error {
			return svc.RemoveLabeledResources(context.Background())
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, "compose.yml"), "services: {}\n")
			runner := &fakeRunner{}

			if err := tt.run(deploy.New(dir, runner)); err == nil {
				t.Fatal("unmanaged directory was accepted")
			}
			if len(runner.Commands) != 0 {
				t.Fatalf("Docker commands ran for unmanaged directory: %#v", runner.Commands)
			}
		})
	}
}

func TestBackupRejectsUnmanagedDirectoryWithoutCreatingFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "compose.yml"), "services: {}\n")
	runner := &fakeRunner{}

	if _, err := deploy.New(dir, runner).Backup(context.Background(), "v1.2.3"); err == nil {
		t.Fatal("unmanaged directory was accepted")
	}
	if len(runner.Commands) != 0 {
		t.Fatalf("Docker commands ran for unmanaged directory: %#v", runner.Commands)
	}
	if _, err := os.Stat(filepath.Join(dir, "backups")); !os.IsNotExist(err) {
		t.Fatalf("backup directory was created in unmanaged path: %v", err)
	}
}

func TestUpRejectsSymlinkedManagedDirectory(t *testing.T) {
	t.Parallel()
	target := t.TempDir()
	writeFile(t, filepath.Join(target, "compose.yml"), "services: {}\n")
	if err := deploy.MarkManagedDirectory(target); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "deployment")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}

	err := deploy.New(link, runner).Up(context.Background())
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("Up error = %v, want symlink refusal", err)
	}
	if len(runner.Commands) != 0 {
		t.Fatalf("Docker commands ran for symlinked directory: %#v", runner.Commands)
	}
}

func TestValidateManagedDirectoryRejectsSymlinkedAncestor(t *testing.T) {
	t.Parallel()
	targetParent := t.TempDir()
	target := filepath.Join(targetParent, "deployment")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := deploy.MarkManagedDirectory(target); err != nil {
		t.Fatal(err)
	}
	linkParent := filepath.Join(t.TempDir(), "linked-parent")
	if err := os.Symlink(targetParent, linkParent); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	err := deploy.ValidateManagedDirectory(filepath.Join(linkParent, "deployment"))
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("ValidateManagedDirectory error = %v, want symlinked-ancestor refusal", err)
	}
}

func TestValidateManagedDirectoryAcceptsDarwinSystemAlias(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "darwin" {
		t.Skip("macOS root aliases only")
	}
	dir := t.TempDir()
	if err := deploy.MarkManagedDirectory(dir); err != nil {
		t.Fatal(err)
	}
	if err := deploy.ValidateManagedDirectory(dir); err != nil {
		t.Fatalf("managed directory through macOS system alias was rejected: %v", err)
	}
}

// TestInitPreservesExistingSecret verifies that Init does not overwrite an
// existing SETTINGS_ENCRYPTION_KEY when the env file is already present.
func TestInitPreservesExistingSecret(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "specgate.env"), "SETTINGS_ENCRYPTION_KEY=keep-me\n")
	svc := newDeployTestService(dir)
	if err := svc.Init(context.Background(), deploy.InitOptions{Seed: deploy.SeedNo}); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, filepath.Join(dir, "specgate.env"))
	if !strings.Contains(got, "SETTINGS_ENCRYPTION_KEY=keep-me") {
		t.Fatalf("secret overwritten: %s", got)
	}
}

func TestInitSecuresExistingSecretFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "specgate.env")
	writeFile(t, path, "SETTINGS_ENCRYPTION_KEY=keep-me\n")
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}

	svc := newDeployTestService(dir)
	if err := svc.Init(context.Background(), deploy.InitOptions{Seed: deploy.SeedNo}); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("specgate.env mode = %04o, want 0600", got)
	}
}

func TestInitSecuresDeploymentDirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}

	svc := newDeployTestService(dir)
	if err := svc.Init(context.Background(), deploy.InitOptions{Seed: deploy.SeedNo}); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0700 {
		t.Fatalf("deployment directory mode = %04o, want 0700", got)
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
	if !runner.Contains("docker compose exec -T --user specgate specgate /usr/local/bin/doc-registry --seed-demo") {
		t.Fatalf("commands = %#v", runner.Commands)
	}
}

func TestExecRunnerIncludesCommandStderr(t *testing.T) {
	t.Parallel()
	err := (deploy.ExecRunner{}).Run(context.Background(), "sh", "-c", "echo port-conflict >&2; exit 1")
	if err == nil || !strings.Contains(err.Error(), "port-conflict") {
		t.Fatalf("Run() error = %v, want stderr detail", err)
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

func TestInitPullsPublishedBundleBeforeStarting(t *testing.T) {
	t.Parallel()
	svc, runner := newDeployTestServiceWithRunner(t.TempDir())

	if err := svc.Init(context.Background(), deploy.InitOptions{
		Seed:          deploy.SeedNo,
		BundleVersion: "v0.1.0-alpha.1",
	}); err != nil {
		t.Fatal(err)
	}

	pullIndex, upIndex := -1, -1
	for i, command := range runner.Commands {
		switch {
		case strings.HasSuffix(command, " pull"):
			pullIndex = i
		case strings.Contains(command, " up -d --wait"):
			upIndex = i
		}
	}
	if pullIndex == -1 {
		t.Fatalf("published init did not pull its pinned image: %#v", runner.Commands)
	}
	if upIndex == -1 || pullIndex > upIndex {
		t.Fatalf("published init must pull before up: %#v", runner.Commands)
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
	if !runner.Contains("docker compose exec -T --user specgate specgate /usr/local/bin/doc-registry --seed-demo --seed-demo-workspace-id ws-1 --seed-demo-created-by thanhtung2693") {
		t.Fatalf("commands = %#v", runner.Commands)
	}
}

func TestBackupStreamsArchiveWhenRunnerSupportsIt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "compose.yml"), "# placeholder\n")
	writeFile(t, filepath.Join(dir, ".env"), "SPECGATE_VERSION=v1.0.0\nSPECGATE_PORT=3000\n")
	writeFile(t, filepath.Join(dir, "specgate.env"), "SETTINGS_ENCRYPTION_KEY=secret\n")
	if err := deploy.MarkManagedDirectory(dir); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{OutputData: backupPayload(t)}
	svc := deploy.New(dir, runner)

	path, err := svc.Backup(context.Background(), "v1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("backup mode = %o, want 600", info.Mode().Perm())
	}
	members := archiveMembers(t, path)
	for _, want := range []string{"data.tar.gz", "deployment/compose.yml", "deployment/.env", "deployment/specgate.env", "recovery.json"} {
		if _, ok := members[want]; !ok {
			t.Fatalf("backup missing %q: %#v", want, members)
		}
	}
	if !bytes.Contains(members["deployment/specgate.env"], []byte("SETTINGS_ENCRYPTION_KEY=secret")) {
		t.Fatalf("backup omitted encryption configuration: %q", members["deployment/specgate.env"])
	}
	if !runner.Contains("docker compose exec -T specgate /usr/local/bin/specgate-backup") {
		t.Fatalf("commands = %#v", runner.Commands)
	}
}

func TestBackupRejectsMalformedPayload(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "compose.yml"), "# placeholder\n")
	writeFile(t, filepath.Join(dir, ".env"), "SPECGATE_VERSION=v1.0.0\n")
	writeFile(t, filepath.Join(dir, "specgate.env"), "SETTINGS_ENCRYPTION_KEY=secret\n")
	if err := deploy.MarkManagedDirectory(dir); err != nil {
		t.Fatal(err)
	}
	svc := deploy.New(dir, &fakeRunner{OutputData: []byte("not an archive")})

	if _, err := svc.Backup(context.Background(), "v1.2.3"); err == nil || !strings.Contains(err.Error(), "validate backup") {
		t.Fatalf("Backup error = %v, want validation failure", err)
	}
	backups, err := filepath.Glob(filepath.Join(dir, "backups", "specgate-before-*.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 0 {
		t.Fatalf("malformed backup was retained: %#v", backups)
	}
}

func TestBackupKeepsExistingRecoveryArchives(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "compose.yml"), "# placeholder\n")
	writeFile(t, filepath.Join(dir, ".env"), "SPECGATE_VERSION=v1.0.0\n")
	writeFile(t, filepath.Join(dir, "specgate.env"), "SETTINGS_ENCRYPTION_KEY=secret\n")
	if err := deploy.MarkManagedDirectory(dir); err != nil {
		t.Fatal(err)
	}
	backupDir := filepath.Join(dir, "backups")
	if err := os.MkdirAll(backupDir, 0700); err != nil {
		t.Fatal(err)
	}
	for index, name := range []string{
		"specgate-before-0.8.0-20260101T000000Z.tar.gz",
		"specgate-before-0.9.0-20260201T000000Z.tar.gz",
		"specgate-before-1.0.0-20260301T000000Z.tar.gz",
	} {
		path := filepath.Join(backupDir, name)
		if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
			t.Fatal(err)
		}
		when := time.Date(2026, time.Month(index+1), 1, 0, 0, 0, 0, time.UTC)
		if err := os.Chtimes(path, when, when); err != nil {
			t.Fatal(err)
		}
	}

	svc := deploy.New(dir, &fakeRunner{OutputData: backupPayload(t)})
	if _, err := svc.Backup(context.Background(), "v1.1.0"); err != nil {
		t.Fatal(err)
	}

	backups, err := filepath.Glob(filepath.Join(backupDir, "specgate-before-*.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 4 {
		t.Fatalf("backup count = %d, want 4: %#v", len(backups), backups)
	}
	if _, err := os.Stat(filepath.Join(backupDir, "specgate-before-0.8.0-20260101T000000Z.tar.gz")); err != nil {
		t.Fatalf("existing recovery archive was removed: %v", err)
	}
}

func backupPayload(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range map[string]string{"docreg.sql": "-- dump\n", "registry/blob": "blob"} {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(tw, body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func archiveMembers(t *testing.T, path string) map[string][]byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	members := map[string][]byte{}
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return members
		}
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		members[header.Name] = body
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
	got := readFile(t, filepath.Join(dir, "specgate.env"))
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
	t.Fatal("SETTINGS_ENCRYPTION_KEY not found in specgate.env")
}

func TestInitDerivesAppBaseURLFromUIPort(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".env"), "SPECGATE_PORT=13103\n")
	svc := newDeployTestService(dir)
	if err := svc.Init(context.Background(), deploy.InitOptions{Seed: deploy.SeedNo}); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, filepath.Join(dir, ".env"))
	if !strings.Contains(got, "APP_BASE_URL=http://localhost:13103") {
		t.Fatalf(".env missing APP_BASE_URL derived from SPECGATE_PORT: %s", got)
	}
}

func TestInitPersistsRuntimePortAndComposeProject(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".env.example"), "SPECGATE_PORT=3000\nSPECGATE_COMPOSE_PROJECT=specgate\nAPP_BASE_URL=http://localhost:3000\n")
	t.Setenv("SPECGATE_PORT", "13001")
	t.Setenv("SPECGATE_COMPOSE_PROJECT", "release-validation")

	svc := newDeployTestService(dir)
	if err := svc.Init(context.Background(), deploy.InitOptions{Seed: deploy.SeedNo}); err != nil {
		t.Fatal(err)
	}

	got := readFile(t, filepath.Join(dir, ".env"))
	for _, want := range []string{
		"SPECGATE_PORT=13001",
		"SPECGATE_COMPOSE_PROJECT=release-validation",
		"APP_BASE_URL=http://localhost:13001",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf(".env missing %q:\n%s", want, got)
		}
	}
}

func TestInitPersistsScopedProjectForAlternateDeployment(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".env.example"), "SPECGATE_PORT=3000\nSPECGATE_COMPOSE_PROJECT=specgate\n")
	project := deploy.ScopedProjectName(dir)

	svc := newDeployTestService(dir)
	if err := svc.Init(context.Background(), deploy.InitOptions{
		Seed: deploy.SeedNo, ComposeProject: project,
	}); err != nil {
		t.Fatal(err)
	}

	got := readFile(t, filepath.Join(dir, ".env"))
	if !strings.Contains(got, "SPECGATE_COMPOSE_PROJECT="+project) {
		t.Fatalf(".env did not isolate alternate deployment %q:\n%s", project, got)
	}
	if project == "specgate" || project != deploy.ScopedProjectName(dir) {
		t.Fatalf("scoped project name is not stable: %q", project)
	}
	other := deploy.ScopedProjectName(filepath.Join(dir, "other"))
	if other == project {
		t.Fatalf("different deployment directories share project %q", project)
	}
}

func TestInitExplicitComposeProjectOverridesScopedDefault(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".env.example"), "SPECGATE_COMPOSE_PROJECT=specgate\n")
	t.Setenv("SPECGATE_COMPOSE_PROJECT", "operator-selected")

	svc := newDeployTestService(dir)
	if err := svc.Init(context.Background(), deploy.InitOptions{
		Seed: deploy.SeedNo, ComposeProject: deploy.ScopedProjectName(dir),
	}); err != nil {
		t.Fatal(err)
	}

	got := readFile(t, filepath.Join(dir, ".env"))
	if !strings.Contains(got, "SPECGATE_COMPOSE_PROJECT=operator-selected") {
		t.Fatalf("explicit project override lost:\n%s", got)
	}
}

func TestInitPreservesExplicitAppBaseURLWithRuntimePortOverride(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".env"), "SPECGATE_PORT=3000\nAPP_BASE_URL=https://specgate.example\n")
	t.Setenv("SPECGATE_PORT", "13001")

	svc := newDeployTestService(dir)
	if err := svc.Init(context.Background(), deploy.InitOptions{Seed: deploy.SeedNo}); err != nil {
		t.Fatal(err)
	}

	got := readFile(t, filepath.Join(dir, ".env"))
	if !strings.Contains(got, "SPECGATE_PORT=13001") || !strings.Contains(got, "APP_BASE_URL=https://specgate.example") {
		t.Fatalf(".env did not retain the explicit public URL with the runtime port override:\n%s", got)
	}
}

func TestUpSynchronizesDerivedAppBaseURLWithConfiguredPort(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".env"), "SPECGATE_PORT=13103\nAPP_BASE_URL=http://localhost:3000\n")

	svc := newDeployTestService(dir)
	if err := svc.Up(context.Background()); err != nil {
		t.Fatal(err)
	}

	got := readFile(t, filepath.Join(dir, ".env"))
	if !strings.Contains(got, "APP_BASE_URL=http://localhost:13103") {
		t.Fatalf(".env missing APP_BASE_URL derived from configured port:\n%s", got)
	}
}

func TestUpPreservesNonLocalhostAppBaseURL(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".env"), "SPECGATE_PORT=13103\nAPP_BASE_URL=http://localhost.example:3000\n")

	svc := newDeployTestService(dir)
	if err := svc.Up(context.Background()); err != nil {
		t.Fatal(err)
	}

	got := readFile(t, filepath.Join(dir, ".env"))
	if !strings.Contains(got, "APP_BASE_URL=http://localhost.example:3000") {
		t.Fatalf(".env did not preserve explicit non-localhost URL:\n%s", got)
	}
}

func TestInitPinsDevBundleVersion(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".env"), "SPECGATE_VERSION=latest\n")
	svc, runner := newDeployTestServiceWithRunner(dir)
	if err := svc.Init(context.Background(), deploy.InitOptions{Seed: deploy.SeedNo, BundleVersion: "dev"}); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, filepath.Join(dir, ".env"))
	if !strings.Contains(got, "SPECGATE_VERSION=dev") {
		t.Fatalf(".env missing dev image tag: %s", got)
	}
	if runner.Contains("docker compose pull") {
		t.Fatalf("dev init must use the contributor-built local image: %#v", runner.Commands)
	}
}

func TestInitPreservesExplicitAppBaseURL(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".env"), "SPECGATE_PORT=13103\nAPP_BASE_URL=https://specgate.example\n")
	svc := newDeployTestService(dir)
	if err := svc.Init(context.Background(), deploy.InitOptions{Seed: deploy.SeedNo}); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, filepath.Join(dir, ".env"))
	if strings.Count(got, "APP_BASE_URL=") != 1 || !strings.Contains(got, "APP_BASE_URL=https://specgate.example") {
		t.Fatalf(".env did not preserve explicit APP_BASE_URL: %s", got)
	}
}
