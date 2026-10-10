package local_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/local"
)

func TestAcceptanceBasisDoesNotImplicitlySelectLatestCheckpoint(t *testing.T) {
	s, w, root := verificationFixture(t)
	if _, err := s.SubmitDelivery(t.Context(), w.WorkspaceID, w.Key, verificationReport(w, ""), root); err != nil {
		t.Fatal(err)
	}
	before, err := s.AcceptanceBasis(t.Context(), w.WorkspaceID, w.Key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateCheckpointWithFiles(t.Context(), w.WorkspaceID, w.Key, "fingerprint", nil, ""); err != nil {
		t.Fatal(err)
	}
	after, err := s.AcceptanceBasis(t.Context(), w.WorkspaceID, w.Key)
	if err != nil {
		t.Fatal(err)
	}
	if before.Digest != after.Digest || after.CheckpointID != "" {
		t.Fatal("unselected checkpoint changed acceptance material")
	}
}

func TestAcceptanceRejectsNoncomparableCheckpointSelection(t *testing.T) {
	s, w, root := verificationFixture(t)
	if _, err := s.SubmitDelivery(t.Context(), w.WorkspaceID, w.Key, verificationReport(w, ""), root); err != nil {
		t.Fatal(err)
	}
	snapshot := local.CheckoutSnapshot{Version: 1, State: "available", CheckoutID: "other-checkout", Head: "head", Tree: "tree", Fingerprint: "fingerprint"}
	checkpoint, err := s.CreateCheckpointWithFiles(t.Context(), w.WorkspaceID, w.Key, snapshot.Fingerprint, nil, "", snapshot)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ checkout, state string }{
		{"current-checkout", "noncomparable"},
		{"other-checkout", "unavailable"},
		{"current-checkout", "unchanged"},
	} {
		_, err := s.AcceptanceBasis(t.Context(), w.WorkspaceID, w.Key, local.AcceptanceOptions{
			RepoRoot: root, CheckoutID: tc.checkout, CheckpointID: checkpoint.ID,
			CheckpointCompare: &local.AcceptanceCheckpointCompare{State: tc.state, BaselineKind: "checkpoint", BaselineID: checkpoint.ID},
		})
		if !errors.Is(err, local.ErrVerificationInvalid) {
			t.Fatalf("invalid checkout=%s state=%s selection succeeded: %v", tc.checkout, tc.state, err)
		}
	}
}

