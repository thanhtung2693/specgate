package deploy

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// SeedChoice controls demo-data seeding behaviour during Init.
type SeedChoice int

const (
	SeedAsk SeedChoice = iota // prompt the user
	SeedNo                    // skip seeding
	SeedYes                   // seed after startup
)

// InitOptions carries parameters for the Init operation.
type InitOptions struct {
	Seed SeedChoice
	// BundleVersion is the release version whose compose bundle to download when
	// the deployment dir has no compose.yml yet (e.g. "v1.2.3"). Defaults to the
	// CLI's own build version at the command layer.
	BundleVersion string
	// BundleBaseURL overrides where the bundle is fetched from; empty uses the
	// GitHub release download URL for the configured version.
	BundleBaseURL string
}

// UpdateOptions carries parameters for refreshing an existing deployment.
type UpdateOptions struct {
	// BundleVersion is the release version to pin in .env and whose compose
	// bundle should be downloaded.
	BundleVersion string
	// BundleBaseURL overrides where the bundle is fetched from; empty uses the
	// GitHub release download URL for the configured version.
	BundleBaseURL string
}

// SeedDemoOptions controls the optional attribution attached to demo work items.
type SeedDemoOptions struct {
	WorkspaceID string
	CreatedBy   string
}

// ServiceStatus is the runtime status of a single compose service.
type ServiceStatus struct {
	Name   string `json:"Name"`
	Status string `json:"Status"`
}

const (
	managedLabelFilter = "label=org.specgate.managed=true"
	projectEnvKey      = "SPECGATE_COMPOSE_PROJECT"
	defaultProjectName = "specgate"
	managedMarkerName  = ".specgate-managed"
	managedMarkerValue = "specgate-deployment-v1\n"
)

// bundleFetcher downloads + extracts the compose bundle into destDir.
type bundleFetcher func(ctx context.Context, baseURL, version, destDir string) error

// Service orchestrates local SpecGate deployment operations.
type Service struct {
	dir         string
	runner      CommandRunner
	fetchBundle bundleFetcher
}

// New creates a Service rooted at dir using the given runner.
func New(dir string, runner CommandRunner) *Service {
	return &Service{dir: dir, runner: runner, fetchBundle: DownloadBundle}
}

// Init prepares the deployment directory and starts the compose stack. When the
// dir has no compose.yml, it downloads + verifies the compose bundle for
// opts.BundleVersion first, then sets up env files and starts the stack.
func (s *Service) Init(ctx context.Context, opts InitOptions) error {
	if err := validateInitTarget(s.dir, opts.BundleVersion == "dev"); err != nil {
		return err
	}

	if err := checkDocker(ctx, s.runner); err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return fmt.Errorf("create deployment dir: %w", err)
	}
	if err := os.Chmod(s.dir, 0700); err != nil {
		return fmt.Errorf("secure deployment dir: %w", err)
	}

	if err := s.ensureBundle(ctx, opts); err != nil {
		return err
	}
	if err := MarkManagedDirectory(s.dir); err != nil {
		return fmt.Errorf("mark deployment directory: %w", err)
	}

	if err := s.setupComposeEnv(); err != nil {
		return fmt.Errorf("compose env setup: %w", err)
	}
	if err := s.persistRuntimeOverrides(); err != nil {
		return fmt.Errorf("persist runtime overrides: %w", err)
	}
	if err := s.setupEnv(); err != nil {
		return fmt.Errorf("env setup: %w", err)
	}

	if err := s.writeVersionEnv(opts.BundleVersion); err != nil {
		return fmt.Errorf("write .env: %w", err)
	}
	if err := s.ensureAppBaseURLEnv(); err != nil {
		return fmt.Errorf("write APP_BASE_URL: %w", err)
	}

	if opts.BundleVersion != "" && opts.BundleVersion != "dev" {
		if err := s.Pull(ctx); err != nil {
			return err
		}
	}
	if err := s.Up(ctx); err != nil {
		return err
	}

	if opts.Seed == SeedYes {
		if err := s.SeedDemo(ctx, SeedDemoOptions{}); err != nil {
			return err
		}
	}

	return nil
}

