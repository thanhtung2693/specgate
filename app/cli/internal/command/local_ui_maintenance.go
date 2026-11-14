package command

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/specgate/specgate/app/cli/internal/buildinfo"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/output"
)

const localUICacheScope = "Only the SpecGate update-check cache. Work, specifications, evidence, history, acceptance records and backups are preserved. Temporary-file cleanup is not enabled."
const localUICacheUnsupported = "Cache cleanup is unavailable on this platform."

type localUIUpdateStatus struct {
	CurrentVersion    string   `json:"current_version"`
	LatestVersion     string   `json:"latest_version"`
	Status            string   `json:"status"`
	Target            string   `json:"target"`
	Scope             []string `json:"scope"`
	ConfirmationToken string   `json:"confirmation_token"`
	Reason            string   `json:"reason,omitempty"`
	RestartRequired   bool     `json:"restart_required"`
}

type localUICacheEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
}

type localUICachePreview struct {
	Entries        []localUICacheEntry `json:"entries"`
	Bytes          int64               `json:"bytes"`
	Count          int                 `json:"count"`
	TemporaryCount int                 `json:"temporary_count"`
	PreviewDigest  string              `json:"preview_digest"`
	Scope          string              `json:"scope"`
	Reason         string              `json:"reason,omitempty"`
}

type localUICacheFile struct {
	path string
	info os.FileInfo
	hash [sha256.Size]byte
}

type localUIMaintenance struct {
	deps         Deps
	mu           sync.Mutex
	updateState  localUIUpdateStatus
	updateTarget os.FileInfo
	checkedAt    time.Time
	cachePreview *localUICachePreview
	cacheFile    *localUICacheFile
}

func newLocalUIMaintenance(deps *Deps) *localUIMaintenance {
	m := &localUIMaintenance{deps: *deps}
	m.updateState = localUIUpdateStatus{CurrentVersion: buildinfo.Version, Status: "unchecked", Scope: []string{"SpecGate CLI executable", "Existing CLI-managed global IDE setup", "Work data and history are preserved"}}
	target, err := updateExecutablePath(deps)
	m.updateState.Target = target
	if err == nil {
		m.updateTarget, err = os.Stat(target)
	}
	_, release := parseReleaseVersion(buildinfo.Version)
	switch {
	case !release || isDevVersion(buildinfo.Version):
		m.updateState.Status = "disabled"
		m.updateState.Reason = "Development builds cannot replace an installed release. Install a published SpecGate CLI to enable updates."
	case err != nil || m.updateTarget == nil || !m.updateTarget.Mode().IsRegular() || (filepath.Base(target) != "specgate" && filepath.Base(target) != "specgate.exe"):
		m.updateState.Status = "disabled"
		m.updateState.Reason = "The running executable is not a verified SpecGate installation target."
	}
	return m
}

func (s *localUIServer) handleMaintenance(w http.ResponseWriter, r *http.Request) bool {
	switch r.URL.Path {
	case "/api/update":
		s.maintenance.update(w, r)
	case "/api/update/check":
		s.maintenance.check(w, r)
	case "/api/cache":
		s.maintenance.cache(w, r)
	default:
		return false
	}
	return true
}

