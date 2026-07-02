package db

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/specgate/doc-registry/internal/integrations"
	"gorm.io/gorm"
)

func mustTimePtr(t *testing.T, raw string) *time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		t.Fatalf("time.Parse(%q): %v", raw, err)
	}
	return &parsed
}

// payload_hash must be the hex SHA-256 of the raw body itself — not the
// `sha256:`-prefixed external-event-id fallback, and not derived from the
// delivery id. Two deliveries with different ids but identical bodies must hash
// the same.
func TestIntegrationRepository_RecordWebhookEventHashesBody(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		ctx := context.Background()
		repo := NewIntegrationRepository(gdb)
		integration, err := repo.CreateIntegration(ctx, integrations.Integration{
			ID: "int-hash-" + name, Provider: integrations.ProviderGitHub, Name: "x", Status: integrations.StatusConnected,
		})
		if err != nil {
			t.Fatalf("CreateIntegration: %v", err)
		}
		body := `{"action":"opened","number":1}`
		sum := sha256.Sum256([]byte(body))
		want := hex.EncodeToString(sum[:])

		_, first, err := repo.RecordWebhookEvent(ctx, integrations.WebhookEvent{
			IntegrationID: integration.ID, Provider: "github", EventType: "merge_request",
			ExternalEventID: "delivery-A", PayloadJSON: body, Status: "pending",
		})
		if err != nil {
			t.Fatalf("RecordWebhookEvent A: %v", err)
		}
		if first.PayloadHash != want {
			t.Fatalf("payload_hash = %q, want %q (hex sha256 of body)", first.PayloadHash, want)
		}
		if strings.HasPrefix(first.PayloadHash, "sha256:") {
			t.Fatalf("payload_hash must not carry the external-id sha256: prefix, got %q", first.PayloadHash)
		}

		_, second, err := repo.RecordWebhookEvent(ctx, integrations.WebhookEvent{
			IntegrationID: integration.ID, Provider: "github", EventType: "merge_request",
			ExternalEventID: "delivery-B", PayloadJSON: body, Status: "pending",
		})
		if err != nil {
			t.Fatalf("RecordWebhookEvent B: %v", err)
		}
		if second.PayloadHash != first.PayloadHash {
			t.Fatalf("identical bodies must hash the same regardless of delivery id: %q vs %q", second.PayloadHash, first.PayloadHash)
		}
	})
}