// Update refreshes an appliance deployment without ever starting the old and
// new appliance against the data volume at the same time.
func (s *Service) Update(ctx context.Context, opts UpdateOptions) error {
	if opts.BundleVersion == "" || opts.BundleVersion == "dev" {
		return fmt.Errorf("published bundle version required")
	}
	if err := ValidateManagedDirectory(s.dir); err != nil {
		return err
	}
	if _, err := os.Stat(s.composePath()); err != nil {
		return fmt.Errorf("deployment bundle not found in %s: %w", s.dir, err)
	}
	if err := checkDocker(ctx, s.runner); err != nil {
		return err
	}
	if err := s.setupComposeEnv(); err != nil {
		return fmt.Errorf("compose env setup: %w", err)
	}
	if err := s.persistRuntimeOverrides(); err != nil {
		return fmt.Errorf("persist runtime overrides: %w", err)
	}
	if err := s.ensureAppBaseURLEnv(); err != nil {
		return fmt.Errorf("write APP_BASE_URL: %w", err)
	}
	baseURL := opts.BundleBaseURL
	if baseURL == "" {
		baseURL = fmt.Sprintf("https://github.com/thanhtung2693/specgate/releases/download/%s", opts.BundleVersion)
	}
	stageDir, err := os.MkdirTemp("", "specgate-update-*")
	if err != nil {
		return fmt.Errorf("create update staging dir: %w", err)
	}
	defer os.RemoveAll(stageDir)
	previousDir, err := os.MkdirTemp("", "specgate-rollback-*")
	if err != nil {
		return fmt.Errorf("create rollback staging dir: %w", err)
	}
	defer os.RemoveAll(previousDir)
	if err := copyDeploymentFiles(s.dir, previousDir); err != nil {
		return fmt.Errorf("stage rollback bundle: %w", err)
	}
	if err := s.fetchBundle(ctx, baseURL, opts.BundleVersion, stageDir); err != nil {
		return fmt.Errorf("download compose bundle %s: %w", opts.BundleVersion, err)
	}
	rollbackCompatible, err := validateUpdateBundle(stageDir)
	if err != nil {
		return fmt.Errorf("validate compose bundle %s: %w", opts.BundleVersion, err)
	}
	if err := MarkManagedDirectory(stageDir); err != nil {
		return fmt.Errorf("mark verified update bundle: %w", err)
	}
	for _, name := range []string{".env", "specgate.env"} {
		if err := copyExistingFile(filepath.Join(s.dir, name), filepath.Join(stageDir, name)); err != nil {
			return fmt.Errorf("stage %s: %w", name, err)
		}
	}
	staged := New(stageDir, s.runner)
	if err := staged.setupEnv(); err != nil {
		return fmt.Errorf("env setup: %w", err)
	}
	if err := staged.setVersionEnv(opts.BundleVersion); err != nil {
		return fmt.Errorf("write .env: %w", err)
	}
	if err := staged.ensureAppBaseURLEnv(); err != nil {
		return fmt.Errorf("write APP_BASE_URL: %w", err)
	}
	if err := staged.Pull(ctx); err != nil {
		return err
	}
	if err := s.runner.Run(ctx, "docker", "compose", "-f", s.composePath(), "up", "-d"); err != nil {
		return fmt.Errorf("start current appliance for backup: %w", err)
	}
	if err := s.waitForSupervisor(ctx); err != nil {
		return fmt.Errorf("wait for current appliance supervisor: %w", err)
	}
	backupPath, err := s.Backup(ctx, opts.BundleVersion)
	if err != nil {
		return err
	}
	if err := s.Down(ctx); err != nil {
		return err
	}
	if err := installBundleFiles(stageDir, s.dir); err != nil {
		return s.restoreUnstartedPreviousAppliance(ctx, previousDir, fmt.Errorf("install bundle: %w", err))
	}
	if err := copyExistingFile(filepath.Join(stageDir, ".env"), filepath.Join(s.dir, ".env")); err != nil {
		return s.restoreUnstartedPreviousAppliance(ctx, previousDir, fmt.Errorf("install .env: %w", err))
	}
	if err := s.Up(ctx); err != nil {
		return s.handleTargetFailure(ctx, previousDir, backupPath, rollbackCompatible, fmt.Errorf("target readiness: %w", err))
	}
	for _, endpoint := range []string{
		"http://127.0.0.1:3000/api/doc-registry/api/v1/meta",
		"http://127.0.0.1:3000/api/agents/openapi.json",
	} {
		if err := s.runner.Run(ctx, "docker", "compose", "-f", s.composePath(), "exec", "-T", "specgate", "curl", "--fail", "--silent", "--show-error", "--max-time", "5", endpoint); err != nil {
			return s.handleTargetFailure(ctx, previousDir, backupPath, rollbackCompatible, fmt.Errorf("target smoke check %s: %w", endpoint, err))
		}
	}
	return nil
}

