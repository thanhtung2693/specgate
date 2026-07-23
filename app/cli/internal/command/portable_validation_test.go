package command_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestPortableImportRejectsUnsupportedRequestTypeBeforeMutation(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	setPortableDestination(t, deps)
	artifact := portableArtifactFixture("local-artifact", "CHECKOUT")
	artifact.RequestType = "feature"
	bundlePath := writePortableBundle(t, local.PortableWorkspace{
		Workspace: local.Workspace{ID: "local-ws", Slug: "local", Name: "Local"},
		Features: []local.PortableFeature{
			{ID: "local-feature", Key: "CHECKOUT", CanonicalArtifactID: artifact.ID, Version: 1},
		},
		Artifacts: []local.PortableArtifact{artifact},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "portable", "import", "--file", bundlePath, "--dry-run")
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want usage; output = %s", code, out.String())
	}
	if fc.lastPublishBody != nil {
		t.Fatalf("invalid bundle published: %#v", fc.lastPublishBody)
	}
}

func TestPortableImportRejectsInvalidFeatureRelationshipsBeforeMutation(t *testing.T) {
	t.Parallel()
	tests := map[string]local.PortableWorkspace{}
	first := portableArtifactFixture("artifact-1", "CHECKOUT")
	second := portableArtifactFixture("artifact-2", "CHECKOUT")
	draftCanonical := portableArtifactFixture("artifact-draft", "DRAFT")
	draftCanonical.Status = "draft"
	unsupportedStatus := portableArtifactFixture("artifact-status", "STATUS")
	unsupportedStatus.Status = "superseded"
	tests["duplicate feature key"] = local.PortableWorkspace{
		Workspace: local.Workspace{ID: "local-ws", Slug: "local", Name: "Local"},
		Features: []local.PortableFeature{
			{ID: "feature-1", Key: "CHECKOUT", CanonicalArtifactID: first.ID, Version: 1},
			{ID: "feature-2", Key: "CHECKOUT", CanonicalArtifactID: second.ID, Version: 1},
		},
		Artifacts: []local.PortableArtifact{first, second},
	}
	tests["draft canonical artifact"] = local.PortableWorkspace{
		Workspace: local.Workspace{ID: "local-ws", Slug: "local", Name: "Local"},
		Features: []local.PortableFeature{
			{ID: "feature-draft", Key: "DRAFT", CanonicalArtifactID: draftCanonical.ID, Version: draftCanonical.Version},
		},
		Artifacts: []local.PortableArtifact{draftCanonical},
	}
	tests["unsupported local artifact status"] = local.PortableWorkspace{
		Workspace: local.Workspace{ID: "local-ws", Slug: "local", Name: "Local"},
		Artifacts: []local.PortableArtifact{unsupportedStatus},
	}
	tests["canonical version mismatch"] = local.PortableWorkspace{
		Workspace: local.Workspace{ID: "local-ws", Slug: "local", Name: "Local"},
		Features: []local.PortableFeature{
			{ID: "feature-version", Key: "CHECKOUT", CanonicalArtifactID: first.ID, Version: first.Version + 1},
		},
		Artifacts: []local.PortableArtifact{first},
	}
	draftWorkArtifact := portableArtifactFixture("artifact-work-draft", "CHECKOUT")
	draftWorkArtifact.Status = "draft"
	tests["work bound to draft artifact"] = local.PortableWorkspace{
		Workspace: local.Workspace{ID: "local-ws", Slug: "local", Name: "Local"},
		Features: []local.PortableFeature{
			{ID: "feature-work", Key: "CHECKOUT", CanonicalArtifactID: first.ID, Version: first.Version},
		},
		Artifacts: []local.PortableArtifact{first, draftWorkArtifact},
		Work: []local.WorkItem{{
			ID: "work-draft", Key: "LOCAL-DRAFT", WorkspaceID: "local-ws",
			FeatureID: "feature-work", ArtifactID: draftWorkArtifact.ID,
			Title: "Invalid work", AcceptanceCriteria: []string{"Must remain governed"}, Phase: "ready",
		}},
	}
	tests["work with invalid criteria"] = local.PortableWorkspace{
		Workspace: local.Workspace{ID: "local-ws", Slug: "local", Name: "Local"},
		Features: []local.PortableFeature{
			{ID: "feature-criteria", Key: "CHECKOUT", CanonicalArtifactID: first.ID, Version: first.Version},
		},
		Artifacts: []local.PortableArtifact{first},
		Work: []local.WorkItem{{
			ID: "work-criteria", Key: "LOCAL-CRITERIA", WorkspaceID: "local-ws",
			FeatureID: "feature-criteria", ArtifactID: first.ID,
			Title: "Invalid work", AcceptanceCriteria: []string{" duplicate ", "duplicate"}, Phase: "ready",
		}},
	}
	unsafePath := portableArtifactFixture("artifact-unsafe-path", "UNSAFE-PATH")
	unsafePath.Documents[0].Path = "../spec.md"
	unsafePath.SnapshotDigest = featureDocumentManifest(unsafePath.Documents[0].Path, unsafePath.Documents[0].Role, unsafePath.Documents[0].Digest)
	tests["unsafe document path"] = local.PortableWorkspace{
		Workspace: local.Workspace{ID: "local-ws", Slug: "local", Name: "Local"},
		Artifacts: []local.PortableArtifact{unsafePath},
	}
	oversizedDocument := portableArtifactWithDocuments("artifact-large-document", "LARGE-DOCUMENT", []string{strings.Repeat("x", (1<<20)+1)})
	tests["oversized document"] = local.PortableWorkspace{
		Workspace: local.Workspace{ID: "local-ws", Slug: "local", Name: "Local"},
		Artifacts: []local.PortableArtifact{oversizedDocument},
	}
	packageDocuments := make([]string, 11)
	for index := range packageDocuments {
		packageDocuments[index] = strings.Repeat("x", 1<<20)
	}
	oversizedPackage := portableArtifactWithDocuments("artifact-large-package", "LARGE-PACKAGE", packageDocuments)
	tests["oversized package"] = local.PortableWorkspace{
		Workspace: local.Workspace{ID: "local-ws", Slug: "local", Name: "Local"},
		Artifacts: []local.PortableArtifact{oversizedPackage},
	}

	for name, payload := range tests {
		payload := payload
		t.Run(name, func(t *testing.T) {
			deps, fc, _, out := newFakeDeps(t)
			setPortableDestination(t, deps)
			bundlePath := writePortableBundle(t, payload)

			code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "portable", "import", "--file", bundlePath, "--dry-run")
			if code != output.ExitUsage {
				t.Fatalf("exit = %d, want usage; output = %s", code, out.String())
			}
			if fc.calls != 0 {
				t.Fatalf("invalid relationships made %d API calls", fc.calls)
			}
		})
	}
}

