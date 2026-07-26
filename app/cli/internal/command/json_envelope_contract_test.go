package command_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/output"
)

// `--json` promises exactly one envelope per invocation. Commands that returned
// an ExitError without printing — a missing work ref, a prompt that cannot run
// under --no-input — exited with a bare code and empty stdout, which an IDE agent
// cannot act on: it learns neither what failed nor what to do.
func TestJSONFailuresAlwaysEmitAnEnvelope(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{
			name: "work show without a ref",
			args: []string{"--json", "work", "show"},
			want: "work.show",
		},
		{
			name: "work context without a ref",
			args: []string{"--json", "work", "context"},
			want: "work.context",
		},
		{
			name: "quick work with nothing to create it from",
			args: []string{"--json", "work", "create-quick"},
			want: "work.create-quick",
		},
		{
			name: "delivery report scaffold without a ref",
			args: []string{"--json", "delivery", "report", "--init"},
			want: "delivery.report",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			deps, _, _, out := newFakeDeps(t)

			code := command.ExecuteForCode(command.NewRootCommand(deps), tc.args...)
			if code == output.ExitOK {
				t.Fatalf("expected a failure, got exit 0; output = %s", out.String())
			}

			body := strings.TrimSpace(out.String())
			if body == "" {
				t.Fatal("machine mode exited without emitting an envelope")
			}
			var envelope struct {
				SchemaVersion string `json:"schema_version"`
				Command       string `json:"command"`
				OK            bool   `json:"ok"`
				Error         struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(body), &envelope); err != nil {
				t.Fatalf("stdout is not one JSON envelope: %v\n%s", err, body)
			}
			if envelope.SchemaVersion != "specgate.cli/v1" || envelope.OK {
				t.Fatalf("envelope = %+v, want a v1 failure", envelope)
			}
			if envelope.Command != tc.want {
				t.Fatalf("command = %q, want %q", envelope.Command, tc.want)
			}
			if envelope.Error.Code == "" || envelope.Error.Message == "" {
				t.Fatalf("failure envelope has no actionable code or message: %+v", envelope.Error)
			}
			// One envelope, not two: a site that already printed must not be
			// completed a second time.
			if strings.Count(body, `"schema_version"`) != 1 {
				t.Fatalf("expected exactly one envelope, got:\n%s", body)
			}
		})
	}
}
