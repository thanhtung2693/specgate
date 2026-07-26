package command

import (
	"encoding/json"
	"net/url"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/client"
)

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
