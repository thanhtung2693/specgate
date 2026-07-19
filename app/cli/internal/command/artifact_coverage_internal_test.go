package command

import (
	"reflect"
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
