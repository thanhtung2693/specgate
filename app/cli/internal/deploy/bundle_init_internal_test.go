package deploy

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type updateRunner struct {
	commands      []string
	targetUpFails bool
}

func (r *updateRunner) Run(_ context.Context, name string, args ...string) error {
	command := strings.Join(append([]string{name}, args...), " ")
	r.commands = append(r.commands, command)
	if r.targetUpFails && strings.HasSuffix(command, " up -d --wait") {
		r.targetUpFails = false
		return errors.New("target readiness failed")
	}
	return nil
}

func (r *updateRunner) Output(context.Context, string, ...string) ([]byte, error) {
	return []byte("[]"), nil
}

func (r *updateRunner) OutputToFile(_ context.Context, path, _ string, _ ...string) error {
	var payload bytes.Buffer
	gz := gzip.NewWriter(&payload)
	tw := tar.NewWriter(gz)
	for name, body := range map[string]string{"docreg.sql": "-- dump\n", "registry/blob": "blob"} {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(body))}); err != nil {
			return err
		}
		if _, err := io.WriteString(tw, body); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	return os.WriteFile(path, payload.Bytes(), 0600)
}

// noopRunner satisfies CommandRunner without executing anything.
type noopRunner struct{}

func (noopRunner) Run(context.Context, string, ...string) error { return nil }
func (noopRunner) Output(context.Context, string, ...string) ([]byte, error) {
	return []byte("[]"), nil
}
func (noopRunner) OutputToFile(context.Context, string, string, ...string) error { return nil }

// TestInitDownloadsBundleWhenAbsent verifies Init fetches the compose bundle
// when the deployment dir has no compose.yml, and pins SPECGATE_VERSION.
func TestInitDownloadsBundleWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	svc := New(dir, noopRunner{})

	var gotBaseURL, gotVersion string
	svc.fetchBundle = func(_ context.Context, baseURL, version, destDir string) error {
		gotBaseURL, gotVersion = baseURL, version
		// Simulate extraction of the bundle into destDir.
		for name, body := range map[string]string{
			"compose.yml":          "# bundle\n",
			".env.example":         "SPECGATE_VERSION=latest\n",
			"specgate.env.example": "SETTINGS_ENCRYPTION_KEY=\n",
			"rollback-compatible":  "true\n",
		} {
			if err := os.WriteFile(filepath.Join(destDir, name), []byte(body), 0644); err != nil {
				return err
			}
		}
		return nil
	}

	if err := svc.Init(context.Background(), InitOptions{Seed: SeedNo, BundleVersion: "v9.9.9"}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if gotVersion != "v9.9.9" {
		t.Fatalf("fetchBundle version = %q, want v9.9.9", gotVersion)
	}
	if !strings.Contains(gotBaseURL, "thanhtung2693/specgate/releases/download/v9.9.9") {
		t.Fatalf("fetchBundle baseURL = %q", gotBaseURL)
	}
	env, _ := os.ReadFile(filepath.Join(dir, ".env"))
	if !strings.Contains(string(env), "SPECGATE_VERSION=v9.9.9") {
		t.Fatalf(".env missing version pin: %q", env)
	}
}

func TestInitRejectsNonEmptyUnmanagedDirectoryWithoutChangingIt(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	userFile := filepath.Join(dir, "user-spec.md")
	if err := os.WriteFile(userFile, []byte("# Keep me\n"), 0644); err != nil {
		t.Fatal(err)
	}
	svc := New(dir, noopRunner{})
	fetched := false
	svc.fetchBundle = func(context.Context, string, string, string) error {
		fetched = true
		return nil
	}

	err := svc.Init(context.Background(), InitOptions{Seed: SeedNo, BundleVersion: "v9.9.9"})
	if err == nil || !strings.Contains(err.Error(), "not an empty or managed SpecGate deployment") {
		t.Fatalf("Init error = %v, want unmanaged-directory refusal", err)
	}
	if fetched {
		t.Fatal("bundle fetched before deployment directory ownership was established")
	}
	if body, readErr := os.ReadFile(userFile); readErr != nil || string(body) != "# Keep me\n" {
		t.Fatalf("user file changed: body=%q err=%v", body, readErr)
	}
	info, statErr := os.Stat(dir)
	if statErr != nil {
		t.Fatal(statErr)
	}
	if got := info.Mode().Perm(); got != 0755 {
		t.Fatalf("directory mode = %04o, want unchanged 0755", got)
	}
}

