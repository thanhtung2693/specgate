package command

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/fsutil"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

const (
	portableSchemaVersion            = "specgate.portable/v1"
	portableWorkSourcePrefix         = "specgate-local-work:"
	portableArtifactDocumentMaxBytes = 1 << 20
	portableArtifactPackageMaxBytes  = 10 << 20
	portableBundleMaxBytes           = 64 << 20
)

type portableBundle struct {
	SchemaVersion string                  `json:"schema_version"`
	SourceMode    config.Mode             `json:"source_mode"`
	ExportedAt    string                  `json:"exported_at"`
	Payload       local.PortableWorkspace `json:"payload"`
	Checksum      string                  `json:"checksum"`
}

type portablePreflight struct {
	SourceWorkspace      string   `json:"source_workspace"`
	DestinationWorkspace string   `json:"destination_workspace"`
	Artifacts            int      `json:"artifacts"`
	Work                 int      `json:"work"`
	Gates                int      `json:"gates"`
	Delivery             int      `json:"delivery"`
	Conflicts            []string `json:"conflicts"`
	WouldWrite           bool     `json:"would_write"`
	existingArtifacts    map[string]client.Artifact
	existingWork         map[string]client.WorkItemSummary
	existingFeatures     map[string]client.Feature
}

type portableImportResult struct {
	portablePreflight
	ImportedArtifacts int               `json:"imported_artifacts"`
	ImportedWork      int               `json:"imported_work"`
	ImportedGates     int               `json:"imported_gates"`
	ImportedDelivery  int               `json:"imported_delivery"`
	ArtifactMapping   map[string]string `json:"artifact_mapping"`
	WorkMapping       map[string]string `json:"work_mapping"`
}

func newPortableCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "portable",
		Short: "Export Local governance data or import it into Full mode",
	}
	cmd.AddCommand(newPortableExportCmd(deps))
	cmd.AddCommand(newPortableImportCmd(deps))
	return cmd
}

func newPortableExportCmd(deps *Deps) *cobra.Command {
	var filePath string
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export the selected Local workspace as a checksummed bundle",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if deps.Topology != config.ModeLocal {
				return incompatibleCommand(deps, "portable.export", "portable export is available only in Local mode")
			}
			if strings.TrimSpace(filePath) == "" {
				return completionValidationError(deps, "portable.export", "--file is required")
			}
			if err := rejectPortableStateDestination(deps, filePath); err != nil {
				return completionValidationError(deps, "portable.export", err.Error())
			}
			store, err := openLocalStore(deps)
			if err != nil {
				return localExitError(deps, "portable.export", err)
			}
			defer store.Close()
			selection, err := localSelection(cmd.Context(), deps, store)
			if err != nil {
				return localExitError(deps, "portable.export", err)
			}
			payload, err := store.ExportWorkspace(cmd.Context(), selection.Workspace.ID)
			if err != nil {
				return localExitError(deps, "portable.export", err)
			}
			bundle := portableBundle{
				SchemaVersion: portableSchemaVersion,
				SourceMode:    config.ModeLocal,
				ExportedAt:    time.Now().UTC().Format(time.RFC3339),
				Payload:       payload,
				Checksum:      jsonChecksum(payload),
			}
			if err := writePortableBundle(filePath, bundle); err != nil {
				return localExitError(deps, "portable.export", err)
			}
			result := map[string]any{
				"path": filePath, "schema_version": bundle.SchemaVersion, "checksum": bundle.Checksum,
				"workspace": payload.Workspace.Slug, "artifacts": len(payload.Artifacts), "work": len(payload.Work),
				"gates": len(payload.Gates), "delivery": len(payload.Delivery),
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("portable.export", result)
				return nil
			}
			fmt.Fprintf(deps.Stdout, "Exported %s (%d artifacts, %d work items)\n", filePath, len(payload.Artifacts), len(payload.Work))
			return nil
		},
	}
	cmd.Flags().StringVar(&filePath, "file", "", "Destination JSON bundle (required)")
	return cmd
}

