package artifact

import "testing"

func TestTableNames(t *testing.T) {
	t.Parallel()
	if (Artifact{}).TableName() != "artifacts" {
		t.Fatal()
	}
	if (ServiceRef{}).TableName() != "artifact_services" {
		t.Fatal()
	}
	if (File{}).TableName() != "artifact_files" {
		t.Fatal()
	}
	if (Event{}).TableName() != "artifact_events" {
		t.Fatal()
	}
}

func TestStatusConstants(t *testing.T) {
	t.Parallel()
	if StatusDraft != "draft" || StatusApproved != "approved" {
		t.Fatal()
	}
}