func TestInitRejectsMarkerlessBundleEvenWhenComposeLabelsLookManaged(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"compose.yml": `services:
  specgate:
    labels:
      org.specgate.managed: "true"
      org.specgate.project: specgate
`,
		".env.example":         "SPECGATE_VERSION=latest\n",
		"specgate.env.example": "SETTINGS_ENCRYPTION_KEY=\n",
		"rollback-compatible":  "true\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	svc := New(dir, noopRunner{})
	fetched := false
	svc.fetchBundle = func(context.Context, string, string, string) error {
		fetched = true
		return nil
	}

	err := svc.Init(context.Background(), InitOptions{Seed: SeedNo, BundleVersion: "v0.2.0"})
	if err == nil || !strings.Contains(err.Error(), "not an empty or managed SpecGate deployment") {
		t.Fatalf("Init error = %v, want markerless-directory refusal", err)
	}
	if fetched {
		t.Fatal("bundle fetched before exact deployment ownership was established")
	}
	if _, err := os.Stat(filepath.Join(dir, managedMarkerName)); !os.IsNotExist(err) {
		t.Fatalf("markerless directory was adopted: %v", err)
	}
}

func TestInitAdoptsExactStagedDevBundle(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"compose.yml":          "services:\n  specgate:\n    image: ghcr.io/thanhtung2693/specgate:${SPECGATE_VERSION}\n",
		".env.example":         "SPECGATE_VERSION=latest\n",
		"specgate.env.example": "SETTINGS_ENCRYPTION_KEY=\n",
		"rollback-compatible":  "true\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if err := New(dir, noopRunner{}).Init(context.Background(), InitOptions{
		Seed: SeedNo, BundleVersion: "dev",
	}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := ValidateManagedDirectory(dir); err != nil {
		t.Fatalf("staged dev bundle was not marked as managed: %v", err)
	}
}

// TestInitErrorsWhenNoBundleAndDevVersion verifies a dev build with no staged
// bundle fails with guidance instead of silently starting nothing.
func TestInitErrorsWhenNoBundleAndDevVersion(t *testing.T) {
	dir := t.TempDir()
	svc := New(dir, noopRunner{})
	svc.fetchBundle = func(context.Context, string, string, string) error {
		t.Fatal("fetchBundle must not be called for a dev build")
		return nil
	}
	err := svc.Init(context.Background(), InitOptions{Seed: SeedNo, BundleVersion: "dev"})
	if err == nil || !strings.Contains(err.Error(), "no compose bundle") {
		t.Fatalf("expected a no-bundle error, got %v", err)
	}
}

// TestInitSkipsDownloadWhenBundlePresent verifies an existing compose.yml is
// left in place and no download is attempted.
func TestInitSkipsDownloadWhenBundlePresent(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "compose.yml"), []byte("# already here\n"), 0644)
	if err := MarkManagedDirectory(dir); err != nil {
		t.Fatal(err)
	}
	svc := New(dir, noopRunner{})
	svc.fetchBundle = func(context.Context, string, string, string) error {
		t.Fatal("fetchBundle must not be called when compose.yml exists")
		return nil
	}
	if err := svc.Init(context.Background(), InitOptions{Seed: SeedNo, BundleVersion: "v9.9.0-rc.1"}); err != nil {
		t.Fatalf("Init: %v", err)
	}
}

func TestUpdateRestoresPreviousBundleWhenTargetReadinessFails(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"compose.yml":  "# old bundle\n",
		".env":         "SPECGATE_VERSION=v1.0.0\nSPECGATE_PORT=3000\n",
		"specgate.env": "SETTINGS_ENCRYPTION_KEY=secret\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := MarkManagedDirectory(dir); err != nil {
		t.Fatal(err)
	}
	runner := &updateRunner{targetUpFails: true}
	svc := New(dir, runner)
	svc.fetchBundle = func(_ context.Context, _, _, destDir string) error {
		if err := os.WriteFile(filepath.Join(destDir, "compose.yml"), []byte("# target bundle\n"), 0644); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(destDir, ".env.example"), []byte("SPECGATE_VERSION=latest\n"), 0644); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(destDir, "specgate.env.example"), []byte("SETTINGS_ENCRYPTION_KEY=\n"), 0644); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(destDir, "rollback-compatible"), []byte("true\n"), 0644)
	}

	err := svc.Update(context.Background(), UpdateOptions{BundleVersion: "v2.0.0", BundleBaseURL: "https://example.invalid"})
	if err == nil || !strings.Contains(err.Error(), "restored previous appliance") {
		t.Fatalf("Update error = %v, want successful rollback message", err)
	}
	compose, err := os.ReadFile(filepath.Join(dir, "compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(compose) != "# old bundle\n" {
		t.Fatalf("compose after rollback = %q", compose)
	}
	upCount := 0
	for _, command := range runner.commands {
		if strings.HasSuffix(command, " up -d --wait") {
			upCount++
		}
	}
	if upCount != 2 {
		t.Fatalf("up --wait count = %d, want target plus rollback; commands=%#v", upCount, runner.commands)
	}
}

