package command_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestPortableExportIsChecksummedPrivateAndCredentialFree(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	stateDir := t.TempDir()
	store, err := local.Open(filepath.Join(stateDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Initialize(context.Background(), local.InitInput{
		WorkspaceName: "Local workspace", DisplayName: "Local User", Username: "local",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := (config.Config{Mode: config.ModeLocal, Local: config.LocalStore{Path: stateDir}}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "export.json")
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "portable", "export", "--file", path)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 600", info.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var bundle struct {
		SchemaVersion string                  `json:"schema_version"`
		Payload       local.PortableWorkspace `json:"payload"`
		Checksum      string                  `json:"checksum"`
	}
	if err := json.Unmarshal(data, &bundle); err != nil {
		t.Fatal(err)
	}
	payloadJSON, _ := json.Marshal(bundle.Payload)
	sum := sha256.Sum256(payloadJSON)
	if bundle.SchemaVersion != "specgate.portable/v1" || bundle.Checksum != "sha256:"+hex.EncodeToString(sum[:]) {
		t.Fatalf("bundle header/checksum = %+v", bundle)
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope) != 5 {
		t.Fatalf("bundle envelope fields = %v", envelope)
	}
	var payloadFields map[string]json.RawMessage
	if err := json.Unmarshal(envelope["payload"], &payloadFields); err != nil {
		t.Fatal(err)
	}
	expectedPayloadFields := []string{"workspace", "features", "artifacts", "work", "gates", "delivery"}
	if len(payloadFields) != len(expectedPayloadFields) {
		t.Fatalf("portable payload fields = %v", payloadFields)
	}
	for _, field := range expectedPayloadFields {
		if _, ok := payloadFields[field]; !ok {
			t.Fatalf("portable payload omitted contract field %q", field)
		}
	}
}

func TestPortableQuickWorkPreflightsForFullImport(t *testing.T) {
	exportDeps, _, _, exportOut := newFakeDeps(t)
	stateDir := t.TempDir()
	store, err := local.Open(filepath.Join(stateDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	selection, err := store.Initialize(t.Context(), local.InitInput{
		WorkspaceName: "Local workspace", DisplayName: "Local User", Username: "local",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateQuickWork(t.Context(), selection.Workspace.ID, local.QuickWorkInput{
		Title: "Fix timeout", AcceptanceCriteria: []string{"Retries stop @check:unit"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := (config.Config{Mode: config.ModeLocal, Local: config.LocalStore{Path: stateDir}}).SaveTo(exportDeps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	bundlePath := filepath.Join(t.TempDir(), "quick.json")
	if code := command.ExecuteForCode(command.NewRootCommand(exportDeps), "--json", "portable", "export", "--file", bundlePath); code != output.ExitOK {
		t.Fatalf("export exit = %d, output = %s", code, exportOut.String())
	}

	importDeps, importClient, _, importOut := newFakeDeps(t)
	setPortableDestination(t, importDeps)
	code := command.ExecuteForCode(command.NewRootCommand(importDeps), "--json", "portable", "import", "--file", bundlePath, "--dry-run")
	if code != output.ExitOK {
		t.Fatalf("import preflight exit = %d, output = %s", code, importOut.String())
	}
	var envelope struct {
		Data struct {
			Work       int      `json:"work"`
			Conflicts  []string `json:"conflicts"`
			WouldWrite bool     `json:"would_write"`
		} `json:"data"`
	}
	if err := json.Unmarshal(importOut.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Work != 1 || len(envelope.Data.Conflicts) != 0 || !envelope.Data.WouldWrite {
		t.Fatalf("preflight = %#v", envelope.Data)
	}

	importOut.Reset()
	code = command.ExecuteForCode(command.NewRootCommand(importDeps), "--json", "--yes", "portable", "import", "--file", bundlePath)
	if code != output.ExitOK {
		t.Fatalf("import exit = %d, output = %s", code, importOut.String())
	}
	if importClient.lastCreateBody["title"] != "Fix timeout" ||
		!strings.HasPrefix(fmt.Sprint(importClient.lastCreateBody["issue_url"]), "specgate-local-work:") {
		t.Fatalf("quick import body = %#v", importClient.lastCreateBody)
	}
	criteria, ok := importClient.lastCreateBody["acceptance_criteria"].([]map[string]string)
	if !ok || len(criteria) != 1 || criteria[0]["text"] != "Retries stop" || criteria[0]["verification_binding"] != "unit" {
		t.Fatalf("quick import criteria = %#v", importClient.lastCreateBody["acceptance_criteria"])
	}
}

func TestPortableExportRefusesToOverwriteActiveSQLiteState(t *testing.T) {
	deps, _, _, out := newFakeDeps(t)
	stateDir := t.TempDir()
	statePath := filepath.Join(stateDir, "state.db")
	store, err := local.Open(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Initialize(context.Background(), local.InitInput{
		WorkspaceName: "Local workspace", DisplayName: "Local User", Username: "local",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := (config.Config{Mode: config.ModeLocal, Local: config.LocalStore{Path: stateDir}}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "portable", "export", "--file", statePath)
	if code == output.ExitOK {
		t.Fatalf("active SQLite state was accepted as export destination: %s", out.String())
	}
	reopened, err := local.Open(statePath)
	if err != nil {
		t.Fatalf("portable export corrupted Local state: %v", err)
	}
	defer reopened.Close()
	if _, err := reopened.Current(context.Background()); err != nil {
		t.Fatalf("Local state no longer opens after rejected export: %v", err)
	}
}

func TestPortableImportDryRunReportsAllConflictsWithoutMutation(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	setPortableDestination(t, deps)
	bundlePath := writePortableBundle(t, local.PortableWorkspace{
		Workspace: local.Workspace{ID: "local-ws", Slug: "local", Name: "Local"},
		Features: []local.PortableFeature{
			{ID: "local-feature", Key: "CHECKOUT", CanonicalArtifactID: "local-artifact", Version: 1},
		},
		Artifacts: []local.PortableArtifact{
			portableArtifactFixture("local-artifact", "CHECKOUT"),
		},
	})
	fc.featuresResult = []client.Feature{{ID: "existing-feature", Key: "CHECKOUT", CanonicalArtifactID: "existing-artifact"}}
	fc.artifactListResult = &client.ArtifactList{Items: []client.Artifact{
		{ID: "existing-import", SourceKind: "specgate-local-import", SourceID: "local-ws:local-artifact"},
	}}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "portable", "import", "--file", bundlePath, "--dry-run")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var env struct {
		Data struct {
			Conflicts  []string `json:"conflicts"`
			WouldWrite bool     `json:"would_write"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out.String())
	}
	if len(env.Data.Conflicts) != 2 || env.Data.WouldWrite {
		t.Fatalf("preflight = %+v", env.Data)
	}
	if fc.lastPublishBody != nil {
		t.Fatalf("dry-run published: %#v", fc.lastPublishBody)
	}
}

func TestPortableImportWritesOnlyWithConfirmationAndPreservesBindings(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	setPortableDestination(t, deps)
	artifact := portableArtifactFixture("local-artifact", "CHECKOUT")
	payload := local.PortableWorkspace{
		Workspace: local.Workspace{ID: "local-ws", Slug: "local", Name: "Local"},
		Features: []local.PortableFeature{
			{ID: "local-feature", Key: "CHECKOUT", CanonicalArtifactID: artifact.ID, Version: 1},
		},
		Artifacts: []local.PortableArtifact{artifact},
		Work: []local.WorkItem{
			{ID: "local-work", Key: "LOCAL-1", WorkspaceID: "local-ws", FeatureID: "local-feature", ArtifactID: artifact.ID, Title: "Deliver checkout", AcceptanceCriteria: []string{"Checkout works"}, Phase: "ready"},
		},
	}
	bundlePath := writePortableBundle(t, payload)
	fc.publishResult = map[string]any{"artifact_id": "full-artifact", "version": "v0.1"}
	fc.artifactResult = &client.Artifact{ID: "full-artifact", Version: "v0.1", Status: "draft", SnapshotDigest: artifact.SnapshotDigest}
	fc.updateStatusResult = &client.Artifact{ID: "full-artifact", Version: "v0.1", Status: "approved", SnapshotDigest: artifact.SnapshotDigest}
	fc.promoteResult = &client.Feature{ID: "full-feature", Key: "CHECKOUT", CanonicalArtifactID: "full-artifact"}
	fc.createWorkItemResult = map[string]any{"change_request_id": "full-work", "change_request_key": "CR-FULL"}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "portable", "import", "--file", bundlePath)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastPublishBody["source_kind"] != "specgate-local-import" || fc.lastPublishBody["source_id"] != "local-ws:local-artifact" {
		t.Fatalf("publish provenance = %#v", fc.lastPublishBody)
	}
	if fc.lastPublishBody["workspace_id"] != "full-ws" || fc.lastPublishBody["created_by"] != "full-user" {
		t.Fatalf("destination attribution = %#v", fc.lastPublishBody)
	}
	if fc.lastDispatchGateTasksID != "full-artifact" {
		t.Fatalf("destination policy tasks were not dispatched: %q", fc.lastDispatchGateTasksID)
	}
	if got := fc.callOrder; len(got) < 2 || got[0] != "artifact_status" || got[1] != "dispatch_gates" {
		t.Fatalf("destination gates dispatched before imported approval: %v", got)
	}
	if fc.lastPromoteID != "full-artifact" {
		t.Fatalf("canonical artifact = %q, want full-artifact", fc.lastPromoteID)
	}
	if fc.lastCreateWorkItem["feature"] != "CHECKOUT" {
		t.Fatalf("work binding = %#v", fc.lastCreateWorkItem)
	}
	if fc.lastCreateWorkItem["artifact_id"] != "full-artifact" {
		t.Fatalf("work artifact version = %#v", fc.lastCreateWorkItem)
	}
	sourceRefs, _ := fc.lastCreateWorkItem["source_refs"].([]string)
	if len(sourceRefs) != 1 || sourceRefs[0] != "specgate-local-work:local-ws:local-work" {
		t.Fatalf("work provenance = %#v", fc.lastCreateWorkItem)
	}
	var env struct {
		Data struct {
			ImportedArtifacts int               `json:"imported_artifacts"`
			ImportedWork      int               `json:"imported_work"`
			ArtifactMapping   map[string]string `json:"artifact_mapping"`
			WorkMapping       map[string]string `json:"work_mapping"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out.String())
	}
	if env.Data.ImportedArtifacts != 1 || env.Data.ImportedWork != 1 ||
		env.Data.ArtifactMapping["local-artifact"] != "full-artifact" ||
		env.Data.WorkMapping["local-work"] != "full-work" {
		t.Fatalf("receipt = %+v", env.Data)
	}
}

func TestPortableImportRejectsOversizedBundleBeforeAPICalls(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	setPortableDestination(t, deps)
	path := filepath.Join(t.TempDir(), "oversized.json")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate((64 << 20) + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "portable", "import", "--file", path, "--dry-run")
	if code == output.ExitOK {
		t.Fatalf("oversized import succeeded: %s", out.String())
	}
	if !strings.Contains(out.String(), "64 MiB") {
		t.Fatalf("missing bundle limit recovery: %s", out.String())
	}
	if fc.calls != 0 {
		t.Fatalf("oversized bundle made %d API calls", fc.calls)
	}
}

func TestPortableImportMapsDeliveryEvidenceBySourceCriterionID(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	setPortableDestination(t, deps)
	artifact := portableArtifactFixture("local-artifact", "CHECKOUT")
	payload := local.PortableWorkspace{
		Workspace: local.Workspace{ID: "local-ws", Slug: "local", Name: "Local"},
		Features: []local.PortableFeature{
			{ID: "local-feature", Key: "CHECKOUT", CanonicalArtifactID: artifact.ID, Version: 1},
		},
		Artifacts: []local.PortableArtifact{artifact},
		Work: []local.WorkItem{
			{
				ID: "local-work", Key: "LOCAL-1", WorkspaceID: "local-ws", FeatureID: "local-feature", ArtifactID: artifact.ID,
				Title: "Deliver checkout", AcceptanceCriteria: []string{"First contract", "Second contract"}, Phase: "ready",
			},
		},
		Delivery: []local.PortableDeliveryEvidence{
			{
				WorkID: "local-work",
				Report: map[string]any{
					"event_type":        "coding_agent.completed",
					"change_request_id": "local-work",
					"context_digest":    "local-context",
					"criteria": []any{
						map[string]any{"criterion_id": "local-2", "claim": "satisfied"},
						map[string]any{"criterion_id": "local-1", "claim": "satisfied"},
					},
				},
			},
		},
	}
	bundlePath := writePortableBundle(t, payload)
	fc.publishResult = map[string]any{"artifact_id": "full-artifact", "version": "v0.1"}
	fc.artifactResult = &client.Artifact{ID: "full-artifact", Version: "v0.1", Status: "draft", SnapshotDigest: artifact.SnapshotDigest}
	fc.updateStatusResult = &client.Artifact{ID: "full-artifact", Version: "v0.1", Status: "approved", SnapshotDigest: artifact.SnapshotDigest}
	fc.promoteResult = &client.Feature{ID: "full-feature", Key: "CHECKOUT", CanonicalArtifactID: "full-artifact"}
	fc.createWorkItemResult = map[string]any{"change_request_id": "full-work", "change_request_key": "CR-FULL"}
	fc.acceptanceCriteria = []client.AcceptanceCriterion{
		{ID: "full-ac-1", Text: "First contract"},
		{ID: "full-ac-2", Text: "Second contract"},
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "portable", "import", "--file", bundlePath)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	rows, _ := fc.lastFeedbackBody["criteria"].([]any)
	if len(rows) != 2 {
		t.Fatalf("criteria = %#v", fc.lastFeedbackBody["criteria"])
	}
	first, _ := rows[0].(map[string]any)
	second, _ := rows[1].(map[string]any)
	if first["criterion_id"] != "full-ac-2" || second["criterion_id"] != "full-ac-1" {
		t.Fatalf("criterion mapping = %#v", rows)
	}
	if fc.lastFeedbackBody["change_request_id"] != "full-work" ||
		fc.lastFeedbackBody["severity"] != "info" {
		t.Fatalf("Full feedback envelope was not translated: %#v", fc.lastFeedbackBody)
	}
	if _, exists := fc.lastFeedbackBody["context_digest"]; exists {
		t.Fatalf("Local-only context_digest leaked into Full feedback: %#v", fc.lastFeedbackBody)
	}
	if fc.deliveryReviewCalls != 1 {
		t.Fatalf("delivery review calls = %d, want 1", fc.deliveryReviewCalls)
	}
}

func TestPortableImportDoesNotReplaySourceGateVerdicts(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	setPortableDestination(t, deps)
	artifact := portableArtifactFixture("local-artifact", "CHECKOUT")
	payload := local.PortableWorkspace{
		Workspace: local.Workspace{ID: "local-ws", Slug: "local", Name: "Local"},
		Features: []local.PortableFeature{
			{ID: "local-feature", Key: "CHECKOUT", CanonicalArtifactID: artifact.ID, Version: 1},
		},
		Artifacts: []local.PortableArtifact{artifact},
		Gates: []local.PortableGateEvidence{{
			TaskID: "local-task", ArtifactID: artifact.ID, GateKey: "policy",
			GateVersion: "local-v1", GateDigest: "local-gate", ArtifactDigest: artifact.SnapshotDigest,
			PolicyDigest: "local-policy", ResultID: "local-result", ResultState: "pass",
		}},
	}
	bundlePath := writePortableBundle(t, payload)
	fc.publishResult = map[string]any{"artifact_id": "full-artifact", "version": "v0.1"}
	fc.artifactResult = &client.Artifact{ID: "full-artifact", Version: "v0.1", Status: "draft", SnapshotDigest: artifact.SnapshotDigest}
	fc.updateStatusResult = &client.Artifact{ID: "full-artifact", Version: "v0.1", Status: "approved", SnapshotDigest: artifact.SnapshotDigest}
	fc.promoteResult = &client.Feature{ID: "full-feature", Key: "CHECKOUT", CanonicalArtifactID: "full-artifact"}
	fc.gateTasks = []client.GateTask{{
		TaskID: "full-task", GateKey: "policy", GateVersion: "full-v2",
		GateDigest: "full-gate", ArtifactDigest: artifact.SnapshotDigest, PolicyDigest: "full-policy",
	}}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "portable", "import", "--file", bundlePath)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastDispatchGateTasksID != "full-artifact" {
		t.Fatalf("destination gates were not dispatched: %q", fc.lastDispatchGateTasksID)
	}
	if fc.submittedGateResults != 0 {
		t.Fatalf("replayed %d source gate verdicts against destination policy", fc.submittedGateResults)
	}
	var env struct {
		Data struct {
			ImportedGates int `json:"imported_gates"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Data.ImportedGates != 0 {
		t.Fatalf("import receipt attested source verdicts: %+v", env.Data)
	}
}

func TestPortableImportResumesExactArtifactWithoutRepublishing(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	setPortableDestination(t, deps)
	artifact := portableArtifactFixture("local-artifact", "CHECKOUT")
	bundlePath := writePortableBundle(t, local.PortableWorkspace{
		Workspace: local.Workspace{ID: "local-ws", Slug: "local", Name: "Local"},
		Features: []local.PortableFeature{
			{ID: "local-feature", Key: "CHECKOUT", CanonicalArtifactID: artifact.ID, Version: 1},
		},
		Artifacts: []local.PortableArtifact{artifact},
	})
	fc.featuresResult = []client.Feature{{ID: "full-feature", Key: "CHECKOUT", CanonicalArtifactID: "full-artifact"}}
	fc.artifactListResult = &client.ArtifactList{Items: []client.Artifact{{
		ID: "full-artifact", FeatureID: "full-feature", Version: "v0.1", Status: "approved",
		SourceKind: "specgate-local-import", SourceID: "local-ws:" + artifact.ID, SourceRevision: artifact.SnapshotDigest,
		SnapshotDigest: artifact.SnapshotDigest,
	}}}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "portable", "import", "--file", bundlePath)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastPublishBody != nil {
		t.Fatalf("exact imported artifact was republished: %#v", fc.lastPublishBody)
	}
	var env struct {
		Data struct {
			Conflicts         []string          `json:"conflicts"`
			ImportedArtifacts int               `json:"imported_artifacts"`
			ArtifactMapping   map[string]string `json:"artifact_mapping"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if len(env.Data.Conflicts) != 0 || env.Data.ImportedArtifacts != 0 || env.Data.ArtifactMapping[artifact.ID] != "full-artifact" {
		t.Fatalf("resume receipt = %+v", env.Data)
	}
}

func TestPortableImportDryRunRejectsDestinationOnlyDraftOnResumedFeature(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	setPortableDestination(t, deps)
	approved := portableArtifactFixture("local-v1", "CHECKOUT")
	draft := portableArtifactFixture("local-v2", "CHECKOUT")
	draft.Version = 2
	draft.Status = "draft"
	bundlePath := writePortableBundle(t, local.PortableWorkspace{
		Workspace: local.Workspace{ID: "local-ws", Slug: "local", Name: "Local"},
		Features: []local.PortableFeature{
			{ID: "local-feature", Key: "CHECKOUT", CanonicalArtifactID: approved.ID, Version: 1},
		},
		Artifacts: []local.PortableArtifact{approved, draft},
	})
	fc.featuresResult = []client.Feature{{ID: "full-feature", Key: "CHECKOUT", CanonicalArtifactID: "full-v1"}}
	fc.artifactListResult = &client.ArtifactList{Items: []client.Artifact{
		{
			ID: "full-v1", FeatureID: "full-feature", Version: "v0.1", Status: "approved",
			SourceKind: "specgate-local-import", SourceID: "local-ws:" + approved.ID, SourceRevision: approved.SnapshotDigest,
			SnapshotDigest: approved.SnapshotDigest,
		},
		{ID: "destination-draft", FeatureID: "full-feature", Version: "v0.2", Status: "draft", SnapshotDigest: "sha256:destination"},
	}}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "portable", "import", "--file", bundlePath, "--dry-run")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var env struct {
		Data struct {
			Conflicts  []string `json:"conflicts"`
			WouldWrite bool     `json:"would_write"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if len(env.Data.Conflicts) != 1 || env.Data.Conflicts[0] != "feature contains destination-only artifact: CHECKOUT" || env.Data.WouldWrite {
		t.Fatalf("preflight = %+v", env.Data)
	}
	if fc.lastPublishBody != nil || len(fc.callOrder) != 0 {
		t.Fatalf("conflicting dry-run mutated destination: publish=%#v calls=%v", fc.lastPublishBody, fc.callOrder)
	}
}

func TestPortableImportDoesNotReuseProvenanceFromAnotherSourceWorkspace(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	setPortableDestination(t, deps)
	artifact := portableArtifactFixture("local-artifact", "CHECKOUT")
	bundlePath := writePortableBundle(t, local.PortableWorkspace{
		Workspace: local.Workspace{ID: "local-ws", Slug: "local", Name: "Local"},
		Features: []local.PortableFeature{
			{ID: "local-feature", Key: "CHECKOUT", CanonicalArtifactID: artifact.ID, Version: 1},
		},
		Artifacts: []local.PortableArtifact{artifact},
	})
	fc.featuresResult = []client.Feature{{ID: "full-feature", Key: "CHECKOUT", CanonicalArtifactID: "other-artifact"}}
	fc.artifactListResult = &client.ArtifactList{Items: []client.Artifact{{
		ID: "other-artifact", FeatureID: "full-feature", Version: "v0.1", Status: "approved",
		SourceKind: "specgate-local-import", SourceID: "other-ws:" + artifact.ID, SourceRevision: artifact.SnapshotDigest,
		SnapshotDigest: artifact.SnapshotDigest,
	}}}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "portable", "import", "--file", bundlePath, "--dry-run")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var env struct {
		Data struct {
			Conflicts []string `json:"conflicts"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if len(env.Data.Conflicts) != 1 || env.Data.Conflicts[0] != "feature key already exists: CHECKOUT" {
		t.Fatalf("other workspace provenance was reused: %+v", env.Data)
	}
}

func TestPortableImportResumesWorkByStructuredSourceRef(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	setPortableDestination(t, deps)
	artifact := portableArtifactFixture("local-artifact", "CHECKOUT")
	sourceWork := local.WorkItem{
		ID: "local-work", Key: "LOCAL-1", WorkspaceID: "local-ws", FeatureID: "local-feature", ArtifactID: artifact.ID,
		Title: "Deliver checkout", AcceptanceCriteria: []string{"Checkout works"}, Phase: "ready",
	}
	bundlePath := writePortableBundle(t, local.PortableWorkspace{
		Workspace: local.Workspace{ID: "local-ws", Slug: "local", Name: "Local"},
		Features: []local.PortableFeature{
			{ID: "local-feature", Key: "CHECKOUT", CanonicalArtifactID: artifact.ID, Version: 1},
		},
		Artifacts: []local.PortableArtifact{artifact},
		Work:      []local.WorkItem{sourceWork},
	})
	fc.featuresResult = []client.Feature{{ID: "full-feature", Key: "CHECKOUT", CanonicalArtifactID: "full-artifact"}}
	fc.artifactListResult = &client.ArtifactList{Items: []client.Artifact{{
		ID: "full-artifact", FeatureID: "full-feature", Version: "v0.1", Status: "approved",
		SourceKind: "specgate-local-import", SourceID: "local-ws:" + artifact.ID, SourceRevision: artifact.SnapshotDigest,
		SnapshotDigest: artifact.SnapshotDigest,
	}}}
	sourceRefs, _ := json.Marshal([]string{"specgate-local-work:local-ws:" + sourceWork.ID})
	fc.workItems = []client.WorkItemSummary{{
		ID: "full-work", Key: "CR-FULL", Title: sourceWork.Title, LeadArtifactID: "full-artifact", SourceRefs: string(sourceRefs),
	}}
	fc.acceptanceCriteria = []client.AcceptanceCriterion{{ID: "full-ac", Text: "Checkout works"}}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "portable", "import", "--file", bundlePath)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastCreateWorkItem != nil {
		t.Fatalf("exact imported work was recreated: %#v", fc.lastCreateWorkItem)
	}
	if fc.lastACWorkspaceID != "full-ws" {
		t.Fatalf("acceptance-criteria lookup workspace = %q, want full-ws", fc.lastACWorkspaceID)
	}
	var env struct {
		Data struct {
			ImportedWork int               `json:"imported_work"`
			WorkMapping  map[string]string `json:"work_mapping"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Data.ImportedWork != 0 || env.Data.WorkMapping[sourceWork.ID] != "full-work" {
		t.Fatalf("resume receipt = %+v", env.Data)
	}
}

func TestPortableImportPreservesExactCanonicalArtifact(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	setPortableDestination(t, deps)
	canonical := portableArtifactFixture("artifact-1", "CHECKOUT")
	newer := portableArtifactFixture("artifact-2", "CHECKOUT")
	newer.Version = 2
	bundlePath := writePortableBundle(t, local.PortableWorkspace{
		Workspace: local.Workspace{ID: "local-ws", Slug: "local", Name: "Local"},
		Features: []local.PortableFeature{
			{ID: "local-feature", Key: "CHECKOUT", CanonicalArtifactID: canonical.ID, Version: canonical.Version},
		},
		Artifacts: []local.PortableArtifact{canonical, newer},
	})
	fc.featuresResult = []client.Feature{{
		ID: "full-feature", Key: "CHECKOUT", CanonicalArtifactID: "full-artifact-1",
	}}
	fc.artifactListResult = &client.ArtifactList{Items: []client.Artifact{
		{
			ID: "full-artifact-1", FeatureID: "full-feature", Version: "v0.1", Status: "approved",
			SnapshotDigest: canonical.SnapshotDigest, SourceKind: "specgate-local-import",
			SourceID: "local-ws:artifact-1", SourceRevision: canonical.SnapshotDigest,
		},
		{
			ID: "full-artifact-2", FeatureID: "full-feature", Version: "v0.2", Status: "approved",
			SnapshotDigest: newer.SnapshotDigest, SourceKind: "specgate-local-import",
			SourceID: "local-ws:artifact-2", SourceRevision: newer.SnapshotDigest,
		},
	}}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "portable", "import", "--file", bundlePath)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastPromoteID != "" {
		t.Fatalf("promoted non-canonical source artifact %q", fc.lastPromoteID)
	}
}

func TestPortableImportDryRunRejectsChangedDestinationCanonical(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	setPortableDestination(t, deps)
	artifact := portableArtifactFixture("local-artifact", "CHECKOUT")
	bundlePath := writePortableBundle(t, local.PortableWorkspace{
		Workspace: local.Workspace{ID: "local-ws", Slug: "local", Name: "Local"},
		Features: []local.PortableFeature{
			{ID: "local-feature", Key: "CHECKOUT", CanonicalArtifactID: artifact.ID, Version: artifact.Version},
		},
		Artifacts: []local.PortableArtifact{artifact},
	})
	fc.featuresResult = []client.Feature{{
		ID: "full-feature", Key: "CHECKOUT", CanonicalArtifactID: "destination-only-artifact",
	}}
	fc.artifactListResult = &client.ArtifactList{Items: []client.Artifact{{
		ID: "full-artifact", FeatureID: "full-feature", Version: "v0.1", Status: "approved",
		SourceKind: "specgate-local-import", SourceID: "local-ws:" + artifact.ID,
		SourceRevision: artifact.SnapshotDigest, SnapshotDigest: artifact.SnapshotDigest,
	}}}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "portable", "import", "--file", bundlePath, "--dry-run")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var env struct {
		Data struct {
			Conflicts  []string `json:"conflicts"`
			WouldWrite bool     `json:"would_write"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if len(env.Data.Conflicts) != 1 || env.Data.Conflicts[0] != "feature canonical differs from source: CHECKOUT" || env.Data.WouldWrite {
		t.Fatalf("preflight = %+v", env.Data)
	}
}

func TestPortableImportResumesArchivedWorkInsteadOfDuplicatingIt(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	setPortableDestination(t, deps)
	artifact := portableArtifactFixture("local-artifact", "CHECKOUT")
	sourceWork := local.WorkItem{
		ID: "local-work", Key: "LOCAL-1", WorkspaceID: "local-ws", FeatureID: "local-feature", ArtifactID: artifact.ID,
		Title: "Deliver checkout", AcceptanceCriteria: []string{"Checkout works"}, Phase: "delivered",
	}
	bundlePath := writePortableBundle(t, local.PortableWorkspace{
		Workspace: local.Workspace{ID: "local-ws", Slug: "local", Name: "Local"},
		Features: []local.PortableFeature{
			{ID: "local-feature", Key: "CHECKOUT", CanonicalArtifactID: artifact.ID, Version: 1},
		},
		Artifacts: []local.PortableArtifact{artifact},
		Work:      []local.WorkItem{sourceWork},
	})
	fc.featuresResult = []client.Feature{{ID: "full-feature", Key: "CHECKOUT", CanonicalArtifactID: "full-artifact"}}
	fc.artifactListResult = &client.ArtifactList{Items: []client.Artifact{{
		ID: "full-artifact", FeatureID: "full-feature", Version: "v0.1", Status: "approved",
		SourceKind: "specgate-local-import", SourceID: "local-ws:" + artifact.ID, SourceRevision: artifact.SnapshotDigest,
		SnapshotDigest: artifact.SnapshotDigest,
	}}}
	sourceRefs, _ := json.Marshal([]string{"specgate-local-work:local-ws:" + sourceWork.ID})
	fc.allWorkItems = []client.WorkItemSummary{{
		ID: "full-work", Key: "CR-FULL", Title: sourceWork.Title, LeadArtifactID: "full-artifact", SourceRefs: string(sourceRefs),
	}}
	fc.acceptanceCriteria = []client.AcceptanceCriterion{{ID: "full-ac", Text: "Checkout works"}}
	fc.createWorkItemResult = map[string]any{"change_request_id": "duplicate-work"}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "portable", "import", "--file", bundlePath)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if !fc.listedArchivedWork {
		t.Fatal("portable retry did not include archived destination work")
	}
	if fc.lastCreateWorkItem != nil {
		t.Fatalf("archived imported work was duplicated: %#v", fc.lastCreateWorkItem)
	}
}

func TestPortableImportDoesNotDuplicateMatchingHumanDeliveryDecision(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	setPortableDestination(t, deps)
	artifact := portableArtifactFixture("local-artifact", "CHECKOUT")
	sourceWork := local.WorkItem{
		ID: "local-work", Key: "LOCAL-1", WorkspaceID: "local-ws", FeatureID: "local-feature", ArtifactID: artifact.ID,
		Title: "Deliver checkout", AcceptanceCriteria: []string{"Checkout works"}, Phase: "delivered",
	}
	bundlePath := writePortableBundle(t, local.PortableWorkspace{
		Workspace: local.Workspace{ID: "local-ws", Slug: "local", Name: "Local"},
		Features: []local.PortableFeature{
			{ID: "local-feature", Key: "CHECKOUT", CanonicalArtifactID: artifact.ID, Version: 1},
		},
		Artifacts: []local.PortableArtifact{artifact},
		Work:      []local.WorkItem{sourceWork},
		Delivery: []local.PortableDeliveryEvidence{{
			WorkID: sourceWork.ID,
			Report: map[string]any{
				"event_type": "coding_agent.completed",
				"criteria": []any{map[string]any{
					"criterion_id": "local-1", "claim": "satisfied",
				}},
			},
			HumanDecision: "approve",
		}},
	})
	fc.featuresResult = []client.Feature{{ID: "full-feature", Key: "CHECKOUT", CanonicalArtifactID: "full-artifact"}}
	fc.artifactListResult = &client.ArtifactList{Items: []client.Artifact{{
		ID: "full-artifact", FeatureID: "full-feature", Version: "v0.1", Status: "approved",
		SourceKind: "specgate-local-import", SourceID: "local-ws:" + artifact.ID, SourceRevision: artifact.SnapshotDigest,
		SnapshotDigest: artifact.SnapshotDigest,
	}}}
	sourceRefs, _ := json.Marshal([]string{"specgate-local-work:local-ws:" + sourceWork.ID})
	fc.workItems = []client.WorkItemSummary{{
		ID: "full-work", Key: "CR-FULL", Title: sourceWork.Title, LeadArtifactID: "full-artifact", SourceRefs: string(sourceRefs),
	}}
	fc.acceptanceCriteria = []client.AcceptanceCriterion{{ID: "full-ac", Text: "Checkout works"}}
	fc.deliveryStatusResult = &client.DeliveryStatusResult{
		ChangeRequestID: "full-work", Found: true, Verdict: "pass", Executor: "human",
		Actor: "full-user", Note: "Imported Local decision:",
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "portable", "import", "--file", bundlePath)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.reportFeedbackCalls != 0 {
		t.Fatalf("matching accepted delivery replayed feedback %d time(s)", fc.reportFeedbackCalls)
	}
	if fc.deliveryReviewCalls != 0 {
		t.Fatalf("matching accepted delivery reran review %d time(s)", fc.deliveryReviewCalls)
	}
	if fc.deliveryDecisionCalls != 0 {
		t.Fatalf("matching human decision was recorded %d additional time(s)", fc.deliveryDecisionCalls)
	}
	if !strings.Contains(out.String(), `"imported_delivery":0`) {
		t.Fatalf("idempotent receipt claimed a delivery write: %s", out.String())
	}
}
