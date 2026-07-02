package seeding

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/integrations"
	"github.com/specgate/doc-registry/internal/knowledge"
	storagedb "github.com/specgate/doc-registry/internal/storage/db"
	"github.com/specgate/doc-registry/internal/workboard"
)

// DemoDeps are the real Doc Registry stores used by SeedDemo. The seed writes
// through production repositories/services so local UI inspection exercises the
// same API paths as ordinary governance data.
type DemoDeps struct {
	WorkBoard    WorkBoardSeedStore
	Artifacts    artifact.Service
	Integrations *storagedb.IntegrationRepository
	Knowledge    *storagedb.KnowledgeRepository
	Logger       *zerolog.Logger
	WorkspaceID  string
	CreatedBy    string
}

type DemoResult struct {
	FeaturesCreated       int
	FeaturesExisting      int
	ChangeRequestsCreated int
	ArtifactsPublished    int
	KnowledgeCreated      int
	FeedbackCreated       int
}

type demoFeature struct {
	ID          string
	Key         string
	Name        string
	Summary     string
	Status      workboard.FeatureStatus
	SummaryMD   string
	CRs         []demoCR
	Knowledge   *demoKnowledge
	ServiceName string
}

type demoCR struct {
	ID                   string
	Key                  string
	Title                string
	WorkType             workboard.WorkType
	IntentMD             string
	GovernanceThreadID   string
	AC                   []map[string]any
	WithLead             bool
	LeadVersion          string
	LeadStatus           artifact.Status
	WithPack             bool
	WithTracker          bool
	TrackerFeedbackState string
	WithDelivery         bool
	WithContextPackStale bool
	DeliveryReview       bool
	GateEvals            []workboard.GateEvaluation
}

type demoKnowledge struct {
	DocumentID string
	Title      string
	Summary    string
}

// SeedDemo creates a compact, realistic product-review dataset for local UI
// development. It is idempotent by stable feature key: if the demo features
// already exist, no duplicate work items or artifacts are created.
func SeedDemo(ctx context.Context, deps DemoDeps) (DemoResult, error) {
	if deps.WorkBoard == nil || deps.Artifacts == nil || deps.Integrations == nil || deps.Knowledge == nil {
		return DemoResult{}, fmt.Errorf("seeding: demo dependencies are required")
	}
	logger := deps.Logger
	if logger == nil {
		l := zerolog.Nop()
		logger = &l
	}

	existing, err := deps.WorkBoard.ListFeatures(ctx)
	if err != nil {
		return DemoResult{}, fmt.Errorf("seeding demo: list features: %w", err)
	}
	if err := ensureDemoIntegrations(ctx, deps.Integrations); err != nil {
		return DemoResult{}, err
	}
	existingByKey := make(map[string]bool, len(existing))
	for _, feature := range existing {
		existingByKey[feature.Key] = true
	}

	var result DemoResult
	for _, seed := range demoFeatures() {
		if existingByKey[seed.Key] {
			result.FeaturesExisting++
			continue
		}
		if err := seedDemoFeature(ctx, deps, seed, &result); err != nil {
			return result, err
		}
		logger.Info().Str("feature_key", seed.Key).Msg("seed-demo: created feature")
	}
	if err := backfillDemoChangeRequestAttribution(ctx, deps); err != nil {
		return result, err
	}
	return result, nil
}

