package command_test

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/local"
)

func TestAcceptanceStatusCannotMixConcurrentCompletionWithOldReview(t *testing.T) {
	for _, family := range []string{"change", "delivery", "work"} {
		t.Run(family, func(t *testing.T) {
			deps, _, _, out := newFakeDeps(t)
			dir, store, sel, work := newLocalChangeWork(t, deps)
			submitLocalChangeDelivery(t, store, sel.Workspace.ID, work, "first", "old-head")
			if _, err := store.CreateCheckpointWithFiles(t.Context(), sel.Workspace.ID, work.Key, "old", nil, ""); err != nil {
				t.Fatal(err)
			}
			closeLocalChangeStore(t, deps, dir, store)
			var newest local.DeliveryReview
			deps.DeployRunner = &fakeDeployRunner{Err: errors.New("no git"), OnCommand: func() {
				writer, err := local.Open(filepath.Join(dir, "state.db"))
				if err != nil {
					t.Fatal(err)
				}
				defer writer.Close()
				submitLocalChangeDelivery(t, writer, sel.Workspace.ID, work, "second", "new-head")
				newest, err = writer.DeliveryStatus(t.Context(), sel.Workspace.ID, work.Key)
				if err != nil {
					t.Fatal(err)
				}
			}}
			verb := "status"
			if family == "work" {
				verb = "resume"
			}
			if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", family, verb, work.Key); code != 0 {
				t.Fatalf("status: %d %s", code, out.String())
			}
			var got struct {
				Data struct {
					ID     string                `json:"id"`
					Review string                `json:"review_id"`
					Basis  local.AcceptanceBasis `json:"acceptance_basis"`
					Status struct {
						Review string                `json:"review_id"`
						Basis  local.AcceptanceBasis `json:"acceptance_basis"`
					} `json:"status"`
				} `json:"data"`
			}
			if err := json.Unmarshal(out.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			reviewID := got.Data.Review
			if family == "delivery" {
				reviewID = got.Data.ID
			}
			if family == "work" {
				reviewID, got.Data.Basis = got.Data.Status.Review, got.Data.Status.Basis
			}
			if newest.ID == "" || reviewID != got.Data.Basis.ReviewID {
				t.Fatalf("mixed status/basis: status=%s basis=%s newest=%s", reviewID, got.Data.Basis.ReviewID, newest.ID)
			}
		})
	}
}

func TestAcceptanceStatusIncludesPeerWrittenDuringCheckoutRead(t *testing.T) {
	deps, _, _, out := newFakeDeps(t)
	dir, store, sel, work := newLocalChangeWork(t, deps)
	submitLocalChangeDelivery(t, store, sel.Workspace.ID, work, "builder", "head")
	if _, err := store.CreateCheckpointWithFiles(t.Context(), sel.Workspace.ID, work.Key, "old", nil, ""); err != nil {
		t.Fatal(err)
	}
	closeLocalChangeStore(t, deps, dir, store)
	deps.DeployRunner = &fakeDeployRunner{Err: errors.New("no git"), OnCommand: func() {
		writer, err := local.Open(filepath.Join(dir, "state.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer writer.Close()
		_, err = writer.PeerReviewDelivery(t.Context(), sel.Workspace.ID, work.Key, map[string]any{
			"agent":          map[string]any{"name": "reviewer"},
			"peer_review_of": map[string]any{"completion_feedback_event_id": latestLocalReportID(t, writer, sel.Workspace.ID, work.Key), "git_receipt": map[string]any{"head_revision": "head"}},
			"criteria":       []any{map[string]any{"criterion_id": "local-1", "claim": "partial", "evidence": map[string]any{"heading": "missing behavior"}}},
		})
		if err != nil {
			t.Fatal(err)
		}
	}}
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "change", "status", work.Key); code != 0 {
		t.Fatalf("status: %d %s", code, out.String())
	}
	var got struct {
		Data struct {
			Peer  string                `json:"peer_state"`
			Basis local.AcceptanceBasis `json:"acceptance_basis"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Data.Peer != "failed" || got.Data.Basis.PeerDigest == "" || got.Data.Basis.PeerReview.State != "failed" {
		t.Fatalf("peer omitted from status: %s", out.String())
	}
}
