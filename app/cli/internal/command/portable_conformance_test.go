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