func (m *localUIMaintenance) check(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeLocalUIError(w, 405, "method_not_allowed", "Use POST to explicitly check the official release service.")
		return
	}
	var request struct{}
	if !decodeLocalUIRequest(w, r, &request) {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.updateState.RestartRequired || m.updateState.Status == "running" {
		writeLocalUIJSON(w, 200, m.updateState)
		return
	}
	m.updateState.ConfirmationToken = ""
	check := m.deps.LocalUICheckUpdate
	if check == nil {
		check = func(ctx context.Context) (string, error) {
			return fetchLatestGitHubRelease(ctx, defaultTimeout, githubReleasesAPI)
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	latest, err := check(ctx)
	if err == nil {
		err = validateReleaseVersion(latest)
		if _, ok := parseReleaseVersion(latest); !ok {
			err = fmt.Errorf("release service returned an invalid release version")
		}
	}
	if err != nil {
		writeLocalUIError(w, 502, "unavailable", "Could not check the official release service. Retry the explicit update check.")
		return
	}
	m.updateState.LatestVersion = latest
	if m.updateState.Status != "disabled" {
		m.updateState.Reason = ""
		m.updateState.Status = "current"
		if isVersionNewer(latest, m.updateState.CurrentVersion) {
			token, err := localUIMaintenanceToken()
			if err != nil {
				writeLocalUIError(w, 500, "unavailable", "Could not create an update confirmation.")
				return
			}
			m.updateState.Status = "available"
			m.updateState.ConfirmationToken = token
			m.checkedAt = time.Now()
		}
	}
	writeLocalUIJSON(w, 200, m.updateState)
}

func (m *localUIMaintenance) update(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeLocalUIError(w, 405, "method_not_allowed", "Use GET or POST.")
		return
	}
	var request struct {
		Version           string `json:"version"`
		ConfirmationToken string `json:"confirmation_token"`
	}
	if r.Method == http.MethodPost && !decodeLocalUIRequest(w, r, &request) {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if r.Method == http.MethodGet {
		writeLocalUIJSON(w, 200, m.updateState)
		return
	}
	if m.updateState.Status != "available" || request.Version != m.updateState.LatestVersion || request.ConfirmationToken == "" || request.ConfirmationToken != m.updateState.ConfirmationToken || time.Since(m.checkedAt) > 10*time.Minute {
		writeLocalUIError(w, 409, "stale_preview", "Check for updates and confirm the exact version and target before updating.")
		return
	}
	target, err := updateExecutablePath(&m.deps)
	var current os.FileInfo
	if err == nil {
		current, err = os.Stat(target)
	}
	if err != nil || target != m.updateState.Target || !localUISameFile(current, m.updateTarget) {
		m.updateState.ConfirmationToken = ""
		writeLocalUIError(w, 409, "stale_preview", "The executable changed after the preview. Restart Local UI before updating.")
		return
	}
	m.updateState.ConfirmationToken = ""
	m.updateState.Status = "running"
	m.updateState.Reason = "The confirmed update is running. Keep Local UI running until the result is shown."
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 10*time.Minute)
	go func() {
		defer cancel()
		var err error
		if m.deps.LocalUIUpdate != nil {
			err = m.deps.LocalUIUpdate(ctx, request.Version)
		} else {
			err = m.runUpdate(ctx, request.Version, target)
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		// The updater can replace the binary before a later IDE setup step fails.
		m.updateState.RestartRequired = true
		m.updateState.Status = "updated"
		m.updateState.Reason = "Update completed. Stop and restart Local UI to use the installed version."
		if err != nil {
			m.updateState.Status = "failed"
			m.updateState.Reason = "Update failed: " + err.Error() + ". The CLI may already have been replaced before an IDE setup failure. Stop and restart Local UI, then inspect the installed version."
		}
	}()
	writeLocalUIJSON(w, http.StatusAccepted, m.updateState)
}

func (m *localUIMaintenance) runUpdate(ctx context.Context, version, target string) error {
	dir, err := os.MkdirTemp("", "specgate-ui-update-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	deps := m.deps
	deps.ConfigPath = filepath.Join(dir, "config.toml")
	// A private Local-only config keeps the existing updater out of appliance paths.
	if err := (config.Config{Mode: config.ModeLocal}).SaveTo(deps.ConfigPath); err != nil {
		return err
	}
	deps.CLIInstallURL = ""
	deps.CLIReleaseBaseURL = ""
	deps.PublicRegistryURL = ""
	deps.SelfUpdateCLI = nil
	deps.RuntimeGOOS = ""
	deps.ExecutablePath = func() (string, error) { return target, nil }
	deps.Stdout = io.Discard
	deps.Stderr = io.Discard
	deps.Stdin = strings.NewReader("")
	deps.Printer = output.NewWithColor(io.Discard, io.Discard, output.ModeJSON, false)
	deps.Mode = output.ModeJSON
	deps.JSONProgress = false
	deps.NoInput = true
	deps.Yes = true
	cmd := newUpdateCmd(&deps)
	cmd.SetContext(ctx)
	if err := cmd.Flags().Set("version", version); err != nil {
		return err
	}
	return cmd.RunE(cmd, nil)
}

func (m *localUIMaintenance) cache(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeLocalUIError(w, 405, "method_not_allowed", "Use GET or POST.")
		return
	}
	var request struct {
		PreviewDigest string `json:"preview_digest"`
	}
	if r.Method == http.MethodPost && !decodeLocalUIRequest(w, r, &request) {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if r.Method == http.MethodGet {
		token, err := localUIMaintenanceToken()
		if err != nil {
			writeLocalUIError(w, 500, "unavailable", "Could not create a cleanup preview.")
			return
		}
		preview := &localUICachePreview{Entries: []localUICacheEntry{}, PreviewDigest: token, Scope: localUICacheScope}
		m.cacheFile = nil
		if os.Getuid() < 0 {
			preview.Reason = localUICacheUnsupported
		} else {
			file, err := m.readCacheFile()
			if err != nil {
				preview.Reason = "SpecGate could not verify that the cache is safe to remove."
			} else if file != nil {
				m.cacheFile = file
				preview.Entries = append(preview.Entries, localUICacheEntry{Name: "update-check.json", Path: file.path, Bytes: file.info.Size()})
				preview.Count = 1
				preview.Bytes = file.info.Size()
			}
		}
		m.cachePreview = preview
		writeLocalUIJSON(w, 200, preview)
		return
	}
	if m.cachePreview == nil || request.PreviewDigest == "" || request.PreviewDigest != m.cachePreview.PreviewDigest {
		writeLocalUIError(w, 409, "stale_preview", "Preview the exact eligible cache before confirming cleanup.")
		return
	}
	preview, file := m.cachePreview, m.cacheFile
	m.cachePreview = nil
	m.cacheFile = nil
	if preview.Reason == localUICacheUnsupported {
		writeLocalUIError(w, 409, "unavailable", localUICacheUnsupported)
		return
	}
	if file != nil {
		fresh, err := m.readCacheFile()
		if err != nil || fresh == nil || fresh.path != file.path || fresh.hash != file.hash || !localUISameFile(fresh.info, file.info) {
			writeLocalUIError(w, 409, "stale_preview", "The cache changed after the preview. Nothing was removed.")
			return
		}
		root, err := m.openCacheDirectory(file.path)
		if err != nil {
			writeLocalUIError(w, 409, "stale_preview", "The cache directory changed. Nothing was removed.")
			return
		}
		defer root.Close()
		current, err := root.Lstat("update-check.json")
		if err != nil || !localUISameFile(current, file.info) {
			writeLocalUIError(w, 409, "stale_preview", "The cache changed after the preview. Nothing was removed.")
			return
		}
		if err := root.Remove("update-check.json"); err != nil {
			writeLocalUIError(w, 500, "unavailable", "The previewed cache could not be removed.")
			return
		}
	}
	writeLocalUIJSON(w, 200, map[string]any{"status": "cleared", "deleted_count": preview.Count, "deleted_bytes": preview.Bytes, "scope": localUICacheScope})
}

func (m *localUIMaintenance) cachePath() (string, error) {
	path := m.deps.UpdateCheckCachePath
	if path == "" {
		if m.deps.UserHomeDir != nil {
			home, err := userHomeDir(&m.deps)
			if err != nil {
				return "", err
			}
			cacheDir := filepath.Join(home, ".cache")
			switch runtime.GOOS {
			case "darwin":
				cacheDir = filepath.Join(home, "Library", "Caches")
			case "windows":
				cacheDir = filepath.Join(home, "AppData", "Local")
			}
			path = filepath.Join(cacheDir, "specgate", "update-check.json")
		} else {
			var err error
			path, err = defaultUpdateCheckCachePath()
			if err != nil {
				return "", err
			}
		}
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Base(path) != "update-check.json" || filepath.Base(filepath.Dir(path)) != "specgate" {
		return "", fmt.Errorf("path is not the known SpecGate update-check cache")
	}
	return path, nil
}

func (m *localUIMaintenance) openCacheDirectory(path string) (*os.Root, error) {
	home, err := userHomeDir(&m.deps)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(home, filepath.Dir(path))
	if err != nil || rel == "." || !filepath.IsLocal(rel) {
		return nil, fmt.Errorf("cache is outside the user home")
	}
	home, err = filepath.EvalSymlinks(home)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(home)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	part := ""
	for _, name := range strings.Split(rel, string(filepath.Separator)) {
		part = filepath.Join(part, name)
		info, err := root.Lstat(part)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("cache parent is not a real directory")
		}
	}
	return root.OpenRoot(rel)
}

func (m *localUIMaintenance) readCacheFile() (*localUICacheFile, error) {
	path, err := m.cachePath()
	if err != nil {
		return nil, err
	}
	root, err := m.openCacheDirectory(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer root.Close()
	info, err := root.Lstat("update-check.json")
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 4096 || info.Size() <= 0 || info.Mode().Perm()&0022 != 0 || !localUISingleOwnedFile(info) {
		return nil, fmt.Errorf("cache is not a bounded, owned regular file")
	}
	f, err := root.Open("update-check.json")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !localUISameFile(opened, info) {
		return nil, fmt.Errorf("cache changed during preview")
	}
	data, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil {
		return nil, err
	}
	if len(data) > 4096 {
		return nil, fmt.Errorf("cache is too large")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var cache updateCheckCache
	if decoder.Decode(&cache) != nil || decoder.Decode(new(any)) != io.EOF || cache.CheckedAt.IsZero() || isDevVersion(cache.LatestVersion) {
		return nil, fmt.Errorf("file is not an owned update-check cache")
	}
	if _, ok := parseReleaseVersion(cache.LatestVersion); !ok {
		return nil, fmt.Errorf("cache version is invalid")
	}
	return &localUICacheFile{path: path, info: info, hash: sha256.Sum256(data)}, nil
}

func localUISameFile(a, b os.FileInfo) bool {
	return a != nil && b != nil && a.Mode().IsRegular() && os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}

func localUISingleOwnedFile(info os.FileInfo) bool {
	stat := reflect.Indirect(reflect.ValueOf(info.Sys()))
	if !stat.IsValid() || stat.Kind() != reflect.Struct {
		return false
	}
	links := stat.FieldByName("Nlink")
	uid := stat.FieldByName("Uid")
	// Fail closed where the platform does not expose link count and ownership.
	return links.IsValid() && links.CanUint() && links.Uint() == 1 && uid.IsValid() && uid.CanUint() && uid.Uint() == uint64(os.Getuid())
}

func localUIMaintenanceToken() (string, error) {
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}
