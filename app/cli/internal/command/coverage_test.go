package command_test

import (
	"encoding/json"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestCoverageFullClassifiesCanonicalSpecificationDelivery(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	setWorkListWorkspace(t, deps)
	fc.featuresResult = []client.Feature{
		{ID: "feature-a", Key: "A", Name: "Delivered", Version: 99, CanonicalArtifactID: "artifact-a2"},
		{ID: "feature-b", Key: "B", Name: "Uncovered", Version: 1, CanonicalArtifactID: "artifact-b1"},
		{ID: "feature-c", Key: "C", Name: "Stale", Version: 2, CanonicalArtifactID: "artifact-c2"},
		{ID: "feature-d", Key: "D", Name: "Unfinished", Version: 1, CanonicalArtifactID: "artifact-d1"},
	}
	fc.artifactListResult = &client.ArtifactList{Items: []client.Artifact{
		{ID: "artifact-a2", FeatureID: "feature-a", Version: "v2", Status: "approved"},
		{ID: "artifact-b1", FeatureID: "feature-b", Version: "v1", Status: "approved"},
		{ID: "artifact-c1", FeatureID: "feature-c", Version: "v1", Status: "superseded"},
		{ID: "artifact-c2", FeatureID: "feature-c", Version: "v2", Status: "approved"},
		{ID: "artifact-d1", FeatureID: "feature-d", Version: "v1", Status: "approved"},
	}}
	fc.allWorkItems = []client.WorkItemSummary{
		{Key: "CR-A", Title: "Deliver A", Phase: "Delivered", LeadArtifactID: "artifact-a2"},
		{Key: "CR-C", Title: "Deliver old C", Phase: "delivered", LeadArtifactID: "artifact-c1"},
		{Key: "CR-D", Title: "Build D", Phase: "ready", LeadArtifactID: "artifact-d1"},
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "coverage")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var env struct {
		Data struct {
			WorkspaceID    string         `json:"workspace_id"`
			Counts         map[string]int `json:"counts"`
			Specifications []struct {
				FeatureKey string `json:"feature_key"`
				ArtifactID string `json:"artifact_id"`
				Version    string `json:"version"`
				State      string `json:"state"`
				NextAction string `json:"next_action"`
				WorkItems  []struct {
					Key        string `json:"key"`
					ArtifactID string `json:"artifact_id"`
				} `json:"work_items"`
			} `json:"specifications"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal output: %v\n%s", err, out.String())
	}
	if env.Data.WorkspaceID != "ws-phase" {
		t.Fatalf("workspace_id = %q", env.Data.WorkspaceID)
	}
	if !fc.listedArchivedWork {
		t.Fatal("workspace coverage omitted archived delivered work")
	}
	states := map[string]string{}
	for _, spec := range env.Data.Specifications {
		states[spec.FeatureKey] = spec.State
		if spec.ArtifactID == "" {
			t.Fatalf("%s omitted governing artifact", spec.FeatureKey)
		}
		if spec.FeatureKey == "A" && spec.Version != "v2" {
			t.Fatalf("version = %q, want canonical artifact version v2", spec.Version)
		}
		if spec.State != "delivered" && spec.NextAction == "" {
			t.Fatalf("%s omitted next action: %+v", spec.FeatureKey, spec)
		}
	}
	want := map[string]string{"A": "delivered", "B": "uncovered", "C": "stale", "D": "unfinished"}
	for key, state := range want {
		if states[key] != state {
			t.Fatalf("%s state = %q, want %q; all=%#v", key, states[key], state, states)
		}
	}
	if env.Data.Counts["delivered"] != 1 || env.Data.Counts["uncovered"] != 1 || env.Data.Counts["stale"] != 1 || env.Data.Counts["unfinished"] != 1 {
		t.Fatalf("counts = %#v", env.Data.Counts)
	}
}

func TestCoverageFullReadsEveryArtifactPage(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	setWorkListWorkspace(t, deps)
	fc.featuresResult = []client.Feature{
		{ID: "feature-a", Key: "A", Name: "Delivered", Version: 2, CanonicalArtifactID: "artifact-a2"},
	}
	firstPage := make([]client.Artifact, 200)
	for index := range firstPage {
		firstPage[index] = client.Artifact{ID: "filler-" + string(rune(index+1)), FeatureID: "other"}
	}
	fc.artifactListResults = map[int]*client.ArtifactList{
		0:   {Items: firstPage, Total: 201},
		200: {Items: []client.Artifact{{ID: "artifact-a2", FeatureID: "feature-a", Version: "v2"}}, Total: 201},
	}
	fc.workItems = []client.WorkItemSummary{
		{Key: "CR-A", Title: "Deliver A", Phase: "delivered", LeadArtifactID: "artifact-a2"},
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "coverage")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var env struct {
		Data struct {
			Counts map[string]int `json:"counts"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Data.Counts["delivered"] != 1 || fc.lastArtifactFilter.Offset != 200 {
		t.Fatalf("coverage did not consume both artifact pages: counts=%v filter=%+v", env.Data.Counts, fc.lastArtifactFilter)
	}
}

func TestCoverageFullRemainsUnfinishedWhileAnyCurrentWorkIsOpen(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	setWorkListWorkspace(t, deps)
	fc.featuresResult = []client.Feature{
		{ID: "feature-a", Key: "A", Name: "Mixed", CanonicalArtifactID: "artifact-a"},
	}
	fc.artifactListResult = &client.ArtifactList{Items: []client.Artifact{
		{ID: "artifact-a", FeatureID: "feature-a", Version: "v1", Status: "approved"},
	}}
	fc.workItems = []client.WorkItemSummary{
		{Key: "CR-DONE", Phase: "delivered", LeadArtifactID: "artifact-a"},
		{Key: "CR-OPEN", Phase: "ready", LeadArtifactID: "artifact-a"},
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "coverage")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var env struct {
		Data struct {
			Specifications []struct {
				State      string `json:"state"`
				NextAction string `json:"next_action"`
			} `json:"specifications"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if len(env.Data.Specifications) != 1 {
		t.Fatalf("specifications = %#v", env.Data.Specifications)
	}
	specification := env.Data.Specifications[0]
	if specification.State != "unfinished" || specification.NextAction != "specgate verify CR-OPEN" {
		t.Fatalf("specification = %#v, want unfinished CR-OPEN", specification)
	}
}
