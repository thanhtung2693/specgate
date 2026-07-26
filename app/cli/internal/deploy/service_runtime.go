package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// SeedChoice controls demo-data seeding behaviour during Init.
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
