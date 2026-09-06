package local_test

import (
	"errors"
	"github.com/specgate/specgate/app/cli/internal/local"
	"os"
	"path/filepath"
	"testing"
)

func verificationFixture(t *testing.T) (*local.Store, local.WorkItem, string) {
	t.Helper()
	root := t.TempDir()
	s, err := local.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	sel, err := s.Initialize(t.Context(), local.InitInput{WorkspaceName: "Verification", Username: "human", DisplayName: "Human"})
	if err != nil {
		t.Fatal(err)
	}
	w, err := s.CreateQuickWork(t.Context(), sel.Workspace.ID, local.QuickWorkInput{Title: "Verify", AcceptanceCriteria: []string{"Tests pass @check:unit"}})
	if err != nil {
		t.Fatal(err)
	}
	return s, w, root
}
func verificationInput(w local.WorkItem) local.VerificationContractInput {
	return local.VerificationContractInput{ContextDigest: w.ContextDigest, Shell: "sh", Checks: []local.VerificationCheck{{Name: "unit", Command: "go test ./...", Cwd: "."}}}
}
func verificationReport(w local.WorkItem, digest string) map[string]any {
	return map[string]any{"agent": map[string]any{"name": "builder"}, "context_digest": w.ContextDigest, "verification_contract_digest": digest,
		"checks": []any{map[string]any{"name": "unit", "command": "go test ./...", "cwd": ".", "status": "pass"}}}
}
func TestVerificationContractRoundTripImmutable(t *testing.T) {
	s, w, root := verificationFixture(t)
	before, err := s.ContextPack(t.Context(), w.WorkspaceID, w.Key)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.GetVerificationContract(t.Context(), w.WorkspaceID, w.Key)
	if err != nil || c.Status != "unconfigured" {
		t.Fatalf("legacy = %#v %v", c, err)
	}
	c, err = s.PinVerificationContract(t.Context(), w.WorkspaceID, w.Key, root, "human", verificationInput(w))
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetVerificationContract(t.Context(), w.WorkspaceID, w.Key)
	if err != nil || got.Digest != c.Digest || got.Status != "pinned" || got.Bindings["local-1"] != "unit" || got.Actor != "human" {
		t.Fatalf("roundtrip = %#v %v", got, err)
	}
	after, err := s.ContextPack(t.Context(), w.WorkspaceID, w.Key)
	if err != nil || after.Digest != before.Digest || after.Markdown != before.Markdown {
		t.Fatal("pin changed Context Pack")
	}
	if _, err := s.PinVerificationContract(t.Context(), w.WorkspaceID, w.Key, root, "human", verificationInput(w)); !errors.Is(err, local.ErrVerificationConflict) {
		t.Fatalf("duplicate pin = %v", err)
	}
	if _, err := s.PreviewVerificationContract(t.Context(), w.WorkspaceID, w.Key, root, "human", verificationInput(w)); !errors.Is(err, local.ErrVerificationConflict) {
		t.Fatalf("preview permits repin: %v", err)
	}
	body := verificationReport(w, c.Digest)
	if err := s.ValidateVerificationReport(t.Context(), w.WorkspaceID, w.Key, root, body); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SubmitDelivery(t.Context(), w.WorkspaceID, w.Key, body, root); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ExportWorkspace(t.Context(), w.WorkspaceID); err == nil {
		t.Fatal("portable export discarded pin")
	}
}
func TestVerificationContractInvalidPins(t *testing.T) {
	for _, kind := range []string{"empty", "unknown", "missing", "duplicate", "shell", "absolute", "escape", "symlink", "context", "late"} {
		t.Run(kind, func(t *testing.T) {
			s, w, root := verificationFixture(t)
			in := verificationInput(w)
			switch kind {
			case "empty":
				in.Checks[0].Command = " "
			case "unknown":
				in.Checks[0].Name = "other"
			case "missing":
				in.Checks = nil
			case "duplicate":
				in.Checks = append(in.Checks, in.Checks[0])
			case "shell":
				in.Shell = "bash"
			case "absolute":
				in.Checks[0].Cwd = root
			case "escape":
				in.Checks[0].Cwd = "../outside"
			case "symlink":
				if err := os.Symlink(t.TempDir(), filepath.Join(root, "outside")); err != nil {
					t.Fatal(err)
				}
				in.Checks[0].Cwd = "outside"
			case "context":
				in.ContextDigest = "wrong"
			case "late":
				if _, err := s.SubmitDelivery(t.Context(), w.WorkspaceID, w.Key, verificationReport(w, "")); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.PinVerificationContract(t.Context(), w.WorkspaceID, w.Key, root, "human", in); err == nil {
				t.Fatalf("accepted %s pin", kind)
			}
			c, err := s.GetVerificationContract(t.Context(), w.WorkspaceID, w.Key)
			if err != nil || c.Status != "unconfigured" {
				t.Fatalf("invalid pin persisted: %#v %v", c, err)
			}
		})
	}
}
func TestVerificationReportRejectsMismatchAtPersistence(t *testing.T) {
	for _, kind := range []string{"command", "cwd", "missing", "duplicate", "extra", "digest"} {
		t.Run(kind, func(t *testing.T) {
			s, w, root := verificationFixture(t)
			c, err := s.PinVerificationContract(t.Context(), w.WorkspaceID, w.Key, root, "human", verificationInput(w))
			if err != nil {
				t.Fatal(err)
			}
			body := verificationReport(w, c.Digest)
			checks := body["checks"].([]any)
			check := checks[0].(map[string]any)
			switch kind {
			case "command":
				check["command"] = "true"
			case "cwd":
				check["cwd"] = "other"
			case "missing":
				body["checks"] = []any{}
			case "duplicate":
				body["checks"] = append(checks, check)
			case "extra":
				body["checks"] = append(checks, map[string]any{"name": "extra", "command": "true", "cwd": "."})
			case "digest":
				body["verification_contract_digest"] = "wrong"
			}
			if err := s.ValidateVerificationReport(t.Context(), w.WorkspaceID, w.Key, root, body); err == nil {
				t.Fatal("preflight accepted mismatch")
			}
			if _, err := s.SubmitDelivery(t.Context(), w.WorkspaceID, w.Key, body, root); err == nil {
				t.Fatal("persistence accepted mismatch")
			}
		})
	}
}
