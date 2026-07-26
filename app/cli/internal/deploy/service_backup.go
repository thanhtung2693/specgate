package deploy

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// SeedChoice controls demo-data seeding behaviour during Init.
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
