package command

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/local"
)

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
				imported, err := importPortableDelivery(cmd, deps, workID, artifactID, delivery, actor, sourceWork.AcceptanceCriteria)
				if err != nil {
					return result, err
				}
				if imported {
					result.ImportedDelivery++
				}
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
			imported, err := importPortableDelivery(cmd, deps, workID, "", delivery, actor, sourceWork.AcceptanceCriteria)
			if err != nil {
				return result, err
			}
			if imported {
				result.ImportedDelivery++
			}
		}
	}
	return result, nil
}

func importPortableDelivery(cmd *cobra.Command, deps *Deps, workID, artifactID string, delivery local.PortableDeliveryEvidence, actor string, sourceCriteria []string) (bool, error) {
	hasDecision := delivery.HumanDecision == "approve" || delivery.HumanDecision == "reject"
	note := strings.TrimSpace("Imported Local decision: " + delivery.ReviewNote)
	expectedVerdict := "fail"
	if delivery.HumanDecision == "approve" {
		expectedVerdict = "pass"
	}
	decisionMatches := func(status *client.DeliveryStatusResult) bool {
		return status != nil && status.Found && status.Executor == "human" &&
			status.Verdict == expectedVerdict && status.Actor == actor && status.Note == note
	}
	if hasDecision {
		status, err := deps.Client.DeliveryStatus(cmd.Context(), workID, true)
		if err != nil {
			return false, err
		}
		if decisionMatches(status) {
			return false, nil
		}
	}
	report := cloneMap(delivery.Report)
	normalizePortableFeedback(report, workID, artifactID)
	if strings.TrimSpace(fmt.Sprint(report["event_type"])) == "" {
		report["event_type"] = "coding_agent.completed"
	}
	criteria, err := deps.Client.ListAcceptanceCriteria(cmd.Context(), workID)
	if err != nil {
		return false, err
	}
	criterionIDs, err := portableCriterionMapping(sourceCriteria, criteria)
	if err != nil {
		return false, err
	}
	if err := remapPortableCriteria(report, criterionIDs); err != nil {
		return false, err
	}
	completion, err := deps.Client.ReportFeedback(cmd.Context(), workID, report)
	if err != nil {
		return false, err
	}
	if delivery.PeerReview != nil {
		peer := cloneMap(delivery.PeerReview)
		normalizePortableFeedback(peer, workID, artifactID)
		if err := remapPortableCriteria(peer, criterionIDs); err != nil {
			return false, err
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
			return false, err
		}
	}
	if _, err := deps.Client.TriggerDeliveryReview(cmd.Context(), workID); err != nil {
		return false, err
	}
	if hasDecision {
		status, err := deps.Client.DeliveryStatus(cmd.Context(), workID, true)
		if err != nil {
			return false, err
		}
		if decisionMatches(status) {
			return true, nil
		}
		_, err = deps.Client.DecideDelivery(cmd.Context(), workID, client.DeliveryDecisionInput{
			Decision: delivery.HumanDecision, Actor: actor, Note: note,
		})
		return err == nil, err
	}
	return true, nil
}

func normalizePortableFeedback(body map[string]any, workID, artifactID string) {
	body["change_request_id"] = workID
	body["severity"] = "info"
	delete(body, "context_digest")
	if artifactID != "" {
		body["artifact_id"] = artifactID
	} else {
		delete(body, "artifact_id")
	}
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
