package local

import (
	"context"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

func TestSQLiteDSNBuildsHierarchicalWindowsFileURIAndEscapesPath(t *testing.T) {
	dsn := sqliteDSN("C:/Users/Jane #1/spec?gate.db")

	if strings.HasPrefix(dsn, "file:C:") {
		t.Fatalf("Windows drive path became an opaque URI: %s", dsn)
	}
	if !strings.Contains(dsn, "%23") || !strings.Contains(dsn, "%3F") {
		t.Fatalf("reserved path characters are not escaped: %s", dsn)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Scheme != "file" || parsed.Path != "/C:/Users/Jane #1/spec?gate.db" {
		t.Fatalf("parsed URI = %#v", parsed)
	}
	if parsed.Query().Get("_txlock") != "immediate" {
		t.Fatalf("SQLite options missing from %s", dsn)
	}
}

func TestValidateSourceCriterionMappingsRejectsUnknownTags(t *testing.T) {
	criteria := []string{"Known @source:req-1", "Unknown @source:req-2"}
	err := validateSourceCriterionMappings(criteria, []SourceCriterion{{ID: "req-1"}})
	if err == nil || !strings.Contains(err.Error(), "req-2") {
		t.Fatalf("error = %v", err)
	}
}

func TestListArtifactsRejectsMalformedSourceCriteria(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	selection, err := store.Initialize(context.Background(), InitInput{WorkspaceName: "Alpha", DisplayName: "Human", Username: "human"})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := store.PublishArtifact(context.Background(), selection.Workspace.ID, ArtifactInput{
		FeatureKey: "LOCAL-MALFORMED-CRITERIA", RequestType: "new_feature",
		Documents: []ArtifactDocumentInput{{Path: "spec.md", Role: "spec", Content: []byte("# Spec")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`UPDATE artifacts SET source_criteria_json = '{' WHERE id = ?`, artifact.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ListArtifacts(context.Background(), selection.Workspace.ID); err == nil {
		t.Fatal("ListArtifacts accepted malformed source criteria")
	}
}