func TestIntegrationRepository_RoundTripResourcesAndWebhookEvents(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		ctx := context.Background()
		repo := NewIntegrationRepository(gdb)

		created, err := repo.CreateIntegration(ctx, integrations.Integration{
			ID:         "int_gitlab",
			Provider:   integrations.ProviderGitLab,
			Name:       "Acme GitLab",
			Status:     integrations.StatusConnected,
			BaseURL:    "https://gitlab.acme.io",
			ConfigJSON: `{"webhook_enabled":true}`,
		})
		if err != nil {
			t.Fatalf("CreateIntegration: %v", err)
		}
		if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
			t.Fatalf("expected timestamps, got created_at=%v updated_at=%v", created.CreatedAt, created.UpdatedAt)
		}

		resource, err := repo.CreateResource(ctx, integrations.Resource{
			IntegrationID: created.ID,
			ResourceType:  integrations.ResourceTypeProject,
			ExternalID:    "321",
			ExternalKey:   "acme/projects/specgate-be",
			DisplayName:   "specgate-be",
			DefaultRef:    "master",
			ConfigJSON:    `{"issue_link_required":true}`,
		})
		if err != nil {
			t.Fatalf("CreateResource: %v", err)
		}
		if resource.ID == "" {
			t.Fatal("expected generated resource id")
		}

		created1, event, err := repo.RecordWebhookEvent(ctx, integrations.WebhookEvent{
			IntegrationID:   created.ID,
			ResourceID:      resource.ID,
			Provider:        integrations.ProviderGitLab,
			EventType:       integrations.WebhookEventMergeRequest,
			ExternalEventID: "gitlab-evt-1",
			PayloadJSON:     `{"object_kind":"merge_request","object_attributes":{"iid":42}}`,
			Status:          integrations.WebhookStatusPending,
		})
		if err != nil {
			t.Fatalf("RecordWebhookEvent: %v", err)
		}
		if !created1 {
			t.Fatal("expected first record to be marked created=true")
		}
		if event.ReceivedAt.IsZero() {
			t.Fatal("expected received_at")
		}
		created2, duplicate, err := repo.RecordWebhookEvent(ctx, integrations.WebhookEvent{
			IntegrationID:   created.ID,
			ResourceID:      resource.ID,
			Provider:        integrations.ProviderGitLab,
			EventType:       integrations.WebhookEventMergeRequest,
			ExternalEventID: "gitlab-evt-1",
			PayloadJSON:     `{"object_kind":"merge_request","object_attributes":{"iid":42}}`,
			Status:          integrations.WebhookStatusPending,
		})
		if err != nil {
			t.Fatalf("RecordWebhookEvent duplicate: %v", err)
		}
		if created2 {
			t.Fatal("expected duplicate record to be marked created=false")
		}
		if duplicate.ID != event.ID {
			t.Fatalf("duplicate webhook should return existing row, got %q want %q", duplicate.ID, event.ID)
		}

		matchedIntegration, matchedResource, err := repo.FindResourceByProvider(ctx, integrations.ProviderGitLab, integrations.ResourceTypeProject, "missing-project-id", resource.ExternalKey)
		if err != nil {
			t.Fatalf("FindResourceByProvider fallback: %v", err)
		}
		if matchedIntegration.ID != created.ID || matchedResource.ID != resource.ID {
			t.Fatalf("unexpected fallback match integration=%#v resource=%#v", matchedIntegration, matchedResource)
		}

		items, err := repo.ListIntegrations(ctx)
		if err != nil {
			t.Fatalf("ListIntegrations: %v", err)
		}
		if len(items) != 1 || items[0].Provider != integrations.ProviderGitLab {
			t.Fatalf("unexpected integrations: %#v", items)
		}

		resources, err := repo.ListResources(ctx, created.ID)
		if err != nil {
			t.Fatalf("ListResources: %v", err)
		}
		if len(resources) != 1 || resources[0].ExternalKey != "acme/projects/specgate-be" {
			t.Fatalf("unexpected resources: %#v", resources)
		}

		events, err := repo.ListWebhookEvents(ctx, integrations.WebhookEventFilter{IntegrationID: created.ID, Limit: 10})
		if err != nil {
			t.Fatalf("ListWebhookEvents: %v", err)
		}
		if len(events) != 1 || events[0].ExternalEventID != "gitlab-evt-1" || events[0].Status != integrations.WebhookStatusPending {
			t.Fatalf("unexpected webhook events: %#v", events)
		}

		created.Status = integrations.StatusDisabled
		created.LastError = "manual pause"
		updated, err := repo.UpdateIntegration(ctx, *created)
		if err != nil {
			t.Fatalf("UpdateIntegration: %v", err)
		}
		if updated.Status != integrations.StatusDisabled || updated.LastError != "manual pause" {
			t.Fatalf("unexpected updated integration: %#v", updated)
		}
	})
}

// End-to-end: a signed Linear Issue webhook against the committed fixture must
// authenticate, normalize, read the `fixes SPECGATE-{key}` correlation, emit a
// delivery.tracker_status_changed feedback event carrying the identifier +
// raw workflow state.type, and mark the webhook event processed. Trackers are
// optional, so a missing work item must NOT gate emission.
func TestIntegrationService_HandleLinearWebhook_EmitsTrackerStatusChanged(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		t.Setenv(integrations.SecretKeyEnvVar, "0000000000000000000000000000000000000000000000000000000000000001")
		ctx := context.Background()
		repo := NewIntegrationRepository(gdb)
		const secret = "lin_wh_secret"
		svc := integrations.NewService(repo).WithWebhookSecrets(integrations.WebhookSecrets{Linear: secret})

		created, err := svc.Create(ctx, integrations.CreateInput{
			Provider:   integrations.ProviderLinear,
			Name:       "Acme Linear",
			ConfigJSON: `{"mcp_server_url":"https://mcp.linear.app/mcp","enabled":true}`,
		})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}

		raw, err := os.ReadFile(filepath.Join("..", "..", "integrations", "testdata", "tracker", "inbound_webhook.linear.json"))
		if err != nil {
			t.Fatalf("read fixture: %v", err)
		}
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(raw)
		sig := hex.EncodeToString(mac.Sum(nil))

		result, err := svc.HandleLinearWebhook(ctx, created.ID, integrations.LinearWebhookInput{
			Signature:   sig,
			PayloadJSON: string(raw),
		})
		if err != nil {
			t.Fatalf("HandleLinearWebhook: %v", err)
		}
		if result.Status != integrations.WebhookStatusProcessed {
			t.Fatalf("status = %q, want processed", result.Status)
		}
		if result.Identifier != "LOY-128" {
			t.Fatalf("identifier = %q, want LOY-128", result.Identifier)
		}
		if result.TrackerState != "started" {
			t.Fatalf("tracker state = %q, want started", result.TrackerState)
		}
		if result.CorrelationID != "CR-LOYALTY-V1" {
			t.Fatalf("correlation = %q, want CR-LOYALTY-V1", result.CorrelationID)
		}
		if len(result.FeedbackEventIDs) != 1 {
			t.Fatalf("feedback ids = %v, want exactly one", result.FeedbackEventIDs)
		}

		items, err := repo.ListGovernanceFeedbackEvents(ctx, integrations.GovernanceFeedbackFilter{Limit: 10})
		if err != nil {
			t.Fatalf("ListGovernanceFeedbackEvents: %v", err)
		}
		if len(items) != 1 || items[0].EventType != integrations.FeedbackEventTrackerStatusChanged {
			t.Fatalf("unexpected feedback items: %#v", items)
		}

		// A bad signature must be rejected (401-mapped ErrUnauthorized).
		if _, err := svc.HandleLinearWebhook(ctx, created.ID, integrations.LinearWebhookInput{
			Signature:   "deadbeef",
			PayloadJSON: string(raw),
		}); err == nil {
			t.Fatal("bad signature must be rejected")
		}

		// Replaying the same webhookId must dedup, not double-emit.
		dup, err := svc.HandleLinearWebhook(ctx, created.ID, integrations.LinearWebhookInput{
			Signature:   sig,
			PayloadJSON: string(raw),
		})
		if err != nil {
			t.Fatalf("HandleLinearWebhook(dup): %v", err)
		}
		if dup.IgnoredReason != "duplicate_webhook_event" {
			t.Fatalf("dup ignored reason = %q, want duplicate_webhook_event", dup.IgnoredReason)
		}
	})
}

