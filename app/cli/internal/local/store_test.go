package local_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/local"
)

func TestOpenRejectsSymlinkedDatabase(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.db")
	store, err := local.Open(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "state.db")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	if store, err := local.Open(link); err == nil {
		store.Close()
		t.Fatal("opened symlinked Local database")
	}
}

func TestOpenRejectsSymlinkedStateDirectory(t *testing.T) {
	target := t.TempDir()
	stateDir := filepath.Join(t.TempDir(), "local")
	if err := os.Symlink(target, stateDir); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	if store, err := local.Open(filepath.Join(stateDir, "state.db")); err == nil {
		store.Close()
		t.Fatal("opened Local database through a symlinked state directory")
	}
}

func TestOpenAllowsDarwinTmpRootAlias(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin standard-root alias regression")
	}
	dir, err := os.MkdirTemp("/tmp", "specgate-local-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	store, err := local.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatalf("open below /tmp alias: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestOpenRejectsSymlinkedAncestorBeforeCreatingState(t *testing.T) {
	external := t.TempDir()
	ancestor := filepath.Join(t.TempDir(), "linked")
	if err := os.Symlink(external, ancestor); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	stateDir := filepath.Join(ancestor, "nested", "local")

	if store, err := local.Open(filepath.Join(stateDir, "state.db")); err == nil {
		store.Close()
		t.Fatal("opened Local database through a symlinked ancestor")
	}
	if _, err := os.Stat(filepath.Join(external, "nested")); !os.IsNotExist(err) {
		t.Fatalf("state was created through symlinked ancestor; stat err=%v", err)
	}
}

func TestOpenRejectsNonRegularDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}

	if store, err := local.Open(path); err == nil {
		store.Close()
		t.Fatal("opened a directory as a Local database")
	}
}

func TestOpenRepairsExistingDatabasePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := local.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}

	reopened, err := local.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("database mode = %o, want 600", info.Mode().Perm())
	}
}

func TestOpenEscapesSQLiteURICharactersInPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state#local.db")
	store, err := local.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Initialize(context.Background(), local.InitInput{
		WorkspaceName: "Safe path",
		DisplayName:   "Human",
		Username:      "human",
	}); err != nil {
		store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() == 0 {
		t.Fatal("escaped database path remained empty")
	}
	if _, err := os.Stat(filepath.Join(dir, "state")); !os.IsNotExist(err) {
		t.Fatalf("SQLite URI fragment created the wrong database; stat err=%v", err)
	}
}

