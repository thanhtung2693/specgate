package deploy

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type bundleEntry struct {
	name     string
	body     string
	typeflag byte
}

func testBundle(t *testing.T, entries []bundleEntry) []byte {
	t.Helper()
	var body bytes.Buffer
	gz := gzip.NewWriter(&body)
	tw := tar.NewWriter(gz)
	for _, entry := range entries {
		typeflag := entry.typeflag
		if typeflag == 0 {
			typeflag = tar.TypeReg
		}
		size := int64(len(entry.body))
		if typeflag != tar.TypeReg {
			size = 0
		}
		if err := tw.WriteHeader(&tar.Header{Name: entry.name, Mode: 0o644, Size: size, Typeflag: typeflag}); err != nil {
			t.Fatal(err)
		}
		if size > 0 {
			if _, err := io.WriteString(tw, entry.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return body.Bytes()
}

func validBundleEntries() []bundleEntry {
	return []bundleEntry{
		{name: "./", typeflag: tar.TypeDir},
		{name: "./compose.yml", body: "services: {}\n"},
		{name: "./.env.example", body: "SPECGATE_VERSION=latest\n"},
		{name: "./specgate.env.example", body: "SETTINGS_ENCRYPTION_KEY=\n"},
		{name: "./rollback-compatible", body: "true\n"},
	}
}

func TestExtractTarGZAcceptsExactReleaseManifest(t *testing.T) {
	dir := t.TempDir()
	if err := extractTarGZ(testBundle(t, validBundleEntries()), dir); err != nil {
		t.Fatal(err)
	}
	for _, name := range releaseBundleFiles {
		if info, err := os.Stat(filepath.Join(dir, name)); err != nil || !info.Mode().IsRegular() {
			t.Fatalf("%s missing or invalid: %v", name, err)
		}
	}
}

func TestExtractTarGZRejectsUnexpectedAndUnsafeEntries(t *testing.T) {
	tests := []bundleEntry{
		{name: "../outside", body: "bad"},
		{name: "extra.txt", body: "bad"},
		{name: "compose.yml", typeflag: tar.TypeSymlink},
	}
	for _, entry := range tests {
		t.Run(entry.name, func(t *testing.T) {
			if err := extractTarGZ(testBundle(t, []bundleEntry{entry}), t.TempDir()); err == nil {
				t.Fatal("unsafe bundle entry was accepted")
			}
		})
	}
}

func TestExtractTarGZRejectsDuplicateAndMissingFiles(t *testing.T) {
	duplicate := append(validBundleEntries(), bundleEntry{name: "compose.yml", body: "other"})
	if err := extractTarGZ(testBundle(t, duplicate), t.TempDir()); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate error = %v", err)
	}
	missing := validBundleEntries()[1:]
	missing = missing[:len(missing)-1]
	if err := extractTarGZ(testBundle(t, missing), t.TempDir()); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("missing-file error = %v", err)
	}
}

func TestExtractTarGZRefusesPreexistingTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "compose.yml")
	if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := extractTarGZ(testBundle(t, validBundleEntries()), dir); err == nil {
		t.Fatal("preexisting target was overwritten")
	}
	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "keep" {
		t.Fatalf("preexisting target changed: %q", body)
	}
}

func TestHTTPGetRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(bytes.Repeat([]byte("x"), 33))
	}))
	t.Cleanup(server.Close)

	if _, err := httpGet(context.Background(), server.URL, 32); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized response error = %v", err)
	}
}
