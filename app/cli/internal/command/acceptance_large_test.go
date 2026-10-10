package command_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/local"
)

func TestLargeAcceptanceAndRepeatedResumeRetainEveryObligationWithoutWrites(t *testing.T) {
	deps, client, _, out := newFakeDeps(t)
	dir, store, sel, _ := newLocalChangeWork(t, deps)
	criteria := make([]string, 150)
	for i := range criteria {
		criteria[i] = fmt.Sprintf("Preserve behavior %d", i+1)
	}
	work, err := store.CreateQuickWork(t.Context(), sel.Workspace.ID, local.QuickWorkInput{Title: "large manual review", AcceptanceCriteria: criteria})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateCheckpointWithFiles(t.Context(), sel.Workspace.ID, work.Key, "unavailable", nil, "baseline"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SubmitDelivery(t.Context(), sel.Workspace.ID, work.Key, map[string]any{"context_digest": work.ContextDigest, "agent": map[string]any{"name": "builder"}}, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	closeLocalChangeStore(t, deps, dir, store)
	db, err := sql.Open("specgate-sqlite", filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	counts := func() [4]int {
		t.Helper()
		var got [4]int
		if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM work_checkpoints), (SELECT COUNT(*) FROM delivery_reports), (SELECT COUNT(*) FROM delivery_reviews), (SELECT COUNT(*) FROM acceptance_bases)`).Scan(&got[0], &got[1], &got[2], &got[3]); err != nil {
			t.Fatal(err)
		}
		return got
	}
	before := counts()
	var digest string
	for _, args := range [][]string{
		{"change", "status", work.Key}, {"change", "status", work.Key, "--detail"},
		{"work", "resume", work.Key}, {"work", "resume", work.Key},
	} {
		out.Reset()
		if code := command.ExecuteForCode(command.NewRootCommand(deps), append([]string{"--json"}, args...)...); code != 0 {
			t.Fatalf("%v: %d %s", args, code, out.String())
		}
		var got struct {
			Data struct {
				Basis  local.AcceptanceBasis `json:"acceptance_basis"`
				Status struct {
					Basis local.AcceptanceBasis `json:"acceptance_basis"`
				} `json:"status"`
				Work local.WorkItem `json:"work"`
			} `json:"data"`
		}
		if err := json.Unmarshal(out.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		basis := got.Data.Basis
		if args[0] == "work" {
			basis = got.Data.Status.Basis
			if got.Data.Work.ContextDigest != work.ContextDigest || len(got.Data.Work.AcceptanceCriteria) != 150 {
				t.Fatal("resume changed approved scope")
			}
		}
		if len(basis.CriterionEvidence) != 150 || len(basis.Gaps) < 150 {
			t.Fatalf("large obligations lost: %s", out.String())
		}
		for i, row := range basis.CriterionEvidence {
			if row.ID != fmt.Sprintf("local-%d", i+1) || row.Claim != "missing" {
				t.Fatalf("obligation %d hidden: %+v", i, row)
			}
		}
		if digest == "" {
			digest = basis.Digest
		} else if basis.Digest != digest {
			t.Fatal("rendering/read changed decision meaning")
		}
	}
	if after := counts(); after != before || client.calls != 0 {
		t.Fatalf("reads wrote rows or used HTTP: before=%v after=%v calls=%d", before, after, client.calls)
	}
}
