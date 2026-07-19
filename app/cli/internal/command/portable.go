package command

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
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
				Checksum:      portableChecksum(payload),
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
			return fmt.Errorf("portable export destination cannot be the active Local SQLite file %s; choose a different --file path", protectedPath)
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

func portableChecksum(payload local.PortableWorkspace) string {
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
	if got := portableChecksum(bundle.Payload); got != bundle.Checksum {
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

func preflightPortableImport(cmd *cobra.Command, deps *Deps, bundle portableBundle, workspaceID string) (portablePreflight, error) {
	result := portablePreflight{
		SourceWorkspace: bundle.Payload.Workspace.Slug, DestinationWorkspace: workspaceID,
		Artifacts: len(bundle.Payload.Artifacts), Work: len(bundle.Payload.Work),
		Gates: len(bundle.Payload.Gates), Delivery: len(bundle.Payload.Delivery),
		Conflicts:         []string{},
		existingArtifacts: map[string]client.Artifact{},
		existingWork:      map[string]client.WorkItemSummary{},
		existingFeatures:  map[string]client.Feature{},
	}
	features, err := listFeaturesForWorkspace(cmd.Context(), deps, workspaceID, "")
	if err != nil {
		return result, err
	}
	for _, feature := range features {
		result.existingFeatures[feature.Key] = feature
	}
	artifacts, err := listAllArtifacts(cmd.Context(), deps, client.ArtifactFilter{WorkspaceID: workspaceID})
	if err != nil {
		return result, err
	}
	bySourceID := make(map[string][]client.Artifact, len(artifacts))
	for _, artifact := range artifacts {
		if artifact.SourceKind == "specgate-local-import" {
			bySourceID[artifact.SourceID] = append(bySourceID[artifact.SourceID], artifact)
		}
	}
	for _, artifact := range bundle.Payload.Artifacts {
		matches := bySourceID[portableArtifactSourceID(bundle.Payload.Workspace.ID, artifact.ID)]
		switch {
		case len(matches) > 1:
			result.Conflicts = append(result.Conflicts, "multiple destination artifacts claim source: "+artifact.ID)
		case len(matches) == 1:
			match := matches[0]
			if match.SourceRevision != artifact.SnapshotDigest || match.SnapshotDigest != artifact.SnapshotDigest {
				result.Conflicts = append(result.Conflicts, "artifact import differs from source: "+artifact.ID)
			} else {
				result.existingArtifacts[artifact.ID] = match
			}
		}
	}
	sourceFeatureKeys := make(map[string]bool, len(bundle.Payload.Features))
	for _, artifact := range bundle.Payload.Artifacts {
		sourceFeatureKeys[artifact.FeatureKey] = true
	}
	ownedFeatureKeys := make(map[string]bool, len(sourceFeatureKeys))
	for featureKey := range sourceFeatureKeys {
		existing, ok := result.existingFeatures[featureKey]
		if !ok {
			continue
		}
		owned := false
		exactArtifactIDs := make(map[string]bool)
		for _, sourceArtifact := range bundle.Payload.Artifacts {
			imported, exact := result.existingArtifacts[sourceArtifact.ID]
			if sourceArtifact.FeatureKey == featureKey && exact && imported.FeatureID == existing.ID {
				owned = true
				exactArtifactIDs[imported.ID] = true
			}
		}
		if !owned {
			result.Conflicts = append(result.Conflicts, "feature key already exists: "+featureKey)
			continue
		}
		ownedFeatureKeys[featureKey] = true
		for _, destinationArtifact := range artifacts {
			if destinationArtifact.FeatureID == existing.ID && !exactArtifactIDs[destinationArtifact.ID] {
				result.Conflicts = append(result.Conflicts, "feature contains destination-only artifact: "+featureKey)
				break
			}
		}
	}
	for _, sourceFeature := range bundle.Payload.Features {
		if !ownedFeatureKeys[sourceFeature.Key] {
			continue
		}
		existing, exists := result.existingFeatures[sourceFeature.Key]
		if !exists || existing.CanonicalArtifactID == "" {
			continue
		}
		importedCanonical, exact := result.existingArtifacts[sourceFeature.CanonicalArtifactID]
		if !exact || importedCanonical.ID != existing.CanonicalArtifactID {
			result.Conflicts = append(result.Conflicts, "feature canonical differs from source: "+sourceFeature.Key)
		}
	}
	workItems, err := deps.Client.ListWorkItemsIncludingArchived(cmd.Context(), workspaceID)
	if err != nil {
		return result, err
	}
	byWorkSource := map[string][]client.WorkItemSummary{}
	for _, item := range workItems {
		for _, sourceRef := range portableWorkSourceRefs(item.SourceRefs) {
			byWorkSource[sourceRef] = append(byWorkSource[sourceRef], item)
		}
	}
	for _, sourceWork := range bundle.Payload.Work {
		matches := byWorkSource[portableWorkSourceRef(bundle.Payload.Workspace.ID, sourceWork.ID)]
		if len(matches) > 1 {
			result.Conflicts = append(result.Conflicts, "multiple destination work items claim source: "+sourceWork.ID)
			continue
		}
		if len(matches) == 0 {
			continue
		}
		match := matches[0]
		importedArtifact, artifactExists := result.existingArtifacts[sourceWork.ArtifactID]
		quickRouteMatches := sourceWork.ArtifactID == "" && match.LeadArtifactID == ""
		artifactRouteMatches := artifactExists && match.LeadArtifactID == importedArtifact.ID
		if match.ID == "" || match.Title != sourceWork.Title || (!quickRouteMatches && !artifactRouteMatches) {
			result.Conflicts = append(result.Conflicts, "work import differs from source: "+sourceWork.ID)
			continue
		}
		criteria, err := deps.Client.ListAcceptanceCriteria(cmd.Context(), match.ID)
		if err != nil {
			return result, err
		}
		if !portableCriteriaEqual(sourceWork.AcceptanceCriteria, criteria) {
			result.Conflicts = append(result.Conflicts, "work acceptance criteria differ from source: "+sourceWork.ID)
			continue
		}
		result.existingWork[sourceWork.ID] = match
	}
	sort.Strings(result.Conflicts)
	result.WouldWrite = len(result.Conflicts) == 0
	return result, nil
}

func portableArtifactSourceID(workspaceID, artifactID string) string {
	return url.QueryEscape(strings.TrimSpace(workspaceID)) + ":" + url.QueryEscape(strings.TrimSpace(artifactID))
}

func portableWorkSourceRef(workspaceID, workID string) string {
	return portableWorkSourcePrefix + url.QueryEscape(strings.TrimSpace(workspaceID)) + ":" + url.QueryEscape(strings.TrimSpace(workID))
}

func portableWorkSourceRefs(raw string) []string {
	var refs []string
	if json.Unmarshal([]byte(raw), &refs) != nil {
		return nil
	}
	var sourceRefs []string
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if strings.HasPrefix(ref, portableWorkSourcePrefix) {
			sourceRefs = append(sourceRefs, ref)
		}
	}
	return sourceRefs
}

func portableCriteriaEqual(source []string, destination []client.AcceptanceCriterion) bool {
	if len(source) != len(destination) {
		return false
	}
	for index := range source {
		text, binding := parseAcceptanceCriterionBinding(source[index])
		if text != destination[index].Text || binding != destination[index].VerificationBinding {
			return false
		}
	}
	return true
}

func executePortableImport(cmd *cobra.Command, deps *Deps, bundle portableBundle, preflight portablePreflight, workspaceID, actor string) (portableImportResult, error) {
	result := portableImportResult{
		portablePreflight: preflight,
		ArtifactMapping:   map[string]string{},
		WorkMapping:       map[string]string{},
	}
	featureByID := make(map[string]local.PortableFeature, len(bundle.Payload.Features))
	canonicalByFeature := make(map[string]string, len(bundle.Payload.Features))
	for _, feature := range bundle.Payload.Features {
		featureByID[feature.ID] = feature
		canonicalByFeature[feature.Key] = feature.CanonicalArtifactID
	}
	workByArtifact := make(map[string][]local.WorkItem)
	for _, item := range bundle.Payload.Work {
		workByArtifact[item.ArtifactID] = append(workByArtifact[item.ArtifactID], item)
	}
	deliveryByWork := make(map[string]local.PortableDeliveryEvidence)
	for _, delivery := range bundle.Payload.Delivery {
		deliveryByWork[delivery.WorkID] = delivery
	}
	artifacts := append([]local.PortableArtifact(nil), bundle.Payload.Artifacts...)
	sort.Slice(artifacts, func(i, j int) bool {
		if artifacts[i].FeatureKey != artifacts[j].FeatureKey {
			return artifacts[i].FeatureKey < artifacts[j].FeatureKey
		}
		return artifacts[i].Version < artifacts[j].Version
	})
	previousVersion := map[string]string{}
	for _, source := range artifacts {
		imported, resumed := preflight.existingArtifacts[source.ID]
		artifactID := imported.ID
		if !resumed {
			documents := make([]map[string]any, 0, len(source.Documents))
			for _, document := range source.Documents {
				documents = append(documents, map[string]any{"path": document.Path, "role": document.Role, "content": document.Content})
			}
			body := map[string]any{
				"feature_key": source.FeatureKey, "feature_name": source.FeatureKey,
				"workspace_id": workspaceID, "created_by": actor,
				"request_type": source.RequestType,
				"source_kind":  "specgate-local-import",
				"source_id":    portableArtifactSourceID(bundle.Payload.Workspace.ID, source.ID), "source_revision": source.SnapshotDigest,
				"documents": documents,
			}
			if base := previousVersion[source.FeatureKey]; base != "" {
				body["base_version"] = base
			}
			published, err := deps.Client.PublishArtifact(requestContextForBody(cmd.Context(), body), body)
			if err != nil {
				return result, err
			}
			artifactID = strings.TrimSpace(fmt.Sprint(published["artifact_id"]))
			if artifactID == "" {
				return result, fmt.Errorf("portable import publish returned no artifact id for %s", source.ID)
			}
			publishedArtifact, err := deps.Client.GetArtifact(cmd.Context(), artifactID)
			if err != nil {
				return result, err
			}
			imported = *publishedArtifact
			result.ImportedArtifacts++
		}
		previousVersion[source.FeatureKey] = imported.Version
		if imported.SnapshotDigest != source.SnapshotDigest {
			return result, fmt.Errorf("imported artifact %s content digest changed: got %s, want %s", source.ID, imported.SnapshotDigest, source.SnapshotDigest)
		}
		result.ArtifactMapping[source.ID] = artifactID
		linkedWork := workByArtifact[source.ID]
		if source.Status == "approved" || len(linkedWork) > 0 {
			if imported.Status != "approved" && imported.Status != "superseded" {
				if _, err := deps.Client.UpdateArtifactStatus(cmd.Context(), artifactID, client.UpdateArtifactStatusInput{
					Status: "approved", ApprovedBy: actor, ActorKind: "human", Note: "Imported from Local portable bundle",
				}); err != nil {
					return result, err
				}
			}
		}
		if _, err := deps.Client.DispatchGateTasks(cmd.Context(), artifactID); err != nil {
			return result, err
		}
		if canonicalByFeature[source.FeatureKey] == source.ID {
			feature := preflight.existingFeatures[source.FeatureKey]
			if feature.CanonicalArtifactID != artifactID {
				if _, err := deps.Client.PromoteArtifactCanonical(cmd.Context(), artifactID, actor); err != nil {
					return result, err
				}
			}
		}
		for _, sourceWork := range linkedWork {
			workID := preflight.existingWork[sourceWork.ID].ID
			if workID == "" {
				feature := featureByID[sourceWork.FeatureID]
				created, err := deps.Client.CreateWorkItem(requestContextForBody(cmd.Context(), map[string]any{"workspace_id": workspaceID}), map[string]any{
					"feature": feature.Key, "title": sourceWork.Title, "description": sourceWork.Description,
					"acceptance_criteria": sourceWork.AcceptanceCriteria, "created_by": actor, "workspace_id": workspaceID,
					"source_refs": []string{portableWorkSourceRef(bundle.Payload.Workspace.ID, sourceWork.ID)},
					"artifact_id": artifactID,
				})
				if err != nil {
					return result, err
				}
				workID = strings.TrimSpace(fmt.Sprint(created["change_request_id"]))
				if workID == "" {
					return result, fmt.Errorf("portable import created no work id for %s", sourceWork.Key)
				}
				result.ImportedWork++
			}
			result.WorkMapping[sourceWork.ID] = workID
			if delivery, ok := deliveryByWork[sourceWork.ID]; ok && delivery.Report != nil {
				if err := importPortableDelivery(cmd, deps, workID, artifactID, delivery, actor, sourceWork.AcceptanceCriteria); err != nil {
					return result, err
				}
				result.ImportedDelivery++
			}
		}
	}
	for _, sourceWork := range bundle.Payload.Work {
		if sourceWork.ArtifactID != "" {
			continue
		}
		workID := preflight.existingWork[sourceWork.ID].ID
		if workID == "" {
			body := map[string]any{
				"title": sourceWork.Title, "description": sourceWork.Description,
				"acceptance_criteria": acceptanceCriteriaBody(sourceWork.AcceptanceCriteria),
				"created_by":          actor, "workspace_id": workspaceID,
				"issue_url": portableWorkSourceRef(bundle.Payload.Workspace.ID, sourceWork.ID),
			}
			created, err := deps.Client.CreateQuickWorkItem(requestContextForBody(cmd.Context(), body), body)
			if err != nil {
				return result, err
			}
			workID = strings.TrimSpace(fmt.Sprint(created["change_request_id"]))
			if workID == "" {
				return result, fmt.Errorf("portable import created no work id for %s", sourceWork.Key)
			}
			result.ImportedWork++
		}
		result.WorkMapping[sourceWork.ID] = workID
		if delivery, ok := deliveryByWork[sourceWork.ID]; ok && delivery.Report != nil {
			if err := importPortableDelivery(cmd, deps, workID, "", delivery, actor, sourceWork.AcceptanceCriteria); err != nil {
				return result, err
			}
			result.ImportedDelivery++
		}
	}
	return result, nil
}

func importPortableDelivery(cmd *cobra.Command, deps *Deps, workID, artifactID string, delivery local.PortableDeliveryEvidence, actor string, sourceCriteria []string) error {
	report := cloneMap(delivery.Report)
	if artifactID != "" {
		report["artifact_id"] = artifactID
	} else {
		delete(report, "artifact_id")
	}
	if strings.TrimSpace(fmt.Sprint(report["event_type"])) == "" {
		report["event_type"] = "coding_agent.completed"
	}
	criteria, err := deps.Client.ListAcceptanceCriteria(cmd.Context(), workID)
	if err != nil {
		return err
	}
	criterionIDs, err := portableCriterionMapping(sourceCriteria, criteria)
	if err != nil {
		return err
	}
	if err := remapPortableCriteria(report, criterionIDs); err != nil {
		return err
	}
	completion, err := deps.Client.ReportFeedback(cmd.Context(), workID, report)
	if err != nil {
		return err
	}
	if delivery.PeerReview != nil {
		peer := cloneMap(delivery.PeerReview)
		if err := remapPortableCriteria(peer, criterionIDs); err != nil {
			return err
		}
		binding, _ := peer["peer_review_of"].(map[string]any)
		if binding == nil {
			binding = map[string]any{}
			peer["peer_review_of"] = binding
		}
		if id := strings.TrimSpace(fmt.Sprint(completion["feedback_event_id"])); id != "" {
			binding["completion_feedback_event_id"] = id
		}
		if _, err := deps.Client.ReportFeedback(cmd.Context(), workID, peer); err != nil {
			return err
		}
	}
	if delivery.HumanDecision == "approve" || delivery.HumanDecision == "reject" {
		note := strings.TrimSpace("Imported Local decision: " + delivery.ReviewNote)
		status, err := deps.Client.DeliveryStatus(cmd.Context(), workID, true)
		if err != nil {
			return err
		}
		expectedVerdict := "fail"
		if delivery.HumanDecision == "approve" {
			expectedVerdict = "pass"
		}
		if status.Found && status.Executor == "human" && status.Verdict == expectedVerdict && status.Actor == actor && status.Note == note {
			return nil
		}
		_, err = deps.Client.DecideDelivery(cmd.Context(), workID, client.DeliveryDecisionInput{
			Decision: delivery.HumanDecision, Actor: actor, Note: note,
		})
		return err
	}
	return nil
}

func cloneMap(source map[string]any) map[string]any {
	data, _ := json.Marshal(source)
	var result map[string]any
	_ = json.Unmarshal(data, &result)
	return result
}

func portableCriterionMapping(source []string, destination []client.AcceptanceCriterion) (map[string]string, error) {
	if len(source) != len(destination) {
		return nil, fmt.Errorf("destination created %d acceptance criteria, want %d", len(destination), len(source))
	}
	result := make(map[string]string, len(source))
	for index, raw := range source {
		text, binding := parseAcceptanceCriterionBinding(raw)
		if destination[index].Text != text || destination[index].VerificationBinding != binding || destination[index].ID == "" {
			return nil, fmt.Errorf("destination acceptance criterion %d does not preserve the source contract", index+1)
		}
		result[fmt.Sprintf("local-%d", index+1)] = destination[index].ID
	}
	return result, nil
}

func remapPortableCriteria(body map[string]any, criterionIDs map[string]string) error {
	rows, _ := body["criteria"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row == nil {
			return fmt.Errorf("delivery criterion evidence must be an object")
		}
		sourceID := strings.TrimSpace(fmt.Sprint(row["criterion_id"]))
		destinationID, ok := criterionIDs[sourceID]
		if !ok {
			return fmt.Errorf("delivery evidence references unknown source criterion %q", sourceID)
		}
		row["criterion_id"] = destinationID
	}
	return nil
}

func printPortablePreflight(deps *Deps, result portablePreflight) {
	fmt.Fprintf(deps.Stdout, "Portable import: %s -> %s\n", result.SourceWorkspace, result.DestinationWorkspace)
	fmt.Fprintf(deps.Stdout, "%d artifacts, %d work items, %d gate records, %d delivery records\n", result.Artifacts, result.Work, result.Gates, result.Delivery)
	if len(result.Conflicts) == 0 {
		fmt.Fprintln(deps.Stdout, "No conflicts. Re-run with --yes to import.")
		return
	}
	for _, conflict := range result.Conflicts {
		fmt.Fprintf(deps.Stdout, "- %s\n", conflict)
	}
}