// A Linear API token round-trips through the encrypted column and surfaces as
// the derived has_api_token presence flag without ever exposing plaintext.
func TestIntegrationService_SetApiToken_EncryptsAndFlags(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		t.Setenv(integrations.SecretKeyEnvVar, "0000000000000000000000000000000000000000000000000000000000000001")
		ctx := context.Background()
		repo := NewIntegrationRepository(gdb)
		svc := integrations.NewService(repo)

		created, err := svc.Create(ctx, integrations.CreateInput{
			Provider: integrations.ProviderLinear,
			Name:     "Acme Linear",
		})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if created.HasAPIToken {
			t.Fatal("fresh integration must not report an API token")
		}
		if err := svc.SetApiToken(ctx, created.ID, "lin_api_token_xyz"); err != nil {
			t.Fatalf("SetApiToken: %v", err)
		}
		got, err := svc.Get(ctx, created.ID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if !got.HasAPIToken {
			t.Fatal("has_api_token must be true after setting the token")
		}
		dec, err := integrations.DecryptSecret(got.APITokenEncrypted)
		if err != nil {
			t.Fatalf("DecryptSecret: %v (ciphertext=%q)", err, got.APITokenEncrypted)
		}
		if dec != "lin_api_token_xyz" {
			t.Fatalf("decrypted token = %q, want lin_api_token_xyz", dec)
		}
	})
}

func TestIntegrationRepository_PersistsOAuthGrantFields(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		ctx := context.Background()
		repo := NewIntegrationRepository(gdb)

		created, err := repo.CreateIntegration(ctx, integrations.Integration{
			ID:                         "int-oauth-grant",
			Provider:                   integrations.ProviderGitLab,
			Name:                       "GitLab OAuth",
			Status:                     integrations.StatusConnected,
			AuthMethod:                 integrations.AuthMethodOAuth,
			OAuthAccessTokenEncrypted:  "enc-access",
			OAuthRefreshTokenEncrypted: "enc-refresh",
			OAuthExpiresAt:             mustTimePtr(t, "2026-06-05T14:00:00Z"),
			OAuthTokenType:             "Bearer",
			OAuthScope:                 "api",
			OAuthAccountID:             "12345",
			OAuthAccountName:           "Alice Dev",
			OAuthAccountEmail:          "alice@example.com",
			OAuthHostKey:               "gitlab.gitlab_com",
		})
		if err != nil {
			t.Fatalf("CreateIntegration: %v", err)
		}
		got, err := repo.GetIntegration(ctx, created.ID)
		if err != nil {
			t.Fatalf("GetIntegration: %v", err)
		}
		if got.AuthMethod != integrations.AuthMethodOAuth {
			t.Fatalf("auth_method = %q, want oauth", got.AuthMethod)
		}
		if got.OAuthAccessTokenEncrypted != "enc-access" || got.OAuthRefreshTokenEncrypted != "enc-refresh" {
			t.Fatalf("unexpected oauth ciphertexts: %#v", got)
		}
		if got.OAuthExpiresAt == nil || !got.OAuthExpiresAt.Equal(*mustTimePtr(t, "2026-06-05T14:00:00Z")) {
			t.Fatalf("oauth_expires_at = %v, want 2026-06-05T14:00:00Z", got.OAuthExpiresAt)
		}
		if got.OAuthAccountName != "Alice Dev" || got.OAuthHostKey != "gitlab.gitlab_com" {
			t.Fatalf("unexpected oauth metadata: %#v", got)
		}
	})
}

