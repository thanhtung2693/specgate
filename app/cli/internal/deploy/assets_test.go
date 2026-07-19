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

func TestReleaseDistributionHasNoMultiServiceBundle(t *testing.T) {
	_, selfPath, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	legacy := filepath.Join(filepath.Dir(selfPath), "../../../../deploy/compose/compose.yml")
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("legacy multi-service release bundle still exists: %v", err)
	}
}

func TestLocalBundleUsesOneApplianceService(t *testing.T) {
	raw := readRepoFile(t, "../../../../deploy/local/compose.yml")
	for _, want := range []string{
		"  specgate:\n",
		"ghcr.io/thanhtung2693/specgate:${SPECGATE_VERSION}",
		"127.0.0.1:${SPECGATE_PORT:-3000}:3000",
		"specgate-data:/data",
		"org.specgate.component: appliance",
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("local compose missing %q", want)
		}
	}
	for _, forbidden := range []string{"  postgres:\n", "  doc-registry:\n", "  agents:\n", "  ui:\n", "redis", "minio"} {
		if strings.Contains(strings.ToLower(raw), forbidden) {
			t.Fatalf("local compose unexpectedly contains %q", forbidden)
		}
	}
}

func TestLocalBundleOverridesInheritedPostgresVolume(t *testing.T) {
	raw := readRepoFile(t, "../../../../deploy/local/compose.yml")
	if !strings.Contains(raw, "    tmpfs:\n      - /var/lib/postgresql\n") {
		t.Fatal("local compose must override the base image's unused PostgreSQL volume with tmpfs")
	}
}

func TestLocalApplianceContainsFullRuntime(t *testing.T) {
	dockerfile := readRepoFile(t, "../../../../docker/Dockerfile.local")
	for _, want := range []string{
		"pgvector/pgvector:0.8.5-pg18-trixie",
		"golang:1.26.5-bookworm",
		"python:3.13-slim-trixie",
		"ghcr.io/astral-sh/uv:0.11.15",
		"github.com/tianon/gosu@1.19",
		"S6_OVERLAY_VERSION=3.2.2.0",
		"uv sync --frozen --no-dev",
		"/out/doc-registry",
		"/usr/share/nginx/html",
		"/opt/specgate/agents",
		"useradd --system --uid 10001",
		"EXPOSE 3000",
	} {
		if !strings.Contains(dockerfile, want) {
			t.Fatalf("local Dockerfile missing %q", want)
		}
	}
	for _, forbidden := range []string{"redis-server", "minio server"} {
		if strings.Contains(strings.ToLower(dockerfile), forbidden) {
			t.Fatalf("local Dockerfile unexpectedly contains %q", forbidden)
		}
	}

	nginx := readRepoFile(t, "../../../../docker/local/nginx.conf")
	for _, want := range []string{
		"listen 3000",
		"error_log stderr warn",
		"location ^~ /api/doc-registry/",
		"location /api/agents/",
		"location = /healthz/components",
		"location = /api/doc-registry/healthz/components",
		"proxy_set_header Host $http_host",
	} {
		if !strings.Contains(nginx, want) {
			t.Fatalf("local nginx config missing %q", want)
		}
	}
	if strings.Contains(nginx, "access_log /dev/stdout") {
		t.Fatal("non-root nginx must not reopen /dev/stdout")
	}
	if !strings.Contains(nginx, "client_max_body_size 32m;") {
		t.Fatal("local nginx must pass the documented Doc Registry upload sizes")
	}
	if !strings.Contains(nginx, `proxy_set_header X-SpecGate-Internal-Agent "";`) {
		t.Fatal("public appliance proxy must strip the internal governance trust header")
	}

	entrypoint := readRepoFile(t, "../../../../docker/local/entrypoint.sh")
	if !strings.Contains(entrypoint, "SETTINGS_ENCRYPTION_KEY is required") {
		t.Fatal("local entrypoint must require the external settings encryption key")
	}
	if strings.Contains(entrypoint, "/data/registry/settings-encryption-key") {
		t.Fatal("local entrypoint must not persist the settings encryption key in the data volume")
	}
	backup := readRepoFile(t, "../../../../docker/local/backup.sh")
	if !strings.Contains(backup, "s6-svc -wD") {
		t.Fatal("local backup must quiesce application writers before snapshotting data")
	}
	healthServer := readRepoFile(t, "../../../../docker/local/health_server.py")
	if strings.Contains(healthServer, `print(f"[health]`) {
		t.Fatal("routine appliance health probes must not grow container logs")
	}
	if !strings.Contains(healthServer, `"agents": probe_port("127.0.0.1", 2024)`) ||
		strings.Contains(healthServer, `probe_url("http://127.0.0.1:2024/openapi.json")`) {
		t.Fatal("routine agent health checks must not generate HTTP access logs")
	}

	finish := readRepoFile(t, "../../../../docker/local/service-finish.sh")
	for _, want := range []string{
		"restart_count",
		"wantedup",
		"/data/diagnostics/last-failure.json",
		"kill -TERM 1",
	} {
		if !strings.Contains(finish, want) {
			t.Fatalf("local supervisor finish handler missing %q", want)
		}
	}

	health := readRepoFile(t, "../../../../docker/local/health_server.py")
	for _, want := range []string{
		`"state"`,
		`"last_failure"`,
		`/data/diagnostics/last-failure.json`,
	} {
		if !strings.Contains(health, want) {
			t.Fatalf("local component diagnostics missing %q", want)
		}
	}

	for _, service := range []string{"postgres", "doc-registry", "agents", "health", "nginx"} {
		finishConfig := readRepoFile(t, "../../../../docker/local/s6-rc.d/"+service+"/finish")
		if !strings.Contains(finishConfig, "specgate-service-finish "+service) {
			t.Fatalf("%s is not wired to bounded restart escalation", service)
		}
	}
	for service, user := range map[string]string{"doc-registry": "specgate", "agents": "agents", "nginx": "specgate"} {
		runConfig := readRepoFile(t, "../../../../docker/local/s6-rc.d/"+service+"/run")
		if !strings.Contains(runConfig, "s6-setuidgid "+user+" sed") {
			t.Fatalf("%s log prefixer must run as %s", service, user)
		}
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
