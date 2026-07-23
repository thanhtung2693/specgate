package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/specgate/doc-registry/internal/integrations"
	storagedb "github.com/specgate/doc-registry/internal/storage/db"
	"github.com/specgate/doc-registry/internal/workboard"
)

// TestMain sets the encryption key process-wide so encrypt/decrypt of stored
// webhook secrets works in parallel webhook tests (t.Setenv is disallowed once
// t.Parallel is called). Individual tests may still t.Setenv the same value.
func doGitLabWebhookEvent(t *testing.T, baseURL string, integrationID string, eventHeader string, signingToken string, eventUUID string, payload map[string]any) *http.Response {
	return doGitLabResourceWebhookEvent(t, baseURL, integrationID, resourceIDForWebhook(t, baseURL, integrationID, payload), eventHeader, signingToken, eventUUID, payload)
}

func TestGitHubResourceWebhookRecordsDeliveryFeedback(t *testing.T) {
	// Not parallel: t.Setenv configures the secret key used to encrypt/decrypt
	// the webhook secret for HMAC verification.
	t.Setenv(integrations.SecretKeyEnvVar, testSecretKey)
	srv, db := integrationsWebhookTestServer(t)
	defer srv.Close()

	ctx := workboard.WithWorkspace(context.Background(), "ws-test")
	workBoardRepo := storagedb.NewWorkBoardRepository(db)
	feature, err := workBoardRepo.CreateFeature(ctx, workboard.Feature{
		ID: "feature-gh", Key: "GH", Name: "GitHub feature", Status: workboard.FeatureStatusPlanned,
	})
	if err != nil {
		t.Fatal(err)
	}
	cr, err := workBoardRepo.CreateChangeRequest(ctx, workboard.ChangeRequest{
		ID: "cr-gh-1", Key: "CR-GH-1", FeatureID: feature.ID, WorkType: workboard.WorkTypeNewFeature, Title: "GH work",
	})
	if err != nil {
		t.Fatal(err)
	}

	intRepo := storagedb.NewIntegrationRepository(db)
	integration, err := intRepo.CreateIntegration(ctx, integrations.Integration{
		ID: "github-main", WorkspaceID: "ws-test", Provider: integrations.ProviderGitHub, Name: "GitHub", Status: integrations.StatusConnected,
	})
	if err != nil {
		t.Fatal(err)
	}
	repo, err := intRepo.CreateResource(ctx, integrations.Resource{
		ID: "repo-gh", IntegrationID: integration.ID, ResourceType: integrations.ResourceTypeRepo,
		ExternalID: "5005", ExternalKey: "acme/specgate", DisplayName: "specgate", DefaultRef: "main",
	})
	if err != nil {
		t.Fatal(err)
	}

	prBody := func(state string, merged bool, mergeSHA string) map[string]any {
		return map[string]any{
			"action": map[bool]string{true: "closed", false: "opened"}[merged],
			"number": 7,
			"pull_request": map[string]any{
				"id": 8001, "number": 7, "title": "Implement CR-GH-1",
				"body": "<!-- specgate-work-ref: CR-GH-1 -->", "html_url": "https://github.com/acme/specgate/pull/7",
				"state": state, "merged": merged, "merge_commit_sha": mergeSHA,
				"head": map[string]any{"ref": "feat/gh"}, "base": map[string]any{"ref": "main"},
			},
			"repository": map[string]any{"id": 5005, "full_name": "acme/specgate"},
		}
	}

	opened := postGitHubWebhook(t, srv.URL, integration.ID, prBody("open", false, ""))
	if opened.Status != integrations.WebhookStatusProcessed || opened.ResourceID != repo.ID || opened.ChangeRequestID != cr.ID {
		t.Fatalf("unexpected opened result: %#v", opened)
	}
	if opened.DeliveryLinkID == "" || len(opened.FeedbackEventIDs) == 0 {
		t.Fatalf("expected delivery link + feedback: %#v", opened)
	}

	merged := postGitHubWebhook(t, srv.URL, integration.ID, prBody("closed", true, "deadbeef"))
	if merged.Status != integrations.WebhookStatusProcessed || merged.ChangeRequestID != cr.ID {
		t.Fatalf("unexpected merged result: %#v", merged)
	}

	feedback := getIntegrationJSON[struct {
		Items []integrations.GovernanceFeedbackEvent `json:"items"`
	}](t, srv.URL+"/governance/feedback-events?status=received")
	if len(feedback.Items) != 2 {
		t.Fatalf("expected two feedback events, got %#v", feedback.Items)
	}
	if feedback.Items[0].EventType != integrations.FeedbackEventPRMerged {
		t.Fatalf("newest feedback should be merge, got %#v", feedback.Items[0])
	}
	if feedback.Items[0].FeatureID != feature.ID || feedback.Items[0].ChangeRequestID != cr.ID {
		t.Fatalf("feedback should target governance work item, got %#v", feedback.Items[0])
	}
}