func ensureDemoIntegrations(ctx context.Context, repo *storagedb.IntegrationRepository) error {
	if _, err := repo.GetIntegration(ctx, "demo-linear"); err != nil {
		if !errors.Is(err, integrations.ErrNotFound) {
			return fmt.Errorf("seeding demo: get linear integration: %w", err)
		}
		if _, err := repo.CreateIntegration(ctx, integrations.Integration{
			ID:         "demo-linear",
			Provider:   integrations.ProviderLinear,
			Name:       "Demo Linear",
			Status:     integrations.StatusConnected,
			BaseURL:    "https://api.linear.app",
			ConfigJSON: `{"demo":true}`,
			AuthMethod: integrations.AuthMethodOAuth,
		}); err != nil && !errors.Is(err, integrations.ErrConflict) {
			return fmt.Errorf("seeding demo: create linear integration: %w", err)
		}
	}
	if _, err := repo.GetIntegration(ctx, "demo-gitlab"); err != nil {
		if !errors.Is(err, integrations.ErrNotFound) {
			return fmt.Errorf("seeding demo: get gitlab integration: %w", err)
		}
		if _, err := repo.CreateIntegration(ctx, integrations.Integration{
			ID:         "demo-gitlab",
			Provider:   integrations.ProviderGitLab,
			Name:       "Demo GitLab",
			Status:     integrations.StatusConnected,
			BaseURL:    "https://gitlab.example",
			ConfigJSON: `{"demo":true}`,
			AuthMethod: integrations.AuthMethodOAuth,
		}); err != nil && !errors.Is(err, integrations.ErrConflict) {
			return fmt.Errorf("seeding demo: create gitlab integration: %w", err)
		}
	}
	if _, err := repo.GetResource(ctx, "demo-gitlab", "demo-project"); err != nil {
		if !errors.Is(err, integrations.ErrNotFound) {
			return fmt.Errorf("seeding demo: get gitlab resource: %w", err)
		}
		if _, err := repo.CreateResource(ctx, integrations.Resource{
			ID:            "demo-project",
			IntegrationID: "demo-gitlab",
			ResourceType:  integrations.ResourceTypeProject,
			ExternalID:    "demo-project",
			ExternalKey:   "specgate/demo",
			DisplayName:   "specgate/demo",
			DefaultRef:    "main",
			ConfigJSON:    `{"demo":true}`,
		}); err != nil && !errors.Is(err, integrations.ErrConflict) {
			return fmt.Errorf("seeding demo: create gitlab resource: %w", err)
		}
	}
	return nil
}