func TestIntegrationRepository_UpdateAndClearOAuthGrant(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		ctx := context.Background()
		repo := NewIntegrationRepository(gdb)

		created, err := repo.CreateIntegration(ctx, integrations.Integration{
			ID:       "int-oauth-upd",
			Provider: integrations.ProviderLinear,
			Name:     "Linear OAuth",
			Status:   integrations.StatusConnected,
		})
		if err != nil {
			t.Fatalf("CreateIntegration: %v", err)
		}

		// A grant with NO refresh token and NO expiry must persist as empty/NULL,
		// not be skipped as zero values (the GORM struct-Updates pitfall).
		if err := repo.UpdateOAuthGrant(ctx, integrations.Integration{
			ID:                        created.ID,
			AuthMethod:                integrations.AuthMethodOAuth,
			OAuthAccessTokenEncrypted: "enc-access-2",
			OAuthScope:                "read write",
			OAuthAccountName:          "Tung",
		}); err != nil {
			t.Fatalf("UpdateOAuthGrant: %v", err)
		}
		got, err := repo.GetIntegration(ctx, created.ID)
		if err != nil {
			t.Fatalf("GetIntegration: %v", err)
		}
		if got.AuthMethod != integrations.AuthMethodOAuth || got.OAuthAccessTokenEncrypted != "enc-access-2" {
			t.Fatalf("oauth grant not persisted: %#v", got)
		}
		if got.OAuthRefreshTokenEncrypted != "" || got.OAuthExpiresAt != nil {
			t.Fatalf("empty refresh / nil expiry should persist as empty/NULL: %#v", got)
		}
		if !got.HasOAuthToken {
			t.Fatalf("has_oauth_token should be true after grant")
		}

		// Disconnect clears all oauth columns + auth_method, and drops the
		// integration out of "connected" so the DTO is not misleading.
		if err := repo.ClearOAuthGrant(ctx, created.ID); err != nil {
			t.Fatalf("ClearOAuthGrant: %v", err)
		}
		cleared, err := repo.GetIntegration(ctx, created.ID)
		if err != nil {
			t.Fatalf("GetIntegration after clear: %v", err)
		}
		if cleared.OAuthAccessTokenEncrypted != "" || cleared.AuthMethod != "" || cleared.HasOAuthToken {
			t.Fatalf("oauth grant not cleared: %#v", cleared)
		}
		if cleared.Status != integrations.StatusDisabled {
			t.Fatalf("status after disconnect = %q, want disabled", cleared.Status)
		}
	})
}

func TestIntegrationRepository_UpdateIntegration_ClearsLastErrorWhenStatusReturnsConnected(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		ctx := context.Background()
		repo := NewIntegrationRepository(gdb)

		created, err := repo.CreateIntegration(ctx, integrations.Integration{
			ID:        "int-clear-last-error",
			Provider:  integrations.ProviderGitLab,
			Name:      "GitLab OAuth",
			Status:    integrations.StatusError,
			LastError: "oauth refresh rejected",
		})
		if err != nil {
			t.Fatalf("CreateIntegration: %v", err)
		}

		updated, err := repo.UpdateIntegration(ctx, integrations.Integration{
			ID:        created.ID,
			Status:    integrations.StatusConnected,
			LastError: "",
		})
		if err != nil {
			t.Fatalf("UpdateIntegration: %v", err)
		}
		if updated.Status != integrations.StatusConnected {
			t.Fatalf("status = %q, want connected", updated.Status)
		}
		if updated.LastError != "" {
			t.Fatalf("last_error = %q, want empty", updated.LastError)
		}
	})
}