func rejectPortableStateDestination(deps *Deps, destination string) error {
	statePath, err := localStatePath(deps)
	if err != nil {
		return err
	}
	target, err := canonicalComparisonPath(destination)
	if err != nil {
		return err
	}
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		protectedPath := statePath + suffix
		protected, err := canonicalComparisonPath(protectedPath)
		if err != nil {
			return err
		}
		samePath := target == protected
		if runtime.GOOS == "windows" {
			samePath = strings.EqualFold(target, protected)
		}
		if samePath || sameExistingFile(destination, protectedPath) {
			return fmt.Errorf("export destination cannot be the active Local SQLite file %s; choose a different --file path", protectedPath)
		}
	}
	return nil
}

func canonicalComparisonPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	current := filepath.Clean(absolute)
	var suffix []string
	for {
		if _, err := os.Lstat(current); err == nil {
			real, err := filepath.EvalSymlinks(current)
			if err != nil {
				return "", err
			}
			for index := len(suffix) - 1; index >= 0; index-- {
				real = filepath.Join(real, suffix[index])
			}
			return filepath.Clean(real), nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return filepath.Clean(absolute), nil
		}
		suffix = append(suffix, filepath.Base(current))
		current = parent
	}
}

func sameExistingFile(left, right string) bool {
	leftInfo, leftErr := os.Stat(left)
	rightInfo, rightErr := os.Stat(right)
	return leftErr == nil && rightErr == nil && os.SameFile(leftInfo, rightInfo)
}

func newPortableImportCmd(deps *Deps) *cobra.Command {
	var (
		filePath string
		dryRun   bool
	)
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Preflight or import a Local bundle into the selected Full workspace",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if deps.Topology != config.ModeFull {
				return incompatibleCommand(deps, "portable.import", "portable import requires Full mode")
			}
			if strings.TrimSpace(filePath) == "" {
				return completionValidationError(deps, "portable.import", "--file is required")
			}
			bundle, err := readPortableBundle(filePath)
			if err != nil {
				return completionValidationError(deps, "portable.import", err.Error())
			}
			workspaceID, err := currentWorkspaceID(cmd.Context(), deps)
			if err != nil {
				return apiExitError(deps, "portable.import", err)
			}
			cfg, _ := config.LoadFrom(deps.ConfigPath)
			actor := strings.TrimSpace(cfg.CurrentUser.Username)
			if workspaceID == "" || actor == "" {
				return completionValidationError(deps, "portable.import", "select a destination workspace and user before import")
			}
			preflight, err := preflightPortableImport(cmd, deps, bundle, workspaceID)
			if err != nil {
				return apiExitError(deps, "portable.import", err)
			}
			if dryRun {
				if deps.Printer.Mode() == output.ModeJSON {
					deps.Printer.Success("portable.import", preflight)
				} else {
					printPortablePreflight(deps, preflight)
				}
				return nil
			}
			if len(preflight.Conflicts) > 0 {
				payload := output.ErrorPayload{
					Code: "conflict", Message: "portable import conflicts must be resolved before mutation",
					Details: map[string]any{"conflicts": preflight.Conflicts},
				}
				code := deps.Printer.Error("portable.import", payload)
				return &output.ExitError{Code: code}
			}
			proceed, err := requireConfirm(deps, fmt.Sprintf("Import %d artifacts and %d work items into workspace %s?", preflight.Artifacts, preflight.Work, preflight.DestinationWorkspace))
			if err != nil || !proceed {
				return err
			}
			result, err := executePortableImport(cmd, deps, bundle, preflight, workspaceID, actor)
			if err != nil {
				return apiExitError(deps, "portable.import", err)
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("portable.import", result)
			} else {
				fmt.Fprintf(deps.Stdout, "Imported %d artifacts and %d work items into %s\n", result.ImportedArtifacts, result.ImportedWork, result.DestinationWorkspace)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&filePath, "file", "", "Portable JSON bundle (required)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Report mapping and all conflicts without writing")
	return cmd
}

func incompatibleCommand(deps *Deps, commandName, message string) error {
	payload := output.ErrorPayload{Code: "incompatible", Message: message}
	code := deps.Printer.Error(commandName, payload)
	return &output.ExitError{Code: code}
}