func TestUpdateDoesNotRestartOldBinaryWhenTargetIsNotRollbackCompatible(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"compose.yml":  "# old bundle\n",
		".env":         "SPECGATE_VERSION=v1.0.0\nSPECGATE_PORT=3000\n",
		"specgate.env": "SETTINGS_ENCRYPTION_KEY=secret\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := MarkManagedDirectory(dir); err != nil {
		t.Fatal(err)
	}
	runner := &updateRunner{targetUpFails: true}
	svc := New(dir, runner)
	svc.fetchBundle = func(_ context.Context, _, _, destDir string) error {
		if err := os.WriteFile(filepath.Join(destDir, "compose.yml"), []byte("# target bundle\n"), 0644); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(destDir, ".env.example"), []byte("SPECGATE_VERSION=latest\n"), 0644); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(destDir, "specgate.env.example"), []byte("SETTINGS_ENCRYPTION_KEY=\n"), 0644); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(destDir, "rollback-compatible"), []byte("false\n"), 0644)
	}

	err := svc.Update(context.Background(), UpdateOptions{BundleVersion: "v2.0.0", BundleBaseURL: "https://example.invalid"})
	if err == nil || !strings.Contains(err.Error(), "non-rollbackable") || !strings.Contains(err.Error(), "backups") {
		t.Fatalf("Update error = %v, want recovery guidance", err)
	}
	compose, readErr := os.ReadFile(filepath.Join(dir, "compose.yml"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(compose) != "# target bundle\n" {
		t.Fatalf("old binary was restored against possibly migrated data: %q", compose)
	}
	upCount := 0
	for _, command := range runner.commands {
		if strings.HasSuffix(command, " up -d --wait") {
			upCount++
		}
	}
	if upCount != 1 {
		t.Fatalf("up --wait count = %d, want target only; commands=%#v", upCount, runner.commands)
	}
}

func TestUpdateRejectsIncompleteBundleBeforeStoppingCurrentAppliance(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"compose.yml":  "# old bundle\n",
		".env":         "SPECGATE_VERSION=v1.0.0\n",
		"specgate.env": "SETTINGS_ENCRYPTION_KEY=secret\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := MarkManagedDirectory(dir); err != nil {
		t.Fatal(err)
	}
	runner := &updateRunner{}
	svc := New(dir, runner)
	svc.fetchBundle = func(_ context.Context, _, _, destDir string) error {
		return os.WriteFile(filepath.Join(destDir, "compose.yml"), []byte("# incomplete\n"), 0644)
	}

	err := svc.Update(context.Background(), UpdateOptions{BundleVersion: "v2.0.0", BundleBaseURL: "https://example.invalid"})
	if err == nil || !strings.Contains(err.Error(), "validate compose bundle") {
		t.Fatalf("Update error = %v, want bundle validation error", err)
	}
	for _, command := range runner.commands {
		if strings.HasSuffix(command, " down") {
			t.Fatalf("current appliance stopped before target bundle was validated: %#v", runner.commands)
		}
	}
}

func TestUpdateRestoresFilesBeforeComposeWhenBundleInstallFails(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"compose.yml":  "# old bundle\n",
		".env":         "SPECGATE_VERSION=v1.0.0\n",
		"specgate.env": "SETTINGS_ENCRYPTION_KEY=secret\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := MarkManagedDirectory(dir); err != nil {
		t.Fatal(err)
	}
	runner := &updateRunner{}
	svc := New(dir, runner)
	svc.fetchBundle = func(_ context.Context, _, _, destDir string) error {
		for name, body := range map[string]string{
			"compose.yml":          "# target bundle\n",
			".env.example":         "SPECGATE_VERSION=latest\n",
			"specgate.env.example": "SETTINGS_ENCRYPTION_KEY=\n",
			"rollback-compatible":  "false\n",
		} {
			if err := os.WriteFile(filepath.Join(destDir, name), []byte(body), 0644); err != nil {
				return err
			}
		}
		return os.Mkdir(filepath.Join(dir, ".env.example"), 0700)
	}

	err := svc.Update(context.Background(), UpdateOptions{BundleVersion: "v2.0.0", BundleBaseURL: "https://example.invalid"})
	if err == nil || !strings.Contains(err.Error(), "restored previous appliance") {
		t.Fatalf("Update error = %v, want restored previous appliance", err)
	}
	compose, readErr := os.ReadFile(filepath.Join(dir, "compose.yml"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(compose) != "# old bundle\n" {
		t.Fatalf("compose after install rollback = %q", compose)
	}
	downCount := 0
	for _, command := range runner.commands {
		if strings.HasSuffix(command, " down") {
			downCount++
		}
	}
	if downCount != 1 {
		t.Fatalf("down count = %d, want only pre-install shutdown; commands=%#v", downCount, runner.commands)
	}
}
