package command

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestArtifactCoverageUsesExactArtifactAssociation(t *testing.T) {
	t.Parallel()

	localItems := localWorkCoverage([]local.WorkItem{
		{Key: "LOCAL-OTHER", ArtifactID: "artifact-other", Phase: "delivered", Title: "Other version"},
		{Key: "LOCAL-THIS", ArtifactID: "artifact-this", Phase: "ready", Title: "This version"},
	}, "artifact-this")
	fullItems := fullWorkCoverage([]client.WorkItemSummary{
		{Key: "SG-OTHER", LeadArtifactID: "artifact-other", Phase: "delivered", Title: "Other version"},
		{Key: "SG-THIS", LeadArtifactID: "artifact-this", Phase: "ready", Title: "This version"},
	}, "artifact-this")

	if got := artifactCoverageView("artifact-this", "approved", localItems); got["state"] != "in_progress" {
		t.Fatalf("local state = %q, want in_progress", got["state"])
	}
	if got := artifactCoverageView("artifact-this", "approved", fullItems); got["state"] != "in_progress" {
		t.Fatalf("full state = %q, want in_progress", got["state"])
	}
	if !reflect.DeepEqual(localItems, []map[string]string{{"key": "LOCAL-THIS", "phase": "ready", "title": "This version"}}) {
		t.Fatalf("local linked items = %#v", localItems)
	}
	if !reflect.DeepEqual(fullItems, []map[string]string{{"key": "SG-THIS", "phase": "ready", "title": "This version"}}) {
		t.Fatalf("full linked items = %#v", fullItems)
	}
}

func TestArtifactCoverageDeliveredRequiresDeliveredWork(t *testing.T) {
	t.Parallel()

	if got := artifactCoverageView("artifact-this", "approved", []map[string]string{{"key": "SG-1", "phase": "ready", "title": "Ready"}}); got["state"] != "in_progress" {
		t.Fatalf("ready state = %q, want in_progress", got["state"])
	}
	if got := artifactCoverageView("artifact-this", "approved", []map[string]string{{"key": "SG-1", "phase": "delivered", "title": "Approved"}}); got["state"] != "delivered" {
		t.Fatalf("delivered state = %q, want delivered", got["state"])
	}
	if got := artifactCoverageView("artifact-this", "approved", []map[string]string{{"key": "SG-1", "phase": "Delivered", "title": "API response"}}); got["state"] != "delivered" {
		t.Fatalf("API delivered state = %q, want delivered", got["state"])
	}
	mixed := []map[string]string{
		{"key": "SG-1", "phase": "delivered", "title": "Delivered slice"},
		{"key": "SG-2", "phase": "ready", "title": "Open slice"},
	}
	if got := artifactCoverageView("artifact-this", "approved", mixed); got["state"] != "in_progress" {
		t.Fatalf("mixed state = %q, want in_progress", got["state"])
	}
	if got := artifactCoverageView("artifact-this", "superseded", nil); got["state"] != "superseded" {
		t.Fatalf("superseded state = %q, want superseded", got["state"])
	}
}

func TestLocalArtifactCoverageReportsSourceCriterionCoverage(t *testing.T) {
	artifact := local.Artifact{ID: "artifact-this", SourceCriteria: []local.SourceCriterion{{ID: "req-1"}}}
	items := []local.WorkItem{{ArtifactID: "artifact-this", Phase: "delivered", AcceptanceCriteria: []string{"Result @source:req-1"}}}
	if got := localArtifactCoverageView(artifact, items)["source_coverage"]; got != "delivered" {
		t.Fatalf("source coverage = %q, want delivered", got)
	}
}

