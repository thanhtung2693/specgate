package db

import (
	"context"
	"strings"

	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/integrations"
	"github.com/specgate/doc-registry/internal/workboard"
)

func (r *WorkBoardRepository) ListStaleWarnings(
	ctx context.Context,
	filter workboard.StaleWarningFilter,
) ([]workboard.StaleWarning, error) {
	if workspaceID := workboard.WorkspaceID(ctx); workspaceID != "" {
		filter.WorkspaceID = workspaceID
	} else if workspaceID := strings.TrimSpace(filter.WorkspaceID); workspaceID != "" {
		filter.WorkspaceID = workspaceID
		ctx = workboard.WithWorkspace(ctx, workspaceID)
	}
	if filter.FeatureID == "" && filter.ChangeRequestID == "" {
		features, err := r.ListFeatures(ctx)
		if err != nil {
			return nil, err
		}
		featureByID := make(map[string]workboard.Feature, len(features))
		var warnings []workboard.StaleWarning
		for _, item := range features {
			featureByID[item.ID] = item
			itemWarnings, err := r.listStaleWarningsForFeatureAndCR(ctx, filter.WorkspaceID, item, workboard.ChangeRequest{}, true)
			if err != nil {
				return nil, err
			}
			warnings = append(warnings, itemWarnings...)
		}
		changeRequests, err := r.ListChangeRequests(ctx, false)
		if err != nil {
			return nil, err
		}
		for _, cr := range changeRequests {
			// Quick-route change requests may have no feature; evaluate their
			// CR-scoped warnings without a feature lookup.
			if cr.FeatureID == "" {
				itemWarnings, err := r.listStaleWarningsForFeatureAndCR(ctx, filter.WorkspaceID, workboard.Feature{}, cr, false)
				if err != nil {
					return nil, err
				}
				warnings = append(warnings, itemWarnings...)
				continue
			}
			feature, ok := featureByID[cr.FeatureID]
			if !ok {
				if err := scopeWorkBoardQuery(r.db.WithContext(ctx), ctx).First(&feature, "id = ?", cr.FeatureID).Error; err != nil {
					return nil, mapWorkBoardNotFound(err)
				}
				featureByID[cr.FeatureID] = feature
			}
			itemWarnings, err := r.listStaleWarningsForFeatureAndCR(ctx, filter.WorkspaceID, feature, cr, false)
			if err != nil {
				return nil, err
			}
			warnings = append(warnings, itemWarnings...)
		}
		return warnings, nil
	}
	var feature workboard.Feature
	var cr workboard.ChangeRequest
	if filter.ChangeRequestID != "" {
		if err := scopeWorkBoardQuery(r.db.WithContext(ctx), ctx).First(&cr, "id = ?", filter.ChangeRequestID).Error; err != nil {
			return nil, mapWorkBoardNotFound(err)
		}
		filter.FeatureID = cr.FeatureID
	}
	if filter.FeatureID == "" {
		return nil, nil
	}
	if err := scopeWorkBoardQuery(r.db.WithContext(ctx), ctx).First(&feature, "id = ?", filter.FeatureID).Error; err != nil {
		return nil, mapWorkBoardNotFound(err)
	}
	return r.listStaleWarningsForFeatureAndCR(ctx, filter.WorkspaceID, feature, cr, true)
}