func TestInitializePersistsStoreUserAndWorkspace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := local.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := store.Initialize(context.Background(), local.InitInput{
		WorkspaceName: "Offline dogfood",
		DisplayName:   "Dogfood Human",
		Username:      "dogfood-human",
	})
	if err != nil {
		t.Fatal(err)
	}
	if selection.StoreID == "" || selection.User.Username != "dogfood-human" || selection.Workspace.Slug != "offline-dogfood" {
		t.Fatalf("selection = %#v", selection)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := local.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	persisted, err := reopened.Current(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if persisted != selection {
		t.Fatalf("persisted = %#v, want %#v", persisted, selection)
	}
}

func TestCreateAndSelectWorkspacePersists(t *testing.T) {
	store, err := local.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.Initialize(context.Background(), local.InitInput{WorkspaceName: "Alpha", DisplayName: "Human", Username: "human"}); err != nil {
		t.Fatal(err)
	}
	beta, err := store.CreateWorkspace(context.Background(), "Beta workspace")
	if err != nil {
		t.Fatal(err)
	}
	if beta.Slug != "beta-workspace" {
		t.Fatalf("workspace = %#v", beta)
	}
	if err := store.SelectWorkspace(context.Background(), beta.Slug); err != nil {
		t.Fatal(err)
	}
	current, err := store.Current(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if current.Workspace != beta {
		t.Fatalf("current workspace = %#v, want %#v", current.Workspace, beta)
	}
}

func TestPublishArtifactVersionsAreImmutableAndWorkspaceScoped(t *testing.T) {
	store, err := local.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	alpha, err := store.Initialize(context.Background(), local.InitInput{WorkspaceName: "Alpha", DisplayName: "Human", Username: "human"})
	if err != nil {
		t.Fatal(err)
	}
	beta, err := store.CreateWorkspace(context.Background(), "Beta")
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.PublishArtifact(context.Background(), alpha.Workspace.ID, local.ArtifactInput{
		FeatureKey:  "LOCAL-ARTIFACTS",
		RequestType: "new_feature",
		Documents:   []local.ArtifactDocumentInput{{Path: "spec.md", Role: "spec", Content: []byte("first")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.PublishArtifact(context.Background(), alpha.Workspace.ID, local.ArtifactInput{
		FeatureKey:  "LOCAL-ARTIFACTS",
		RequestType: "new_feature",
		Documents:   []local.ArtifactDocumentInput{{Path: "spec.md", Role: "spec", Content: []byte("second")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Version != 1 || second.Version != 2 || first.ID == second.ID {
		t.Fatalf("versions = %#v, %#v", first, second)
	}
	stored, err := store.GetArtifact(context.Background(), alpha.Workspace.ID, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if string(stored.Documents[0].Content) != "first" {
		t.Fatalf("first artifact body = %q", stored.Documents[0].Content)
	}
	if stored.PolicyDigest == "" || stored.PolicySnapshot == "" {
		t.Fatalf("stored policy snapshot = %#v", stored)
	}
	items, err := store.ListArtifacts(context.Background(), beta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("beta artifacts = %#v", items)
	}
}

func TestArtifactDocumentsMatchFullPathAndRoleRules(t *testing.T) {
	t.Parallel()
	store, err := local.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	selection, err := store.Initialize(context.Background(), local.InitInput{
		WorkspaceName: "Document metadata", DisplayName: "Human", Username: "human",
	})
	if err != nil {
		t.Fatal(err)
	}

	artifact, err := store.PublishArtifact(context.Background(), selection.Workspace.ID, local.ArtifactInput{
		FeatureKey:  "LOCAL-DOCUMENT-METADATA",
		RequestType: "new_feature",
		Documents: []local.ArtifactDocumentInput{{
			Path: "docs/./spec.md", Role: " Invented ", Content: []byte("# Contract"),
		}},
	})
	if err != nil {
		t.Fatalf("PublishArtifact normalized metadata: %v", err)
	}
	if len(artifact.Documents) != 1 || artifact.Documents[0].Path != "docs/spec.md" || artifact.Documents[0].Role != "unspecified" {
		t.Fatalf("normalized document = %+v", artifact.Documents)
	}

	for index, unsafe := range []string{"../outside.md", "/absolute.md", `docs\windows.md`, "docs/\x00bad.md"} {
		_, err := store.PublishArtifact(context.Background(), selection.Workspace.ID, local.ArtifactInput{
			FeatureKey:  fmt.Sprintf("LOCAL-UNSAFE-%d", index),
			RequestType: "new_feature",
			Documents: []local.ArtifactDocumentInput{{
				Path: unsafe, Role: "spec", Content: []byte("# Unsafe"),
			}},
		})
		if err == nil {
			t.Fatalf("PublishArtifact accepted unsafe document path %q", unsafe)
		}
	}
}

func TestReadinessMustPassBeforeHumanArtifactApproval(t *testing.T) {
	store, err := local.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	selection, err := store.Initialize(context.Background(), local.InitInput{WorkspaceName: "Alpha", DisplayName: "Human", Username: "human"})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := store.PublishArtifact(context.Background(), selection.Workspace.ID, local.ArtifactInput{
		FeatureKey: "LOCAL-READINESS", RequestType: "new_feature",
		Documents: []local.ArtifactDocumentInput{{Path: "spec.md", Role: "spec", Content: []byte("acceptance criteria: works")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ApproveArtifact(context.Background(), selection.Workspace.ID, artifact.ID, "human", ""); err == nil {
		t.Fatal("approval succeeded before readiness")
	}
	run, err := store.RunReadiness(context.Background(), selection.Workspace.ID, artifact.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Aggregate != "not_run" {
		t.Fatalf("readiness = %#v", run)
	}
	completeLocalGateTasks(t, store, selection, artifact.ID)
	if err := store.ApproveArtifact(context.Background(), selection.Workspace.ID, artifact.ID, "human", "approved"); err != nil {
		t.Fatal(err)
	}
	approved, err := store.GetArtifact(context.Background(), selection.Workspace.ID, artifact.ID)
	if err != nil {
		t.Fatal(err)
	}
	if approved.Status != "approved" {
		t.Fatalf("artifact status = %q", approved.Status)
	}
}

func TestWorkAuditRetainsArtifactApprovalActorAndNote(t *testing.T) {
	store, err := local.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	selection, err := store.Initialize(context.Background(), local.InitInput{WorkspaceName: "Alpha", DisplayName: "Human", Username: "human"})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := store.PublishArtifact(context.Background(), selection.Workspace.ID, local.ArtifactInput{
		FeatureKey: "LOCAL-AUDIT", RequestType: "new_feature",
		Documents: []local.ArtifactDocumentInput{{Path: "spec.md", Role: "spec", Content: []byte("approved scope")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RunReadiness(context.Background(), selection.Workspace.ID, artifact.ID); err != nil {
		t.Fatal(err)
	}
	completeLocalGateTasks(t, store, selection, artifact.ID)
	if err := store.ApproveArtifact(context.Background(), selection.Workspace.ID, artifact.ID, "reviewer", "scope is ready"); err != nil {
		t.Fatal(err)
	}
	feature, err := store.PromoteArtifact(context.Background(), selection.Workspace.ID, artifact.ID)
	if err != nil {
		t.Fatal(err)
	}
	work, err := store.CreateWork(context.Background(), selection.Workspace.ID, local.WorkInput{FeatureRef: feature.Key, Title: "Audit approval", AcceptanceCriteria: []string{"Approval is traceable"}})
	if err != nil {
		t.Fatal(err)
	}
	events, err := store.Audit(context.Background(), selection.Workspace.ID, work.Key)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Action != "artifact.approved" || !strings.Contains(events[0].Detail, "reviewer") || !strings.Contains(events[0].Detail, "scope is ready") {
		t.Fatalf("events = %#v", events)
	}
}

func TestPromotedArtifactCreatesContextBoundWork(t *testing.T) {
	store, err := local.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	selection, err := store.Initialize(context.Background(), local.InitInput{WorkspaceName: "Alpha", DisplayName: "Human", Username: "human"})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := store.PublishArtifact(context.Background(), selection.Workspace.ID, local.ArtifactInput{FeatureKey: "LOCAL-WORK", RequestType: "new_feature", Documents: []local.ArtifactDocumentInput{{Path: "spec.md", Role: "spec", Content: []byte("# Local work\n\nAcceptance criteria: works")}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RunReadiness(context.Background(), selection.Workspace.ID, artifact.ID); err != nil {
		t.Fatal(err)
	}
	completeLocalGateTasks(t, store, selection, artifact.ID)
	if err := store.ApproveArtifact(context.Background(), selection.Workspace.ID, artifact.ID, "human", ""); err != nil {
		t.Fatal(err)
	}
	feature, err := store.PromoteArtifact(context.Background(), selection.Workspace.ID, artifact.ID)
	if err != nil {
		t.Fatal(err)
	}
	work, err := store.CreateWork(context.Background(), selection.Workspace.ID, local.WorkInput{FeatureRef: feature.Key, Title: "Implement local workflow", AcceptanceCriteria: []string{"Context Pack is used"}})
	if err != nil {
		t.Fatal(err)
	}
	if work.Phase != "ready" || work.ContextDigest == "" {
		t.Fatalf("work = %#v", work)
	}
	contextPack, err := store.ContextPack(context.Background(), selection.Workspace.ID, work.Key)
	if err != nil {
		t.Fatal(err)
	}
	if contextPack.Digest != work.ContextDigest || !strings.Contains(contextPack.Markdown, "# Local work") {
		t.Fatalf("context = %#v", contextPack)
	}
}

func TestQuickWorkCreatesArtifactFreeImmutableContextPack(t *testing.T) {
	store, err := local.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	selection, err := store.Initialize(context.Background(), local.InitInput{
		WorkspaceName: "Alpha", DisplayName: "Human", Username: "human",
	})
	if err != nil {
		t.Fatal(err)
	}

	work, err := store.CreateQuickWork(context.Background(), selection.Workspace.ID, local.QuickWorkInput{
		Title:              "Fix timeout",
		Description:        "Stop retrying after the configured limit.",
		AcceptanceCriteria: []string{"Retries stop after three attempts @check:unit"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if work.FeatureID != "" || work.ArtifactID != "" || work.Phase != "ready" {
		t.Fatalf("quick work = %#v", work)
	}
	pack, err := store.ContextPack(context.Background(), selection.Workspace.ID, work.Key)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"# Implementation Context Pack",
		"Quick Handoff",
		"Stop retrying after the configured limit.",
		"Retries stop after three attempts @check:unit",
		"Local quick-route policy",
	} {
		if !strings.Contains(pack.Markdown, want) {
			t.Fatalf("Context Pack missing %q:\n%s", want, pack.Markdown)
		}
	}
	if pack.Digest == "" || pack.Digest != work.ContextDigest {
		t.Fatalf("Context Pack digest = %q, work digest = %q", pack.Digest, work.ContextDigest)
	}
}

func TestDeliveryDecisionWithoutReportRoutesToChangeSubmit(t *testing.T) {
	store, err := local.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	selection, err := store.Initialize(context.Background(), local.InitInput{WorkspaceName: "Alpha", DisplayName: "Human", Username: "human"})
	if err != nil {
		t.Fatal(err)
	}
	work := readyLocalWork(t, store, selection)

	err = store.DecideDelivery(context.Background(), selection.Workspace.ID, work.Key, "approve", "human", "looks good")
	if err == nil || !strings.Contains(err.Error(), "specgate change submit "+work.Key) {
		t.Fatalf("decision error = %v, want change submit recovery", err)
	}
}

func TestDeliveryEvidenceBindsContextAndNeedsHumanApproval(t *testing.T) {
	store, err := local.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	selection, err := store.Initialize(context.Background(), local.InitInput{WorkspaceName: "Alpha", DisplayName: "Human", Username: "human"})
	if err != nil {
		t.Fatal(err)
	}
	work := readyLocalWork(t, store, selection)
	review, err := store.SubmitDelivery(context.Background(), selection.Workspace.ID, work.Key, map[string]any{
		"context_digest": work.ContextDigest,
		"agent":          map[string]any{"name": "builder"},
		"criteria":       []any{map[string]any{"criterion_id": "local-1", "claim": "satisfied", "evidence": map[string]any{"summary": "implemented"}}},
		"checks":         []any{map[string]any{"name": "unit", "status": "pass"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if review.Verdict != "passed" {
		t.Fatalf("review = %#v", review)
	}
	if err := store.DecideDelivery(context.Background(), selection.Workspace.ID, work.Key, "approve", "human", "looks good"); err != nil {
		t.Fatal(err)
	}
	updated, err := store.GetWork(context.Background(), selection.Workspace.ID, work.Key)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Phase != "delivered" {
		t.Fatalf("phase = %q", updated.Phase)
	}
	events, err := store.Audit(context.Background(), selection.Workspace.ID, work.Key)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 || events[0].Action != "artifact.approved" || events[1].Action != "delivery.reviewed" || events[2].Action != "delivery.approve" || !strings.Contains(events[2].Detail, "human") || !strings.Contains(events[2].Detail, "looks good") {
		t.Fatalf("events = %#v", events)
	}
	stats, err := store.Stats(context.Background(), selection.Workspace.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stats.WorkItems != 1 || stats.Delivered != 1 || stats.DeliveryReviews != 1 {
		t.Fatalf("stats = %#v", stats)
	}
}

func TestSubmitDeliveryRejectsMissingCompletionAgent(t *testing.T) {
	store, err := local.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	selection, err := store.Initialize(context.Background(), local.InitInput{WorkspaceName: "Alpha", DisplayName: "Human", Username: "human"})
	if err != nil {
		t.Fatal(err)
	}
	work := readyLocalWork(t, store, selection)

	_, err = store.SubmitDelivery(context.Background(), selection.Workspace.ID, work.Key, map[string]any{
		"context_digest": work.ContextDigest,
	})
	if err == nil || !strings.Contains(err.Error(), "completion agent.name is required") {
		t.Fatalf("SubmitDelivery error = %v", err)
	}
}

func TestPeerReviewRequiresDifferentAgentAndMatchingLatestReceipt(t *testing.T) {
	store, err := local.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	selection, err := store.Initialize(context.Background(), local.InitInput{WorkspaceName: "Alpha", DisplayName: "Human", Username: "human"})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := store.PublishArtifact(context.Background(), selection.Workspace.ID, local.ArtifactInput{
		FeatureKey: "LOCAL-PEER", RequestType: "new_feature",
		Documents: []local.ArtifactDocumentInput{{Path: "spec.md", Role: "spec", Content: []byte("# Spec")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RunReadiness(context.Background(), selection.Workspace.ID, artifact.ID); err != nil {
		t.Fatal(err)
	}
	completeLocalGateTasks(t, store, selection, artifact.ID)
	if err := store.ApproveArtifact(context.Background(), selection.Workspace.ID, artifact.ID, "human", ""); err != nil {
		t.Fatal(err)
	}
	feature, err := store.PromoteArtifact(context.Background(), selection.Workspace.ID, artifact.ID)
	if err != nil {
		t.Fatal(err)
	}
	work, err := store.CreateWork(context.Background(), selection.Workspace.ID, local.WorkInput{FeatureRef: feature.Key, Title: "Peer review", AcceptanceCriteria: []string{"Implementation works"}})
	if err != nil {
		t.Fatal(err)
	}
	receipt := map[string]any{"repository": "https://example.test/project.git", "head_revision": "abc"}
	completion := map[string]any{
		"context_digest": work.ContextDigest,
		"agent":          map[string]any{"name": "builder"},
		"git_receipt":    receipt,
		"criteria":       []any{map[string]any{"criterion_id": "local-1", "claim": "satisfied", "evidence": map[string]any{"summary": "implemented"}}},
		"checks":         []any{map[string]any{"name": "tests", "status": "pass"}},
	}
	if _, err := store.SubmitDelivery(context.Background(), selection.Workspace.ID, work.Key, completion); err != nil {
		t.Fatal(err)
	}
	latest, err := store.LatestDeliveryReport(context.Background(), selection.Workspace.ID, work.Key)
	if err != nil {
		t.Fatal(err)
	}
	peer := map[string]any{
		"agent":          map[string]any{"name": "reviewer"},
		"peer_review_of": map[string]any{"completion_feedback_event_id": latest.ID, "git_receipt": receipt},
		"criteria":       []any{map[string]any{"criterion_id": "local-1", "claim": "satisfied", "evidence": map[string]any{"summary": "reviewed code"}}},
	}
	if _, err := store.PeerReviewDelivery(context.Background(), selection.Workspace.ID, work.Key, peer); err != nil {
		t.Fatalf("PeerReviewDelivery: %v", err)
	}
	peer["criteria"] = []any{map[string]any{"criterion_id": "local-1", "claim": "SATISFIED", "evidence": map[string]any{"summary": "reviewed code"}}}
	if _, err := store.PeerReviewDelivery(context.Background(), selection.Workspace.ID, work.Key, peer); err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("uppercase peer claim error = %v, want exact enum validation", err)
	}
	peer["criteria"] = []any{map[string]any{"criterion_id": "local-1", "claim": "satisfied", "evidence": map[string]any{"summary": "reviewed code"}}}
	peerStatus, err := store.PeerReviewStatus(context.Background(), selection.Workspace.ID, work.Key)
	if err != nil || peerStatus.State != "passed" || peerStatus.AgentName != "reviewer" {
		t.Fatalf("peer status = %#v, err = %v", peerStatus, err)
	}
	completion["summary"] = "new completion"
	if _, err := store.SubmitDelivery(context.Background(), selection.Workspace.ID, work.Key, completion); err != nil {
		t.Fatal(err)
	}
	peerStatus, err = store.PeerReviewStatus(context.Background(), selection.Workspace.ID, work.Key)
	if err != nil || peerStatus.State != "stale" {
		t.Fatalf("stale peer status = %#v, err = %v", peerStatus, err)
	}
	peer["agent"] = map[string]any{"name": "builder"}
	if _, err := store.PeerReviewDelivery(context.Background(), selection.Workspace.ID, work.Key, peer); err == nil || !strings.Contains(err.Error(), "own completion") {
		t.Fatalf("self peer review error = %v", err)
	}
	peer["agent"] = map[string]any{"name": "reviewer"}
	latest, err = store.LatestDeliveryReport(context.Background(), selection.Workspace.ID, work.Key)
	if err != nil {
		t.Fatal(err)
	}
	peer["peer_review_of"] = map[string]any{"completion_feedback_event_id": latest.ID, "git_receipt": map[string]any{"repository": "https://example.test/project.git", "head_revision": "changed"}}
	if _, err := store.PeerReviewDelivery(context.Background(), selection.Workspace.ID, work.Key, peer); err == nil || !strings.Contains(err.Error(), "git_receipt") {
		t.Fatalf("stale receipt error = %v", err)
	}
}

func TestApprovedDeliveryRejectsLaterAgentSubmission(t *testing.T) {
	store, err := local.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	selection, err := store.Initialize(context.Background(), local.InitInput{WorkspaceName: "Alpha", DisplayName: "Human", Username: "human"})
	if err != nil {
		t.Fatal(err)
	}
	work := readyLocalWork(t, store, selection)
	body := map[string]any{
		"context_digest": work.ContextDigest,
		"agent":          map[string]any{"name": "builder"},
		"criteria":       []any{map[string]any{"criterion_id": "local-1", "claim": "satisfied", "evidence": map[string]any{"summary": "implemented"}}},
		"checks":         []any{map[string]any{"name": "unit", "status": "pass"}},
	}
	if _, err := store.SubmitDelivery(context.Background(), selection.Workspace.ID, work.Key, body); err != nil {
		t.Fatal(err)
	}
	if err := store.DecideDelivery(context.Background(), selection.Workspace.ID, work.Key, "approve", "human", "looks good"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SubmitDelivery(context.Background(), selection.Workspace.ID, work.Key, body); err == nil || !strings.Contains(err.Error(), "already approved") {
		t.Fatalf("post-approval submission error = %v", err)
	}
	status, err := store.DeliveryStatus(context.Background(), selection.Workspace.ID, work.Key)
	if err != nil {
		t.Fatal(err)
	}
	if status.HumanDecision != "approve" {
		t.Fatalf("human decision = %q, want approve", status.HumanDecision)
	}
	if err := store.DecideDelivery(context.Background(), selection.Workspace.ID, work.Key, "reject", "human", "changed my mind"); err == nil || !strings.Contains(err.Error(), "already recorded") {
		t.Fatalf("second decision error = %v", err)
	}
}

func TestDeliveryReviewRequiresOneEvidenceBackedClaimPerCriterion(t *testing.T) {
	store, err := local.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	selection, err := store.Initialize(context.Background(), local.InitInput{WorkspaceName: "Alpha", DisplayName: "Human", Username: "human"})
	if err != nil {
		t.Fatal(err)
	}
	readyLocalWork(t, store, selection)
	work, err := store.CreateWork(context.Background(), selection.Workspace.ID, local.WorkInput{
		FeatureRef:         "LOCAL-DELIVERY",
		Title:              "Two criteria",
		AcceptanceCriteria: []string{"One", "Two"},
	})
	if err != nil {
		t.Fatal(err)
	}
	review, err := store.SubmitDelivery(context.Background(), selection.Workspace.ID, work.Key, map[string]any{
		"context_digest": work.ContextDigest,
		"agent":          map[string]any{"name": "builder"},
		"criteria": []any{
			map[string]any{"criterion_id": "local-1", "claim": "satisfied", "evidence": map[string]any{"summary": "first"}},
			map[string]any{"criterion_id": "local-1", "claim": "satisfied", "evidence": map[string]any{"summary": "duplicate"}},
		},
		"checks": []any{map[string]any{"name": "unit", "status": "pass"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if review.Verdict != "failed" || !strings.Contains(review.Summary, "each acceptance criterion") {
		t.Fatalf("review = %#v", review)
	}
}

func readyLocalWork(t *testing.T, store *local.Store, selection local.Selection) local.WorkItem {
	t.Helper()
	artifact, err := store.PublishArtifact(context.Background(), selection.Workspace.ID, local.ArtifactInput{FeatureKey: "LOCAL-DELIVERY", RequestType: "new_feature", Documents: []local.ArtifactDocumentInput{{Path: "spec.md", Role: "spec", Content: []byte("# Local delivery")}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RunReadiness(context.Background(), selection.Workspace.ID, artifact.ID); err != nil {
		t.Fatal(err)
	}
	completeLocalGateTasks(t, store, selection, artifact.ID)
	if err := store.ApproveArtifact(context.Background(), selection.Workspace.ID, artifact.ID, "human", ""); err != nil {
		t.Fatal(err)
	}
	feature, err := store.PromoteArtifact(context.Background(), selection.Workspace.ID, artifact.ID)
	if err != nil {
		t.Fatal(err)
	}
	work, err := store.CreateWork(context.Background(), selection.Workspace.ID, local.WorkInput{FeatureRef: feature.Key, Title: "Deliver", AcceptanceCriteria: []string{"Works"}})
	if err != nil {
		t.Fatal(err)
	}
	return work
}
