package deploy

import (
	"context"
	"encoding/json"
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

const managedLabelFilter = "label=org.specgate.managed=true"

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
	if err := os.MkdirAll(s.dir, 0755); err != nil {
		return fmt.Errorf("create deployment dir: %w", err)
	}

	if err := checkDocker(ctx, s.runner); err != nil {
		return err
	}

	if err := s.ensureBundle(ctx, opts); err != nil {
		return err
	}

	if err := s.setupEnv(); err != nil {
		return fmt.Errorf("env setup: %w", err)
	}

	if err := s.writeVersionEnv(opts.BundleVersion); err != nil {
		return fmt.Errorf("write .env: %w", err)
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

// Update refreshes an existing release-bundle deployment to opts.BundleVersion.
// It replaces bundle-managed files such as compose.yml and env examples,
// preserves active env files and volumes, then pulls images and restarts.
func (s *Service) Update(ctx context.Context, opts UpdateOptions) error {
	if opts.BundleVersion == "" || opts.BundleVersion == "dev" {
		return fmt.Errorf("published bundle version required")
	}
	if _, err := os.Stat(s.composePath()); err != nil {
		return fmt.Errorf("deployment bundle not found in %s: %w", s.dir, err)
	}
	if err := checkDocker(ctx, s.runner); err != nil {
		return err
	}
	baseURL := opts.BundleBaseURL
	if baseURL == "" {
		baseURL = fmt.Sprintf("https://github.com/thanhtung2693/specgate/releases/download/%s", opts.BundleVersion)
	}
	if err := s.fetchBundle(ctx, baseURL, opts.BundleVersion, s.dir); err != nil {
		return fmt.Errorf("download compose bundle %s: %w", opts.BundleVersion, err)
	}
	if err := s.setupEnv(); err != nil {
		return fmt.Errorf("env setup: %w", err)
	}
	if err := s.setVersionEnv(opts.BundleVersion); err != nil {
		return fmt.Errorf("write .env: %w", err)
	}
	if err := s.Pull(ctx); err != nil {
		return err
	}
	return s.Up(ctx)
}

// SeedDemo creates or refreshes the bundled demo planning data in the running
// stack. Optional attribution lets interactive init place seeded work in the
// selected workspace immediately.
func (s *Service) SeedDemo(ctx context.Context, opts SeedDemoOptions) error {
	args := []string{"compose", "-f", s.composePath(), "run", "--rm", "doc-registry", "--seed-demo"}
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
	if err := s.fetchBundle(ctx, baseURL, version, s.dir); err != nil {
		return fmt.Errorf("download compose bundle %s: %w", version, err)
	}
	return nil
}

// writeVersionEnv pins SPECGATE_VERSION in the compose .env so the bundle's
// image tags (${SPECGATE_VERSION}) resolve. No-op for a dev build or when the
// key is already set.
func (s *Service) writeVersionEnv(version string) error {
	if version == "" || version == "dev" {
		return nil
	}
	envPath := filepath.Join(s.dir, ".env")
	if existing, err := os.ReadFile(envPath); err == nil &&
		strings.Contains(string(existing), "SPECGATE_VERSION=") {
		return nil
	}
	f, err := os.OpenFile(envPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "SPECGATE_VERSION=%s\n", version)
	return err
}

func (s *Service) setVersionEnv(version string) error {
	if version == "" || version == "dev" {
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

// Up starts the compose stack in detached mode and waits until healthy.
func (s *Service) Up(ctx context.Context) error {
	return s.runner.Run(ctx, "docker", "compose",
		"-f", s.composePath(), "up", "-d", "--wait")
}

// Pull refreshes the images referenced by the compose bundle.
func (s *Service) Pull(ctx context.Context) error {
	return s.runner.Run(ctx, "docker", "compose",
		"-f", s.composePath(), "pull")
}

// Down stops and removes the compose stack containers.
func (s *Service) Down(ctx context.Context) error {
	return s.runner.Run(ctx, "docker", "compose",
		"-f", s.composePath(), "down")
}

// DownWithVolumes stops the stack and removes compose-managed volumes.
func (s *Service) DownWithVolumes(ctx context.Context) error {
	return s.runner.Run(ctx, "docker", "compose",
		"-f", s.composePath(), "down", "-v")
}

// Images returns images referenced by the compose bundle.
func (s *Service) Images(ctx context.Context) ([]string, error) {
	out, err := s.runner.Output(ctx, "docker", "compose",
		"-f", s.composePath(), "config", "--images")
	if err != nil {
		return nil, err
	}
	return parseLines(out), nil
}

// RemoveImages removes the provided Docker images.
func (s *Service) RemoveImages(ctx context.Context, images []string) error {
	if len(images) == 0 {
		return nil
	}
	args := append([]string{"image", "rm"}, images...)
	return s.runner.Run(ctx, "docker", args...)
}

// RemoveLabeledResources removes Docker resources marked as managed by
// SpecGate. It is intentionally label-based so cleanup does not rely on compose
// project names or user-chosen deployment directories.
func (s *Service) RemoveLabeledResources(ctx context.Context, includeImages bool) error {
	steps := []struct {
		kind string
		args []string
	}{
		{kind: "container", args: []string{"container", "rm", "-f"}},
		{kind: "volume", args: []string{"volume", "rm"}},
		{kind: "network", args: []string{"network", "rm"}},
	}
	if includeImages {
		steps = append(steps, struct {
			kind string
			args []string
		}{kind: "image", args: []string{"image", "rm"}})
	}
	for _, step := range steps {
		ids, err := s.labeledResourceIDs(ctx, step.kind)
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

func (s *Service) labeledResourceIDs(ctx context.Context, kind string) ([]string, error) {
	out, err := s.runner.Output(ctx, "docker", kind, "ls", "-q", "--filter", managedLabelFilter)
	if err != nil {
		return nil, err
	}
	return parseLines(out), nil
}

// LocalStatus returns the runtime status of each compose service.
func (s *Service) LocalStatus(ctx context.Context) ([]ServiceStatus, error) {
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

// setupEnv copies .example files to active env files when they are absent,
// then ensures SETTINGS_ENCRYPTION_KEY is non-empty in doc-registry.env.
func (s *Service) setupEnv() error {
	pairs := []struct{ src, dst string }{
		{"doc-registry.env.example", "doc-registry.env"},
		{"agents.env.example", "agents.env"},
	}
	for _, p := range pairs {
		if err := copyIfSrcExists(
			filepath.Join(s.dir, p.src),
			filepath.Join(s.dir, p.dst),
		); err != nil {
			return err
		}
	}
	envFile := filepath.Join(s.dir, "doc-registry.env")
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