func seedDemoFeature(ctx context.Context, deps DemoDeps, seed demoFeature, result *DemoResult) error {
	createdBy := demoCreatedBy(deps)
	workspaceID := strings.TrimSpace(deps.WorkspaceID)
	feature, err := deps.WorkBoard.CreateFeature(ctx, workboard.Feature{
		ID:      seed.ID,
		Key:     seed.Key,
		Name:    seed.Name,
		Summary: seed.Summary,
		Status:  seed.Status,
	})
	if err != nil {
		return fmt.Errorf("seeding demo: create feature %s: %w", seed.Key, err)
	}
	result.FeaturesCreated++

	canonical, err := publishDemoArtifact(ctx, deps.Artifacts, feature.ID, "v1.0", artifact.StatusApproved, artifact.ArtifactPhasePhase1, seed.Name, seed.ServiceName, createdBy)
	if err != nil {
		return fmt.Errorf("seeding demo: publish canonical artifact for %s: %w", seed.Key, err)
	}
	result.ArtifactsPublished++
	if _, err := deps.Artifacts.UpdateStatus(ctx, canonical.ID, artifact.StatusUpdate{
		Status: artifact.StatusApproved,
		Actor:  createdBy,
		Note:   "Approved demo source of truth",
	}); err != nil {
		return fmt.Errorf("seeding demo: approve canonical artifact for %s: %w", seed.Key, err)
	}
	if _, err := deps.WorkBoard.SetFeatureCanonicalArtifact(ctx, feature.ID, canonical.ID, createdBy); err != nil {
		return fmt.Errorf("seeding demo: set canonical artifact for %s: %w", seed.Key, err)
	}
	if seed.SummaryMD != "" {
		if _, err := deps.WorkBoard.SetFeatureSummary(ctx, feature.ID, seed.SummaryMD, "v0.9"); err != nil {
			return fmt.Errorf("seeding demo: set feature summary for %s: %w", seed.Key, err)
		}
	}
	if seed.Status == workboard.FeatureStatusCandidate {
		if _, err := publishDemoArtifact(ctx, deps.Artifacts, feature.ID, "draft-review", artifact.StatusDraft, artifact.ArtifactPhasePhase1, seed.Name+" Draft", seed.ServiceName, createdBy); err != nil {
			return fmt.Errorf("seeding demo: publish draft artifact for %s: %w", seed.Key, err)
		}
		result.ArtifactsPublished++
	}
	if seed.Knowledge != nil {
		if err := createDemoKnowledge(ctx, deps.Knowledge, feature.ID, *seed.Knowledge, createdBy); err != nil {
			return fmt.Errorf("seeding demo: create knowledge for %s: %w", seed.Key, err)
		}
		result.KnowledgeCreated++
	}

	for _, crSeed := range seed.CRs {
		leadID := ""
		if crSeed.WithLead {
			if crSeed.LeadVersion != "" || crSeed.LeadStatus != "" {
				leadStatus := crSeed.LeadStatus
				if leadStatus == "" {
					leadStatus = artifact.StatusDraft
				}
				leadVersion := crSeed.LeadVersion
				if leadVersion == "" {
					leadVersion = "draft-" + crSeed.Key
				}
				lead, err := publishDemoArtifact(ctx, deps.Artifacts, feature.ID, leadVersion, leadStatus, artifact.ArtifactPhasePhase1, crSeed.Title+" Working Spec", seed.ServiceName, createdBy)
				if err != nil {
					return fmt.Errorf("seeding demo: publish lead artifact for %s: %w", crSeed.Key, err)
				}
				result.ArtifactsPublished++
				leadID = lead.ID
			} else {
				leadID = canonical.ID
			}
		}
		packID := ""
		if crSeed.WithPack {
			pack, err := publishDemoArtifact(ctx, deps.Artifacts, feature.ID, "pack-"+crSeed.Key, artifact.StatusApproved, artifact.ArtifactPhasePhase2, crSeed.Title+" Context Pack", seed.ServiceName, createdBy)
			if err != nil {
				return fmt.Errorf("seeding demo: publish context pack for %s: %w", crSeed.Key, err)
			}
			result.ArtifactsPublished++
			packID = pack.ID
		}
		cr, err := deps.WorkBoard.CreateChangeRequest(ctx, workboard.ChangeRequest{
			ID:                    crSeed.ID,
			Key:                   crSeed.Key,
			FeatureID:             feature.ID,
			WorkType:              crSeed.WorkType,
			Title:                 crSeed.Title,
			IntentMD:              crSeed.IntentMD,
			AcceptanceCriteria:    mustJSONString(crSeed.AC),
			LeadArtifactID:        leadID,
			ContextPackArtifactID: packID,
			GovernanceThreadID:    crSeed.GovernanceThreadID,
			WorkspaceID:           workspaceID,
			CreatedBy:             createdBy,
		})
		if err != nil {
			return fmt.Errorf("seeding demo: create change request %s: %w", crSeed.Key, err)
		}
		result.ChangeRequestsCreated++
		if _, err := deps.WorkBoard.RefreshGateRuns(ctx, workboard.RefreshGateRunsInput{
			ChangeRequestID: cr.ID,
			Evaluations:     crSeed.GateEvals,
		}); err != nil {
			return fmt.Errorf("seeding demo: refresh gate runs for %s: %w", crSeed.Key, err)
		}
		if crSeed.WithTracker {
			if _, err := deps.Integrations.UpsertTrackerLink(ctx, integrations.TrackerLink{
				IntegrationID:   "demo-linear",
				FeatureID:       feature.ID,
				ChangeRequestID: cr.ID,
				Lane:            "full",
				ExternalID:      "demo-" + crSeed.Key,
				ExternalKey:     "SPECGATE-" + crSeed.Key,
				URL:             "https://linear.example/specgate-" + crSeed.Key,
				Title:           crSeed.Title,
				State:           integrations.TrackerStateOpened,
				TrackerState:    "started",
			}); err != nil {
				return fmt.Errorf("seeding demo: create tracker link for %s: %w", crSeed.Key, err)
			}
			if strings.TrimSpace(crSeed.TrackerFeedbackState) != "" {
				payload := mustJSONString(map[string]any{
					"provider":       "linear",
					"identifier":     "SPECGATE-" + crSeed.Key,
					"tracker_state":  crSeed.TrackerFeedbackState,
					"state_name":     crSeed.TrackerFeedbackState,
					"issue_url":      "https://linear.example/specgate-" + crSeed.Key,
					"issue_title":    crSeed.Title,
					"correlation_id": crSeed.Key,
				})
				if _, err := deps.Integrations.CreateGovernanceFeedbackEvent(ctx, integrations.GovernanceFeedbackEvent{
					IntegrationID: "demo-linear",
					FeatureID:     feature.ID,
					EventType:     integrations.FeedbackEventTrackerStatusChanged,
					PayloadJSON:   payload,
					Status:        integrations.FeedbackStatusPending,
				}); err != nil {
					return fmt.Errorf("seeding demo: create tracker feedback for %s: %w", crSeed.Key, err)
				}
				result.FeedbackCreated++
			}
		}
		if crSeed.WithDelivery {
			link, err := deps.Integrations.UpsertDeliveryLink(ctx, integrations.DeliveryLink{
				IntegrationID:   "demo-gitlab",
				ResourceID:      "demo-project",
				FeatureID:       feature.ID,
				ChangeRequestID: cr.ID,
				ExternalType:    integrations.ExternalTypeMergeRequest,
				ExternalID:      "mr-" + crSeed.Key,
				ExternalIID:     crSeed.Key,
				ExternalKey:     "!" + crSeed.Key[len(crSeed.Key)-3:],
				URL:             "https://gitlab.example/specgate/-/merge_requests/" + crSeed.Key,
				Title:           crSeed.Title,
				State:           integrations.DeliveryStateMerged,
				SourceBranch:    "demo/" + crSeed.Key,
				TargetBranch:    "main",
				MergeCommitSHA:  "demo" + crSeed.Key,
			})
			if err != nil {
				return fmt.Errorf("seeding demo: create delivery link for %s: %w", crSeed.Key, err)
			}
			payload := mustJSONString(map[string]any{
				"summary":        "Demo implementation completed and merged.",
				"correlation_id": crSeed.Key,
				"url":            link.URL,
			})
			if _, err := deps.Integrations.CreateGovernanceFeedbackEvent(ctx, integrations.GovernanceFeedbackEvent{
				IntegrationID:   "demo-gitlab",
				ResourceID:      "demo-project",
				DeliveryLinkID:  link.ID,
				FeatureID:       feature.ID,
				ChangeRequestID: cr.ID,
				ArtifactID:      leadID,
				EventType:       integrations.FeedbackEventCodingAgentCompleted,
				PayloadJSON:     payload,
				Status:          integrations.FeedbackStatusPending,
			}); err != nil {
				return fmt.Errorf("seeding demo: create feedback for %s: %w", crSeed.Key, err)
			}
			result.FeedbackCreated++
		}
		if crSeed.WithContextPackStale {
			if _, err := deps.Integrations.CreateGovernanceFeedbackEvent(ctx, integrations.GovernanceFeedbackEvent{
				IntegrationID:   "demo-gitlab",
				ResourceID:      "demo-project",
				FeatureID:       feature.ID,
				ChangeRequestID: cr.ID,
				ArtifactID:      packID,
				EventType:       integrations.FeedbackEventContextPackStale,
				PayloadJSON:     mustJSONString(map[string]any{"provider": "gitlab", "correlation_id": crSeed.Key}),
				Status:          integrations.FeedbackStatusPending,
				Reason:          "Spec changed after the last delivery pack was assembled.",
			}); err != nil {
				return fmt.Errorf("seeding demo: create context-pack stale feedback for %s: %w", crSeed.Key, err)
			}
			result.FeedbackCreated++
		}
		if crSeed.DeliveryReview {
			if _, err := deps.WorkBoard.RefreshGateRuns(ctx, workboard.RefreshGateRunsInput{
				ChangeRequestID: cr.ID,
				Evaluations: []workboard.GateEvaluation{{
					Gate:             "delivery_review",
					State:            workboard.NextActionStateNeedsHumanReview,
					Hint:             "2 met · 1 unclear after merged delivery",
					Confidence:       0.82,
					JudgeModel:       "demo-reviewer",
					EvalSuiteVersion: "demo-v1",
					Evidence:         deliveryReviewEvidence(crSeed.AC),
				}},
			}); err != nil {
				return fmt.Errorf("seeding demo: create delivery review for %s: %w", crSeed.Key, err)
			}
		}
	}
	return nil
}

