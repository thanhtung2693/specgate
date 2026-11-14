package command_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/executil"
	"github.com/specgate/specgate/app/cli/internal/output"
	"github.com/specgate/specgate/app/cli/internal/workflow"
)

type fakeCommandRunner struct {
	Commands  []string
	Err       error
	OnCommand func()
	// OutputData, when non-nil, overrides the command's default empty output.
	OutputData      []byte
	OutputByCommand map[string][]byte
}

func (f *fakeCommandRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
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
	if name == "git" {
		return nil, f.Err
	}
	return []byte("[]"), f.Err
}

func (f *fakeCommandRunner) runHook() {
	if f.OnCommand == nil {
		return
	}
	hook := f.OnCommand
	f.OnCommand = nil
	hook()
}

var _ executil.CommandRunner = (*fakeCommandRunner)(nil)

func submitLocalGateResults(t *testing.T, deps *command.Deps, out *bytes.Buffer, artifactID string) {
	t.Helper()
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "gates", "tasks", "list", artifactID); code != output.ExitOK {
		t.Fatalf("list exit=%d output=%s", code, out.String())
	}
	var listed struct {
		Data struct {
			Tasks []workflow.GateTask `json:"tasks"`
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

// newTestDeps isolates streams and filesystem state. Tests select their server
// explicitly through command arguments or config; this helper does not route HTTP.
func newTestDeps(t *testing.T) (*command.Deps, *bytes.Buffer) {
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