func TestLocalArtifactCoverageIncludesExactSourceRequirementRows(t *testing.T) {
	artifact := local.Artifact{ID: "artifact-this", SourceCriteria: []local.SourceCriterion{
		{ID: "req-linked", Text: "Keep the endpoint", SourcePath: "spec.md"},
		{ID: "req-missing", Text: "Add an alert", SourcePath: "spec.md"},
	}}
	items := []local.WorkItem{
		{Key: "LOCAL-2", Title: "Other version", ArtifactID: "artifact-other", Phase: "delivered", AcceptanceCriteria: []string{"Alert exists @source:req-missing"}},
		{Key: "LOCAL-1", Title: "Keep endpoint", ArtifactID: "artifact-this", Phase: "delivered", AcceptanceCriteria: []string{"Endpoint stays @source:req-linked"}},
	}

	view := localArtifactCoverageView(artifact, items)
	rows, ok := view["source_requirements"].([]sourceRequirementCoverage)
	if !ok || len(rows) != 2 {
		t.Fatalf("source rows = %#v", view["source_requirements"])
	}
	if rows[0].State != "delivered" || len(rows[0].WorkItems) != 1 || rows[0].WorkItems[0] != (sourceRequirementWork{Key: "LOCAL-1", Title: "Keep endpoint", Phase: "delivered"}) {
		t.Fatalf("linked requirement = %#v", rows[0])
	}
	if rows[1].State != "unassigned" || len(rows[1].WorkItems) != 0 {
		t.Fatalf("missing requirement = %#v", rows[1])
	}
	if got := view["source_next_action"]; got != "specgate artifact show artifact-this --json" {
		t.Fatalf("source next = %q", got)
	}
}

func TestArtifactCoverageHumanOutputShowsEverySourceRequirementAndLink(t *testing.T) {
	var out bytes.Buffer
	data := map[string]any{
		"artifact_id": "artifact-this", "state": "delivered", "source_coverage": "unassigned", "work_items": []map[string]string{},
		"source_requirements": []sourceRequirementCoverage{
			{ID: "req-delivered", Text: "Keep endpoint", SourcePath: "spec.md", State: "delivered", WorkItems: []sourceRequirementWork{{Key: "LOCAL-1", Title: "Keep endpoint", Phase: "delivered"}}},
			{ID: "req-missing", Text: "Add alert", SourcePath: "spec.md", State: "unassigned", WorkItems: []sourceRequirementWork{}},
			{ID: "req-deferred", Text: "Add dashboard", SourcePath: "spec.md", State: "deferred", DeferredReason: "After the first release", WorkItems: []sourceRequirementWork{}},
		},
		"source_next_action": "specgate artifact show artifact-this --json",
	}
	if err := printArtifactCoverage(&Deps{Stdout: &out, Printer: output.NewWithCapabilities(&out, io.Discard, output.ModeHuman, false, false)}, data); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"[delivered] req-delivered — Keep endpoint (spec.md)", "LOCAL-1 [delivered] Keep endpoint", "[unassigned] req-missing — Add alert (spec.md)", "[deferred] req-deferred — Add dashboard (spec.md) — After the first release", "Source next: specgate artifact show artifact-this --json"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, out.String())
		}
	}
}

func TestArtifactCoverageHumanOutputDoesNotRenderSourceControlCharacters(t *testing.T) {
	var out bytes.Buffer
	data := map[string]any{
		"artifact_id": "artifact-this", "state": "uncovered", "source_coverage": "unassigned", "work_items": []map[string]string{},
		"source_requirements": []sourceRequirementCoverage{{
			ID: "req-alert", Text: "Add alert\nnow\x1b[31m", SourcePath: "specs/health\t.md", State: "deferred", DeferredReason: "Later\r",
			WorkItems: []sourceRequirementWork{{Key: "LOCAL-1", Title: "Do alert\nwork", Phase: "ready"}},
		}},
	}
	if err := printArtifactCoverage(&Deps{Stdout: &out, Printer: output.NewWithCapabilities(&out, io.Discard, output.ModeHuman, false, false)}, data); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"Add alert now", "[31m (specs/health .md) — Later", "LOCAL-1 [ready] Do alert work"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing sanitized %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "\x1b") {
		t.Fatalf("output preserved terminal escape: %q", got)
	}
}

func TestWorkspaceCoverageHumanOutputIncludesSourceCoverage(t *testing.T) {
	var out bytes.Buffer
	requirements := []sourceRequirementCoverage{
		{ID: "req-missing", Text: "Add readiness output", SourcePath: "spec.md", State: "unassigned", WorkItems: []sourceRequirementWork{}},
		{ID: "req-deferred", Text: "Add dashboard", SourcePath: "spec.md", State: "deferred", DeferredReason: "Later", WorkItems: []sourceRequirementWork{}},
	}
	printWorkspaceCoverage(&Deps{Stdout: &out}, workspaceCoverage{
		Workspace: "Alpha",
		Counts:    map[string]int{},
		Specifications: []specificationCoverage{{
			FeatureKey: "LOCAL-1", Version: "v1", ArtifactID: "artifact-1", State: "delivered", SourceCoverage: "unassigned", SourceRequirements: &requirements, SourceNextAction: "specgate artifact show artifact-1 --json",
		}},
	})
	for _, want := range []string{"Source requirements: unassigned", "[unassigned] req-missing — Add readiness output (spec.md)", "[deferred] req-deferred — Add dashboard (spec.md) — Later", "Source next: specgate artifact show artifact-1 --json"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("human coverage output omitted %q:\n%s", want, out.String())
		}
	}
}