func demoCreatedBy(deps DemoDeps) string {
	if createdBy := strings.TrimSpace(deps.CreatedBy); createdBy != "" {
		return createdBy
	}
	return "demo-seed"
}

func backfillDemoChangeRequestAttribution(ctx context.Context, deps DemoDeps) error {
	workspaceID := strings.TrimSpace(deps.WorkspaceID)
	createdBy := strings.TrimSpace(deps.CreatedBy)
	if workspaceID == "" && createdBy == "" {
		return nil
	}

	requests, err := deps.WorkBoard.ListChangeRequests(ctx, true)
	if err != nil {
		return fmt.Errorf("seeding demo: list change requests for attribution backfill: %w", err)
	}
	demoKeys := demoChangeRequestKeys()
	for _, cr := range requests {
		if !demoKeys[cr.Key] {
			continue
		}
		changed := false
		if workspaceID != "" && cr.WorkspaceID != workspaceID {
			cr.WorkspaceID = workspaceID
			changed = true
		}
		if createdBy != "" && cr.CreatedBy != createdBy {
			cr.CreatedBy = createdBy
			changed = true
		}
		if !changed {
			continue
		}
		if _, err := deps.WorkBoard.SetChangeRequestAttribution(ctx, cr.ID, cr.WorkspaceID, cr.CreatedBy); err != nil {
			return fmt.Errorf("seeding demo: backfill attribution for %s: %w", cr.Key, err)
		}
	}
	return nil
}

