package command

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/local"
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

func TestWorkspaceCoverageHumanOutputIncludesSourceCoverage(t *testing.T) {
	var out bytes.Buffer
	printWorkspaceCoverage(&Deps{Stdout: &out}, workspaceCoverage{
		Workspace: "Alpha",
		Counts:    map[string]int{},
		Specifications: []specificationCoverage{{
			FeatureKey: "LOCAL-1", Version: "v1", ArtifactID: "artifact-1", State: "delivered", SourceCoverage: "unassigned",
		}},
	})
	if !strings.Contains(out.String(), "Source requirements: unassigned") {
		t.Fatalf("human coverage output omitted source requirements:\n%s", out.String())
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