func TestSourceRequirementRowsShowUnassignedDeferredAndLinkedWork(t *testing.T) {
	criteria := []sourceCriterion{
		{ID: "req-delivered", Text: "Keep the health endpoint", SourcePath: "specs/health.md"},
		{ID: "req-missing", Text: "Add readiness output", SourcePath: "specs/health.md"},
		{ID: "req-deferred", Text: "Add a dashboard", SourcePath: "specs/health.md", DeferredReason: "Not in this release"},
		{ID: "req-open", Text: "Add an integration test", SourcePath: "specs/health.md"},
	}
	work := []coverageWork{
		{Key: "LOCAL-05", Title: "Earlier health slice", Current: true, Phase: "delivered", AcceptanceCriteria: []string{"Health endpoint stays @source:req-delivered"}},
		{Key: "LOCAL-20", Title: "Finish integration", Current: true, Phase: "ready", AcceptanceCriteria: []string{"Integration test exists @source:req-open"}},
		{Key: "LOCAL-10", Title: "Keep health", Current: true, Phase: "delivered", AcceptanceCriteria: []string{"Health endpoint exists @source:req-delivered", "Same source again @source:req-delivered"}},
		{Key: "LOCAL-99", Title: "Old artifact", Current: false, Phase: "delivered", AcceptanceCriteria: []string{"Old result @source:req-missing"}},
		{Key: "LOCAL-30", Title: "Deferred work", Current: true, Phase: "delivered", AcceptanceCriteria: []string{"Dashboard exists @source:req-deferred"}},
	}

	got := sourceRequirementRows(criteria, work)
	if len(got) != 4 {
		t.Fatalf("row count = %d, want 4", len(got))
	}
	if got[0].State != "delivered" || len(got[0].WorkItems) != 2 || got[0].WorkItems[0].Key != "LOCAL-05" || got[0].WorkItems[1].Key != "LOCAL-10" {
		t.Fatalf("delivered row = %#v", got[0])
	}
	if got[1].State != "unassigned" || len(got[1].WorkItems) != 0 {
		t.Fatalf("unassigned row = %#v", got[1])
	}
	if got[2].State != "deferred" || got[2].DeferredReason != "Not in this release" || len(got[2].WorkItems) != 1 {
		t.Fatalf("deferred row = %#v", got[2])
	}
	if got[3].State != "in_progress" || len(got[3].WorkItems) != 1 || got[3].WorkItems[0].Key != "LOCAL-20" {
		t.Fatalf("open row = %#v", got[3])
	}
}

func TestWorkspaceCoverageAddsSourceNextActionForUnassignedRequirement(t *testing.T) {
	result := buildWorkspaceCoverage(config.ModeLocal, "workspace", "Alpha",
		[]coverageFeature{{ID: "feature", Key: "health", CanonicalArtifactID: "artifact-current"}},
		[]coverageArtifact{{ID: "artifact-current", FeatureID: "feature", Version: "v2", SourceCriteria: []sourceCriterion{
			{ID: "req-delivered", Text: "Keep health", SourcePath: "spec.md"},
			{ID: "req-missing", Text: "Add ready", SourcePath: "spec.md"},
		}}},
		[]coverageWork{{Key: "LOCAL-1", Title: "Keep health", ArtifactID: "artifact-current", Phase: "delivered", AcceptanceCriteria: []string{"Keep health @source:req-delivered"}}},
	)

	if len(result.Specifications) != 1 {
		t.Fatalf("specifications = %#v", result.Specifications)
	}
	spec := result.Specifications[0]
	if spec.State != "delivered" || spec.SourceCoverage != "unassigned" {
		t.Fatalf("coverage state = %+v", spec)
	}
	if spec.SourceNextAction != "specgate artifact show artifact-current --json" {
		t.Fatalf("source next = %q", spec.SourceNextAction)
	}
	if spec.SourceRequirements == nil || len(*spec.SourceRequirements) != 2 || (*spec.SourceRequirements)[1].State != "unassigned" {
		t.Fatalf("source requirements = %#v", spec.SourceRequirements)
	}
}