func demoChangeRequestKeys() map[string]bool {
	keys := make(map[string]bool)
	for _, feature := range demoFeatures() {
		for _, cr := range feature.CRs {
			keys[cr.Key] = true
		}
	}
	return keys
}

func publishDemoArtifact(
	ctx context.Context,
	svc artifact.Service,
	featureID string,
	version string,
	status artifact.Status,
	phase artifact.ArtifactPhase,
	title string,
	serviceName string,
	createdBy string,
) (*artifact.Artifact, error) {
	if strings.TrimSpace(createdBy) == "" {
		createdBy = "demo-seed"
	}
	confidence := 0.86
	ambiguity := 0.18
	return svc.Publish(ctx, artifact.PublishInput{
		FeatureID:            featureID,
		Version:              version,
		Status:               status,
		RequestType:          artifact.RequestTypeChangeRequest,
		ImpactLevel:          artifact.ImpactLevelMedium,
		ArtifactPhase:        phase,
		ArtifactCompleteness: artifact.ArtifactCompletenessFull,
		ConfidenceScore:      &confidence,
		AmbiguityScore:       &ambiguity,
		GovernanceVersion:    "demo-seed-v1",
		CreatedBy:            createdBy,
		ImpactedServices: []artifact.ServiceRef{{
			Name: serviceName,
			Kind: "service",
		}},
		Documents: []artifact.DocumentInput{
			{Path: artifact.FixedKeyToPath("prd"), Role: string(artifact.RoleSpec), Content: []byte("# " + title + " PRD\n\nDemo user intent, goals, non-goals, and review notes.\n")},
			{Path: artifact.FixedKeyToPath("spec"), Role: string(artifact.RoleSpec), Content: []byte("# " + title + " Spec\n\nDemo contract, lifecycle notes, API expectations, and edge cases.\n")},
			{Path: artifact.FixedKeyToPath("tasks_fe"), Role: string(artifact.RolePlan), Content: []byte("# FE Tasks\n\n- Review UI states\n- Add focused component coverage\n")},
			{Path: artifact.FixedKeyToPath("tasks_be"), Role: string(artifact.RolePlan), Content: []byte("# BE Tasks\n\n- Preserve registry contract\n- Keep MCP handoff stable\n")},
			{Path: artifact.FixedKeyToPath("tasks_qa"), Role: string(artifact.RoleVerification), Content: []byte("# QA Tasks\n\n- Verify happy path\n- Verify stale and delivery states\n")},
			{Path: artifact.FixedKeyToPath("rollout"), Role: string(artifact.RoleReference), Content: []byte("# Rollout\n\nShip behind normal review flow.\n")},
			{Path: artifact.FixedKeyToPath("risks"), Role: string(artifact.RoleReference), Content: []byte("# Risks\n\nDemo data should stay local-only.\n")},
			{Path: artifact.FixedKeyToPath("manifest"), Role: string(artifact.RoleReference), Content: []byte(`{"source":"seed-demo","version":"v1"}`)},
		},
	})
}

