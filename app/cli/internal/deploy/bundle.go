package deploy

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const (
	maxBundleDownloadBytes   = 16 << 20
	maxChecksumDownloadBytes = 1 << 20
	maxBundleExtractedBytes  = 64 << 20
)

var releaseBundleFiles = []string{
	"compose.yml",
	".env.example",
	"specgate.env.example",
	"rollback-compatible",
}

// DownloadBundle downloads specgate-compose_<version>.tar.gz from baseURL,
// verifies the SHA-256 checksum, and extracts it to destDir.
func DownloadBundle(ctx context.Context, baseURL, version, destDir string) error {
	tarName := fmt.Sprintf("specgate-compose_%s.tar.gz", version)
	sumsName := fmt.Sprintf("specgate-compose_%s_checksums.txt", version)

	sumsData, err := httpGet(ctx, baseURL+"/"+sumsName, maxChecksumDownloadBytes)
	if err != nil {
		return fmt.Errorf("download checksums: %w", err)
	}
	tarData, err := httpGet(ctx, baseURL+"/"+tarName, maxBundleDownloadBytes)
	if err != nil {
		return fmt.Errorf("download bundle: %w", err)
	}
	if err := verifySHA256(tarData, tarName, sumsData); err != nil {
		return err
	}
	return extractTarGZ(tarData, destDir)
}

func httpGet(ctx context.Context, url string, maxBytes int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("response from %s exceeds %d bytes", url, maxBytes)
	}
	return body, nil
}

func verifySHA256(data []byte, filename string, sumsData []byte) error {
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	for _, line := range strings.Split(string(sumsData), "\n") {
		parts := strings.Fields(line)
		if len(parts) == 2 && parts[1] == filename {
			if parts[0] != got {
				return fmt.Errorf("checksum mismatch for %s: got %s, want %s", filename, got, parts[0])
			}
			return nil
		}
	}
	return fmt.Errorf("checksum for %q not found in checksums file", filename)
}

// extractTarGZ extracts a .tar.gz archive to destDir, rejecting absolute paths
// and path traversal sequences.
func extractTarGZ(data []byte, destDir string) error {
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	defer gr.Close()
	tr := tar.NewReader(gr)
	allowed := make(map[string]bool, len(releaseBundleFiles))
	for _, name := range releaseBundleFiles {
		allowed[name] = true
	}
	seen := make(map[string]bool, len(releaseBundleFiles))
	var extracted int64
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar: %w", err)
		}
		name, err := validateTarPath(hdr.Name)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if name != "." {
				return fmt.Errorf("unexpected directory in appliance bundle: %q", hdr.Name)
			}
			continue
		case tar.TypeReg:
			if !allowed[name] {
				return fmt.Errorf("unexpected file in appliance bundle: %q", hdr.Name)
			}
			if seen[name] {
				return fmt.Errorf("duplicate file in appliance bundle: %q", name)
			}
			if hdr.Size < 0 || hdr.Size > maxBundleExtractedBytes-extracted {
				return fmt.Errorf("appliance bundle exceeds %d extracted bytes", maxBundleExtractedBytes)
			}
			extracted += hdr.Size
			target := filepath.Join(destDir, filepath.FromSlash(name))
			f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				_ = f.Close()
				_ = os.Remove(target)
				return err
			}
			if err := f.Close(); err != nil {
				_ = os.Remove(target)
				return err
			}
			seen[name] = true
		default:
			return fmt.Errorf("unsupported entry type in appliance bundle: %q", hdr.Name)
		}
	}
	for _, name := range releaseBundleFiles {
		if !seen[name] {
			return fmt.Errorf("appliance bundle missing %s", name)
		}
	}
	return nil
}

func validateTarPath(name string) (string, error) {
	if name == "" || strings.ContainsRune(name, 0) || strings.ContainsRune(name, '\\') || filepath.IsAbs(name) {
		return "", fmt.Errorf("unsafe archive entry: %q", name)
	}
	clean := path.Clean(name)
	if clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return "", fmt.Errorf("unsafe archive entry: %q", name)
	}
	return clean, nil
}
