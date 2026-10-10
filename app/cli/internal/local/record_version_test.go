package local

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestUnsupportedLocalRecordVersionsAreIncompatible(t *testing.T) {
	for _, kind := range []string{"verification", "checkpoint", "acceptance", "lineage", "observed_run"} {
		t.Run(kind, func(t *testing.T) {
			s, err := Open(filepath.Join(t.TempDir(), "state.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			sel, err := s.Initialize(t.Context(), InitInput{WorkspaceName: "versions", Username: "human", DisplayName: "Human"})
			if err != nil {
				t.Fatal(err)
			}
			work, err := s.CreateQuickWork(t.Context(), sel.Workspace.ID, QuickWorkInput{Title: "record versions", AcceptanceCriteria: []string{"Works"}})
			if err != nil {
				t.Fatal(err)
			}
			exec := func(statement string, args ...any) {
				t.Helper()
				if _, err := s.db.ExecContext(t.Context(), statement, args...); err != nil {
					t.Fatal(err)
				}
			}
			var got error
			switch kind {
			case "observed_run":
				review, err := s.SubmitDelivery(t.Context(), work.WorkspaceID, work.Key, map[string]any{"context_digest": work.ContextDigest, "agent": map[string]any{"name": "builder"}}, t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				exec(`UPDATE delivery_reports SET body=? WHERE id=?`, `{"checks":[{"test_run":{"version":99}}]}`, review.ReportID)
				_, got = s.LatestDeliveryReport(t.Context(), work.WorkspaceID, work.Key)
				if _, err := s.InspectAcceptance(t.Context(), work.WorkspaceID, work.Key, AcceptanceOptions{}); !errors.Is(err, ErrStoreIncompatible) {
					t.Fatalf("inspection observed run version accepted: %v", err)
				}
			case "lineage":
				artifact, err := s.PublishArtifact(t.Context(), work.WorkspaceID, ArtifactInput{FeatureKey: "versions", RequestType: "new_feature", Documents: []ArtifactDocumentInput{{Path: "spec.md", Role: "spec", Content: []byte("spec")}}})
				if err != nil {
					t.Fatal(err)
				}
				exec(`UPDATE artifacts SET source_lineage_json=? WHERE id=?`, `{"version":99}`, artifact.ID)
				_, got = s.GetArtifact(t.Context(), work.WorkspaceID, artifact.ID)
				if _, err := s.ListArtifacts(t.Context(), work.WorkspaceID); !errors.Is(err, ErrStoreIncompatible) {
					t.Fatalf("artifact list version accepted: %v", err)
				}
			case "verification":
				exec(`INSERT INTO verification_contracts(workspace_id,work_id,body) VALUES(?,?,?)`, work.WorkspaceID, work.ID, `{"version":99}`)
				_, got = s.GetVerificationContract(t.Context(), work.WorkspaceID, work.Key)
			case "checkpoint":
				cp, err := s.CreateCheckpointWithFiles(t.Context(), work.WorkspaceID, work.Key, "fingerprint", nil, "")
				if err != nil {
					t.Fatal(err)
				}
				exec(`UPDATE work_checkpoints SET body=? WHERE id=?`, `{"snapshot":{"version":99}}`, cp.ID)
				_, got = s.GetCheckpoint(t.Context(), work.WorkspaceID, work.Key, cp.ID)
				if _, err := s.LatestCheckpointForCheckout(t.Context(), work.WorkspaceID, work.Key, "current-checkout"); !errors.Is(err, ErrStoreIncompatible) {
					t.Fatalf("latest checkpoint version accepted: %v", err)
				}
			case "acceptance":
				review, err := s.SubmitDelivery(t.Context(), work.WorkspaceID, work.Key, map[string]any{"context_digest": work.ContextDigest, "agent": map[string]any{"name": "builder"}}, t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				exec(`INSERT INTO acceptance_bases(review_id,workspace_id,work_id,digest,body,created_at) VALUES(?,?,?,?,?,?)`, review.ID, work.WorkspaceID, work.ID, "unknown", `{"version":99}`, "now")
				_, got = s.InspectAcceptance(t.Context(), work.WorkspaceID, work.Key, AcceptanceOptions{})
			}
			if !errors.Is(got, ErrStoreIncompatible) {
				t.Fatalf("unsupported %s record accepted: %v", kind, got)
			}
		})
	}
}
