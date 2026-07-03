package deploy_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// readRepoFile reads a file relative to this test file's location in the repo.
func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	_, selfPath, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	abs := filepath.Join(filepath.Dir(selfPath), rel)
	b, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("readRepoFile(%q): %v", rel, err)
	}
	return string(b)
}

func TestReleaseComposeUsesPublishedImages(t *testing.T) {
	raw := readRepoFile(t, "../../../../deploy/compose/compose.yml")
	for _, want := range []string{
		"ghcr.io/thanhtung2693/doc-registry:${SPECGATE_VERSION}",
		"ghcr.io/thanhtung2693/agents:${SPECGATE_VERSION}",
		"ghcr.io/thanhtung2693/ui:${SPECGATE_VERSION}",
		"name: ${SPECGATE_COMPOSE_PROJECT:-specgate}",
		"127.0.0.1:${POSTGRES_PORT:-5432}:5432",
		"${DOC_REGISTRY_PORT:-8080}:8080",
		"${AGENTS_PORT:-2024}:8000",
		"${UI_PORT:-3000}:80",
		"org.specgate.managed: \"true\"",
		"org.specgate.project: ${SPECGATE_COMPOSE_PROJECT:-specgate}",
		"org.specgate.component: network",
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("compose missing %q", want)
		}
	}
	if strings.Contains(raw, "build:") {
		t.Fatal("release compose must not build source images")
	}
}

func TestDocRegistryImagePreparesLocalBlobRoot(t *testing.T) {
	raw := readRepoFile(t, "../../../../docker/Dockerfile.doc-registry")
	for _, want := range []string{
		"mkdir -p /data/blobs",
		"chown -R app:app /data",
		"org.specgate.managed=\"true\"",
		"org.specgate.component=\"doc-registry\"",
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("doc-registry Dockerfile missing %q", want)
		}
	}
}

func TestRuntimeImagesCarrySpecGateLabels(t *testing.T) {
	for _, item := range []struct {
		path      string
		component string
	}{
		{"../../../../docker/Dockerfile.agents", "agents"},
		{"../../../../docker/Dockerfile.ui", "ui"},
	} {
		raw := readRepoFile(t, item.path)
		for _, want := range []string{
			"org.specgate.managed=\"true\"",
			"org.specgate.component=\"" + item.component + "\"",
		} {
			if !strings.Contains(raw, want) {
				t.Fatalf("%s missing %q", item.path, want)
			}
		}
	}
}
