package deploy

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	// ComposeProject isolates a newly created alternate deployment from the
	// default appliance's containers, network, and data volume. Existing .env
	// files retain their recorded project so upgrades cannot orphan data.
	ComposeProject string
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
	_, composeEnvErr := os.Lstat(filepath.Join(s.dir, ".env"))
	newComposeEnv := os.IsNotExist(composeEnvErr)
	if composeEnvErr != nil && !newComposeEnv {
		return fmt.Errorf("inspect compose env: %w", composeEnvErr)
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
	if newComposeEnv && strings.TrimSpace(opts.ComposeProject) != "" {
		if err := s.persistComposeProject(opts.ComposeProject); err != nil {
			return fmt.Errorf("persist compose project: %w", err)
		}
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

// ScopedProjectName returns a stable, path-private Compose project for an
// alternate deployment directory.
func ScopedProjectName(dir string) string {
	absolute, err := filepath.Abs(dir)
	if err != nil {
		absolute = filepath.Clean(dir)
	}
	sum := sha256.Sum256([]byte(filepath.Clean(absolute)))
	return fmt.Sprintf("specgate-%x", sum[:6])
}

func (s *Service) persistComposeProject(project string) error {
	envPath := filepath.Join(s.dir, ".env")
	lines, err := readEnvLines(envPath)
	if os.IsNotExist(err) {
		lines = nil
	} else if err != nil {
		return err
	}
	return setEnvVar(envPath, lines, projectEnvKey, strings.TrimSpace(project))
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
