package command_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestStatusRetainsLocalWorkspaceTotals(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	stateDir, store, selected, work := newLocalChangeWork(t, deps)
	submitLocalChangeDelivery(t, store, selected.Workspace.ID, work, "builder", "")
	if err := store.DecideDeliveryWithBasis(t.Context(), selected.Workspace.ID, work.Key, "approve", "human", "fixture decision", localReviewID(t, store, selected.Workspace.ID, work.Key), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateQuickWork(t.Context(), selected.Workspace.ID, local.QuickWorkInput{Title: "Ready", AcceptanceCriteria: []string{"A named requirement"}}); err != nil {
		t.Fatal(err)
	}
	closeLocalChangeStore(t, deps, stateDir, store)
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "status"); code != output.ExitOK {
		t.Fatalf("status exit=%d output=%s", code, out.String())
	}
	var response struct {
		OK   bool `json:"ok"`
		Data struct {
			Mode      string `json:"mode"`
			WorkItems int    `json:"work_items"`
			Ready     int    `json:"ready"`
			Delivered int    `json:"delivered"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &response); err != nil || !response.OK || response.Data.Mode != "local" || response.Data.WorkItems != 2 || response.Data.Ready != 1 || response.Data.Delivered != 1 {
		t.Fatalf("unexpected Local status: %s (%v)", out.String(), err)
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "status"); code != output.ExitOK || !strings.Contains(out.String(), "Ready: 1\nDelivered: 1") {
		t.Fatalf("plain status exit=%d output=%s", code, out.String())
	}
}
