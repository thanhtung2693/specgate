package command

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/local"
)

func TestWritePortableBundleRejectsOversizedOutputBeforeReplacingDestination(t *testing.T) {
	path := filepath.Join(t.TempDir(), "portable.json")
	if err := os.WriteFile(path, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	bundle := portableBundle{
		SchemaVersion: portableSchemaVersion,
		SourceMode:    config.ModeLocal,
		Payload: local.PortableWorkspace{
			Workspace: local.Workspace{
				ID:   "workspace",
				Slug: "workspace",
				Name: strings.Repeat("x", portableBundleMaxBytes),
			},
		},
	}

	err := writePortableBundle(path, bundle)
	if err == nil || !strings.Contains(err.Error(), "64 MiB") {
		t.Fatalf("error = %v, want 64 MiB limit", err)
	}
	body, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(body) != "keep" {
		t.Fatalf("destination replaced with %d bytes", len(body))
	}
}

func TestPortableRelationshipConformance(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("testdata", "portable-conformance.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		SchemaVersion string `json:"schema_version"`
		Cases         []struct {
			Name    string                  `json:"name"`
			Valid   bool                    `json:"valid"`
			Error   string                  `json:"error"`
			Payload local.PortableWorkspace `json:"payload"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.SchemaVersion != "specgate.portable-conformance/v1" || len(fixture.Cases) == 0 {
		t.Fatalf("invalid conformance fixture header: %q", fixture.SchemaVersion)
	}
	for _, testCase := range fixture.Cases {
		testCase := testCase
		t.Run(testCase.Name, func(t *testing.T) {
			err := validatePortableRelationships(testCase.Payload)
			if testCase.Valid {
				if err != nil {
					t.Fatalf("valid fixture rejected: %v", err)
				}
				return
			}
			if err == nil || err.Error() != testCase.Error {
				t.Fatalf("error = %v, want %q", err, testCase.Error)
			}
		})
	}
}

func TestPortableBundleReadsMultiRoleLocalArtifact(t *testing.T) {
	store, err := local.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	sel, err := store.Initialize(t.Context(), local.InitInput{WorkspaceName: "Multi-role", Username: "fixture", DisplayName: "Fixture"})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := store.PublishArtifact(t.Context(), sel.Workspace.ID, local.ArtifactInput{
		FeatureKey: "ONE-DOC", RequestType: "new_feature",
		Documents: []local.ArtifactDocumentInput{
			{Path: "design.md", Role: "spec", Content: []byte("One source owns specification and plan.")},
			{Path: "design.md", Role: "plan", Content: []byte("One source owns specification and plan.")},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := store.ExportWorkspace(t.Context(), sel.Workspace.ID)
	if err != nil {
		t.Fatal(err)
	}
	bundle := portableBundle{SchemaVersion: portableSchemaVersion, SourceMode: config.ModeLocal, Payload: payload, Checksum: jsonChecksum(payload)}
	path := filepath.Join(t.TempDir(), "export.json")
	if err := writePortableBundle(path, bundle); err != nil {
		t.Fatal(err)
	}
	got, err := readPortableBundle(path)
	if err != nil {
		t.Fatalf("reader refused valid Local multi-role export: %v", err)
	}
	if len(got.Payload.Artifacts) != 1 || len(got.Payload.Artifacts[0].Documents) != 2 || got.Payload.Artifacts[0].SnapshotDigest != artifact.SnapshotDigest {
		t.Fatalf("multi-role snapshot changed: %#v", got.Payload.Artifacts)
	}
	roles := map[string]string{}
	for _, doc := range got.Payload.Artifacts[0].Documents {
		if doc.Path != "design.md" {
			t.Fatalf("source path changed: %q", doc.Path)
		}
		roles[doc.Role] = doc.Content
	}
	for _, role := range []string{"spec", "plan"} {
		if roles[role] != "One source owns specification and plan." {
			t.Fatalf("missing or changed role %s: %#v", role, roles)
		}
	}
	// Distinct roles are valid, but a duplicate of the exact pair is not.
	bundle.Payload.Artifacts[0].Documents = append(bundle.Payload.Artifacts[0].Documents, bundle.Payload.Artifacts[0].Documents[0])
	bundle.Checksum = jsonChecksum(bundle.Payload)
	if err := writePortableBundle(path, bundle); err != nil {
		t.Fatal(err)
	}
	if _, err := readPortableBundle(path); err == nil || !strings.Contains(err.Error(), "invalid or duplicate document") {
		t.Fatalf("duplicate path/role accepted: %v", err)
	}
}