func (r *WorkBoardRepository) listStaleWarningsForFeatureAndCR(
	ctx context.Context,
	workspaceID string,
	feature workboard.Feature,
	cr workboard.ChangeRequest,
	includeFeatureWarnings bool,
) ([]workboard.StaleWarning, error) {
	// A loaded change request pins the workspace; otherwise use the caller's
	// (selected) workspace. Knowledge freshness is skipped when neither is known.
	if cr.WorkspaceID != "" {
		workspaceID = cr.WorkspaceID
	} else if workspaceID == "" {
		workspaceID = feature.WorkspaceID
	}
	var warnings []workboard.StaleWarning
	if includeFeatureWarnings {
		if feature.Status == workboard.FeatureStatusDeprecated {
			warnings = append(warnings, staleWarning(workboard.WarningFeatureDeprecated, feature.ID, cr.ID, "", "Feature is deprecated."))
		}
		if feature.CanonicalArtifactID == "" {
			warnings = append(warnings, staleWarning(workboard.WarningCanonicalArtifactMissing, feature.ID, cr.ID, "", "Feature has no canonical artifact."))
		} else {
			var a artifact.Artifact
			artifactQuery := r.db.WithContext(ctx).Where("id = ?", feature.CanonicalArtifactID)
			if workspaceID != "" {
				artifactQuery = artifactQuery.Where("workspace_id = ?", workspaceID)
			}
			if err := artifactQuery.First(&a).Error; err == nil {
				if a.Status != artifact.StatusApproved {
					warnings = append(warnings, staleWarning(workboard.WarningCanonicalArtifactUnapproved, feature.ID, cr.ID, a.ID, "Canonical artifact is not approved."))
				}
				if a.Status == artifact.StatusSuperseded {
					warnings = append(warnings, staleWarning(workboard.WarningCanonicalArtifactSuperseded, feature.ID, cr.ID, a.ID, "Canonical artifact is superseded."))
				}
				linkedKnowledgeWarning, err := r.linkedKnowledgeNewerWarning(ctx, workspaceID, feature, a, cr.ID)
				if err != nil {
					return nil, err
				}
				if linkedKnowledgeWarning != nil {
					warnings = append(warnings, *linkedKnowledgeWarning)
				}
			}
		}
	}
	if cr.ID != "" && cr.LeadArtifactID != "" {
		var lead artifact.Artifact
		artifactQuery := r.db.WithContext(ctx).Where("id = ?", cr.LeadArtifactID)
		if workspaceID != "" {
			artifactQuery = artifactQuery.Where("workspace_id = ?", workspaceID)
		}
		if err := artifactQuery.First(&lead).Error; err == nil {
			if lead.Status == artifact.StatusSuperseded {
				warnings = append(warnings, staleWarning(workboard.WarningLeadArtifactSuperseded, feature.ID, cr.ID, lead.ID, "Lead artifact is superseded."))
			}
			if lead.Status == artifact.StatusApproved && feature.CanonicalArtifactID != lead.ID {
				warnings = append(warnings, staleWarning(workboard.WarningCanonicalPromotionAvailable, feature.ID, cr.ID, lead.ID, "Approved lead artifact can be promoted to canonical."))
			}
		}
	}
	// Tracker status augments the derived phase but must not override the
	// git delivery evidence; a clear contradiction surfaces as a warning.
	// Only meaningful for a loaded CR (the feature-level board loop passes no
	// cr.ID, so this never fires there).
	if cr.ID != "" {
		conflict, err := r.trackerStatusConflictWarning(ctx, feature, cr)
		if err != nil {
			return nil, err
		}
		if conflict != nil {
			warnings = append(warnings, *conflict)
		}
		priorityUrgent, err := r.trackerPriorityUrgentWarning(ctx, feature, cr)
		if err != nil {
			return nil, err
		}
		if priorityUrgent != nil {
			warnings = append(warnings, *priorityUrgent)
		}
		deliveryReview, err := r.AuthoritativeDeliveryReviewRun(ctx, cr.ID)
		if err != nil {
			return nil, err
		}
		deliveryStale := r.deliveryStaleWarning(feature, cr, deliveryReview)
		if deliveryStale != nil {
			warnings = append(warnings, *deliveryStale)
		}
		approvalRequired := deliveryApprovalRequiredWarning(feature, cr, deliveryReview)
		if approvalRequired != nil {
			warnings = append(warnings, *approvalRequired)
		}
	}
	// Check for an open MR/PR — a delivery link in state "opened" means
	// delivery is underway. Match by feature_id (ID or Key) per spec §14. // per spec §14
	featureRefs := []string{feature.ID}
	if strings.TrimSpace(feature.Key) != "" && feature.Key != feature.ID {
		featureRefs = append(featureRefs, feature.Key)
	}
	var openLinkCount int64
	deliveryLinks := r.db.WithContext(ctx).
		Model(&integrations.DeliveryLink{}).
		Where("feature_id IN ? AND external_type = ? AND state = ?", featureRefs, integrations.ExternalTypeMergeRequest, integrations.DeliveryStateOpened)
	if workspaceID != "" {
		deliveryLinks = deliveryLinks.Joins("JOIN integrations ON integrations.id = integration_delivery_links.integration_id").Where("integrations.workspace_id = ?", workspaceID)
	}
	if err := deliveryLinks.Count(&openLinkCount).Error; err != nil {
		return nil, err
	}
	if openLinkCount > 0 {
		warnings = append(warnings, workboard.StaleWarning{
			Code:      workboard.WarningDeliveryInProgress,
			Severity:  "info",
			Message:   "Active delivery: an open MR/PR is linked to this feature.",
			FeatureID: feature.ID,
		})
	}
	return warnings, nil
}