func createDemoKnowledge(ctx context.Context, repo *storagedb.KnowledgeRepository, featureID string, seed demoKnowledge, uploadedBy string) error {
	if strings.TrimSpace(uploadedBy) == "" {
		uploadedBy = "demo-seed"
	}
	now := time.Now().UTC().Add(2 * time.Hour)
	return repo.CreateVersion(ctx, &knowledge.Document{
		DocumentID:       seed.DocumentID,
		Version:          "v2",
		IsLatest:         true,
		Title:            seed.Title,
		DocumentType:     knowledge.DocumentTypeProductBrief,
		AuthorityLevel:   knowledge.AuthorityHigh,
		SourceKind:       knowledge.SourceKindText,
		SourceURI:        "demo://knowledge/" + seed.DocumentID,
		MimeType:         "text/markdown",
		Status:           knowledge.StatusIndexed,
		LinkedFeatureID:  featureID,
		UploadedBy:       uploadedBy,
		CreatedAt:        now,
		UpdatedAt:        now,
		Summary:          seed.Summary,
		TagsJSON:         `["demo","freshness"]`,
		OriginalFilename: seed.DocumentID + ".md",
	}, nil)
}

func deliveryReviewEvidence(ac []map[string]any) string {
	criteria := make([]map[string]any, 0, len(ac))
	for i, item := range ac {
		id, _ := item["id"].(string)
		if id == "" {
			continue
		}
		verdict := "met"
		why := "Verified in the merged demo implementation."
		if i == len(ac)-1 {
			verdict = "unclear"
			why = "Needs a human look in the delivered UI."
		}
		criteria = append(criteria, map[string]any{
			"criterion_id": id,
			"verdict":      verdict,
			"why":          why,
		})
	}
	return mustJSONString(map[string]any{
		"criteria": criteria,
		"checks": []map[string]any{
			{"name": "tests", "status": "pass", "detail": "Targeted demo tests passed"},
			{"name": "types", "status": "pass", "detail": "Typecheck completed"},
			{"name": "lint", "status": "skipped", "detail": "Demo evidence only"},
			{"name": "build", "status": "skipped", "detail": "Demo evidence only"},
		},
	})
}

