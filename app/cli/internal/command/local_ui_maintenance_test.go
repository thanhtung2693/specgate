package command

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/specgate/specgate/app/cli/internal/buildinfo"
)

func maintenanceFixture(t *testing.T) (*Deps, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	path := filepath.Join(home, ".cache", "specgate", "update-check.json")
	return &Deps{UserHomeDir: func() (string, error) { return home, nil }, UpdateCheckCachePath: path}, path
}

func maintenanceRequest(t *testing.T, fn http.HandlerFunc, method, path, body string) (int, map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	fn(w, httptest.NewRequest(method, path, strings.NewReader(body)))
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode %q: %v", w.Body.String(), err)
	}
	if data, ok := got["data"].(map[string]any); ok {
		got = data
	}
	return w.Code, got
}

func writeMaintenanceCache(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(updateCheckCache{CheckedAt: time.Now(), LatestVersion: "v1.2.3"})
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func waitMaintenanceUpdate(t *testing.T, m *localUIMaintenance) map[string]any {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		_, got := maintenanceRequest(t, m.update, "GET", "/api/update", "")
		if got["status"] != "running" {
			return got
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("updater did not finish")
	return nil
}

func TestLocalUIMaintenanceCacheDeletesOnlyConfirmedOwnedCache(t *testing.T) {
	deps, path := maintenanceFixture(t)
	writeMaintenanceCache(t, path)
	home, _ := deps.UserHomeDir()
	preserved := []string{".specgate/work/spec.md", ".specgate/local/store.db", ".specgate/backups/recovery.zip", ".specgate/evidence/test.json", ".specgate/history/events.json", ".specgate/acceptance/receipt.json", ".cache/specgate/other.json"}
	for _, name := range preserved {
		target := filepath.Join(home, name)
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte("preserve"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	m := newLocalUIMaintenance(deps)
	status, preview := maintenanceRequest(t, m.cache, "GET", "/api/cache", "")
	want := float64(1)
	if runtime.GOOS == "windows" {
		want = 0
		if preview["reason"] != "Cache cleanup is unavailable on this platform." {
			t.Fatalf("missing unsupported platform reason: %#v", preview)
		}
	}
	if status != 200 || preview["count"] != want {
		t.Fatalf("preview: %d %#v", status, preview)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("preview deleted cache", err)
	}
	status, _ = maintenanceRequest(t, m.cache, "POST", "/api/cache", `{"preview_digest":"wrong"}`)
	if status != 409 {
		t.Fatalf("missing confirmation status=%d", status)
	}
	body, _ := json.Marshal(map[string]any{"preview_digest": preview["preview_digest"]})
	status, result := maintenanceRequest(t, m.cache, "POST", "/api/cache", string(body))
	if want == 0 {
		if status != 409 {
			t.Fatalf("unsupported cleanup: %d %#v", status, result)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("unsupported cleanup changed the cache: %v", err)
		}
	} else {
		if status != 200 || result["deleted_count"] != want {
			t.Fatalf("cleanup: %d %#v", status, result)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("cache survives: %v", err)
		}
	}
	for _, name := range preserved {
		data, err := os.ReadFile(filepath.Join(home, name))
		if err != nil || string(data) != "preserve" {
			t.Fatalf("changed %s: %q %v", name, data, err)
		}
	}
	status, _ = maintenanceRequest(t, m.cache, "POST", "/api/cache", string(body))
	if status != 409 {
		t.Fatalf("replayed preview status=%d", status)
	}
}

func TestLocalUIMaintenanceCacheRejectsChangedPreview(t *testing.T) {
	deps, path := maintenanceFixture(t)
	writeMaintenanceCache(t, path)
	m := newLocalUIMaintenance(deps)
	_, preview := maintenanceRequest(t, m.cache, "GET", "/api/cache", "")
	if runtime.GOOS == "windows" {
		t.Skip("cache cleanup is unsupported on Windows")
	}
	if err := os.WriteFile(path, []byte("user evidence replaces cache"), 0600); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"preview_digest": preview["preview_digest"]})
	status, _ := maintenanceRequest(t, m.cache, "POST", "/api/cache", string(body))
	if status != 409 {
		t.Fatalf("changed preview status=%d", status)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "user evidence replaces cache" {
		t.Fatal("changed file deleted")
	}
}

func TestLocalUIMaintenanceCacheRejectsUnsafeFiles(t *testing.T) {
	for _, kind := range []string{"symlink", "parent_symlink", "hardlink", "unowned_json", "oversize", "arbitrary_path"} {
		t.Run(kind, func(t *testing.T) {
			deps, path := maintenanceFixture(t)
			home, _ := deps.UserHomeDir()
			sentinel := filepath.Join(home, "spec.md")
			if err := os.WriteFile(sentinel, []byte("preserve"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "symlink":
				if err := os.Symlink(sentinel, path); err != nil {
					t.Skip(err)
				}
			case "parent_symlink":
				if err := os.Remove(filepath.Dir(path)); err != nil {
					t.Fatal(err)
				}
				external := t.TempDir()
				writeMaintenanceCache(t, filepath.Join(external, "update-check.json"))
				if err := os.Symlink(external, filepath.Dir(path)); err != nil {
					t.Skip(err)
				}
			case "hardlink":
				writeMaintenanceCache(t, path)
				if err := os.Link(path, filepath.Join(home, "copy.json")); err != nil {
					t.Skip(err)
				}
			case "unowned_json":
				if err := os.WriteFile(path, []byte(`{"spec":"preserve"}`), 0600); err != nil {
					t.Fatal(err)
				}
			case "oversize":
				if err := os.WriteFile(path, []byte(strings.Repeat(" ", 8192)), 0600); err != nil {
					t.Fatal(err)
				}
			case "arbitrary_path":
				deps.UpdateCheckCachePath = sentinel
			}
			m := newLocalUIMaintenance(deps)
			status, preview := maintenanceRequest(t, m.cache, "GET", "/api/cache", "")
			if status != 200 || preview["count"] != float64(0) {
				t.Fatalf("unsafe cache offered: %d %#v", status, preview)
			}
			body, _ := json.Marshal(map[string]any{"preview_digest": preview["preview_digest"], "path": sentinel})
			status, _ = maintenanceRequest(t, m.cache, "POST", "/api/cache", string(body))
			if status != 400 {
				t.Fatalf("arbitrary path accepted status=%d", status)
			}
			data, err := os.ReadFile(sentinel)
			if err != nil || string(data) != "preserve" {
				t.Fatalf("sentinel changed: %q %v", data, err)
			}
		})
	}
}

func TestLocalUIMaintenanceUpdateRequiresExactPreviewAndRunsOnce(t *testing.T) {
	old := buildinfo.Version
	buildinfo.Version = "v1.0.0"
	t.Cleanup(func() { buildinfo.Version = old })
	deps, _ := maintenanceFixture(t)
	home, _ := deps.UserHomeDir()
	exe := filepath.Join(home, "specgate")
	if err := os.WriteFile(exe, []byte("release"), 0700); err != nil {
		t.Fatal(err)
	}
	deps.ExecutablePath = func() (string, error) { return exe, nil }
	checks := 0
	var runs atomic.Int32
	finish := make(chan struct{})
	deps.LocalUICheckUpdate = func(context.Context) (string, error) { checks++; return "v1.1.0", nil }
	deps.LocalUIUpdate = func(_ context.Context, v string) error {
		runs.Add(1)
		if v != "v1.1.0" {
			t.Errorf("version=%s", v)
		}
		<-finish
		return nil
	}
	m := newLocalUIMaintenance(deps)
	status, got := maintenanceRequest(t, m.update, "GET", "/api/update", "")
	if status != 200 || checks != 0 || got["status"] != "unchecked" {
		t.Fatalf("GET checked network: %d %#v checks=%d", status, got, checks)
	}
	_, preview := maintenanceRequest(t, m.check, "POST", "/api/update/check", `{}`)
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		t.Fatal(err)
	}
	if checks != 1 || preview["status"] != "available" || preview["target"] != resolved {
		t.Fatalf("preview=%#v checks=%d", preview, checks)
	}
	bad, _ := json.Marshal(map[string]any{"version": "v9.9.9", "confirmation_token": preview["confirmation_token"]})
	status, _ = maintenanceRequest(t, m.update, "POST", "/api/update", string(bad))
	if status != 409 || runs.Load() != 0 {
		t.Fatalf("arbitrary version accepted: status=%d runs=%d", status, runs.Load())
	}
	body, _ := json.Marshal(map[string]any{"version": "v1.1.0", "confirmation_token": preview["confirmation_token"]})
	status, result := maintenanceRequest(t, m.update, "POST", "/api/update", string(body))
	if status != 202 || result["status"] != "running" {
		t.Fatalf("start update: %d %#v", status, result)
	}
	_, checking := maintenanceRequest(t, m.check, "POST", "/api/update/check", `{}`)
	if checking["status"] != "running" || checks != 1 {
		t.Errorf("checking released active update lock: %#v checks=%d", checking, checks)
	}
	status, _ = maintenanceRequest(t, m.update, "POST", "/api/update", string(body))
	if status != 409 {
		t.Errorf("in-flight replay status=%d", status)
	}
	close(finish)
	result = waitMaintenanceUpdate(t, m)
	if result["status"] != "updated" || result["restart_required"] != true || runs.Load() != 1 {
		t.Fatalf("finished update: %#v runs=%d", result, runs.Load())
	}
	status, _ = maintenanceRequest(t, m.update, "POST", "/api/update", string(body))
	if status != 409 || runs.Load() != 1 {
		t.Fatalf("replayed update: %d runs=%d", status, runs.Load())
	}
}

func TestLocalUIMaintenanceDevelopmentUpdateDisabled(t *testing.T) {
	old := buildinfo.Version
	buildinfo.Version = "dev"
	t.Cleanup(func() { buildinfo.Version = old })
	deps, _ := maintenanceFixture(t)
	deps.LocalUIUpdate = func(context.Context, string) error { t.Fatal("development build attempted install"); return nil }
	m := newLocalUIMaintenance(deps)
	_, got := maintenanceRequest(t, m.update, "GET", "/api/update", "")
	if got["status"] != "disabled" || got["reason"] == "" {
		t.Fatalf("dev not disabled: %#v", got)
	}
	status, _ := maintenanceRequest(t, m.update, "POST", "/api/update", `{"version":"v1.1.0","confirmation_token":"guess"}`)
	if status != 409 {
		t.Fatalf("dev mutation status=%d", status)
	}
}

func TestLocalUIMaintenanceUpdateFailureRemainsVisible(t *testing.T) {
	old := buildinfo.Version
	buildinfo.Version = "v1.0.0"
	t.Cleanup(func() { buildinfo.Version = old })
	deps, _ := maintenanceFixture(t)
	home, _ := deps.UserHomeDir()
	exe := filepath.Join(home, "specgate")
	if err := os.WriteFile(exe, []byte("release"), 0700); err != nil {
		t.Fatal(err)
	}
	deps.ExecutablePath = func() (string, error) { return exe, nil }
	deps.LocalUICheckUpdate = func(context.Context) (string, error) { return "v1.1.0", nil }
	deps.LocalUIUpdate = func(context.Context, string) error { return errors.New("installation failed") }
	m := newLocalUIMaintenance(deps)
	_, preview := maintenanceRequest(t, m.check, "POST", "/api/update/check", `{}`)
	body, _ := json.Marshal(map[string]any{"version": "v1.1.0", "confirmation_token": preview["confirmation_token"]})
	status, got := maintenanceRequest(t, m.update, "POST", "/api/update", string(body))
	if status != 202 || got["status"] != "running" {
		t.Fatalf("start=%d %#v", status, got)
	}
	got = waitMaintenanceUpdate(t, m)
	if got["status"] != "failed" || got["restart_required"] != true || !strings.Contains(got["reason"].(string), "installation failed") {
		t.Fatalf("failure lost: %#v", got)
	}
}

func TestLocalUIMaintenanceUpdateRejectsStaleTargetAndExpiredToken(t *testing.T) {
	old := buildinfo.Version
	buildinfo.Version = "v1.0.0"
	t.Cleanup(func() { buildinfo.Version = old })
	for _, kind := range []string{"expired", "replaced_target", "changed_target", "invalid_release"} {
		t.Run(kind, func(t *testing.T) {
			deps, _ := maintenanceFixture(t)
			home, _ := deps.UserHomeDir()
			exe := filepath.Join(home, "specgate")
			if err := os.WriteFile(exe, []byte("release"), 0700); err != nil {
				t.Fatal(err)
			}
			deps.ExecutablePath = func() (string, error) { return exe, nil }
			deps.LocalUICheckUpdate = func(context.Context) (string, error) {
				if kind == "invalid_release" {
					return "https://attacker.invalid/release", nil
				}
				return "v1.1.0", nil
			}
			deps.LocalUIUpdate = func(context.Context, string) error { t.Error("stale preview started updater"); return nil }
			m := newLocalUIMaintenance(deps)
			status, preview := maintenanceRequest(t, m.check, "POST", "/api/update/check", `{}`)
			if kind == "invalid_release" {
				if status != 502 {
					t.Fatalf("invalid release status=%d", status)
				}
				return
			}
			switch kind {
			case "expired":
				m.checkedAt = time.Now().Add(-11 * time.Minute)
			case "replaced_target":
				replacement := exe + ".new"
				if err := os.WriteFile(replacement, []byte("release"), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(replacement, exe); err != nil {
					t.Fatal(err)
				}
			case "changed_target":
				if err := os.WriteFile(exe, []byte("changed release contents"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			body, _ := json.Marshal(map[string]any{"version": "v1.1.0", "confirmation_token": preview["confirmation_token"]})
			status, _ = maintenanceRequest(t, m.update, "POST", "/api/update", string(body))
			if status != 409 {
				t.Fatalf("stale preview status=%d", status)
			}
		})
	}
}

func TestLocalUIMaintenanceMissingCacheDoesNotScanTemporaryOrWorkFiles(t *testing.T) {
	deps, _ := maintenanceFixture(t)
	deps.UpdateCheckCachePath = ""
	home, _ := deps.UserHomeDir()
	path := filepath.Join(home, ".specgate", "work", "update-check.json")
	writeMaintenanceCache(t, path)
	m := newLocalUIMaintenance(deps)
	_, preview := maintenanceRequest(t, m.cache, "GET", "/api/cache", "")
	if preview["count"] != float64(0) || preview["temporary_count"] != float64(0) {
		t.Fatalf("broad scan: %#v", preview)
	}
	body, _ := json.Marshal(map[string]any{"preview_digest": preview["preview_digest"]})
	status, result := maintenanceRequest(t, m.cache, "POST", "/api/cache", string(body))
	if runtime.GOOS == "windows" && status != 409 {
		t.Fatalf("unsupported cleanup: %d %#v", status, result)
	}
	if runtime.GOOS != "windows" && (status != 200 || result["deleted_count"] != float64(0)) {
		t.Fatalf("empty cleanup: %d %#v", status, result)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("work file changed: %v", err)
	}
}
