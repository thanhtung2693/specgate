package command_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestCapabilitiesLocalJSONUsesStableManifest(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	stateDir := t.TempDir()
	store, err := local.Open(filepath.Join(stateDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Initialize(context.Background(), local.InitInput{
		WorkspaceName: "Local workspace",
		DisplayName:   "Local developer",
		Username:      "local",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := (config.Config{
		Mode:  config.ModeLocal,
		Local: config.LocalStore{Path: stateDir},
	}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "capabilities")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}

	var env struct {
		Data struct {
			Mode         string `json:"mode"`
			Capabilities []struct {
				ID    string `json:"id"`
				State string `json:"state"`
			} `json:"capabilities"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal output: %v\n%s", err, out.String())
	}
	if env.Data.Mode != "local" {
		t.Fatalf("mode = %q, want local", env.Data.Mode)
	}
	states := capabilityStates(env.Data.Capabilities)
	if states["governed_delivery"] != "available" {
		t.Fatalf("governed_delivery = %q, want available; output = %s", states["governed_delivery"], out.String())
	}
	if states["web_ui"] != "unavailable" || states["platform_model"] != "unavailable" {
		t.Fatalf("optional capability states = %#v, output = %s", states, out.String())
	}
}

func TestCapabilitiesFullJSONPreservesConfigurationRequired(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.metaResult = &client.Meta{
		APIVersion: "specgate.api/v1",
		WebURL:     "https://specgate.test",
		CapabilityDetails: map[string]client.CapabilityDetail{
			"agents": {State: "available"},
			"platform_model": {
				State:       "configuration_required",
				Reason:      "choose a model provider and API key",
				NextCommand: "specgate model set",
			},
			"web_ui": {State: "available"},
		},
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "capabilities")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}

	var env struct {
		Data struct {
			Mode         string `json:"mode"`
			Capabilities []struct {
				ID    string `json:"id"`
				State string `json:"state"`
			} `json:"capabilities"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal output: %v\n%s", err, out.String())
	}
	states := capabilityStates(env.Data.Capabilities)
	if env.Data.Mode != "full" || states["platform_model"] != "configuration_required" {
		t.Fatalf("unexpected manifest: mode=%q states=%#v", env.Data.Mode, states)
	}
}

func TestVerifyFullJSONReportsArtifactCriteriaChecksAndCloseout(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.resolvedWork = &client.ResolvedWork{
		ChangeRequestID:  "cr-1",
		ChangeRequestKey: "CR-101",
		Title:            "Close the loop",
		Phase:            "Delivered",
	}
	fc.contextPack = &client.ContextPackResult{
		State:      "assembled",
		ArtifactID: "artifact-1",
	}
	fc.artifactResult = &client.Artifact{
		ID:             "artifact-1",
		Version:        "v3",
		Status:         "approved",
		SnapshotDigest: "sha256:abc",
	}
	fc.acceptanceCriteria = []client.AcceptanceCriterion{
		{ID: "ac-1", Text: "Every criterion has evidence", Done: true},
	}
	fc.deliveryStatusResult = &client.DeliveryStatusResult{
		ChangeRequestID: "cr-1",
		Found:           true,
		Verdict:         "pass",
		Summary:         "All evidence is present.",
		PerCriterion: []client.CriterionReview{
			{CriterionID: "ac-1", Text: "Every criterion has evidence", Verdict: "pass", Why: "targeted test passed"},
		},
		Checks: []client.CheckResult{
			{Name: "go test ./...", Status: "pass", Detail: "ok"},
		},
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "verify", "CR-101")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}

	var env struct {
		Data struct {
			Work struct {
				Key   string `json:"key"`
				Phase string `json:"phase"`
			} `json:"work"`
			Artifact struct {
				ID             string `json:"id"`
				Version        string `json:"version"`
				SnapshotDigest string `json:"snapshot_digest"`
			} `json:"artifact"`
			Criteria        []client.CriterionReview `json:"criteria"`
			Checks          []client.CheckResult     `json:"checks"`
			DeliveryVerdict string                   `json:"delivery_verdict"`
			CleanupEligible bool                     `json:"cleanup_eligible"`
			NextCommand     string                   `json:"next_command"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal output: %v\n%s", err, out.String())
	}
	if env.Data.Work.Key != "CR-101" || env.Data.Artifact.ID != "artifact-1" || env.Data.Artifact.Version != "v3" {
		t.Fatalf("wrong work/artifact identity: %+v", env.Data)
	}
	if len(env.Data.Criteria) != 1 || env.Data.Criteria[0].Verdict != "pass" {
		t.Fatalf("criteria = %#v", env.Data.Criteria)
	}
	if len(env.Data.Checks) != 1 || env.Data.Checks[0].Status != "pass" {
		t.Fatalf("checks = %#v", env.Data.Checks)
	}
	if env.Data.DeliveryVerdict != "pass" || !env.Data.CleanupEligible || env.Data.NextCommand == "" {
		t.Fatalf("closeout = %+v", env.Data)
	}
}

func TestVerifyFullAcceptedQuickWorkIsCleanupEligibleWithoutArtifact(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.resolvedWork = &client.ResolvedWork{
		ChangeRequestID: "cr-quick", ChangeRequestKey: "CR-QUICK",
		Title: "Quick fix", Phase: "Delivered",
	}
	fc.contextPack = &client.ContextPackResult{State: "assembled"}
	fc.acceptanceCriteria = []client.AcceptanceCriterion{{ID: "ac-1", Text: "Fixed", Done: true}}
	fc.deliveryStatusResult = &client.DeliveryStatusResult{
		ChangeRequestID: "cr-quick", Found: true, Verdict: "pass",
		PerCriterion: []client.CriterionReview{{CriterionID: "ac-1", Text: "Fixed", Verdict: "pass"}},
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "verify", "CR-QUICK")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var envelope struct {
		Data struct {
			Artifact        *struct{} `json:"artifact"`
			QuickRoute      bool      `json:"quick_route"`
			CleanupEligible bool      `json:"cleanup_eligible"`
			NextCommand     string    `json:"next_command"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Artifact != nil || !envelope.Data.QuickRoute ||
		!envelope.Data.CleanupEligible || envelope.Data.NextCommand != "specgate cleanup --work --dry-run" {
		t.Fatalf("accepted Full quick closeout is not actionable: %s", out.String())
	}
}

func TestVerifyLocalRequiresAndReportsHumanApprovedDelivery(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	stateDir := t.TempDir()
	store, err := local.Open(filepath.Join(stateDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	selection, err := store.Initialize(context.Background(), local.InitInput{
		WorkspaceName: "Local workspace",
		DisplayName:   "Local developer",
		Username:      "local",
	})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := store.PublishArtifact(context.Background(), selection.Workspace.ID, local.ArtifactInput{
		FeatureKey:  "CLOSEOUT",
		RequestType: "new_feature",
		Documents: []local.ArtifactDocumentInput{
			{Path: "spec.md", Role: "spec", Content: []byte("# Closeout\n\n## Acceptance criteria\n\n1. Evidence is visible.")},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RunReadiness(context.Background(), selection.Workspace.ID, artifact.ID); err != nil {
		t.Fatal(err)
	}
	tasks, err := store.ListGateTasks(context.Background(), selection.Workspace.ID, artifact.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		result := local.GateResultInput{
			Gate:        task.GateKey,
			GateDigest:  task.GateDigest,
			InputDigest: task.ArtifactDigest,
			State:       "pass",
			Summary:     "reviewed",
		}
		result.Evaluator.Executor = task.Executor
		if _, err := store.SubmitGateResult(context.Background(), selection.Workspace.ID, task.TaskID, result); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.ApproveArtifact(context.Background(), selection.Workspace.ID, artifact.ID, "human", "approved"); err != nil {
		t.Fatal(err)
	}
	feature, err := store.PromoteArtifact(context.Background(), selection.Workspace.ID, artifact.ID)
	if err != nil {
		t.Fatal(err)
	}
	work, err := store.CreateWork(context.Background(), selection.Workspace.ID, local.WorkInput{
		FeatureRef:         feature.Key,
		Title:              "Prove closeout",
		AcceptanceCriteria: []string{"Evidence is visible."},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.SubmitDelivery(context.Background(), selection.Workspace.ID, work.Key, map[string]any{
		"context_digest": work.ContextDigest,
		"agent":          map[string]any{"name": "builder"},
		"criteria": []any{
			map[string]any{
				"criterion_id": "local-1",
				"claim":        "satisfied",
				"evidence":     map[string]any{"summary": "targeted test passed"},
			},
		},
		"checks": []any{
			map[string]any{"name": "go test ./...", "status": "pass", "detail": "ok"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DecideDelivery(context.Background(), selection.Workspace.ID, work.Key, "approve", "human", "verified"); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := (config.Config{
		Mode:  config.ModeLocal,
		Local: config.LocalStore{Path: stateDir},
	}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "verify", work.Key)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var env struct {
		Data struct {
			Mode            string                   `json:"mode"`
			Criteria        []client.CriterionReview `json:"criteria"`
			Checks          []client.CheckResult     `json:"checks"`
			DeliveryVerdict string                   `json:"delivery_verdict"`
			CleanupEligible bool                     `json:"cleanup_eligible"`
			NextCommand     string                   `json:"next_command"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal output: %v\n%s", err, out.String())
	}
	if env.Data.Mode != "local" || env.Data.DeliveryVerdict != "passed" || !env.Data.CleanupEligible {
		t.Fatalf("closeout = %+v", env.Data)
	}
	if len(env.Data.Criteria) != 1 || env.Data.Criteria[0].Verdict != "pass" || len(env.Data.Checks) != 1 {
		t.Fatalf("evidence = %+v", env.Data)
	}
	if env.Data.NextCommand != "specgate cleanup --work --dry-run" {
		t.Fatalf("next command = %q", env.Data.NextCommand)
	}
}

func TestVerifyLocalSupportsAcceptedQuickWorkWithoutArtifact(t *testing.T) {
	deps, _, _, out := newFakeDeps(t)
	stateDir := t.TempDir()
	store, err := local.Open(filepath.Join(stateDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	selection, err := store.Initialize(t.Context(), local.InitInput{
		WorkspaceName: "Local workspace", DisplayName: "Local developer", Username: "local",
	})
	if err != nil {
		t.Fatal(err)
	}
	work, err := store.CreateQuickWork(t.Context(), selection.Workspace.ID, local.QuickWorkInput{
		Title: "Fix timeout", AcceptanceCriteria: []string{"Retries stop @check:unit"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SubmitDelivery(t.Context(), selection.Workspace.ID, work.Key, map[string]any{
		"context_digest": work.ContextDigest,
		"agent":          map[string]any{"name": "builder"},
		"criteria": []any{map[string]any{
			"criterion_id": "local-1", "claim": "satisfied",
			"evidence": map[string]any{"summary": "unit check passed"},
		}},
		"checks": []any{map[string]any{"name": "unit", "status": "pass"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.DecideDelivery(t.Context(), selection.Workspace.ID, work.Key, "approve", "human", "verified"); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := (config.Config{Mode: config.ModeLocal, Local: config.LocalStore{Path: stateDir}}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "verify", work.Key)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var envelope struct {
		Data struct {
			Artifact        *struct{}                `json:"artifact"`
			QuickRoute      bool                     `json:"quick_route"`
			Criteria        []client.CriterionReview `json:"criteria"`
			CleanupEligible bool                     `json:"cleanup_eligible"`
			NextCommand     string                   `json:"next_command"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Artifact != nil || !envelope.Data.QuickRoute {
		t.Fatalf("quick work unexpectedly has artifact: %s", out.String())
	}
	if len(envelope.Data.Criteria) != 1 || envelope.Data.Criteria[0].VerificationBinding != "unit" {
		t.Fatalf("criteria = %#v", envelope.Data.Criteria)
	}
	if !envelope.Data.CleanupEligible || envelope.Data.NextCommand != "specgate cleanup --work --dry-run" {
		t.Fatalf("accepted quick closeout is not actionable: %s", out.String())
	}
}

func TestVerifyLocalHumanOverrideIsCleanupEligibleWithoutHidingEvidenceGap(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	stateDir, store, selection, work := newLocalChangeWork(t, deps)
	review, err := store.SubmitDelivery(t.Context(), selection.Workspace.ID, work.Key, map[string]any{
		"context_digest": work.ContextDigest,
		"agent":          map[string]any{"name": "builder"},
		"criteria": []any{map[string]any{
			"criterion_id": "local-1",
			"claim":        "unsatisfied",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if review.Verdict != "failed" {
		t.Fatalf("review = %#v, want failed evidence", review)
	}
	if err := store.DecideDelivery(
		t.Context(),
		selection.Workspace.ID,
		work.Key,
		"approve",
		"human",
		"reviewed false negative",
	); err != nil {
		t.Fatal(err)
	}
	closeLocalChangeStore(t, deps, stateDir, store)

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "verify", work.Key)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var env struct {
		Data struct {
			DeliveryVerdict string `json:"delivery_verdict"`
			CleanupEligible bool   `json:"cleanup_eligible"`
			NextCommand     string `json:"next_command"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Data.DeliveryVerdict != "failed" || !env.Data.CleanupEligible {
		t.Fatalf("closeout = %+v, want accepted authority with failed evidence disclosed", env.Data)
	}
	if env.Data.NextCommand != "specgate cleanup --work --dry-run" {
		t.Fatalf("next command = %q, want cleanup after explicit human acceptance", env.Data.NextCommand)
	}
}

func capabilityStates[T interface {
	~struct {
		ID    string `json:"id"`
		State string `json:"state"`
	}
}](items []T) map[string]string {
	states := make(map[string]string, len(items))
	for _, item := range items {
		data, _ := json.Marshal(item)
		var row struct {
			ID    string `json:"id"`
			State string `json:"state"`
		}
		_ = json.Unmarshal(data, &row)
		states[row.ID] = row.State
	}
	return states
}