func (s *Service) waitForSupervisor(ctx context.Context) error {
	return s.runner.Run(ctx, "docker", "compose", "-f", s.composePath(), "exec", "-T", "specgate", "sh", "-c",
		`i=0; while [ "$i" -lt 60 ]; do [ -d /run/service/agents ] && [ -d /run/service/doc-registry ] && exit 0; i=$((i+1)); sleep 1; done; exit 1`)
}

func (s *Service) handleTargetFailure(ctx context.Context, previousDir, backupPath string, rollbackCompatible bool, targetErr error) error {
	if rollbackCompatible {
		return s.restorePreviousAppliance(ctx, previousDir, targetErr)
	}
	return fmt.Errorf("update failed (%v); target is non-rollbackable, so the old binary was not restarted; recovery archive: %s", targetErr, backupPath)
}

func (s *Service) restorePreviousAppliance(ctx context.Context, previousDir string, targetErr error) error {
	if err := s.Down(ctx); err != nil {
		return fmt.Errorf("update failed (%v); stop target for rollback: %w", targetErr, err)
	}
	return s.restoreUnstartedPreviousAppliance(ctx, previousDir, targetErr)
}

func (s *Service) restoreUnstartedPreviousAppliance(ctx context.Context, previousDir string, targetErr error) error {
	if err := copyDeploymentFiles(previousDir, s.dir); err != nil {
		return fmt.Errorf("update failed (%v); restore previous deployment files: %w", targetErr, err)
	}
	if err := s.Up(ctx); err != nil {
		return fmt.Errorf("update failed (%v); previous appliance also failed to restart: %w", targetErr, err)
	}
	return fmt.Errorf("update failed (%v); restored previous appliance", targetErr)
}