func TestLocalWorkspaceCoverageSerializesEmptySourceRequirementRowsWithoutInventory(t *testing.T) {
	result := buildWorkspaceCoverage(config.ModeLocal, "workspace", "Alpha",
		[]coverageFeature{{ID: "feature", Key: "health", CanonicalArtifactID: "artifact-current"}},
		[]coverageArtifact{{ID: "artifact-current", FeatureID: "feature", Version: "v1"}},
		nil,
	)
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Specifications []struct {
			SourceCoverage     string                      `json:"source_coverage"`
			SourceRequirements []sourceRequirementCoverage `json:"source_requirements"`
		} `json:"specifications"`
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if got := decoded.Specifications[0]; got.SourceCoverage != "unknown" || got.SourceRequirements == nil || len(got.SourceRequirements) != 0 {
		t.Fatalf("local source detail = %#v", got)
	}
}

func TestFullWorkspaceCoverageDoesNotSerializeLocalSourceRequirementRows(t *testing.T) {
	result := buildWorkspaceCoverage(config.ModeFull, "workspace", "Alpha",
		[]coverageFeature{{ID: "feature", Key: "health", CanonicalArtifactID: "artifact-current"}},
		[]coverageArtifact{{ID: "artifact-current", FeatureID: "feature", Version: "v1", SourceCriteria: []sourceCriterion{{ID: "req-1", Text: "Local only"}}}},
		nil,
	)
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "source_requirements") || strings.Contains(string(encoded), "source_next_action") {
		t.Fatalf("Full coverage leaked Local-only source detail: %s", encoded)
	}
}

func TestSourceCoverageRequiresEveryExplicitMappingToBeDelivered(t *testing.T) {
	criteria := []sourceCriterion{{ID: "req-1"}, {ID: "req-2"}}
	if got := sourceCoverage(criteria, []coverageWork{{Current: true, Phase: "delivered", AcceptanceCriteria: []string{"one @source:req-1"}}}); got != "unassigned" {
		t.Fatalf("missing mapping = %q", got)
	}
	if got := sourceCoverage(criteria, []coverageWork{{Current: true, Phase: "ready", AcceptanceCriteria: []string{"one @source:req-1", "two @source:req-2"}}}); got != "accounted_for" {
		t.Fatalf("open mapping = %q", got)
	}
	if got := sourceCoverage(criteria, []coverageWork{{Current: true, Phase: "delivered", AcceptanceCriteria: []string{"one @source:req-1", "two @source:req-2"}}}); got != "delivered" {
		t.Fatalf("delivered mapping = %q", got)
	}
	if got := sourceCoverage([]sourceCriterion{{ID: "req-1"}}, []coverageWork{{Current: true, Phase: "delivered", AcceptanceCriteria: []string{"other @source:req-10"}}}); got != "unassigned" {
		t.Fatalf("prefix match = %q", got)
	}
}

func TestSourceCoverageUsesOnlyCanonicalWorkAndPrioritizesUnassignedCriteria(t *testing.T) {
	if got := sourceCoverage([]sourceCriterion{{ID: "req-1"}}, []coverageWork{{Current: false, Phase: "delivered", AcceptanceCriteria: []string{"@source:req-1"}}}); got != "unassigned" {
		t.Fatalf("stale work coverage = %q, want unassigned", got)
	}
	criteria := []sourceCriterion{{ID: "req-1"}, {ID: "req-2"}}
	if got := sourceCoverage(criteria, []coverageWork{{Current: true, Phase: "ready", AcceptanceCriteria: []string{"@source:req-1"}}}); got != "unassigned" {
		t.Fatalf("partial mapping coverage = %q, want unassigned", got)
	}
}

func TestSourceCoverageDeferredCriterionNeverDeliversSnapshot(t *testing.T) {
	if got := sourceCoverage([]sourceCriterion{{ID: "req-1", DeferredReason: "Out of scope"}}, nil); got != "accounted_for" {
		t.Fatalf("deferred coverage = %q", got)
	}
}
