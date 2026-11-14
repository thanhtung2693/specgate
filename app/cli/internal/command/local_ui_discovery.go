package command

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/output"
	"github.com/spf13/cobra"
)

type localUIDiscovery struct {
	URL         string `json:"url"`
	SessionID   string `json:"session_id"`
	WorkspaceID string `json:"workspace_id"`
	RepoRoot    string `json:"repo_root"`
}

// Windows inherits user-directory ACLs; discovery stores a public nonce, never auth tokens.
func localUIPrivatePermissions(mode os.FileMode, goos string) bool {
	return goos == "windows" || mode.Perm()&0077 == 0
}

func localUIDiscoveryDir(deps *Deps, create bool) (string, error) {
	state, err := localStatePath(deps)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(filepath.Dir(state), "ui")
	if create {
		if err := os.Mkdir(dir, 0700); err != nil && !os.IsExist(err) {
			return "", err
		}
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !localUIPrivatePermissions(info.Mode(), runtime.GOOS) {
		return "", fmt.Errorf("local UI discovery must be a private real directory")
	}
	return dir, nil
}
func writeLocalUIDiscovery(deps *Deps, descriptor localUIDiscovery) (string, error) {
	dir, err := localUIDiscoveryDir(deps, true)
	if err != nil {
		return "", err
	}
	file, err := os.CreateTemp(dir, "session-*.json")
	if err != nil {
		return "", err
	}
	name := file.Name()
	keep := false
	defer func() {
		file.Close()
		if !keep {
			os.Remove(name)
		}
	}()
	if err := json.NewEncoder(file).Encode(descriptor); err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	keep = true
	return name, nil
}
func discoverLocalUI(ctx context.Context, deps *Deps, workspaceID, repoRoot string) (string, error) {
	dir, err := localUIDiscoveryDir(deps, false)
	if err != nil {
		return "", fmt.Errorf("no live Local UI for this repository/workspace; run `specgate ui --no-open` and keep it running")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() > entries[j].Name() })
	client := &http.Client{Timeout: 500 * time.Millisecond, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "session-") || !strings.HasSuffix(entry.Name(), ".json") || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || !localUIPrivatePermissions(info.Mode(), runtime.GOOS) || info.Size() > 4096 {
			continue
		}
		file, err := os.Open(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var descriptor localUIDiscovery
		err = json.NewDecoder(io.LimitReader(file, 4097)).Decode(&descriptor)
		file.Close()
		if err != nil || descriptor.WorkspaceID != workspaceID || descriptor.RepoRoot != repoRoot || len(descriptor.SessionID) != 64 {
			continue
		}
		u, err := url.Parse(descriptor.URL)
		if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
			continue
		}
		if _, _, err := net.SplitHostPort(u.Host); err != nil {
			continue
		}
		req, err := http.NewRequestWithContext(ctx, "GET", descriptor.URL+"/api/ping", nil)
		if err != nil {
			continue
		}
		response, err := client.Do(req)
		if err != nil {
			continue
		}
		var ping struct {
			OK   bool `json:"ok"`
			Data struct {
				SessionID string `json:"session_id"`
			} `json:"data"`
		}
		err = json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&ping)
		response.Body.Close()
		if err == nil && response.StatusCode == 200 && ping.OK && ping.Data.SessionID == descriptor.SessionID {
			return descriptor.URL, nil
		}
	}
	return "", fmt.Errorf("no live Local UI for this repository/workspace; run `specgate ui --no-open` and keep it running")
}
func openLocalUI(cmd *cobra.Command, deps *Deps, args []string, artifactID string, printOnly bool) error {
	if artifactID != "" {
		return incompatibleCommand(deps, "open", "Local UI deep links use a work reference; run `specgate ui <work-ref>`")
	}
	root, ok := config.FindProjectRoot(deps.WorkingDir)
	if !ok {
		return fmt.Errorf("local UI requires a repository; run from the intended repository")
	}
	store, err := openLocalStore(deps)
	if err != nil {
		return localExitError(deps, "open", err)
	}
	defer store.Close()
	selection, err := localSelection(cmd.Context(), deps, store)
	if err != nil {
		return localExitError(deps, "open", err)
	}
	target, err := discoverLocalUI(cmd.Context(), deps, selection.Workspace.ID, root)
	if err != nil {
		code := deps.Printer.Error("open", output.ErrorPayload{Code: "unavailable", Message: err.Error()})
		return &output.ExitError{Code: code, Err: err}
	}
	if len(args) > 0 && args[0] != "work" && args[0] != "reviews" {
		if args[0] == "artifacts" {
			return incompatibleCommand(deps, "open", "Local UI inspects pinned artifacts from work detail; pass a work reference")
		}
		work, err := store.GetWork(cmd.Context(), selection.Workspace.ID, args[0])
		if err != nil {
			return localExitError(deps, "open", err)
		}
		target += "?ref=" + url.QueryEscape(work.Key)
	}
	if !printOnly {
		if deps.Opener == nil {
			return fmt.Errorf("no opener configured")
		}
		if err := deps.Opener(target); err != nil {
			return err
		}
	}
	if deps.Printer.Mode() == output.ModeJSON {
		deps.Printer.Success("open", map[string]string{"url": target})
	} else {
		fmt.Fprintln(deps.Stdout, target)
	}
	return nil
}
