package local_test

import (
	"errors"
	"github.com/specgate/specgate/app/cli/internal/local"
	"os"
	"path/filepath"
	"strings"
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

func TestWatchedPathRejectsSymlinkedParent(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "private.txt"), []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := local.ResolveVerificationFile(root, "linked/private.txt"); err == nil {
		t.Fatal("accepted file outside repository")
	}
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

func TestExportWorkspaceRejectsSourceCriteria(t *testing.T) {
	s, err := local.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	selection, err := s.Initialize(t.Context(), local.InitInput{WorkspaceName: "Source criteria", Username: "human", DisplayName: "Human"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PublishArtifact(t.Context(), selection.Workspace.ID, local.ArtifactInput{
		FeatureKey:     "SOURCE-CRITERIA-EXPORT",
		RequestType:    "new_feature",
		Documents:      []local.ArtifactDocumentInput{{Path: "spec.md", Role: "spec", Content: []byte("# Spec")}},
		SourceCriteria: []local.SourceCriterion{{ID: "req-1", Text: "Persist source coverage", SourcePath: "spec.md"}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ExportWorkspace(t.Context(), selection.Workspace.ID); err == nil || !strings.Contains(err.Error(), "source criteria") || !strings.Contains(err.Error(), "Local database backup") {
		t.Fatalf("export with source criteria error = %v", err)
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

func TestReportEnabledPinRequiresExactSelectorsAndRecordsWatchedBytes(t *testing.T) {
	s, w, root := verificationFixture(t)
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	in := verificationInput(w)
	in.WatchedPaths = []string{"go.mod"}
	in.Checks[0].TestReport = &local.JUnitTestReport{Format: "junit", Selectors: map[string][]local.SelectedTestCase{
		"local-1": {{ClassName: "pkg.Test", Name: "works"}},
	}}
	c, err := s.PinVerificationContract(t.Context(), w.WorkspaceID, w.Key, root, "human", in)
	if err != nil {
		t.Fatal(err)
	}
	if c.Version != 2 || len(c.WatchedPaths) != 1 || c.WatchedPaths[0].Path != "go.mod" || c.WatchedPaths[0].Digest == "" {
		t.Fatalf("v2 contract = %#v", c)
	}
	if c.Digest == "" || c.Digest == verificationInput(w).ContextDigest {
		t.Fatalf("missing v2 digest: %#v", c)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	drift := local.WatchedPathDriftFor(root, c.WatchedPaths)
	if len(drift) != 1 || drift[0].State != "changed" || drift[0].CurrentDigest == drift[0].PinnedDigest {
		t.Fatalf("watched drift = %#v", drift)
	}
}

func TestReportEnabledPinRejectsMissingOrAmbiguousSelectors(t *testing.T) {
	for _, selectors := range []map[string][]local.SelectedTestCase{
		nil,
		{"local-1": {{ClassName: "pkg.Test", Name: "works"}, {ClassName: "pkg.Test", Name: "works"}}},
		{"unknown": {{ClassName: "pkg.Test", Name: "works"}}},
	} {
		s, w, root := verificationFixture(t)
		in := verificationInput(w)
		in.Checks[0].TestReport = &local.JUnitTestReport{Format: "junit", Selectors: selectors}
		if _, err := s.PinVerificationContract(t.Context(), w.WorkspaceID, w.Key, root, "human", in); err == nil {
			t.Fatalf("accepted selectors %#v", selectors)
		}
	}
}

func TestWatchedFilesBoundedAndMissingState(t *testing.T) {
	s, w, root := verificationFixture(t)
	path := filepath.Join(root, "large.bin")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(16*1024*1024 + 1); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	in := verificationInput(w)
	in.WatchedPaths = []string{"large.bin"}
	if _, err := s.PinVerificationContract(t.Context(), w.WorkspaceID, w.Key, root, "human", in); err == nil {
		t.Fatal("accepted oversized watched file")
	}
	drift := local.WatchedPathDriftFor(root, []local.WatchedPath{{Path: "missing.txt", Digest: "old"}})
	if drift[0].State != "missing" {
		t.Fatalf("missing state = %s", drift[0].State)
	}
}
