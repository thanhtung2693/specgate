package command_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/deploy"
	"github.com/specgate/specgate/app/cli/internal/output"
)

type fakeDeployRunner struct {
	Commands  []string
	Err       error
	OnCommand func()
	// OutputData, when non-nil, is returned by Output instead of "[]".
	OutputData      []byte
	OutputByCommand map[string][]byte
}

func (f *fakeDeployRunner) Run(_ context.Context, name string, args ...string) error {
	f.runHook()
	all := append([]string{name}, args...)
	f.Commands = append(f.Commands, strings.Join(all, " "))
	return f.Err
}

func (f *fakeDeployRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	f.runHook()
	all := append([]string{name}, args...)
	cmd := strings.Join(all, " ")
	f.Commands = append(f.Commands, cmd)
	if f.OutputByCommand != nil {
		if data, ok := f.OutputByCommand[cmd]; ok {
			return data, f.Err
		}
	}
	if f.OutputData != nil {
		return f.OutputData, f.Err
	}
	return []byte("[]"), f.Err
}

func (f *fakeDeployRunner) OutputToFile(ctx context.Context, path, name string, args ...string) error {
	data, err := f.Output(ctx, name, args...)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func (f *fakeDeployRunner) runHook() {
	if f.OnCommand == nil {
		return
	}
	hook := f.OnCommand
	f.OnCommand = nil
	hook()
}

var _ deploy.CommandRunner = (*fakeDeployRunner)(nil)

// setupTestBundle creates a minimal compose bundle in dir so Init can proceed.
func setupTestBundle(t *testing.T, dir string) {
	t.Helper()
	os.WriteFile(filepath.Join(dir, "compose.yml"), []byte("# placeholder\n"), 0644)
	os.WriteFile(filepath.Join(dir, "specgate.env.example"), []byte("SETTINGS_ENCRYPTION_KEY=\n"), 0644)
	if err := deploy.MarkManagedDirectory(dir); err != nil {
		t.Fatal(err)
	}
}

type fakeServer struct {
	metaHandler      http.HandlerFunc
	statusHandler    http.HandlerFunc
	settingsHandler  http.HandlerFunc
	schemaHandler    http.HandlerFunc
	workspaceHandler http.HandlerFunc
}

func (f *fakeServer) build(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	if f.metaHandler != nil {
		mux.HandleFunc("/api/v1/meta", f.metaHandler)
	}
	if f.statusHandler != nil {
		mux.HandleFunc("/api/v1/status", f.statusHandler)
	} else {
		// Doctor probes the board endpoint; default to an empty healthy board.
		mux.HandleFunc("/api/v1/status", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"counts":{},"attention":[]}`))
		})
	}
	if f.settingsHandler != nil {
		mux.HandleFunc("/settings", f.settingsHandler)
	}
	if f.schemaHandler != nil {
		mux.HandleFunc("/api/v1/schema/status", f.schemaHandler)
	}
	if f.workspaceHandler != nil {
		mux.HandleFunc("/api/v1/workspaces/", f.workspaceHandler)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func jsonMeta(apiVersion string, capabilities map[string]bool) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		details := make(map[string]map[string]string, len(capabilities))
		for name, available := range capabilities {
			state := "unavailable"
			if available {
				state = "available"
			}
			details[name] = map[string]string{"state": state}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"body": map[string]any{
				"api_version":        apiVersion,
				"server_version":     "dev",
				"capability_details": details,
			},
		})
	}
}

func submitLocalGateResults(t *testing.T, deps *command.Deps, out *bytes.Buffer, artifactID string) {
	t.Helper()
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "gates", "tasks", "list", artifactID); code != output.ExitOK {
		t.Fatalf("list exit=%d output=%s", code, out.String())
	}
	var listed struct {
		Data struct {
			Tasks []client.GateTask `json:"tasks"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	for _, task := range listed.Data.Tasks {
		resultPath := filepath.Join(t.TempDir(), task.TaskID+".json")
		body := fmt.Sprintf(`{"gate":%q,"gate_digest":%q,"input_digest":%q,"state":"pass","evaluator":{"executor":"ide_agent"}}`, task.GateKey, task.GateDigest, task.ArtifactDigest)
		if err := os.WriteFile(resultPath, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		out.Reset()
		if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "gates", "tasks", "submit-result", task.TaskID, "--file", resultPath); code != output.ExitOK {
			t.Fatalf("submit exit=%d output=%s", code, out.String())
		}
	}
}

func jsonStatus(total, ready int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"body": map[string]any{
				"counts":          map[string]any{"total": total, "ready": ready},
				"needs_attention": []any{},
			},
		})
	}
}

func newTestDeps(t *testing.T, srvURL string) (*command.Deps, *bytes.Buffer) {
	t.Helper()
	var out bytes.Buffer
	homeDir := t.TempDir()
	deps := &command.Deps{
		Stdout: &out,
		Stderr: io.Discard,
		Stdin:  strings.NewReader(""),
		Opener: func(_ string) error { return nil },
		// Isolate config to a temp file so commands that persist config (init,
		// identity, config set) never write to the developer's real
		// ~/.config/specgate/config.json when the suite runs. Tests that need a
		// specific path may override deps.ConfigPath after this call.
		ConfigPath: filepath.Join(t.TempDir(), "config.json"),
		WorkingDir: t.TempDir(),
		UserHomeDir: func() (string, error) {
			return homeDir, nil
		},
	}
	_ = srvURL // deps.Client left nil → PersistentPreRunE constructs it
	return deps, &out
}

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func localBackupPayload(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range map[string]string{"docreg.sql": "-- dump\n", "registry/blob": "blob"} {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(tw, body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func newComposeBundleRegistry(t *testing.T, version string) *httptest.Server {
	t.Helper()
	files := map[string]string{
		"compose.yml":          "# bundle " + version + "\n",
		".env.example":         "SPECGATE_VERSION=latest\n",
		"specgate.env.example": "SETTINGS_ENCRYPTION_KEY=\n",
		"rollback-compatible":  "false\n",
	}
	var tarBuf bytes.Buffer
	gz := gzip.NewWriter(&tarBuf)
	tw := tar.NewWriter(gz)
	for path, body := range files {
		hdr := &tar.Header{Name: path, Mode: 0644, Size: int64(len(body))}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	tarName := fmt.Sprintf("specgate-compose_%s.tar.gz", version)
	sum := sha256.Sum256(tarBuf.Bytes())
	checksums := fmt.Sprintf("%x  %s\n", sum[:], tarName)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/" + version + "/" + tarName:
			_, _ = w.Write(tarBuf.Bytes())
		case "/" + version + "/" + fmt.Sprintf("specgate-compose_%s_checksums.txt", version):
			_, _ = io.WriteString(w, checksums)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}