func (r *WorkBoardRepository) NextActions(
	ctx context.Context,
	changeRequestID string,
) ([]workboard.NextAction, error) {
	var cr workboard.ChangeRequest
	if err := scopeWorkBoardQuery(r.db.WithContext(ctx), ctx).First(&cr, "id = ?", changeRequestID).Error; err != nil {
		return nil, mapWorkBoardNotFound(err)
	}
	// Quick-route change requests may have no feature; a zero-value feature
	// behaves like one without a canonical artifact.
	var feature workboard.Feature
	if cr.FeatureID != "" {
		featureQuery := r.db.WithContext(ctx).Where("id = ?", cr.FeatureID)
		if cr.WorkspaceID != "" {
			featureQuery = featureQuery.Where("workspace_id = ?", cr.WorkspaceID)
		}
		if err := featureQuery.First(&feature).Error; err != nil {
			return nil, mapWorkBoardNotFound(err)
		}
	}
	var lead *artifact.Artifact
	if cr.LeadArtifactID != "" {
		var a artifact.Artifact
		artifactQuery := r.db.WithContext(ctx).Where("id = ?", cr.LeadArtifactID)
		if cr.WorkspaceID != "" {
			artifactQuery = artifactQuery.Where("workspace_id = ?", cr.WorkspaceID)
		}
		if err := artifactQuery.First(&a).Error; err == nil {
			lead = &a
		}
	}
	warnings, err := r.ListStaleWarnings(ctx, workboard.StaleWarningFilter{ChangeRequestID: changeRequestID})
	if err != nil {
		return nil, err
	}
	knowledgeWarn := false
	for _, warning := range warnings {
		if warning.Code == workboard.WarningLinkedKnowledgeNewer {
			knowledgeWarn = true
			break
		}
	}
	isApproved := lead != nil && lead.Status == artifact.StatusApproved
	isCanonical := cr.LeadArtifactID != "" && feature.CanonicalArtifactID == cr.LeadArtifactID
	canonicalDrifted := feature.CanonicalArtifactID != "" && cr.LeadArtifactID != "" && feature.CanonicalArtifactID != cr.LeadArtifactID
	actions := []workboard.NextAction{
		{
			Gate:  "spec_drafted",
			State: stateIf(cr.LeadArtifactID != "", workboard.NextActionStatePass, workboard.NextActionStatePending),
			Hint:  valueOrDefault(cr.LeadArtifactID, "No working spec attached"),
		},
		{
			Gate:  "spec_approved",
			State: stateIf(isApproved, workboard.NextActionStatePass, workboard.NextActionStatePending),
			Hint:  hintForArtifactStatus(lead, "No working spec attached"),
		},
		{
			Gate:  "no_conflicts",
			State: stateIf(lead != nil, workboard.NextActionStatePass, workboard.NextActionStatePending),
			Hint:  hintForConflictGate(lead),
		},
		{
			Gate:  "knowledge_fresh",
			State: stateIf(!knowledgeWarn && lead != nil, workboard.NextActionStatePass, stateIf(knowledgeWarn, workboard.NextActionStateWarn, workboard.NextActionStatePending)),
			Hint:  hintForKnowledgeGate(knowledgeWarn),
		},
		{
			Gate:  "canonical_spec",
			State: stateIf(isCanonical, workboard.NextActionStatePass, stateIf(canonicalDrifted, workboard.NextActionStateWarn, workboard.NextActionStatePending)),
			Hint:  hintForCanonicalGate(isCanonical, canonicalDrifted, feature.CanonicalArtifactID),
		},
	}
	// Quick-route CRs never grow a working spec, so the full-artifact-flow
	// gates are persisted as not_applicable for audit instead of pending
	// forever. Context Packs are derived on read, so they are not a gate.
	if cr.IsQuickRoute() {
		for i := range actions {
			actions[i].State = workboard.NextActionStateNotApplicable
			actions[i].Hint = "Not required for quick-route work"
		}
	}
	return actions, nil
}