func TestIntegrationRepository_CreateGetConsumeOAuthState(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		ctx := context.Background()
		repo := NewIntegrationRepository(gdb)

		if _, err := repo.CreateIntegration(ctx, integrations.Integration{
			ID:       "int-123",
			Provider: integrations.ProviderGitHub,
			Name:     "GitHub OAuth",
			Status:   integrations.StatusConnected,
		}); err != nil {
			t.Fatalf("CreateIntegration: %v", err)
		}

		state, err := repo.CreateOAuthState(ctx, integrations.OAuthState{
			State:          "opaque-state-token",
			IntegrationID:  "int-123",
			Provider:       integrations.ProviderGitHub,
			HostKey:        "github.github_com",
			RedirectTarget: "/settings/integrations",
			ExpiresAt:      *mustTimePtr(t, "2026-06-05T15:00:00Z"),
		})
		if err != nil {
			t.Fatalf("CreateOAuthState: %v", err)
		}
		if state.ID == "" {
			t.Fatal("expected generated OAuth state id")
		}
		got, err := repo.GetOAuthState(ctx, "opaque-state-token")
		if err != nil {
			t.Fatalf("GetOAuthState: %v", err)
		}
		if got.IntegrationID != "int-123" || got.Provider != integrations.ProviderGitHub {
			t.Fatalf("unexpected oauth state: %#v", got)
		}
		if got.ConsumedAt != nil {
			t.Fatalf("fresh oauth state should not be consumed: %#v", got)
		}

		consumed, err := repo.ConsumeOAuthState(ctx, "opaque-state-token")
		if err != nil {
			t.Fatalf("ConsumeOAuthState: %v", err)
		}
		if consumed.ConsumedAt == nil {
			t.Fatalf("consumed oauth state should have consumed_at: %#v", consumed)
		}
		again, err := repo.GetOAuthState(ctx, "opaque-state-token")
		if err != nil {
			t.Fatalf("GetOAuthState after consume: %v", err)
		}
		if again.ConsumedAt == nil {
			t.Fatalf("consumed oauth state should persist consumed_at: %#v", again)
		}
	})
}

func TestIntegrationRepository_DeliveryLinksAndGovernanceFeedback(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		ctx := context.Background()
		repo := NewIntegrationRepository(gdb)

		integration, err := repo.CreateIntegration(ctx, integrations.Integration{
			ID:       "int_gitlab_delivery",
			Provider: integrations.ProviderGitLab,
			Name:     "Acme GitLab",
			Status:   integrations.StatusConnected,
		})
		if err != nil {
			t.Fatalf("CreateIntegration: %v", err)
		}
		resource, err := repo.CreateResource(ctx, integrations.Resource{
			IntegrationID: integration.ID,
			ResourceType:  integrations.ResourceTypeProject,
			ExternalID:    "321",
			ExternalKey:   "acme/projects/specgate-fe",
		})
		if err != nil {
			t.Fatalf("CreateResource: %v", err)
		}
		_, event, err := repo.RecordWebhookEvent(ctx, integrations.WebhookEvent{
			IntegrationID: integration.ID,
			ResourceID:    resource.ID,
			Provider:      integrations.ProviderGitLab,
			EventType:     integrations.WebhookEventMergeRequest,
			PayloadJSON:   `{"object_kind":"merge_request"}`,
			Status:        integrations.WebhookStatusPending,
		})
		if err != nil {
			t.Fatalf("RecordWebhookEvent: %v", err)
		}

		link, err := repo.UpsertDeliveryLink(ctx, integrations.DeliveryLink{
			IntegrationID:   integration.ID,
			ResourceID:      resource.ID,
			FeatureID:       "feature-loyalty",
			ChangeRequestID: "cr-loyalty-v1",
			ExternalType:    integrations.ExternalTypeMergeRequest,
			ExternalID:      "9001",
			ExternalIID:     "42",
			ExternalKey:     "acme/projects/specgate-fe!42",
			URL:             "https://gitlab.acme.io/acme/projects/specgate-fe/-/merge_requests/42",
			Title:           "CR-LOYALTY-V1 FE",
			State:           integrations.DeliveryStateOpened,
			LastEventID:     event.ID,
		})
		if err != nil {
			t.Fatalf("UpsertDeliveryLink(opened): %v", err)
		}
		merged, err := repo.UpsertDeliveryLink(ctx, integrations.DeliveryLink{
			IntegrationID:   integration.ID,
			ResourceID:      resource.ID,
			FeatureID:       "feature-loyalty",
			ChangeRequestID: "cr-loyalty-v1",
			ExternalType:    integrations.ExternalTypeMergeRequest,
			ExternalID:      "9001",
			ExternalIID:     "42",
			ExternalKey:     "acme/projects/specgate-fe!42",
			URL:             link.URL,
			Title:           link.Title,
			State:           integrations.DeliveryStateMerged,
			MergeCommitSHA:  "abc123",
			LastEventID:     event.ID,
		})
		if err != nil {
			t.Fatalf("UpsertDeliveryLink(merged): %v", err)
		}
		if merged.ID != link.ID || merged.State != integrations.DeliveryStateMerged || merged.MergeCommitSHA != "abc123" {
			t.Fatalf("unexpected merged link: %#v opened=%#v", merged, link)
		}

		feedback, err := repo.CreateGovernanceFeedbackEvent(ctx, integrations.GovernanceFeedbackEvent{
			IntegrationID:   integration.ID,
			ResourceID:      resource.ID,
			WebhookEventID:  event.ID,
			DeliveryLinkID:  link.ID,
			FeatureID:       "feature-loyalty",
			ChangeRequestID: "cr-loyalty-v1",
			EventType:       integrations.FeedbackEventPRMerged,
			PayloadJSON:     `{"mr_iid":42}`,
			Status:          integrations.FeedbackStatusPending,
		})
		if err != nil {
			t.Fatalf("CreateGovernanceFeedbackEvent: %v", err)
		}
		items, err := repo.ListGovernanceFeedbackEvents(ctx, integrations.GovernanceFeedbackFilter{Status: integrations.FeedbackStatusPending, Limit: 10})
		if err != nil {
			t.Fatalf("ListGovernanceFeedbackEvents: %v", err)
		}
		if len(items) != 1 || items[0].ID != feedback.ID || items[0].EventType != integrations.FeedbackEventPRMerged {
			t.Fatalf("unexpected feedback items: %#v", items)
		}
	})
}