func demoFeatures() []demoFeature {
	return []demoFeature{
		{
			ID:          "demo-feature-checkout",
			Key:         "DEMO-CHECKOUT",
			Name:        "Checkout review handoff",
			Summary:     "A ready-to-execute checkout improvement with a compact review surface.",
			Status:      workboard.FeatureStatusPlanned,
			ServiceName: "checkout-web",
			SummaryMD:   "# Checkout review handoff\n\nA focused checkout update used to inspect Feature Map cards, readiness gates, and the work-item review modal.",
			CRs: []demoCR{
				{
					ID:       "demo-cr-101",
					Key:      "DEMO-101",
					Title:    "Add saved-card eligibility copy",
					WorkType: workboard.WorkTypeFeatureChange,
					IntentMD: "Clarify when a saved card can be reused during checkout.",
					AC: []map[string]any{
						{"id": "demo-101-ac-1", "text": "Checkout shows eligibility copy before payment submission.", "done": false, "source": "human"},
						{"id": "demo-101-ac-2", "text": "Copy has an empty state when no saved card exists.", "done": false, "source": "human"},
					},
					WithLead: true,
					GateEvals: []workboard.GateEvaluation{
						llmGate("scope_clear", workboard.NextActionStatePass, "Scope is narrow and tied to checkout copy.", 0.91),
						llmGate("acceptance_criteria_verifiable", workboard.NextActionStateNeedsHumanReview, "One criterion should name the exact empty-state behavior.", 0.74),
					},
				},
				{
					ID:                 "demo-cr-103",
					Key:                "DEMO-103",
					Title:              "Review the revised checkout terms",
					WorkType:           workboard.WorkTypeFeatureChange,
					IntentMD:           "Hold the checkout terms update for product review before approval.",
					GovernanceThreadID: "demo-thread-checkout-review",
					AC: []map[string]any{
						{"id": "demo-103-ac-1", "text": "Draft spec reflects the latest legal language.", "done": false, "source": "human"},
						{"id": "demo-103-ac-2", "text": "Product review confirms the customer-facing tone before handoff.", "done": false, "source": "human"},
					},
					WithLead:    true,
					LeadVersion: "draft-review",
					LeadStatus:  artifact.StatusDraft,
					GateEvals: []workboard.GateEvaluation{
						llmGate("scope_clear", workboard.NextActionStateWarn, "Needs product approval before engineering sees the update.", 0.72),
					},
				},
				{
					ID:       "demo-cr-102",
					Key:      "DEMO-102",
					Title:    "Refresh saved-card copy from policy note",
					WorkType: workboard.WorkTypeFeatureChange,
					IntentMD: "Refresh checkout copy after the support policy note changed.",
					AC: []map[string]any{
						{"id": "demo-102-ac-1", "text": "Spec compares current canonical copy with the newer policy note.", "done": false, "source": "human"},
						{"id": "demo-102-ac-2", "text": "Governance proposes the minimal copy update before handoff.", "done": false, "source": "human"},
					},
					WithLead:             true,
					WithContextPackStale: true,
					WithPack:             true,
					GateEvals: []workboard.GateEvaluation{
						llmGate("success_metric_measurable", workboard.NextActionStateWarn, "Success metric should name support-ticket deflection.", 0.68),
					},
				},
			},
			Knowledge: &demoKnowledge{
				DocumentID: "demo-knowledge-checkout-policy",
				Title:      "Checkout support policy update",
				Summary:    "Newer product note that should trigger the linked-knowledge freshness warning.",
			},
		},
		{
			ID:          "demo-feature-onboarding",
			Key:         "DEMO-ONBOARDING",
			Name:        "Onboarding draft review",
			Summary:     "A draft artifact waiting for human review in the Artifacts workspace.",
			Status:      workboard.FeatureStatusCandidate,
			ServiceName: "onboarding",
			SummaryMD:   "# Onboarding draft review\n\nA candidate feature with an implementation spec still waiting for review.",
			CRs: []demoCR{{
				ID:                 "demo-cr-201",
				Key:                "DEMO-201",
				Title:              "Draft first-run checklist",
				WorkType:           workboard.WorkTypeNewFeature,
				IntentMD:           "Help new workspace owners finish the first-run checklist.",
				GovernanceThreadID: "demo-thread-onboarding-draft",
				AC: []map[string]any{
					{"id": "demo-201-ac-1", "text": "Checklist shows three setup steps with completion state.", "done": false, "source": "llm"},
					{"id": "demo-201-ac-2", "text": "Incomplete setup keeps the handoff action secondary.", "done": false, "source": "llm"},
				},
				GateEvals: []workboard.GateEvaluation{
					llmGate("rollback_plan_present", workboard.NextActionStatePending, "No rollout section has been reviewed yet.", 0.55),
				},
			}},
		},
		{
			ID:          "demo-feature-delivery",
			Key:         "DEMO-DELIVERY",
			Name:        "Delivery evidence loop",
			Summary:     "A handed-off work item with merged delivery evidence and a delivery review verdict.",
			Status:      workboard.FeatureStatusActive,
			ServiceName: "handoff-service",
			SummaryMD:   "# Delivery evidence loop\n\nA live feature that demonstrates tracker links, completion feedback, and delivered-phase AC verdicts.",
			CRs: []demoCR{{
				ID:       "demo-cr-301",
				Key:      "DEMO-301",
				Title:    "Close the delivery evidence loop",
				WorkType: workboard.WorkTypeFeatureChange,
				IntentMD: "Confirm a coding-agent delivery against the original acceptance criteria.",
				AC: []map[string]any{
					{"id": "demo-301-ac-1", "text": "Merged delivery is linked to the work item.", "done": false, "source": "human"},
					{"id": "demo-301-ac-2", "text": "Delivery review maps evidence back to each criterion.", "done": false, "source": "human"},
					{"id": "demo-301-ac-3", "text": "Reviewer can re-handoff if an acceptance criterion is unclear.", "done": false, "source": "human"},
				},
				WithLead:             true,
				WithPack:             true,
				WithTracker:          true,
				TrackerFeedbackState: "started",
				WithDelivery:         true,
				DeliveryReview:       true,
				GateEvals: []workboard.GateEvaluation{
					llmGate("implementation_plan_traceable", workboard.NextActionStatePass, "Implementation plan traces to the canonical spec.", 0.93),
				},
			}},
		},
	}
}

func llmGate(gate string, state workboard.NextActionState, hint string, confidence float64) workboard.GateEvaluation {
	return workboard.GateEvaluation{
		Gate:             gate,
		State:            state,
		Hint:             hint,
		Confidence:       confidence,
		JudgeModel:       "demo-judge",
		EvalSuiteVersion: "demo-v1",
		Evidence:         hint,
	}
}

func mustJSONString(v any) string {
	body, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(body)
}