func TestGitHubResourceWebhookMatchesWorkOnlyInsideIntegrationWorkspace(t *testing.T) {
	t.Setenv(integrations.SecretKeyEnvVar, testSecretKey)
	srv, db := integrationsWebhookTestServer(t)
	defer srv.Close()

	workBoardRepo := storagedb.NewWorkBoardRepository(db)
	ownerCtx := workboard.WithWorkspace(context.Background(), "ws-test")
	otherCtx := workboard.WithWorkspace(context.Background(), "ws-other")
	ownerFeature, err := workBoardRepo.CreateFeature(ownerCtx, workboard.Feature{
		ID: "feature-owner", Key: "SHARED", Name: "Owner feature", Status: workboard.FeatureStatusPlanned,
	})
	if err != nil {
		t.Fatal(err)
	}
	ownerCR, err := workBoardRepo.CreateChangeRequest(ownerCtx, workboard.ChangeRequest{
		ID: "cr-owner", Key: "CR-SHARED", FeatureID: ownerFeature.ID, Title: "Owner work",
		CreatedAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	otherFeature, err := workBoardRepo.CreateFeature(otherCtx, workboard.Feature{
		ID: "feature-other", Key: "SHARED", Name: "Other feature", Status: workboard.FeatureStatusPlanned,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = workBoardRepo.CreateChangeRequest(otherCtx, workboard.ChangeRequest{
		ID: "cr-other", Key: "CR-SHARED", FeatureID: otherFeature.ID, Title: "Other work",
		CreatedAt: time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}

	intRepo := storagedb.NewIntegrationRepository(db)
	integration, err := intRepo.CreateIntegration(ownerCtx, integrations.Integration{
		ID: "github-workspace-owner", WorkspaceID: "ws-test", Provider: integrations.ProviderGitHub,
		Name: "GitHub owner", Status: integrations.StatusConnected,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = intRepo.CreateResource(ownerCtx, integrations.Resource{
		ID: "repo-workspace-owner", IntegrationID: integration.ID, ResourceType: integrations.ResourceTypeRepo,
		ExternalID: "9001", ExternalKey: "acme/workspace-owner", DisplayName: "workspace-owner",
	})
	if err != nil {
		t.Fatal(err)
	}

	result := postGitHubWebhook(t, srv.URL, integration.ID, map[string]any{
		"action": "opened",
		"number": 11,
		"pull_request": map[string]any{
			"id": 9011, "number": 11, "title": "Shared work",
			"body": "<!-- specgate-work-ref: CR-SHARED -->", "html_url": "https://github.com/acme/workspace-owner/pull/11",
			"state": "open", "merged": false,
			"head": map[string]any{"ref": "feature/shared"}, "base": map[string]any{"ref": "main"},
		},
		"repository": map[string]any{"id": 9001, "full_name": "acme/workspace-owner"},
	})
	if result.ChangeRequestID != ownerCR.ID || result.FeatureID != ownerFeature.ID {
		t.Fatalf("webhook linked across workspace: result=%#v", result)
	}
}

func TestGitHubResourceWebhookRejectsBadSignature(t *testing.T) {
	t.Setenv(integrations.SecretKeyEnvVar, testSecretKey)
	srv, db := integrationsWebhookTestServer(t)
	defer srv.Close()
	ctx := context.Background()
	intRepo := storagedb.NewIntegrationRepository(db)
	integration, err := intRepo.CreateIntegration(ctx, integrations.Integration{
		ID: "github-bad", WorkspaceID: "ws-test", Provider: integrations.ProviderGitHub, Name: "GitHub", Status: integrations.StatusConnected,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := intRepo.CreateResource(ctx, integrations.Resource{
		ID: "repo-github-bad", IntegrationID: integration.ID, ResourceType: integrations.ResourceTypeRepo,
		ExternalID: "1", ExternalKey: "a/b",
	}); err != nil {
		t.Fatal(err)
	}

	raw, _ := json.Marshal(map[string]any{"action": "opened", "repository": map[string]any{"id": 1, "full_name": "a/b"}})
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/integrations/"+integration.ID+"/resources/repo-github-bad/github/webhook", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-Hub-Signature-256", "sha256=deadbeef")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad signature: status %d body=%s", resp.StatusCode, readBody(resp))
	}
}

func TestGitHubResourceWebhookIssueCommentCreatesScopeDriftFeedback(t *testing.T) {
	t.Setenv(integrations.SecretKeyEnvVar, testSecretKey)
	srv, db := integrationsWebhookTestServer(t)
	defer srv.Close()
	ctx := workboard.WithWorkspace(context.Background(), "ws-test")

	workBoardRepo := storagedb.NewWorkBoardRepository(db)
	feature, err := workBoardRepo.CreateFeature(ctx, workboard.Feature{
		ID: "feature-gh-comment", Key: "GH-COMMENT", Name: "GitHub Comment", Status: workboard.FeatureStatusPlanned,
	})
	if err != nil {
		t.Fatal(err)
	}
	cr, err := workBoardRepo.CreateChangeRequest(ctx, workboard.ChangeRequest{
		ID: "cr-gh-comment-1", Key: "CR-GH-COMMENT-1", FeatureID: feature.ID, WorkType: workboard.WorkTypeFeatureChange, Title: "comment drift",
	})
	if err != nil {
		t.Fatal(err)
	}
	intRepo := storagedb.NewIntegrationRepository(db)
	integration, err := intRepo.CreateIntegration(ctx, integrations.Integration{
		ID: "github-comment", WorkspaceID: "ws-test", Provider: integrations.ProviderGitHub, Name: "GitHub", Status: integrations.StatusConnected,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := intRepo.CreateResource(ctx, integrations.Resource{
		ID: "repo-gh-comment", IntegrationID: integration.ID, ResourceType: integrations.ResourceTypeRepo,
		ExternalID: "5005", ExternalKey: "acme/specgate", DisplayName: "specgate", DefaultRef: "main",
	}); err != nil {
		t.Fatal(err)
	}

	payload := map[string]any{
		"action": "created",
		"comment": map[string]any{
			"id":       9901,
			"body":     "This changes the acceptance criteria: partial refunds must reverse points proportionally.\n\n<!-- specgate-work-ref: CR-GH-COMMENT-1 -->",
			"html_url": "https://github.com/acme/specgate/issues/9#issuecomment-9901",
			"user":     map[string]any{"login": "alice"},
		},
		"issue": map[string]any{
			"number": 9,
			"title":  "Checkout refunds",
		},
		"repository": map[string]any{"id": 5005, "full_name": "acme/specgate"},
	}

	first := postGitHubWebhookEvent(t, srv.URL, integration.ID, "issue_comment", payload)
	if first.Status != integrations.WebhookStatusProcessed || first.ChangeRequestID != cr.Key {
		t.Fatalf("unexpected result: %#v", first)
	}
	second := postGitHubWebhookEvent(t, srv.URL, integration.ID, "issue_comment", payload)
	if second.IgnoredReason != "duplicate_webhook_event" {
		t.Fatalf("expected duplicate second delivery, got %#v", second)
	}

	feedback := getIntegrationJSON[struct {
		Items []integrations.GovernanceFeedbackEvent `json:"items"`
	}](t, srv.URL+"/governance/feedback-events?status=received")
	if len(feedback.Items) != 1 {
		t.Fatalf("want 1 feedback event, got %#v", feedback.Items)
	}
	ev := feedback.Items[0]
	// The exact work marker resolves to the work item's UUID + feature so the
	// drift comment surfaces on the work item (the UI queries by change_request_id).
	if ev.EventType != integrations.FeedbackEventCommentScopeDrift || ev.ChangeRequestID != cr.ID || ev.FeatureID != feature.ID {
		t.Fatalf("unexpected feedback event: %#v", ev)
	}
	var payloadJSON map[string]any
	if err := json.Unmarshal([]byte(ev.PayloadJSON), &payloadJSON); err != nil {
		t.Fatal(err)
	}
	if payloadJSON["author"] != "alice" || payloadJSON["correlation_id"] != cr.Key {
		t.Fatalf("unexpected payload: %#v", payloadJSON)
	}
}

func TestGitLabResourceWebhookClosedWithoutMergeWarnsWithoutRollback(t *testing.T) {
	t.Parallel()
	srv, db := integrationsWebhookTestServer(t)
	defer srv.Close()
	ctx := workboard.WithWorkspace(context.Background(), "ws-test")

	workBoardRepo := storagedb.NewWorkBoardRepository(db)
	feature, err := workBoardRepo.CreateFeature(ctx, workboard.Feature{
		ID: "feature-abandon", Key: "ABN", Name: "Abandon", Status: workboard.FeatureStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	cr, err := workBoardRepo.CreateChangeRequest(ctx, workboard.ChangeRequest{
		ID: "cr-abn-1", Key: "CR-ABN-1", FeatureID: feature.ID, WorkType: workboard.WorkTypeNewFeature, Title: "abandon work",
	})
	if err != nil {
		t.Fatal(err)
	}
	intRepo := storagedb.NewIntegrationRepository(db)
	integration, err := intRepo.CreateIntegration(ctx, integrations.Integration{
		ID: "gitlab-abn", WorkspaceID: "ws-test", Provider: integrations.ProviderGitLab, Name: "GL", Status: integrations.StatusConnected,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := intRepo.CreateResource(ctx, integrations.Resource{
		ID: "repo-abn", IntegrationID: integration.ID, ResourceType: integrations.ResourceTypeProject,
		ExternalID: "700", ExternalKey: "ns/abn", DefaultRef: "main",
	}); err != nil {
		t.Fatal(err)
	}

	res := postGitLabWebhook(t, srv.URL, integration.ID, map[string]any{
		"object_kind": "merge_request", "event_type": "merge_request",
		"project": map[string]any{"id": 700, "path_with_namespace": "ns/abn"},
		"object_attributes": map[string]any{
			"id": 7100, "iid": 3, "action": "close", "state": "closed",
			"title": "CR-ABN-1 abandoned attempt", "description": "Abandoned attempt.\n\n<!-- specgate-work-ref: CR-ABN-1 -->", "url": "https://gl/ns/abn/-/merge_requests/3",
			"source_branch": "specgate/cr-abn-1", "target_branch": "main", "target_project_id": 700,
		},
	})
	if res.Status != integrations.WebhookStatusProcessed || res.ChangeRequestID != cr.ID {
		t.Fatalf("unexpected result: %#v", res)
	}

	feedback := getIntegrationJSON[struct {
		Items []integrations.GovernanceFeedbackEvent `json:"items"`
	}](t, srv.URL+"/governance/feedback-events?status=received")
	var sawWarning bool
	for _, it := range feedback.Items {
		if it.EventType == integrations.FeedbackEventPRClosed {
			sawWarning = true
		}
	}
	if !sawWarning {
		t.Fatalf("expected a %s review warning, got %#v", integrations.FeedbackEventPRClosed, feedback.Items)
	}

	// A closed-without-merge webhook must NOT roll back or mutate governance
	// state — it only emits a review warning for a human.
	after, err := workBoardRepo.GetFeature(ctx, feature.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != workboard.FeatureStatusActive {
		t.Fatalf("feature status changed to %q — closed webhook must not mutate state", after.Status)
	}
}

func TestGitLabResourceWebhookRecordsCorrelationIDFromWorkMarker(t *testing.T) {
	t.Parallel()
	srv, db := integrationsWebhookTestServer(t)
	defer srv.Close()
	ctx := workboard.WithWorkspace(context.Background(), "ws-test")

	workBoardRepo := storagedb.NewWorkBoardRepository(db)
	feature, err := workBoardRepo.CreateFeature(ctx, workboard.Feature{
		ID: "feature-corr", Key: "CORR", Name: "Corr", Status: workboard.FeatureStatusPlanned,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workBoardRepo.CreateChangeRequest(ctx, workboard.ChangeRequest{
		ID: "cr-corr-1", Key: "CR-CORR-1", FeatureID: feature.ID, WorkType: workboard.WorkTypeNewFeature, Title: "corr work",
	}); err != nil {
		t.Fatal(err)
	}
	intRepo := storagedb.NewIntegrationRepository(db)
	integration, err := intRepo.CreateIntegration(ctx, integrations.Integration{
		ID: "gitlab-corr", WorkspaceID: "ws-test", Provider: integrations.ProviderGitLab, Name: "GL", Status: integrations.StatusConnected,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := intRepo.CreateResource(ctx, integrations.Resource{
		ID: "repo-corr", IntegrationID: integration.ID, ResourceType: integrations.ResourceTypeProject,
		ExternalID: "800", ExternalKey: "ns/corr", DefaultRef: "main",
	}); err != nil {
		t.Fatal(err)
	}

	postGitLabWebhook(t, srv.URL, integration.ID, map[string]any{
		"object_kind": "merge_request", "event_type": "merge_request",
		"project": map[string]any{"id": 800, "path_with_namespace": "ns/corr"},
		"object_attributes": map[string]any{
			"id": 8100, "iid": 4, "action": "open", "state": "opened",
			"title": "Implement checkout", "description": "Work body.\n\n<!-- specgate-work-ref: CR-CORR-1 -->",
			"url":           "https://gl/ns/corr/-/merge_requests/4",
			"source_branch": "feature/checkout", "target_branch": "main", "target_project_id": 800,
		},
	})

	events := getIntegrationJSON[struct {
		Items []integrations.WebhookEvent `json:"items"`
	}](t, srv.URL+"/integrations/"+integration.ID+"/webhook-events")
	if len(events.Items) != 1 {
		t.Fatalf("expected one recorded webhook event, got %#v", events.Items)
	}
	if events.Items[0].CorrelationID != "CR-CORR-1" {
		t.Fatalf("correlation_id = %q, want CR-CORR-1 (from work marker)", events.Items[0].CorrelationID)
	}
	if events.Items[0].PayloadHash == "" {
		t.Fatalf("expected payload_hash to be recorded, got empty")
	}
}

func TestGitLabResourceWebhookNoteCreatesScopeDriftFeedback(t *testing.T) {
	t.Parallel()
	srv, db := integrationsWebhookTestServer(t)
	defer srv.Close()
	ctx := workboard.WithWorkspace(context.Background(), "ws-test")

	workBoardRepo := storagedb.NewWorkBoardRepository(db)
	feature, err := workBoardRepo.CreateFeature(ctx, workboard.Feature{
		ID: "feature-note", Key: "NOTE", Name: "Note", Status: workboard.FeatureStatusPlanned,
	})
	if err != nil {
		t.Fatal(err)
	}
	cr, err := workBoardRepo.CreateChangeRequest(ctx, workboard.ChangeRequest{
		ID: "cr-note-1", Key: "CR-NOTE-1", FeatureID: feature.ID, WorkType: workboard.WorkTypeFeatureChange, Title: "note drift",
	})
	if err != nil {
		t.Fatal(err)
	}
	intRepo := storagedb.NewIntegrationRepository(db)
	integration, err := intRepo.CreateIntegration(ctx, integrations.Integration{
		ID: "gitlab-note", WorkspaceID: "ws-test", Provider: integrations.ProviderGitLab, Name: "GL", Status: integrations.StatusConnected,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := intRepo.CreateResource(ctx, integrations.Resource{
		ID: "repo-note", IntegrationID: integration.ID, ResourceType: integrations.ResourceTypeProject,
		ExternalID: "901", ExternalKey: "ns/note", DefaultRef: "main",
	}); err != nil {
		t.Fatal(err)
	}

	res := doGitLabWebhookWithEvent(t, srv.URL, integration.ID, "Note Hook", testGitLabSigningToken, "note-evt-1", map[string]any{
		"object_kind":   "note",
		"event_type":    "note",
		"project":       map[string]any{"id": 901, "path_with_namespace": "ns/note"},
		"user":          map[string]any{"username": "bob"},
		"merge_request": map[string]any{"title": "Refunds"},
		"object_attributes": map[string]any{
			"id":                771,
			"note":              "This changes the acceptance criteria: partial refunds must reverse points proportionally.\n\n<!-- specgate-work-ref: CR-NOTE-1 -->",
			"noteable_type":     "MergeRequest",
			"url":               "https://gitlab.example/ns/note/-/merge_requests/4#note_771",
			"target_project_id": 901,
		},
	})
	if res.Status != integrations.WebhookStatusProcessed || res.ChangeRequestID != cr.Key {
		t.Fatalf("unexpected result: %#v", res)
	}
	feedback := getIntegrationJSON[struct {
		Items []integrations.GovernanceFeedbackEvent `json:"items"`
	}](t, srv.URL+"/governance/feedback-events?status=received")
	if len(feedback.Items) != 1 || feedback.Items[0].EventType != integrations.FeedbackEventCommentScopeDrift {
		t.Fatalf("unexpected feedback events: %#v", feedback.Items)
	}
}

func TestGitLabResourceWebhookRejectsDeliveryForAnotherResource(t *testing.T) {
	t.Parallel()
	srv, db := integrationsWebhookTestServer(t)
	defer srv.Close()

	ctx := context.Background()
	intRepo := storagedb.NewIntegrationRepository(db)
	integration, err := intRepo.CreateIntegration(ctx, integrations.Integration{
		ID: "gitlab-resource-isolation", WorkspaceID: "ws-test", Provider: integrations.ProviderGitLab,
		Name: "GitLab", Status: integrations.StatusConnected,
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := intRepo.CreateResource(ctx, integrations.Resource{
		ID: "repo-first", IntegrationID: integration.ID, ResourceType: integrations.ResourceTypeProject,
		ExternalID: "100", ExternalKey: "acme/first",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := intRepo.CreateResource(ctx, integrations.Resource{
		ID: "repo-second", IntegrationID: integration.ID, ResourceType: integrations.ResourceTypeProject,
		ExternalID: "200", ExternalKey: "acme/second",
	}); err != nil {
		t.Fatal(err)
	}

	result := postGitLabResourceWebhook(t, srv.URL, integration.ID, first.ID, map[string]any{
		"object_kind": "merge_request",
		"project":     map[string]any{"id": 200, "path_with_namespace": "acme/second"},
		"object_attributes": map[string]any{
			"id": 2, "iid": 2, "action": "open", "state": "opened",
			"title": "wrong resource", "source_branch": "feature/x", "target_branch": "main",
		},
	})
	if result.Status != integrations.WebhookStatusIgnored || result.IgnoredReason != "resource_project_mismatch" {
		t.Fatalf("resource-isolation result = %#v", result)
	}
}

func TestGitLabResourceWebhook_UsesResourceSigningToken(t *testing.T) {
	t.Parallel()
	srv, db := integrationsWebhookTestServer(t)
	defer srv.Close()
	ctx := workboard.WithWorkspace(context.Background(), "ws-test")

	workBoardRepo := storagedb.NewWorkBoardRepository(db)
	feature, err := workBoardRepo.CreateFeature(ctx, workboard.Feature{
		ID:     "feature-resource-hook",
		Key:    "RESOURCE-HOOK",
		Name:   "Resource hook",
		Status: workboard.FeatureStatusPlanned,
	})
	if err != nil {
		t.Fatal(err)
	}
	cr, err := workBoardRepo.CreateChangeRequest(ctx, workboard.ChangeRequest{
		ID:        "cr-resource-hook",
		Key:       "CR-RESOURCE-HOOK",
		FeatureID: feature.ID,
		WorkType:  workboard.WorkTypeNewFeature,
		Title:     "Resource webhook path",
	})
	if err != nil {
		t.Fatal(err)
	}
	enc, err := integrations.EncryptSecret(testGitLabSigningToken)
	if err != nil {
		t.Fatal(err)
	}
	intRepo := storagedb.NewIntegrationRepository(db)
	integration, err := intRepo.CreateIntegration(ctx, integrations.Integration{
		ID:          "gitlab-resource",
		WorkspaceID: "ws-test",
		Provider:    integrations.ProviderGitLab,
		Name:        "Resource GitLab",
		Status:      integrations.StatusConnected,
		BaseURL:     "https://gitlab.example.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	resource, err := intRepo.CreateResource(ctx, integrations.Resource{
		ID:                     "repo-resource",
		IntegrationID:          integration.ID,
		ResourceType:           integrations.ResourceTypeProject,
		ExternalID:             "321",
		ExternalKey:            "acme/web",
		DisplayName:            "Web",
		DefaultRef:             "main",
		WebhookSecretEncrypted: enc,
	})
	if err != nil {
		t.Fatal(err)
	}

	result := postGitLabResourceWebhook(t, srv.URL, integration.ID, resource.ID, map[string]any{
		"object_kind": "merge_request",
		"event_type":  "merge_request",
		"project": map[string]any{
			"id":                  321,
			"path_with_namespace": "acme/web",
			"web_url":             "https://gitlab.example.test/acme/web",
		},
		"object_attributes": map[string]any{
			"id":                9001,
			"iid":               42,
			"action":            "open",
			"state":             "opened",
			"title":             "CR-RESOURCE-HOOK implement webhook",
			"description":       "Implements SpecGate work item CR-RESOURCE-HOOK.\n\n<!-- specgate-work-ref: CR-RESOURCE-HOOK -->",
			"url":               "https://gitlab.example.test/acme/web/-/merge_requests/42",
			"source_branch":     "feat/resource-hook",
			"target_branch":     "main",
			"target_project_id": 321,
		},
	})
	if result.Status != integrations.WebhookStatusProcessed || result.ResourceID != resource.ID || result.ChangeRequestID != cr.ID {
		t.Fatalf("unexpected resource webhook result: %#v", result)
	}
	duplicate := postGitLabResourceWebhook(t, srv.URL, integration.ID, resource.ID, map[string]any{
		"object_kind": "merge_request",
		"event_type":  "merge_request",
		"project": map[string]any{
			"id":                  321,
			"path_with_namespace": "acme/web",
			"web_url":             "https://gitlab.example.test/acme/web",
		},
		"object_attributes": map[string]any{
			"id":                9001,
			"iid":               42,
			"action":            "open",
			"state":             "opened",
			"title":             "CR-RESOURCE-HOOK implement webhook",
			"description":       "Implements SpecGate work item CR-RESOURCE-HOOK.\n\n<!-- specgate-work-ref: CR-RESOURCE-HOOK -->",
			"url":               "https://gitlab.example.test/acme/web/-/merge_requests/42",
			"source_branch":     "feat/resource-hook",
			"target_branch":     "main",
			"target_project_id": 321,
		},
	})
	if duplicate.WebhookEventID != result.WebhookEventID || duplicate.IgnoredReason != "duplicate_webhook_event" {
		t.Fatalf("duplicate resource webhook = %#v, want existing event %q", duplicate, result.WebhookEventID)
	}
}

func TestGitLabResourceWebhookRejectsNonSigningToken(t *testing.T) {
	t.Parallel()
	srv, db := integrationsWebhookTestServer(t)
	defer srv.Close()
	ctx := context.Background()

	const secretToken = "gl-secret-token-abc123" // not a whsec_ signing token
	enc, err := integrations.EncryptSecret(secretToken)
	if err != nil {
		t.Fatal(err)
	}
	intRepo := storagedb.NewIntegrationRepository(db)
	integration, err := intRepo.CreateIntegration(ctx, integrations.Integration{
		ID: "gitlab-secret-token", WorkspaceID: "ws-test", Provider: integrations.ProviderGitLab,
		Name: "Secret-token GitLab", Status: integrations.StatusConnected, BaseURL: "https://gitlab.example.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	resource, err := intRepo.CreateResource(ctx, integrations.Resource{
		ID: "repo-secret-token", IntegrationID: integration.ID, ResourceType: integrations.ResourceTypeProject,
		ExternalID: "321", ExternalKey: "acme/web", DisplayName: "Web", DefaultRef: "main",
		WebhookSecretEncrypted: enc,
	})
	if err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{
		"object_kind": "merge_request",
		"project":     map[string]any{"id": 321, "path_with_namespace": "acme/web"},
	}

	// A persisted non-signing token cannot authenticate a delivery.
	url := srv.URL + "/integrations/" + integration.ID + "/resources/" + resource.ID + "/gitlab/webhook"
	raw, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gitlab-Event", "Merge Request Hook")
	req.Header.Set("X-Gitlab-Token", secretToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("non-signing token: want 401, got %d", resp.StatusCode)
	}
}

func TestLinearResourceWebhookCommentCreatesScopeDriftFeedback(t *testing.T) {
	t.Setenv(integrations.SecretKeyEnvVar, testSecretKey)
	srv, db := integrationsWebhookTestServer(t)
	defer srv.Close()
	ctx := workboard.WithWorkspace(context.Background(), "ws-test")

	workBoardRepo := storagedb.NewWorkBoardRepository(db)
	feature, err := workBoardRepo.CreateFeature(ctx, workboard.Feature{
		ID: "feature-linear-comment", Key: "LIN-COMMENT", Name: "Linear Comment", Status: workboard.FeatureStatusPlanned,
	})
	if err != nil {
		t.Fatal(err)
	}
	cr, err := workBoardRepo.CreateChangeRequest(ctx, workboard.ChangeRequest{
		ID: "cr-linear-comment-1", Key: "CR-LINEAR-COMMENT-1", FeatureID: feature.ID, WorkType: workboard.WorkTypeFeatureChange, Title: "linear comment drift",
	})
	if err != nil {
		t.Fatal(err)
	}
	intRepo := storagedb.NewIntegrationRepository(db)
	integration, err := intRepo.CreateIntegration(ctx, integrations.Integration{
		ID: "linear-comment", WorkspaceID: "ws-test", Provider: integrations.ProviderLinear, Name: "Linear", Status: integrations.StatusConnected,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := intRepo.CreateResource(ctx, integrations.Resource{
		ID: "team-linear-comment", IntegrationID: integration.ID, ResourceType: integrations.ResourceTypeTeam,
		ExternalID: "team-acme", ExternalKey: "acme",
	}); err != nil {
		t.Fatal(err)
	}

	payload := map[string]any{
		"type":      "Comment",
		"webhookId": "linear-comment-evt-1",
		"data": map[string]any{
			"id":   "comment-1",
			"body": "This changes the acceptance criteria: partial refunds must reverse points proportionally.\n\n<!-- specgate-work-ref: CR-LINEAR-COMMENT-1 -->",
			"url":  "https://linear.app/acme/issue/LOY-1#comment-1",
			"user": map[string]any{"name": "Carol"},
			"issue": map[string]any{
				"id":         "issue-1",
				"identifier": "LOY-1",
				"title":      "Refunds",
				"url":        "https://linear.app/acme/issue/LOY-1",
			},
		},
	}

	res := postLinearWebhook(t, srv.URL, integration.ID, "team-linear-comment", payload)
	if res.Status != integrations.WebhookStatusProcessed || res.CorrelationID != cr.Key {
		t.Fatalf("unexpected result: %#v", res)
	}
	feedback := getIntegrationJSON[struct {
		Items []integrations.GovernanceFeedbackEvent `json:"items"`
	}](t, srv.URL+"/governance/feedback-events?status=received")
	if len(feedback.Items) != 1 || feedback.Items[0].EventType != integrations.FeedbackEventCommentScopeDrift {
		t.Fatalf("unexpected feedback events: %#v", feedback.Items)
	}
}

func TestGitHubResourceWebhookRejectsWhenNoSecretConfigured(t *testing.T) {
	t.Parallel()
	srv, db := integrationsWebhookTestServer(t)
	defer srv.Close()
	ctx := context.Background()
	intRepo := storagedb.NewIntegrationRepository(db)
	integration, err := intRepo.CreateIntegration(ctx, integrations.Integration{
		ID: "github-nosecret", WorkspaceID: "ws-test", Provider: integrations.ProviderGitHub, Name: "GitHub", Status: integrations.StatusConnected,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := intRepo.CreateResource(ctx, integrations.Resource{
		ID: "repo-github-no-secret", IntegrationID: integration.ID, ResourceType: integrations.ResourceTypeRepo,
		ExternalID: "1", ExternalKey: "a/b", ConfigJSON: `{"test_no_webhook_secret":true}`,
	}); err != nil {
		t.Fatal(err)
	}
	// No secret is configured on the resource, so HMAC cannot be
	// verified — the endpoint must refuse rather than become an open relay.
	raw, _ := json.Marshal(map[string]any{"action": "opened", "repository": map[string]any{"id": 1, "full_name": "a/b"}})
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/integrations/"+integration.ID+"/resources/repo-github-no-secret/github/webhook", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-Hub-Signature-256", "sha256=anything")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("integration with no recoverable secret: status %d body=%s", resp.StatusCode, readBody(resp))
	}
}

func postGitHubWebhook(t *testing.T, baseURL string, integrationID string, payload map[string]any) integrations.GitLabWebhookResult {
	return postGitHubWebhookEvent(t, baseURL, integrationID, "pull_request", payload)
}

func setIntegrationAPIToken(t *testing.T, baseURL string, integrationID string, token string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"api_token": token})
	req, err := http.NewRequest(http.MethodPut, baseURL+"/integrations/"+integrationID+"/api-token?workspace_id=ws-test", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("set token: status %d body=%s", resp.StatusCode, readBody(resp))
	}
}