func TestPortableImportPreservesUnpromotedDraftArtifact(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	setPortableDestination(t, deps)
	artifact := portableArtifactFixture("local-draft", "DRAFT-ONLY")
	artifact.Status = "draft"
	bundlePath := writePortableBundle(t, local.PortableWorkspace{
		Workspace: local.Workspace{ID: "local-ws", Slug: "local", Name: "Local"},
		Artifacts: []local.PortableArtifact{artifact},
	})
	fc.publishResult = map[string]any{"artifact_id": "full-draft", "version": "v0.1"}
	fc.artifactResult = &client.Artifact{
		ID: "full-draft", Version: "v0.1", Status: "draft", SnapshotDigest: artifact.SnapshotDigest,
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "portable", "import", "--file", bundlePath)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastStatusID != "" || fc.lastPromoteID != "" {
		t.Fatalf("draft import changed governance state: status=%q promote=%q", fc.lastStatusID, fc.lastPromoteID)
	}
}

func TestPortableImportDraftOnlyArtifactConflictsWithUnownedDestinationFeature(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	setPortableDestination(t, deps)
	artifact := portableArtifactFixture("local-draft", "DRAFT-ONLY")
	artifact.Status = "draft"
	bundlePath := writePortableBundle(t, local.PortableWorkspace{
		Workspace: local.Workspace{ID: "local-ws", Slug: "local", Name: "Local"},
		Artifacts: []local.PortableArtifact{artifact},
	})
	fc.featuresResult = []client.Feature{{ID: "unowned-feature", Key: artifact.FeatureKey}}

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
	if len(env.Data.Conflicts) != 1 || env.Data.Conflicts[0] != "feature key already exists: DRAFT-ONLY" {
		t.Fatalf("conflicts = %#v", env.Data.Conflicts)
	}
}

func setPortableDestination(t *testing.T, deps *command.Deps) {
	t.Helper()
	if err := (config.Config{
		Mode:        config.ModeFull,
		CurrentUser: config.CurrentUser{ID: "full-user-id", Username: "full-user", DisplayName: "Full User"},
		Workspace:   config.CurrentWorkspace{ID: "full-ws", Slug: "full", Name: "Full"},
	}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
}

func portableArtifactFixture(id, featureKey string) local.PortableArtifact {
	return portableArtifactWithDocuments(id, featureKey, []string{"# Checkout\n"})
}

func portableArtifactWithDocuments(id, featureKey string, contents []string) local.PortableArtifact {
	documents := make([]local.PortableArtifactDocument, 0, len(contents))
	for index, content := range contents {
		contentSum := sha256.Sum256([]byte(content))
		documents = append(documents, local.PortableArtifactDocument{
			Path: fmt.Sprintf("spec-%02d.md", index), Role: "spec", Content: content,
			Digest: "sha256:" + hex.EncodeToString(contentSum[:]),
		})
	}
	return local.PortableArtifact{
		ID: id, FeatureKey: featureKey, RequestType: "new_feature", Version: 1, Status: "approved",
		SnapshotDigest: portableDocumentManifest(documents),
		Documents:      documents,
	}
}

func featureDocumentManifest(path, role, digest string) string {
	sum := sha256.Sum256([]byte(path + "\x00" + role + "\x00" + digest + "\n"))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func portableDocumentManifest(documents []local.PortableArtifactDocument) string {
	ordered := append([]local.PortableArtifactDocument(nil), documents...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Path != ordered[j].Path {
			return ordered[i].Path < ordered[j].Path
		}
		return ordered[i].Role < ordered[j].Role
	})
	hash := sha256.New()
	for _, document := range ordered {
		_, _ = fmt.Fprintf(hash, "%s\x00%s\x00%s\n", document.Path, document.Role, document.Digest)
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

func writePortableBundle(t *testing.T, payload local.PortableWorkspace) string {
	t.Helper()
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(payloadJSON)
	bundle := map[string]any{
		"schema_version": "specgate.portable/v1",
		"source_mode":    "local",
		"exported_at":    "2026-07-17T00:00:00Z",
		"payload":        payload,
		"checksum":       "sha256:" + hex.EncodeToString(sum[:]),
	}
	data, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "specgate-portable.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