// jsonChecksum is the digest format shared by every checksummed bundle the CLI
// writes, so an exported file and its verifier can never disagree on it.
func jsonChecksum(payload any) string {
	data, _ := json.Marshal(payload)
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func writePortableBundle(path string, bundle portableBundle) error {
	data, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if len(data) > portableBundleMaxBytes {
		return fmt.Errorf("portable bundle exceeds the 64 MiB limit")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return fsutil.AtomicWriteFile(path, data, 0o600)
}

func readPortableBundle(path string) (portableBundle, error) {
	file, err := os.Open(path)
	if err != nil {
		return portableBundle{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return portableBundle{}, err
	}
	if !info.Mode().IsRegular() {
		return portableBundle{}, fmt.Errorf("portable bundle must be a regular file")
	}
	if info.Size() > portableBundleMaxBytes {
		return portableBundle{}, fmt.Errorf("portable bundle exceeds the 64 MiB limit")
	}
	data, err := io.ReadAll(io.LimitReader(file, portableBundleMaxBytes+1))
	if err != nil {
		return portableBundle{}, err
	}
	if len(data) > portableBundleMaxBytes {
		return portableBundle{}, fmt.Errorf("portable bundle exceeds the 64 MiB limit")
	}
	var bundle portableBundle
	if err := json.Unmarshal(data, &bundle); err != nil {
		return bundle, fmt.Errorf("read portable bundle: %w", err)
	}
	if bundle.SchemaVersion != portableSchemaVersion || bundle.SourceMode != config.ModeLocal {
		return bundle, fmt.Errorf("unsupported portable bundle %q from mode %q", bundle.SchemaVersion, bundle.SourceMode)
	}
	if bundle.Payload.Workspace.ID == "" || bundle.Payload.Workspace.Slug == "" {
		return bundle, fmt.Errorf("portable bundle has no source workspace mapping")
	}
	if got := jsonChecksum(bundle.Payload); got != bundle.Checksum {
		return bundle, fmt.Errorf("portable bundle checksum mismatch")
	}
	if err := validatePortableRelationships(bundle.Payload); err != nil {
		return bundle, err
	}
	return bundle, nil
}

func validatePortableRelationships(payload local.PortableWorkspace) error {
	artifacts := make(map[string]local.PortableArtifact, len(payload.Artifacts))
	features := make(map[string]local.PortableFeature, len(payload.Features))
	featureKeys := make(map[string]local.PortableFeature, len(payload.Features))
	work := make(map[string]bool, len(payload.Work))
	for _, artifact := range payload.Artifacts {
		if artifact.ID == "" || strings.TrimSpace(artifact.FeatureKey) == "" || artifact.Version <= 0 || artifacts[artifact.ID].ID != "" {
			return fmt.Errorf("portable bundle contains an invalid or duplicate artifact")
		}
		switch artifact.RequestType {
		case "new_feature", "change_request", "bugfix", "unknown":
		default:
			return fmt.Errorf("artifact %s has unsupported request type %q", artifact.ID, artifact.RequestType)
		}
		switch artifact.Status {
		case "draft", "approved":
		default:
			return fmt.Errorf("artifact %s has unsupported Local status %q", artifact.ID, artifact.Status)
		}
		seenPaths := make(map[string]bool, len(artifact.Documents))
		packageBytes := 0
		for _, document := range artifact.Documents {
			normalizedPath, safe := normalizeArtifactDocumentPath(document.Path)
			normalizedRole := normalizeArtifactDocumentRole(document.Role)
			if !safe || normalizedPath != document.Path || normalizedRole != document.Role || seenPaths[document.Path] {
				return fmt.Errorf("artifact %s contains an invalid or duplicate document", artifact.ID)
			}
			if len(document.Content) > portableArtifactDocumentMaxBytes {
				return fmt.Errorf("artifact %s document %q exceeds the 1 MiB limit", artifact.ID, document.Path)
			}
			packageBytes += len(document.Content)
			if packageBytes > portableArtifactPackageMaxBytes {
				return fmt.Errorf("artifact %s package exceeds the 10 MiB limit", artifact.ID)
			}
			seenPaths[document.Path] = true
		}
		if digest := portableArtifactDigest(artifact); digest != artifact.SnapshotDigest {
			return fmt.Errorf("artifact %s content digest mismatch", artifact.ID)
		}
		artifacts[artifact.ID] = artifact
	}
	for _, feature := range payload.Features {
		if feature.ID == "" || feature.Key == "" || features[feature.ID].ID != "" || featureKeys[feature.Key].ID != "" {
			return fmt.Errorf("portable bundle contains an invalid feature")
		}
		canonical := artifacts[feature.CanonicalArtifactID]
		if canonical.ID == "" {
			return fmt.Errorf("feature %s references missing canonical artifact %s", feature.Key, feature.CanonicalArtifactID)
		}
		if canonical.FeatureKey != feature.Key {
			return fmt.Errorf("feature %s canonical artifact belongs to feature %s", feature.Key, canonical.FeatureKey)
		}
		if canonical.Status != "approved" || canonical.Version != feature.Version {
			return fmt.Errorf("feature %s canonical artifact is not the approved feature version", feature.Key)
		}
		features[feature.ID] = feature
		featureKeys[feature.Key] = feature
	}
	for _, item := range payload.Work {
		sourceArtifact := artifacts[item.ArtifactID]
		quickRoute := item.FeatureID == "" && item.ArtifactID == ""
		artifactRoute := features[item.FeatureID].ID != "" && sourceArtifact.ID != ""
		if item.ID == "" || work[item.ID] || strings.TrimSpace(item.Title) == "" || (!quickRoute && !artifactRoute) {
			return fmt.Errorf("work %s has an invalid feature or artifact relationship", item.Key)
		}
		if item.WorkspaceID != payload.Workspace.ID {
			return fmt.Errorf("work %s belongs to workspace %s, want %s", item.Key, item.WorkspaceID, payload.Workspace.ID)
		}
		if artifactRoute && sourceArtifact.FeatureKey != features[item.FeatureID].Key {
			return fmt.Errorf("work %s artifact does not belong to feature %s", item.Key, features[item.FeatureID].Key)
		}
		if artifactRoute && sourceArtifact.Status != "approved" {
			return fmt.Errorf("work %s is bound to an unapproved artifact", item.Key)
		}
		if item.Phase != "ready" && item.Phase != "delivered" {
			return fmt.Errorf("work %s has unsupported Local phase %q", item.Key, item.Phase)
		}
		if !validPortableCriteria(item.AcceptanceCriteria) {
			return fmt.Errorf("work %s has invalid acceptance criteria", item.Key)
		}
		work[item.ID] = true
	}
	for _, gate := range payload.Gates {
		artifact := artifacts[gate.ArtifactID]
		if artifact.ID == "" {
			return fmt.Errorf("gate %s references missing artifact %s", gate.GateKey, gate.ArtifactID)
		}
		if gate.ArtifactDigest != artifact.SnapshotDigest {
			return fmt.Errorf("gate %s artifact digest does not match artifact %s", gate.GateKey, gate.ArtifactID)
		}
	}
	deliveryWork := make(map[string]bool, len(payload.Delivery))
	for _, delivery := range payload.Delivery {
		if !work[delivery.WorkID] {
			return fmt.Errorf("delivery evidence references missing work %s", delivery.WorkID)
		}
		if deliveryWork[delivery.WorkID] {
			return fmt.Errorf("portable bundle contains duplicate delivery evidence for work %s", delivery.WorkID)
		}
		deliveryWork[delivery.WorkID] = true
	}
	return nil
}

func validPortableCriteria(criteria []string) bool {
	if len(criteria) == 0 {
		return false
	}
	seen := make(map[string]bool, len(criteria))
	for _, criterion := range criteria {
		if criterion == "" || criterion != strings.TrimSpace(criterion) || seen[criterion] {
			return false
		}
		seen[criterion] = true
	}
	return true
}

func portableArtifactDigest(artifact local.PortableArtifact) string {
	documents := append([]local.PortableArtifactDocument(nil), artifact.Documents...)
	sort.Slice(documents, func(i, j int) bool {
		if documents[i].Path != documents[j].Path {
			return documents[i].Path < documents[j].Path
		}
		return documents[i].Role < documents[j].Role
	})
	hash := sha256.New()
	for _, document := range documents {
		content := sha256.Sum256([]byte(document.Content))
		contentDigest := "sha256:" + hex.EncodeToString(content[:])
		if document.Digest != contentDigest {
			return ""
		}
		fmt.Fprintf(hash, "%s\x00%s\x00%s\n", document.Path, document.Role, document.Digest)
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}
