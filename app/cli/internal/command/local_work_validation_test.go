package command_test

import (
	"encoding/json"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestLocalWorkInvalidCriteriaIsValidationWithoutCreatingWork(t *testing.T) {
	for _, criterion := range []string{"Works @source:foreign", " ", "Works @check:unit @check:integration"} {
		t.Run(criterion, func(t *testing.T) {
			deps, fake, _, out := newFakeDeps(t)
			stateDir, store, selection, original := newLocalChangeWork(t, deps)
			closeLocalChangeStore(t, deps, stateDir, store)
			code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "work", "create",
				"--feature", original.FeatureKey, "--title", "Invalid criteria", "--ac", criterion)
			var envelope struct {
				OK    bool `json:"ok"`
				Error struct {
					Code      string `json:"code"`
					Transient bool   `json:"transient"`
				} `json:"error"`
			}
			if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if code != output.ExitUsage || envelope.OK || envelope.Error.Code != "validation" || envelope.Error.Transient {
				t.Fatalf("exit=%d output=%s", code, out.String())
			}
			if fake.calls != 0 {
				t.Fatal("Local validation called Full API")
			}
			store, err := local.Open(stateDir + "/state.db")
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			works, err := store.ListWork(t.Context(), selection.Workspace.ID)
			if err != nil || len(works) != 1 || works[0].ID != original.ID {
				t.Fatalf("invalid input mutated work: %v %#v", err, works)
			}
		})
	}
}
