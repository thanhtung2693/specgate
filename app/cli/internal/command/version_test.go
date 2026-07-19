package command_test

import (
	"encoding/json"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestVersionJSONUsesSuccessEnvelope(t *testing.T) {
	deps, out := newTestDeps(t, "")
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "version"); code != output.ExitOK {
		t.Fatalf("exit = %d output = %s", code, out.String())
	}
	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			Version string `json:"version"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil || !envelope.OK || envelope.Data.Version == "" {
		t.Fatalf("version output = %s, err = %v", out.String(), err)
	}
}