func TestExplicitBasisOnLegacyWorkUpgradesStore(t *testing.T) {
	s, w, root := verificationFixture(t)
	if _, err := s.SubmitDelivery(t.Context(), w.WorkspaceID, w.Key, verificationReport(w, ""), root); err != nil {
		t.Fatal(err)
	}
	before, err := s.PendingStoreUpgrade(t.Context())
	if err != nil || before == nil {
		t.Fatalf("expected legacy store: %v", err)
	}
	basis, err := s.AcceptanceBasis(t.Context(), w.WorkspaceID, w.Key)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DecideDeliveryWithBasis(t.Context(), w.WorkspaceID, w.Key, "reject", "", "reviewed", basis.ReviewID, basis.Digest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(before.BackupPath); err != nil {
		t.Fatalf("basis bypassed backup/guard: %v", err)
	}
	after, err := s.PendingStoreUpgrade(t.Context())
	if err != nil || after != nil {
		t.Fatalf("not upgraded: %#v %v", after, err)
	}
	inspection, err := s.InspectAcceptance(t.Context(), w.WorkspaceID, w.Key, local.AcceptanceOptions{RepoRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	recorded := inspection.Recorded
	if recorded == nil || recorded.Actor != "human" {
		t.Fatalf("actor not normalized: %#v", recorded)
	}
}

func TestAcceptanceRejectsChangedWatchedBytes(t *testing.T) {
	s, w, root := verificationFixture(t)
	path := filepath.Join(root, "watched.txt")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	in := verificationInput(w)
	in.WatchedPaths = []string{"watched.txt"}
	pin, err := s.PinVerificationContract(t.Context(), w.WorkspaceID, w.Key, root, "human", in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SubmitDelivery(t.Context(), w.WorkspaceID, w.Key, verificationReport(w, pin.Digest), root); err != nil {
		t.Fatal(err)
	}
	opts := local.AcceptanceOptions{RepoRoot: root}
	basis, err := s.AcceptanceBasis(t.Context(), w.WorkspaceID, w.Key, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("weakened"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.DecideDeliveryWithBasis(t.Context(), w.WorkspaceID, w.Key, "approve", "human", "", basis.ReviewID, basis.Digest, opts); err == nil {
		t.Fatal("accepted stale watched evidence")
	}
}

func TestAcceptanceRejectsUnrelatedImpactPair(t *testing.T) {
	s, w, root := verificationFixture(t)
	if _, err := s.SubmitDelivery(t.Context(), w.WorkspaceID, w.Key, verificationReport(w, ""), root); err != nil {
		t.Fatal(err)
	}
	base, err := s.PublishArtifact(t.Context(), w.WorkspaceID, local.ArtifactInput{FeatureKey: "UNRELATED", RequestType: "new_feature", Documents: []local.ArtifactDocumentInput{{Path: "spec.md", Role: "spec", Content: []byte("base")}}})
	if err != nil {
		t.Fatal(err)
	}
	target, err := s.PublishArtifact(t.Context(), w.WorkspaceID, local.ArtifactInput{FeatureKey: "UNRELATED", RequestType: "change_request", BaseVersion: "v1", Documents: []local.ArtifactDocumentInput{{Path: "spec.md", Role: "spec", Content: []byte("target")}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AcceptanceBasis(t.Context(), w.WorkspaceID, w.Key, local.AcceptanceOptions{ImpactBase: base.ID, ImpactTarget: target.ID}); !errors.Is(err, local.ErrVerificationInvalid) {
		t.Fatalf("quick work must reject an existing unrelated impact pair: %v", err)
	}
}

func TestEnhancedDecisionRequiresExactAcceptanceBasis(t *testing.T) {
	s, work, root := verificationFixture(t)
	input := verificationInput(work)
	input.Checks[0].TestReport = &local.JUnitTestReport{Format: "junit", Selectors: map[string][]local.SelectedTestCase{
		"local-1": {{ClassName: "pkg", Name: "works"}},
	}}
	contract, err := s.PinVerificationContract(t.Context(), work.WorkspaceID, work.Key, root, "human", input)
	if err != nil {
		t.Fatal(err)
	}
	body := verificationReport(work, contract.Digest)
	body["criteria"] = []any{map[string]any{"criterion_id": "local-1", "claim": "satisfied", "evidence": map[string]any{"heading": "proof"}}}
	if _, err := s.SubmitDelivery(t.Context(), work.WorkspaceID, work.Key, body, root); err != nil {
		t.Fatal(err)
	}
	review, err := s.DeliveryStatus(t.Context(), work.WorkspaceID, work.Key)
	if err != nil {
		t.Fatal(err)
	}
	if review.Verdict == "passed" {
		t.Fatal("report-enabled check passed without an observed report")
	}
	if err := s.DecideDeliveryWithBasis(t.Context(), work.WorkspaceID, work.Key, "approve", "human", "", review.ID, ""); err == nil || !strings.Contains(err.Error(), "basis") {
		t.Fatalf("missing basis decision = %v", err)
	}
	basis, err := s.AcceptanceBasis(t.Context(), work.WorkspaceID, work.Key)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DecideDeliveryWithBasis(t.Context(), work.WorkspaceID, work.Key, "approve", "human", "", review.ID, basis.Digest); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentHumanDecisionsCannotOverwriteRecordedBasis(t *testing.T) {
	s, w, root := verificationFixture(t)
	if err := s.EnableEnhancedStore(t.Context()); err != nil {
		t.Fatal(err)
	}
	review, err := s.SubmitDelivery(t.Context(), w.WorkspaceID, w.Key, verificationReport(w, ""), root)
	if err != nil {
		t.Fatal(err)
	}
	opts := local.AcceptanceOptions{RepoRoot: root}
	basis, err := s.AcceptanceBasis(t.Context(), w.WorkspaceID, w.Key, opts)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, decision := range []string{"approve", "reject"} {
		wg.Add(1)
		go func(decision string) {
			defer wg.Done()
			<-start
			results <- s.DecideDeliveryWithBasis(t.Context(), w.WorkspaceID, w.Key, decision, "human-"+decision, "", review.ID, basis.Digest, opts)
		}(decision)
	}
	close(start)
	wg.Wait()
	close(results)
	succeeded, conflicted := 0, 0
	for err := range results {
		if err == nil {
			succeeded++
		} else if errors.Is(err, local.ErrDecisionRecorded) {
			conflicted++
		} else {
			t.Fatalf("concurrent decision error = %v", err)
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("success/conflict = %d/%d", succeeded, conflicted)
	}
	inspection, err := s.InspectAcceptance(t.Context(), w.WorkspaceID, w.Key, opts)
	recorded := inspection.Recorded
	if err != nil || recorded == nil || recorded.Digest != basis.Digest || (recorded.Decision != "approve" && recorded.Decision != "reject") {
		t.Fatalf("recorded decision basis = %#v, err=%v", recorded, err)
	}
}