// A handoff persists one tracker_links row per lane (FE/BE), kept distinct by the
// (integration_id, external_key) upsert key. TrackerLinkByExternal resolves
// either by the immutable id or the human key, and returns (nil, nil) on no match.
func TestIntegrationRepository_TrackerLinkByExternal(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		ctx := context.Background()
		repo := NewIntegrationRepository(gdb)
		integration, err := repo.CreateIntegration(ctx, integrations.Integration{
			ID:       "int_linear_tracker",
			Provider: integrations.ProviderLinear,
			Name:     "Linear",
			Status:   integrations.StatusConnected,
		})
		if err != nil {
			t.Fatalf("CreateIntegration: %v", err)
		}
		for _, lane := range []struct{ id, key, name string }{{"lin-uuid-fe", "ZOP-6", "fe"}, {"lin-uuid-be", "ZOP-7", "be"}} {
			if _, err := repo.UpsertTrackerLink(ctx, integrations.TrackerLink{
				IntegrationID:   integration.ID,
				FeatureID:       "feat-1",
				ChangeRequestID: "cr-1",
				Lane:            lane.name,
				ExternalID:      lane.id,
				ExternalKey:     lane.key,
			}); err != nil {
				t.Fatalf("UpsertTrackerLink(%s): %v", lane.key, err)
			}
		}

		byID, err := repo.TrackerLinkByExternal(ctx, integration.ID, "lin-uuid-be", "")
		if err != nil || byID == nil || byID.ExternalKey != "ZOP-7" || byID.ChangeRequestID != "cr-1" || byID.Lane != "be" {
			t.Fatalf("by id: %#v (err %v)", byID, err)
		}
		byKey, err := repo.TrackerLinkByExternal(ctx, integration.ID, "", "ZOP-6")
		if err != nil || byKey == nil || byKey.ExternalID != "lin-uuid-fe" {
			t.Fatalf("by key: %#v (err %v)", byKey, err)
		}
		// Inbound event carries both; an id match must not be defeated by the OR.
		both, err := repo.TrackerLinkByExternal(ctx, integration.ID, "lin-uuid-fe", "ZOP-6")
		if err != nil || both == nil || both.ExternalKey != "ZOP-6" {
			t.Fatalf("by both: %#v (err %v)", both, err)
		}
		none, err := repo.TrackerLinkByExternal(ctx, integration.ID, "nope", "NOPE-1")
		if err != nil || none != nil {
			t.Fatalf("no match: %#v (err %v)", none, err)
		}
		// The work item's "linked issues" surface lists all its lanes.
		byCR, err := repo.ListTrackerLinksByChangeRequest(ctx, "cr-1")
		if err != nil || len(byCR) != 2 {
			t.Fatalf("list by change request = %d (err %v)", len(byCR), err)
		}
		// Re-emit of the same key updates in place (state transition), not a new row.
		if _, err := repo.UpsertTrackerLink(ctx, integrations.TrackerLink{
			IntegrationID: integration.ID, ChangeRequestID: "cr-1", ExternalKey: "ZOP-6", State: integrations.TrackerStateClosed,
		}); err != nil {
			t.Fatalf("re-upsert: %v", err)
		}
		updated, _ := repo.TrackerLinkByExternal(ctx, integration.ID, "", "ZOP-6")
		if updated == nil || updated.State != integrations.TrackerStateClosed {
			t.Fatalf("re-upsert did not update in place: %#v", updated)
		}
	})
}

