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
	"path/filepath"
	"strings"
)

// DownloadBundle downloads specgate-compose_<version>.tar.gz from baseURL,
// verifies the SHA-256 checksum, and extracts it to destDir.
func DownloadBundle(ctx context.Context, baseURL, version, destDir string) error {
	tarName := fmt.Sprintf("specgate-compose_%s.tar.gz", version)
	sumsName := fmt.Sprintf("specgate-compose_%s_checksums.txt", version)

	sumsData, err := httpGet(ctx, baseURL+"/"+sumsName)
	if err != nil {
		return fmt.Errorf("download checksums: %w", err)
	}
	tarData, err := httpGet(ctx, baseURL+"/"+tarName)
	if err != nil {
		return fmt.Errorf("download bundle: %w", err)
	}
	if err := verifySHA256(tarData, tarName, sumsData); err != nil {
		return err
	}
	return extractTarGZ(tarData, destDir)
}

func httpGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	return io.ReadAll(resp.Body)
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
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar: %w", err)
		}
		if err := validateTarPath(hdr.Name); err != nil {
			return err
		}
		target := filepath.Join(destDir, filepath.Clean(hdr.Name))
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, hdr.FileInfo().Mode())
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			f.Close()
		}
	}
	return nil
}

func validateTarPath(name string) error {
	if filepath.IsAbs(name) {
		return fmt.Errorf("unsafe archive entry (absolute path): %q", name)
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("unsafe archive entry (path traversal): %q", name)
	}
	return nil
}
