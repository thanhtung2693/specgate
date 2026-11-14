package command_test

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestTransferPreservesFrozenApprovedHistoryAndReceipt(t *testing.T) {
	deps, out := newTestDeps(t)
	source := t.TempDir()
	store, err := local.Open(filepath.Join(source, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	selection, err := store.Initialize(ctx, local.InitInput{Username: "reviewer", DisplayName: "Reviewer", WorkspaceName: "Approved source"})
	if err != nil {
		t.Fatal(err)
	}
	ws := selection.Workspace.ID
	docs := []local.ArtifactDocumentInput{
		{Path: "spec.md", Role: "spec", Content: []byte("# Scope\n\n## Acceptance criteria\n\nPreserve this immutable spec.")},
		{Path: "plan.md", Role: "plan", Content: []byte("# Plan\n\nRun the reviewed check.")},
	}
	requirements := []local.SourceCriterion{{ID: "req1", Text: "Preserve this immutable spec.", SourcePath: "spec.md"}}
	base, err := store.PublishArtifact(ctx, ws, local.ArtifactInput{
		FeatureKey: "CARRY", RequestType: "new_feature", Documents: docs, SourceCriteria: requirements,
	})
	if err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	writeTestFile(t, filepath.Join(repo, ".specgate", "rubrics", "scope_clear.md"), "Check the frozen project contract.")
	rubrics, err := local.ResolveProjectRubrics(repo)
	if err != nil {
		t.Fatal(err)
	}
	target, err := store.PublishArtifact(ctx, ws, local.ArtifactInput{
		FeatureKey: base.FeatureKey, RequestType: "change_request", BaseVersion: "v1", Documents: docs, ProjectRubrics: rubrics,
		SourceCriteria: requirements,
		SourceLineage:  &local.SourceLineage{Version: 1, BaseArtifactID: base.ID, BaseDigest: base.SnapshotDigest, Rows: []local.LineageRow{{BaseID: "req1", TargetIDs: []string{"req1"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RunReadiness(ctx, ws, target.ID); err != nil {
		t.Fatal(err)
	}
	tasks, err := store.ListGateTasks(ctx, ws, target.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		result := local.GateResultInput{Gate: task.GateKey, GateDigest: task.GateDigest, InputDigest: task.ArtifactDigest, State: "pass", Summary: "frozen rubric reviewed"}
		result.Evaluator.Executor = task.Executor
		if _, err := store.SubmitGateResult(ctx, ws, task.TaskID, result); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.ApproveArtifact(ctx, ws, target.ID, "reviewer", "immutable approval"); err != nil {
		t.Fatal(err)
	}
	feature, err := store.PromoteArtifact(ctx, ws, target.ID)
	if err != nil {
		t.Fatal(err)
	}
	work, err := store.CreateWork(ctx, ws, local.WorkInput{FeatureRef: feature.Key, Title: "Carry history", AcceptanceCriteria: []string{"Carry @check:unit"}})
	if err != nil {
		t.Fatal(err)
	}
	pack, err := store.ContextPack(ctx, ws, work.ID)
	if err != nil {
		t.Fatal(err)
	}
	pin, err := store.PinVerificationContract(ctx, ws, work.ID, repo, "reviewer", local.VerificationContractInput{
		ContextDigest: work.ContextDigest, Shell: "sh",
		Checks: []local.VerificationCheck{{Name: "unit", Command: "printf '%s' 'do not execute during transfer'", Cwd: "."}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SubmitDelivery(ctx, ws, work.ID, map[string]any{
		"context_digest": work.ContextDigest, "verification_contract_digest": pin.Digest,
		"agent":    map[string]any{"name": "builder"},
		"checks":   []any{map[string]any{"name": "unit", "command": pin.Checks[0].Command, "cwd": ".", "status": "pass"}},
		"criteria": []any{map[string]any{"criterion_id": "local-1", "claim": "satisfied", "evidence": map[string]any{"heading": "Fixture check evidence"}}},
	}, repo); err != nil {
		t.Fatal(err)
	}
	checkpoint, err := store.CreateCheckpointWithFiles(ctx, ws, work.ID, "checkout", []string{".specgate/work/external.txt"}, "A checkpoint has references")
	if err != nil {
		t.Fatal(err)
	}
	options := local.AcceptanceOptions{RepoRoot: repo}
	review, err := store.InspectAcceptance(ctx, ws, work.ID, options)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DecideDeliveryWithBasis(ctx, ws, work.ID, "approve", "reviewer", "accepted fixture", review.Review.ID, review.Basis.Digest, options); err != nil {
		t.Fatal(err)
	}
	accepted, err := store.InspectAcceptance(ctx, ws, work.ID, options)
	if err != nil {
		t.Fatal(err)
	}
	audit, err := store.Audit(ctx, ws, work.ID)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(source, "extra-runtime-file"), "no archive entry for this")
	closeLocalChangeStore(t, deps, source, store)

	archive := filepath.Join(t.TempDir(), "history.zip")
	prepared := runTransfer(t, deps, out, output.ExitOK, "prepare", "--file", archive, "--work", work.ID, "--checkpoint", checkpoint.ID, "--yes")
	sha := prepared["snapshot_sha256"].(string)
	var previousGeneration string
	for _, dir := range []string{"first", "second"} {
		from := filepath.Join(t.TempDir(), dir)
		result := runTransfer(t, deps, out, output.ExitOK, "receive", "--file", archive, "--into", from, "--expect-snapshot", sha)
		original, err := local.Open(filepath.Join(from, "state.db"))
		if err != nil {
			t.Fatal(err)
		}
		generation, err := original.CheckoutGeneration(ctx)
		if err != nil || generation != result["receiving_generation"] || generation == previousGeneration {
			t.Fatalf("receiving generation not fresh and durable: %v %q", err, generation)
		}
		previousGeneration = generation
		if got, err := original.ContextPack(ctx, ws, work.ID); err != nil || got != pack {
			t.Fatalf("frozen Context Pack changed: %v", err)
		}
		if got, err := original.GetVerificationContract(ctx, ws, work.ID); err != nil || !reflect.DeepEqual(got, pin) {
			t.Fatalf("pinned command or verification changed: %v", err)
		}
		if got, err := original.GetCheckpoint(ctx, ws, work.ID, checkpoint.ID); err != nil || !reflect.DeepEqual(got, checkpoint) {
			t.Fatalf("historical checkpoint changed: %v", err)
		}
		receivingOptions := options
		receivingOptions.CheckoutGeneration = generation
		got, err := original.InspectAcceptance(ctx, ws, work.ID, receivingOptions)
		if err != nil {
			t.Fatal(err)
		}
		if got.Review != accepted.Review || !reflect.DeepEqual(got.Report, accepted.Report) ||
			!reflect.DeepEqual(got.Artifact, accepted.Artifact) || !reflect.DeepEqual(got.Recorded, accepted.Recorded) {
			t.Fatal("frozen artifact, policy, lineage, review, report, or decision basis changed")
		}
		if got, err := original.Audit(ctx, ws, work.ID); err != nil || !reflect.DeepEqual(got, audit) {
			t.Fatalf("approval and decision audit changed: %v", err)
		}
		if _, err := os.Lstat(filepath.Join(from, "extra-runtime-file")); !os.IsNotExist(err) {
			t.Fatalf("runtime file reached receiving store: %v", err)
		}
		if err := original.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTransferPreEnhancementArtifactSchemaMigratesOnlyInScratch(t *testing.T) {
	deps, out, source, work, archive := newTransferSafetyFixture(t)
	sourceDB := filepath.Join(source, "state.db")
	db, err := sql.Open("sqlite", sourceDB)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"ALTER TABLE artifacts DROP COLUMN source_criteria_json",
		"ALTER TABLE artifacts DROP COLUMN source_lineage_json",
		"DROP TABLE delivery_peer_reviews",
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	before := transferReadFile(t, sourceDB)
	prepared := runTransfer(t, deps, out, output.ExitOK, "prepare", "--file", archive, "--work", work.ID, "--yes")
	runTransfer(t, deps, out, output.ExitOK, "inspect", "--file", archive)
	from := filepath.Join(t.TempDir(), "receive")
	runTransfer(t, deps, out, output.ExitOK, "receive", "--file", archive, "--into", from, "--expect-snapshot", prepared["snapshot_sha256"].(string))
	if !bytes.Equal(transferReadFile(t, sourceDB), before) {
		t.Fatal("transfer migrated the legacy source")
	}
	received, err := local.Open(filepath.Join(from, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer received.Close()
	if got, err := received.ContextPack(t.Context(), work.WorkspaceID, work.ID); err != nil || got.Digest != work.ContextDigest {
		t.Fatalf("legacy Context Pack incorrectly restored: %v %v", got, err)
	}
}