// Coding-agent feedback (reported via MCP) has no originating integration, so it
// stores integration_id=”. Regression for the dropped FK: the insert must
// succeed without any integrations row.
func TestIntegrationRepository_CreateGovernanceFeedbackEvent_AgentOriginatedNoIntegration(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewIntegrationRepository(gdb)
		ctx := context.Background()
		fb, err := repo.CreateGovernanceFeedbackEvent(ctx, integrations.GovernanceFeedbackEvent{
			ChangeRequestID: "cr-loyalty-redeem",
			ArtifactID:      "art-1",
			EventType:       integrations.FeedbackEventCodingAgentCompleted,
			PayloadJSON:     `{"summary":"done"}`,
			Status:          integrations.FeedbackStatusReceived,
			Reason:          "Implemented redeem with idempotency",
			// IntegrationID deliberately empty — agent feedback has no integration.
		})
		if err != nil {
			t.Fatalf("CreateGovernanceFeedbackEvent (no integration): %v", err)
		}
		if fb.IntegrationID != "" {
			t.Fatalf("integration_id = %q, want empty", fb.IntegrationID)
		}
	})
}

func TestIntegrationRepository_UpdateGovernanceFeedbackEventStatus(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		ctx := context.Background()
		repo := NewIntegrationRepository(gdb)

		integration, err := repo.CreateIntegration(ctx, integrations.Integration{
			ID:       "int_reconcile",
			Provider: integrations.ProviderGitLab,
			Name:     "Reconcile GitLab",
			Status:   integrations.StatusConnected,
		})
		if err != nil {
			t.Fatalf("CreateIntegration: %v", err)
		}
		feedback, err := repo.CreateGovernanceFeedbackEvent(ctx, integrations.GovernanceFeedbackEvent{
			IntegrationID:   integration.ID,
			ChangeRequestID: "cr-loyalty-v1",
			EventType:       integrations.FeedbackEventPRMerged,
			PayloadJSON:     `{"mr_iid":42}`,
			Status:          integrations.FeedbackStatusPending,
		})
		if err != nil {
			t.Fatalf("CreateGovernanceFeedbackEvent: %v", err)
		}

		updated, err := repo.UpdateGovernanceFeedbackEventStatus(
			ctx, feedback.ID, integrations.FeedbackStatusProcessed, "artifact-update proposal approved",
		)
		if err != nil {
			t.Fatalf("UpdateGovernanceFeedbackEventStatus: %v", err)
		}
		if updated.Status != integrations.FeedbackStatusProcessed || updated.Reason != "artifact-update proposal approved" {
			t.Fatalf("unexpected updated event: %#v", updated)
		}
		if !updated.UpdatedAt.After(feedback.UpdatedAt) && !updated.UpdatedAt.Equal(feedback.UpdatedAt) {
			t.Fatalf("updated_at not advanced: before=%v after=%v", feedback.UpdatedAt, updated.UpdatedAt)
		}

		pending, err := repo.ListGovernanceFeedbackEvents(ctx, integrations.GovernanceFeedbackFilter{Status: integrations.FeedbackStatusPending, Limit: 10})
		if err != nil {
			t.Fatalf("ListGovernanceFeedbackEvents(pending): %v", err)
		}
		if len(pending) != 0 {
			t.Fatalf("expected no pending events, got %#v", pending)
		}
		processed, err := repo.ListGovernanceFeedbackEvents(ctx, integrations.GovernanceFeedbackFilter{Status: integrations.FeedbackStatusProcessed, Limit: 10})
		if err != nil {
			t.Fatalf("ListGovernanceFeedbackEvents(processed): %v", err)
		}
		if len(processed) != 1 || processed[0].ID != feedback.ID {
			t.Fatalf("expected 1 processed event, got %#v", processed)
		}

		if _, err := repo.UpdateGovernanceFeedbackEventStatus(ctx, "missing", integrations.FeedbackStatusIgnored, ""); err == nil {
			t.Fatalf("expected error updating unknown feedback event")
		}
	})
}

