package local_test

import (
	"encoding/json"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/local"
)

func TestCheckpointRetainsExactDeliveryIdentities(t *testing.T) {
	s, work, root := verificationFixture(t)
	before, err := s.CreateCheckpointWithFiles(t.Context(), work.WorkspaceID, work.Key, "before", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	review, err := s.SubmitDelivery(t.Context(), work.WorkspaceID, work.Key, verificationReport(work, ""), root)
	if err != nil {
		t.Fatal(err)
	}
	after, err := s.CreateCheckpointWithFiles(t.Context(), work.WorkspaceID, work.Key, "after", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SubmitDelivery(t.Context(), work.WorkspaceID, work.Key, verificationReport(work, ""), root); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ id, reviewID, completionID string }{{before.ID, "", ""}, {after.ID, review.ID, review.ReportID}} {
		stored, err := s.GetCheckpoint(t.Context(), work.WorkspaceID, work.Key, tc.id)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(stored)
		if err != nil {
			t.Fatal(err)
		}
		var identity struct {
			ReviewID     string `json:"review_id"`
			CompletionID string `json:"completion_id"`
		}
		if err := json.Unmarshal(encoded, &identity); err != nil {
			t.Fatal(err)
		}
		if identity.ReviewID != tc.reviewID || identity.CompletionID != tc.completionID {
			t.Fatalf("checkpoint lost historical delivery identities: got=%+v want=%+v", identity, tc)
		}
	}
}

func TestCheckpointIsAppendOnlyAndWorkScoped(t *testing.T) {
	s, work, _ := verificationFixture(t)
	first, err := s.CreateCheckpointWithFiles(t.Context(), work.WorkspaceID, work.Key, "checkout-a", nil, "before handoff")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CreateCheckpointWithFiles(t.Context(), work.WorkspaceID, work.Key, "checkout-b", nil, "after handoff")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID || first.ContextDigest != work.ContextDigest {
		t.Fatalf("checkpoints = %#v %#v", first, second)
	}
	for _, expected := range []struct{ id, fingerprint, note string }{
		{first.ID, "checkout-a", "before handoff"},
		{second.ID, "checkout-b", "after handoff"},
	} {
		stored, err := s.GetCheckpoint(t.Context(), work.WorkspaceID, work.Key, expected.id)
		if err != nil || stored.CheckoutFingerprint != expected.fingerprint || stored.Note != expected.note {
			t.Fatalf("historical checkpoint changed: %#v %v", stored, err)
		}
	}
	other, err := s.CreateQuickWork(t.Context(), work.WorkspaceID, local.QuickWorkInput{Title: "Other", AcceptanceCriteria: []string{"works"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetCheckpoint(t.Context(), work.WorkspaceID, other.Key, first.ID); err == nil {
		t.Fatalf("foreign checkpoint accepted: %v", err)
	}
}

func TestLatestCheckpointForCheckoutUsesOnlyMatchingSnapshot(t *testing.T) {
	s, work, _ := verificationFixture(t)
	first, err := s.CreateCheckpointWithFiles(t.Context(), work.WorkspaceID, work.Key, "a", nil, "", local.CheckoutSnapshot{Version: 1, State: "available", CheckoutID: "one", Head: "a", Tree: "a", Fingerprint: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateCheckpointWithFiles(t.Context(), work.WorkspaceID, work.Key, "b", nil, "", local.CheckoutSnapshot{Version: 1, State: "available", CheckoutID: "two", Head: "b", Tree: "b", Fingerprint: "b"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.LatestCheckpointForCheckout(t.Context(), work.WorkspaceID, work.Key, "one")
	if err != nil || got.ID != first.ID {
		t.Fatalf("matching checkpoint = %#v %v", got, err)
	}
	if _, err := s.LatestCheckpointForCheckout(t.Context(), work.WorkspaceID, work.Key, "three"); err == nil {
		t.Fatal("foreign checkout received a baseline")
	}
	latest, err := s.CreateCheckpointWithFiles(t.Context(), work.WorkspaceID, work.Key, "c", nil, "", local.CheckoutSnapshot{Version: 1, State: "available", CheckoutID: "one", Head: "c", Tree: "c", Fingerprint: "c"})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.LatestCheckpointForCheckout(t.Context(), work.WorkspaceID, work.Key, "one"); err != nil || got.ID != latest.ID {
		t.Fatalf("newest same-checkout baseline = %#v %v", got, err)
	}
}