// Backup writes a logical database + registry archive outside the appliance
// data volume before an update changes the running image.
func (s *Service) Backup(ctx context.Context, targetVersion string) (string, error) {
	if err := ValidateManagedDirectory(s.dir); err != nil {
		return "", err
	}
	backupDir := filepath.Join(s.dir, "backups")
	if err := ensurePrivateDirectory(backupDir); err != nil {
		return "", fmt.Errorf("create backup dir: %w", err)
	}
	versionLabel := strings.NewReplacer("/", "-", `\`, "-", ":", "-").Replace(strings.TrimPrefix(targetVersion, "v"))
	name := fmt.Sprintf("specgate-before-%s-%s.tar.gz",
		versionLabel, time.Now().UTC().Format("20060102T150405Z"))
	path := filepath.Join(backupDir, name)
	workDir, err := os.MkdirTemp(backupDir, ".backup-*")
	if err != nil {
		return "", fmt.Errorf("create backup work dir: %w", err)
	}
	defer os.RemoveAll(workDir)
	payloadPath := filepath.Join(workDir, "data.tar.gz")
	args := []string{"compose", "-f", s.composePath(), "exec", "-T", "specgate", "/usr/local/bin/specgate-backup"}
	if err := s.runner.OutputToFile(ctx, payloadPath, "docker", args...); err != nil {
		return "", fmt.Errorf("backup before %s: %w", targetVersion, err)
	}
	if err := validateBackupPayload(payloadPath); err != nil {
		return "", fmt.Errorf("validate backup before %s: %w", targetVersion, err)
	}
	currentVersion, _ := envFileValue(filepath.Join(s.dir, ".env"), "SPECGATE_VERSION")
	metadata, err := json.Marshal(map[string]string{
		"current_version": currentVersion,
		"target_version":  targetVersion,
		"image":           "ghcr.io/thanhtung2693/specgate:" + currentVersion,
	})
	if err != nil {
		return "", fmt.Errorf("encode recovery metadata: %w", err)
	}
	files := map[string]string{
		"data.tar.gz":             payloadPath,
		"deployment/compose.yml":  s.composePath(),
		"deployment/.env":         filepath.Join(s.dir, ".env"),
		"deployment/specgate.env": filepath.Join(s.dir, "specgate.env"),
	}
	if err := writeRecoveryArchive(path, files, metadata); err != nil {
		return "", fmt.Errorf("write recovery archive: %w", err)
	}
	return path, nil
}

// MarkManagedDirectory records that dir is owned by the SpecGate appliance
// lifecycle. Destructive commands require this marker before touching dir.
func MarkManagedDirectory(dir string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, managedMarkerName), []byte(managedMarkerValue), 0600)
}

// ValidateManagedDirectory rejects arbitrary directories that were not created
// by the SpecGate appliance lifecycle.
func ValidateManagedDirectory(dir string) error {
	if err := rejectSymlinkedManagedDirectoryAncestors(dir); err != nil {
		return err
	}
	dirInfo, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("%s is not a managed SpecGate deployment: %w", dir, err)
	}
	if dirInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is not a managed SpecGate deployment: deployment directory is a symlink", dir)
	}
	if !dirInfo.IsDir() {
		return fmt.Errorf("%s is not a managed SpecGate deployment: not a directory", dir)
	}
	info, err := os.Lstat(filepath.Join(dir, managedMarkerName))
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%s is not a managed SpecGate deployment (missing %s)", dir, managedMarkerName)
		}
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a managed SpecGate deployment (invalid %s)", dir, managedMarkerName)
	}
	body, err := os.ReadFile(filepath.Join(dir, managedMarkerName))
	if err != nil {
		return err
	}
	if string(body) != managedMarkerValue {
		return fmt.Errorf("%s is not a managed SpecGate deployment (invalid %s)", dir, managedMarkerName)
	}
	return nil
}

func rejectSymlinkedManagedDirectoryAncestors(dir string) error {
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if runtime.GOOS == "darwin" {
		for _, alias := range []string{"/tmp", "/var"} {
			if absolute == alias || strings.HasPrefix(absolute, alias+"/") {
				absolute = "/private" + absolute
				break
			}
		}
	}
	for current := filepath.Dir(filepath.Clean(absolute)); ; {
		info, err := os.Lstat(current)
		switch {
		case err == nil && info.Mode()&os.ModeSymlink != 0:
			return fmt.Errorf("%s is not a managed SpecGate deployment: deployment path contains symlinked ancestor %s", dir, current)
		case err != nil && !os.IsNotExist(err):
			return err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
		current = parent
	}
}

func ensurePrivateDirectory(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return os.MkdirAll(path, 0o700)
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%s must be a real directory, not a symlink or file", path)
	}
	return os.Chmod(path, 0o700)
}

func validateInitTarget(dir string, allowStagedDevBundle bool) error {
	info, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is not an empty or managed SpecGate deployment directory", dir)
	}
	if ValidateManagedDirectory(dir) == nil {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if allowStagedDevBundle && len(entries) == len(releaseBundleFiles) {
		for _, name := range releaseBundleFiles {
			fileInfo, statErr := os.Lstat(filepath.Join(dir, name))
			if statErr != nil || !fileInfo.Mode().IsRegular() {
				return fmt.Errorf("invalid staged dev bundle file %s", name)
			}
		}
		if _, validateErr := validateUpdateBundle(dir); validateErr != nil {
			return fmt.Errorf("invalid staged dev bundle: %w", validateErr)
		}
		return nil
	}
	if len(entries) != 0 {
		return fmt.Errorf("%s is not an empty or managed SpecGate deployment directory", dir)
	}
	return nil
}

func validateBackupPayload(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	foundDatabase, foundRegistry := false, false
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := strings.TrimPrefix(filepath.ToSlash(header.Name), "./")
		foundDatabase = foundDatabase || name == "docreg.sql"
		foundRegistry = foundRegistry || name == "registry" || strings.HasPrefix(name, "registry/")
	}
	if !foundDatabase || !foundRegistry {
		return fmt.Errorf("payload missing docreg.sql or registry archive")
	}
	return nil
}

func writeRecoveryArchive(path string, files map[string]string, metadata []byte) (err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	complete := false
	defer func() {
		if !complete {
			_ = os.Remove(path)
		}
	}()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, name := range []string{"data.tar.gz", "deployment/compose.yml", "deployment/.env", "deployment/specgate.env"} {
		if err := addRecoveryFile(tw, name, files[name]); err != nil {
			_ = tw.Close()
			_ = gz.Close()
			_ = f.Close()
			return err
		}
	}
	if err := tw.WriteHeader(&tar.Header{Name: "recovery.json", Mode: 0600, Size: int64(len(metadata))}); err != nil {
		return err
	}
	if _, err := tw.Write(metadata); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	complete = true
	return nil
}

func addRecoveryFile(tw *tar.Writer, archiveName, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if err := tw.WriteHeader(&tar.Header{Name: archiveName, Mode: 0600, Size: info.Size()}); err != nil {
		return err
	}
	_, err = io.Copy(tw, f)
	return err
}

func copyExistingFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, info.Mode().Perm())
}

func installBundleFiles(srcDir, dstDir string) error {
	for _, name := range releaseBundleFiles {
		if err := copyRequiredFile(filepath.Join(srcDir, name), filepath.Join(dstDir, name)); err != nil {
			return err
		}
	}
	return nil
}

func copyDeploymentFiles(srcDir, dstDir string) error {
	for _, name := range []string{"compose.yml", ".env", "specgate.env", ".env.example", "specgate.env.example", "rollback-compatible"} {
		if err := copyExistingFile(filepath.Join(srcDir, name), filepath.Join(dstDir, name)); err != nil {
			return err
		}
	}
	return nil
}

func copyRequiredFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, info.Mode().Perm())
}

func validateUpdateBundle(dir string) (bool, error) {
	for _, name := range releaseBundleFiles {
		if info, err := os.Stat(filepath.Join(dir, name)); err != nil || !info.Mode().IsRegular() {
			if err == nil {
				err = fmt.Errorf("not a regular file")
			}
			return false, fmt.Errorf("%s: %w", name, err)
		}
	}
	value, err := os.ReadFile(filepath.Join(dir, "rollback-compatible"))
	if err != nil {
		return false, err
	}
	switch strings.TrimSpace(string(value)) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("rollback-compatible must be true or false")
	}
}

// SeedDemo creates or refreshes the bundled demo governance data in the running
// stack. Optional attribution lets interactive init place seeded work in the
// selected workspace immediately.
func (s *Service) SeedDemo(ctx context.Context, opts SeedDemoOptions) error {
	if err := ValidateManagedDirectory(s.dir); err != nil {
		return err
	}
	args := []string{"compose", "-f", s.composePath(), "exec", "-T", "--user", "specgate", "specgate", "/usr/local/bin/doc-registry", "--seed-demo"}
	if workspaceID := strings.TrimSpace(opts.WorkspaceID); workspaceID != "" {
		args = append(args, "--seed-demo-workspace-id", workspaceID)
	}
	if createdBy := strings.TrimSpace(opts.CreatedBy); createdBy != "" {
		args = append(args, "--seed-demo-created-by", createdBy)
	}
	if err := s.runner.Run(ctx, "docker", args...); err != nil {
		return fmt.Errorf("seed: %w", err)
	}
	return nil
}

// ensureBundle downloads the compose bundle into the deployment dir when no
// compose.yml is present yet. A bundle already in place (re-run, or a dev who
// staged it manually) is left untouched.
func (s *Service) ensureBundle(ctx context.Context, opts InitOptions) error {
	if _, err := os.Stat(s.composePath()); err == nil {
		return nil
	}
	version := opts.BundleVersion
	if version == "" || version == "dev" {
		return fmt.Errorf(
			"no compose bundle in %s and no published version to download (CLI build is %q); "+
				"install a released specgate or pass --bundle-version", s.dir, version)
	}
	baseURL := opts.BundleBaseURL
	if baseURL == "" {
		baseURL = fmt.Sprintf("https://github.com/thanhtung2693/specgate/releases/download/%s", version)
	}
	stageDir, err := os.MkdirTemp("", "specgate-init-*")
	if err != nil {
		return fmt.Errorf("create bundle staging dir: %w", err)
	}
	defer os.RemoveAll(stageDir)
	if err := s.fetchBundle(ctx, baseURL, version, stageDir); err != nil {
		return fmt.Errorf("download compose bundle %s: %w", version, err)
	}
	if _, err := validateUpdateBundle(stageDir); err != nil {
		return fmt.Errorf("validate compose bundle %s: %w", version, err)
	}
	if err := installBundleFiles(stageDir, s.dir); err != nil {
		return fmt.Errorf("install compose bundle %s: %w", version, err)
	}
	return nil
}

// writeVersionEnv pins SPECGATE_VERSION in the compose .env so the bundle's
// image tags (${SPECGATE_VERSION}) resolve. An explicit init version overrides
// a template/default value.
func (s *Service) writeVersionEnv(version string) error {
	return s.setVersionEnv(version)
}

func (s *Service) setVersionEnv(version string) error {
	if version == "" {
		return nil
	}
	envPath := filepath.Join(s.dir, ".env")
	existing, err := os.ReadFile(envPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	lines := strings.Split(strings.TrimRight(string(existing), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		lines = nil
	}
	updated := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "SPECGATE_VERSION=") {
			lines[i] = "SPECGATE_VERSION=" + version
			updated = true
		}
	}
	if !updated {
		lines = append(lines, "SPECGATE_VERSION="+version)
	}
	data := []byte(strings.Join(lines, "\n") + "\n")
	return os.WriteFile(envPath, data, 0644)
}

func (s *Service) ensureAppBaseURLEnv() error {
	envPath := filepath.Join(s.dir, ".env")
	lines, err := readEnvLines(envPath)
	if os.IsNotExist(err) {
		lines = nil
	} else if err != nil {
		return err
	}
	value := envLinesValue(lines, "APP_BASE_URL")
	if value != "" && !isGeneratedLocalAppURL(value) {
		return nil
	}
	port := "3000"
	if value := envLinesValue(lines, "SPECGATE_PORT"); strings.TrimSpace(value) != "" {
		port = value
	}
	return setEnvVar(envPath, lines, "APP_BASE_URL", "http://localhost:"+port)
}

func isGeneratedLocalAppURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "http" && parsed.Hostname() == "localhost"
}

func envLinesValue(lines []string, key string) string {
	prefix := key + "="
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if after, ok := strings.CutPrefix(line, prefix); ok {
			return cleanEnvValue(after)
		}
	}
	return ""
}

// Up starts the compose stack in detached mode and waits until healthy.
func (s *Service) Up(ctx context.Context) error {
	if err := ValidateManagedDirectory(s.dir); err != nil {
		return err
	}
	if err := s.ensureAppBaseURLEnv(); err != nil {
		return fmt.Errorf("write APP_BASE_URL: %w", err)
	}
	return s.runner.Run(ctx, "docker", "compose",
		"-f", s.composePath(), "up", "-d", "--wait")
}

// Pull refreshes the images referenced by the compose bundle.
func (s *Service) Pull(ctx context.Context) error {
	if err := ValidateManagedDirectory(s.dir); err != nil {
		return err
	}
	return s.runner.Run(ctx, "docker", "compose",
		"-f", s.composePath(), "pull")
}

// Down stops and removes the compose stack containers.
func (s *Service) Down(ctx context.Context) error {
	if err := ValidateManagedDirectory(s.dir); err != nil {
		return err
	}
	return s.runner.Run(ctx, "docker", "compose",
		"-f", s.composePath(), "down")
}

// DownWithVolumes stops the stack and removes compose-managed volumes.
func (s *Service) DownWithVolumes(ctx context.Context) error {
	if err := ValidateManagedDirectory(s.dir); err != nil {
		return err
	}
	return s.runner.Run(ctx, "docker", "compose",
		"-f", s.composePath(), "down", "-v")
}

// RemoveLabeledResources removes leftover Docker resources for this SpecGate
// deployment. It uses SpecGate labels plus the deployment's compose project so
// purging one Full appliance does not remove another.
func (s *Service) RemoveLabeledResources(ctx context.Context) error {
	if err := ValidateManagedDirectory(s.dir); err != nil {
		return err
	}
	projectFilter := "label=org.specgate.project=" + s.ProjectName()
	steps := []struct {
		kind string
		args []string
	}{
		{kind: "container", args: []string{"container", "rm", "-f"}},
		{kind: "volume", args: []string{"volume", "rm"}},
		{kind: "network", args: []string{"network", "rm"}},
	}
	for _, step := range steps {
		ids, err := s.labeledResourceIDs(ctx, step.kind, projectFilter)
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			continue
		}
		args := append(append([]string{}, step.args...), ids...)
		if err := s.runner.Run(ctx, "docker", args...); err != nil {
			return err
		}
	}
	return nil
}

// ProjectName returns the compose project used by this deployment.
func (s *Service) ProjectName() string {
	value, err := envFileValue(filepath.Join(s.dir, ".env"), projectEnvKey)
	if err == nil && value != "" {
		return value
	}
	return defaultProjectName
}

func (s *Service) labeledResourceIDs(ctx context.Context, kind string, filters ...string) ([]string, error) {
	args := []string{kind, "ls", "-q"}
	for _, filter := range append([]string{managedLabelFilter}, filters...) {
		args = append(args, "--filter", filter)
	}
	out, err := s.runner.Output(ctx, "docker", args...)
	if err != nil {
		return nil, err
	}
	return parseLines(out), nil
}

func envFileValue(path, key string) (string, error) {
	lines, err := readEnvLines(path)
	if err != nil {
		return "", err
	}
	prefix := key + "="
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if after, ok := strings.CutPrefix(line, prefix); ok {
			return cleanEnvValue(after), nil
		}
	}
	return "", nil
}

func cleanEnvValue(value string) string {
	return strings.Trim(strings.TrimSpace(value), `"'`)
}

// LocalStatus returns the runtime status of each compose service.
func (s *Service) LocalStatus(ctx context.Context) ([]ServiceStatus, error) {
	if err := ValidateManagedDirectory(s.dir); err != nil {
		return nil, err
	}
	out, err := s.runner.Output(ctx, "docker", "compose",
		"-f", s.composePath(), "ps", "--format", "json")
	if err != nil {
		return nil, err
	}
	return parseComposePS(out)
}

func parseLines(data []byte) []string {
	seen := map[string]bool{}
	var lines []string
	for _, line := range strings.Split(string(data), "\n") {
		value := strings.TrimSpace(line)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		lines = append(lines, value)
	}
	return lines
}

func (s *Service) composePath() string {
	return filepath.Join(s.dir, "compose.yml")
}

// setupComposeEnv copies the Compose runtime defaults when absent.
func (s *Service) setupComposeEnv() error {
	return copyIfSrcExists(
		filepath.Join(s.dir, ".env.example"),
		filepath.Join(s.dir, ".env"),
	)
}

// persistRuntimeOverrides makes one-shot runtime selection durable. Compose
// otherwise consumes an exported value only for the current process, leaving
// later update and purge commands to fall back to the default project or port.
func (s *Service) persistRuntimeOverrides() error {
	envPath := filepath.Join(s.dir, ".env")
	for _, key := range []string{"SPECGATE_PORT", projectEnvKey} {
		value := strings.TrimSpace(os.Getenv(key))
		if value == "" {
			continue
		}
		lines, err := readEnvLines(envPath)
		if os.IsNotExist(err) {
			lines = nil
		} else if err != nil {
			return err
		}
		if err := setEnvVar(envPath, lines, key, value); err != nil {
			return err
		}
	}
	return nil
}

// setupEnv copies the appliance environment example when absent, then ensures
// SETTINGS_ENCRYPTION_KEY is non-empty.
func (s *Service) setupEnv() error {
	if err := copyIfSrcExists(
		filepath.Join(s.dir, "specgate.env.example"),
		filepath.Join(s.dir, "specgate.env"),
	); err != nil {
		return err
	}
	envFile := filepath.Join(s.dir, "specgate.env")
	if _, err := os.Stat(envFile); err == nil {
		return ensureEncryptionKey(envFile)
	}
	return nil
}

// parseComposePS parses the output of `docker compose ps --format json`.
// It handles both JSON-array output (older Compose) and JSONL (newer Compose).
func parseComposePS(data []byte) ([]ServiceStatus, error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil, nil
	}
	if trimmed[0] == '[' {
		var items []ServiceStatus
		if err := json.Unmarshal([]byte(trimmed), &items); err != nil {
			return nil, fmt.Errorf("parse compose ps: %w", err)
		}
		return items, nil
	}
	// JSONL: one object per line.
	var items []ServiceStatus
	for _, line := range strings.Split(trimmed, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var item ServiceStatus
		if err := json.Unmarshal([]byte(line), &item); err == nil {
			items = append(items, item)
		}
	}
	return items, nil
}