func TestIntegrationRepository_ListGovernanceFeedbackEventsFiltersByChangeRequestAndArtifact(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		ctx := context.Background()
		repo := NewIntegrationRepository(gdb)

		integration, err := repo.CreateIntegration(ctx, integrations.Integration{
			ID:       "int_feedback_filter",
			Provider: integrations.ProviderGitHub,
			Name:     "Feedback Filter",
			Status:   integrations.StatusConnected,
		})
		if err != nil {
			t.Fatalf("CreateIntegration: %v", err)
		}

		first, err := repo.CreateGovernanceFeedbackEvent(ctx, integrations.GovernanceFeedbackEvent{
			IntegrationID:   integration.ID,
			FeatureID:       "feature-1",
			ChangeRequestID: "cr-1",
			ArtifactID:      "art-1",
			EventType:       integrations.FeedbackEventCodingAgentBlockedAmbiguity,
			PayloadJSON:     `{"summary":"clarify refunds"}`,
			Status:          integrations.FeedbackStatusPending,
			Reason:          "Clarify refunds.",
		})
		if err != nil {
			t.Fatalf("CreateGovernanceFeedbackEvent(first): %v", err)
		}
		second, err := repo.CreateGovernanceFeedbackEvent(ctx, integrations.GovernanceFeedbackEvent{
			IntegrationID:   integration.ID,
			FeatureID:       "feature-1",
			ChangeRequestID: "cr-2",
			ArtifactID:      "art-2",
			EventType:       integrations.FeedbackEventCodingAgentCompleted,
			PayloadJSON:     `{"summary":"done"}`,
			Status:          integrations.FeedbackStatusPending,
			Reason:          "Completed.",
		})
		if err != nil {
			t.Fatalf("CreateGovernanceFeedbackEvent(second): %v", err)
		}

		byCR, err := repo.ListGovernanceFeedbackEvents(ctx, integrations.GovernanceFeedbackFilter{
			ChangeRequestID: "cr-1",
			Limit:           10,
		})
		if err != nil {
			t.Fatalf("ListGovernanceFeedbackEvents(change_request): %v", err)
		}
		if len(byCR) != 1 || byCR[0].ID != first.ID {
			t.Fatalf("unexpected change request filtered rows: %#v", byCR)
		}

		byArtifact, err := repo.ListGovernanceFeedbackEvents(ctx, integrations.GovernanceFeedbackFilter{
			ArtifactID: "art-2",
			Limit:      10,
		})
		if err != nil {
			t.Fatalf("ListGovernanceFeedbackEvents(artifact): %v", err)
		}
		if len(byArtifact) != 1 || byArtifact[0].ID != second.ID {
			t.Fatalf("unexpected artifact filtered rows: %#v", byArtifact)
		}
	})
}

func TestIntegrationRepository_DeleteIntegrationCascadesEverything(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		ctx := context.Background()
		repo := NewIntegrationRepository(gdb)

		integration, err := repo.CreateIntegration(ctx, integrations.Integration{
			ID:       "int_delete",
			Provider: integrations.ProviderGitLab,
			Name:     "To Delete",
			Status:   integrations.StatusConnected,
		})
		if err != nil {
			t.Fatalf("CreateIntegration: %v", err)
		}
		resource, err := repo.CreateResource(ctx, integrations.Resource{
			IntegrationID: integration.ID,
			ResourceType:  integrations.ResourceTypeProject,
			ExternalID:    "555",
			ExternalKey:   "ns/cascade-test",
		})
		if err != nil {
			t.Fatalf("CreateResource: %v", err)
		}
		_, _, err = repo.RecordWebhookEvent(ctx, integrations.WebhookEvent{
			IntegrationID:   integration.ID,
			ResourceID:      resource.ID,
			Provider:        integrations.ProviderGitLab,
			EventType:       integrations.WebhookEventMergeRequest,
			ExternalEventID: "evt-cascade",
			Status:          integrations.WebhookStatusProcessed,
		})
		if err != nil {
			t.Fatalf("RecordWebhookEvent: %v", err)
		}

		if err := repo.DeleteIntegration(ctx, integration.ID); err != nil {
			t.Fatalf("DeleteIntegration: %v", err)
		}

		// Top-level row gone.
		if _, err := repo.GetIntegration(ctx, integration.ID); err == nil {
			t.Fatal("expected GetIntegration to return ErrNotFound after delete")
		}
		// Cascade should have nuked the resource and the webhook event row.
		var resourceCount int64
		if err := gdb.Model(&integrations.Resource{}).Where("integration_id = ?", integration.ID).Count(&resourceCount).Error; err != nil {
			t.Fatal(err)
		}
		if resourceCount != 0 {
			t.Fatalf("expected resources cascaded; remaining=%d", resourceCount)
		}
		var eventCount int64
		if err := gdb.Model(&integrations.WebhookEvent{}).Where("integration_id = ?", integration.ID).Count(&eventCount).Error; err != nil {
			t.Fatal(err)
		}
		if eventCount != 0 {
			t.Fatalf("expected webhook events cascaded; remaining=%d", eventCount)
		}

		// Idempotent: second delete returns ErrNotFound.
		if err := repo.DeleteIntegration(ctx, integration.ID); err != integrations.ErrNotFound {
			t.Fatalf("second delete should ErrNotFound, got %v", err)
		}
	})
}
