package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/specgate/doc-registry/internal/integrations"
	linearprovider "github.com/specgate/doc-registry/internal/integrations/linear"
	"github.com/specgate/doc-registry/internal/workboard"
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

func TestIntegrationRepository_LinearHandoffLockSerializesConcurrentCallbacks(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewIntegrationRepository(gdb)
		ctx := context.Background()
		start := make(chan struct{})
		var active, maxActive atomic.Int32
		var wg sync.WaitGroup
		errs := make(chan error, 2)
		for range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				errs <- repo.WithChangeRequestHandoffLock(ctx, "cr-concurrent", func(integrations.TrackerLinkStore) error {
					current := active.Add(1)
					for {
						seen := maxActive.Load()
						if current <= seen || maxActive.CompareAndSwap(seen, current) {
							break
						}
					}
					time.Sleep(75 * time.Millisecond)
					active.Add(-1)
					return nil
				})
			}()
		}
		close(start)
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		if got := maxActive.Load(); got != 1 {
			t.Fatalf("concurrent lock callbacks = %d, want 1", got)
		}
	})
}

// TestHandoffLinear_ConcurrentPostgresCallsCreateOneIssueAndLink proves the
// production Service uses the repository's advisory-lock transaction for the
// whole lookup/create/persist sequence, not just a unit-test lock seam.
func TestHandoffLinear_ConcurrentPostgresCallsCreateOneIssueAndLink(t *testing.T) {
	t.Setenv(integrations.SecretKeyEnvVar, "0000000000000000000000000000000000000000000000000000000000000001")
	oldGraphQLURL := linearprovider.GraphQLURL
	t.Cleanup(func() { linearprovider.GraphQLURL = oldGraphQLURL })
	var creates atomic.Int32
	linear := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if strings.Contains(request.Query, "IssueCreate") {
			creates.Add(1)
			_, _ = w.Write([]byte(`{"data":{"issueCreate":{"success":true,"issue":{"id":"linear-issue","identifier":"ENG-1","url":"https://linear.app/acme/issue/ENG-1","team":{"id":"team-concurrent"}}}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"issue":null}}`))
	}))
	t.Cleanup(linear.Close)
	linearprovider.GraphQLURL = linear.URL

	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		workspaceID := "ws-concurrent"
		ctx := integrations.WithWorkspace(context.Background(), workspaceID)
		workCtx := workboard.WithWorkspace(context.Background(), workspaceID)
		workRepo := NewWorkBoardRepository(gdb)
		cr, err := workRepo.CreateChangeRequest(workCtx, workboard.ChangeRequest{
			ID: "cr-concurrent-handoff", Key: "SG-CONCURRENT", WorkType: workboard.WorkTypeBugFix,
			Title: "Concurrent handoff", IntentMD: "Create exactly one issue.",
		})
		if err != nil {
			t.Fatal(err)
		}
		repo := NewIntegrationRepository(gdb)
		token, err := integrations.EncryptSecret("linear-token")
		if err != nil {
			t.Fatal(err)
		}
		integration, err := repo.CreateIntegration(ctx, integrations.Integration{
			ID: "int-concurrent", Provider: integrations.ProviderLinear, Name: "Linear", Status: integrations.StatusConnected, APITokenEncrypted: token,
		})
		if err != nil {
			t.Fatal(err)
		}
		resource, err := repo.CreateResource(ctx, integrations.Resource{
			ID: "team-concurrent", IntegrationID: integration.ID, ResourceType: integrations.ResourceTypeTeam, ExternalID: "team-concurrent",
		})
		if err != nil {
			t.Fatal(err)
		}

		svc := integrations.NewServiceWithWorkBoard(repo, workRepo)
		start := make(chan struct{})
		results := make(chan *integrations.LinearHandoffResult, 2)
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		for range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				result, err := svc.HandoffLinear(ctx, integrations.LinearHandoffInput{ChangeRequestID: cr.ID, IntegrationID: integration.ID, ResourceID: resource.ID})
				if err != nil {
					errs <- err
					return
				}
				results <- result
			}()
		}
		close(start)
		wg.Wait()
		close(results)
		close(errs)
		for err := range errs {
			t.Fatal(err)
		}
		var created, repeated int
		for result := range results {
			if result.Link.ResourceID != resource.ID || result.Link.ExternalID != "linear-issue" {
				t.Fatalf("handoff result = %#v", result)
			}
			if result.Created {
				created++
			} else {
				repeated++
			}
		}
		if created != 1 || repeated != 1 || creates.Load() != 1 {
			t.Fatalf("created=%d repeated=%d Linear creates=%d, want 1/1/1", created, repeated, creates.Load())
		}
		links, err := repo.ListTrackerLinksByChangeRequest(ctx, cr.ID)
		if err != nil || len(links) != 1 || links[0].ResourceID != resource.ID {
			t.Fatalf("persisted links=%#v err=%v", links, err)
		}
	})
}

// payload_hash must be the hex SHA-256 of the raw body itself — not the
// `sha256:`-prefixed external-event-id fallback, and not derived from the
// delivery id. Two deliveries with different ids but identical bodies must hash
// the same.
func TestIntegrationRepository_RecordWebhookEventHashesBody(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		ctx := integrations.WithWorkspace(context.Background(), "ws-test")
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

func TestIntegrationRepository_ClaimsFailedWebhookEventOnce(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		ctx := integrations.WithWorkspace(context.Background(), "ws-test")
		repo := NewIntegrationRepository(gdb)
		integration, err := repo.CreateIntegration(ctx, integrations.Integration{
			ID: "int-retry", Provider: integrations.ProviderGitHub, Name: "GitHub", Status: integrations.StatusConnected,
		})
		if err != nil {
			t.Fatalf("CreateIntegration: %v", err)
		}
		_, event, err := repo.RecordWebhookEvent(ctx, integrations.WebhookEvent{
			IntegrationID: integration.ID, Provider: integrations.ProviderGitHub,
			EventType: integrations.WebhookEventMergeRequest, ExternalEventID: "delivery-retry",
			PayloadJSON: `{}`, Status: integrations.WebhookStatusFailed, Error: "temporary failure",
		})
		if err != nil {
			t.Fatalf("RecordWebhookEvent: %v", err)
		}

		claimed, pending, err := repo.ClaimFailedWebhookEvent(ctx, event.ID)
		if err != nil {
			t.Fatalf("ClaimFailedWebhookEvent: %v", err)
		}
		if !claimed || pending.Status != integrations.WebhookStatusPending || pending.Error != "" || pending.ProcessedAt != nil {
			t.Fatalf("first claim = claimed:%t event:%#v, want clean pending event", claimed, pending)
		}

		claimed, existing, err := repo.ClaimFailedWebhookEvent(ctx, event.ID)
		if err != nil {
			t.Fatalf("second ClaimFailedWebhookEvent: %v", err)
		}
		if claimed || existing.Status != integrations.WebhookStatusPending {
			t.Fatalf("second claim = claimed:%t event:%#v, want unclaimed pending event", claimed, existing)
		}

		otherWorkspace := integrations.WithWorkspace(context.Background(), "ws-other")
		if _, _, err := repo.ClaimFailedWebhookEvent(otherWorkspace, event.ID); !errors.Is(err, integrations.ErrNotFound) {
			t.Fatalf("cross-workspace claim error = %v, want ErrNotFound", err)
		}
	})
}

func TestIntegrationRepository_WorkspaceScopesIntegrationRoot(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewIntegrationRepository(gdb)
		wsA := integrations.WithWorkspace(context.Background(), "ws-a")
		wsB := integrations.WithWorkspace(context.Background(), "ws-b")

		for _, tc := range []struct {
			ctx context.Context
			id  string
		}{
			{wsA, "int-ws-a"},
			{wsB, "int-ws-b"},
		} {
			if _, err := repo.CreateIntegration(tc.ctx, integrations.Integration{
				ID: tc.id, Provider: integrations.ProviderGitHub, Name: "Shared GitHub", Status: integrations.StatusConnected,
			}); err != nil {
				t.Fatalf("CreateIntegration(%s): %v", tc.id, err)
			}
		}

		items, err := repo.ListIntegrations(wsA)
		if err != nil {
			t.Fatalf("ListIntegrations(ws-a): %v", err)
		}
		if len(items) != 1 || items[0].ID != "int-ws-a" || items[0].WorkspaceID != "ws-a" {
			t.Fatalf("workspace A list = %#v", items)
		}
		if _, err := repo.GetIntegration(wsA, "int-ws-b"); !errors.Is(err, integrations.ErrNotFound) {
			t.Fatalf("cross-workspace GetIntegration error = %v, want ErrNotFound", err)
		}
		if err := repo.DeleteIntegration(wsA, "int-ws-b"); !errors.Is(err, integrations.ErrNotFound) {
			t.Fatalf("cross-workspace DeleteIntegration error = %v, want ErrNotFound", err)
		}
		if _, err := repo.CreateIntegration(wsA, integrations.Integration{
			ID: "int-ws-a-duplicate", Provider: integrations.ProviderGitHub, Name: "Shared GitHub", Status: integrations.StatusConnected,
		}); !errors.Is(err, integrations.ErrConflict) {
			t.Fatalf("same-workspace duplicate error = %v, want ErrConflict", err)
		}

		resourceA, err := repo.CreateResource(wsA, integrations.Resource{
			ID: "resource-ws-a", IntegrationID: "int-ws-a", ResourceType: integrations.ResourceTypeRepo, ExternalKey: "acme/a",
		})
		if err != nil {
			t.Fatalf("CreateResource(ws-a): %v", err)
		}
		if _, err := repo.CreateResource(wsA, integrations.Resource{ID: "resource-ws-b", IntegrationID: "int-ws-b", ResourceType: integrations.ResourceTypeRepo, ExternalKey: "acme/b"}); !errors.Is(err, integrations.ErrNotFound) {
			t.Fatalf("cross-workspace CreateResource error = %v, want ErrNotFound", err)
		}
		resources, err := repo.ListResources(wsA, "int-ws-b")
		if err != nil {
			t.Fatalf("ListResources(ws-a, ws-b): %v", err)
		}
		if len(resources) != 0 {
			t.Fatalf("cross-workspace resources = %#v, want empty", resources)
		}
		if err := repo.UpdateResourceConfigJSON(wsA, "int-ws-b", resourceA.ID, `{"bad":true}`); !errors.Is(err, integrations.ErrNotFound) {
			t.Fatalf("cross-workspace UpdateResourceConfigJSON error = %v, want ErrNotFound", err)
		}
		if err := repo.UpdateApiTokenEncrypted(wsA, "int-ws-b", "ciphertext"); !errors.Is(err, integrations.ErrNotFound) {
			t.Fatalf("cross-workspace UpdateApiTokenEncrypted error = %v, want ErrNotFound", err)
		}
	})
}

func TestIntegrationRepository_WorkspaceScopesIntegrationChildren(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewIntegrationRepository(gdb)
		wsA := integrations.WithWorkspace(context.Background(), "ws-a")
		wsB := integrations.WithWorkspace(context.Background(), "ws-b")

		intA, err := repo.CreateIntegration(wsA, integrations.Integration{ID: "int-child-a", Provider: integrations.ProviderGitHub, Name: "A", Status: integrations.StatusConnected})
		if err != nil {
			t.Fatalf("CreateIntegration A: %v", err)
		}
		intB, err := repo.CreateIntegration(wsB, integrations.Integration{ID: "int-child-b", Provider: integrations.ProviderGitHub, Name: "B", Status: integrations.StatusConnected})
		if err != nil {
			t.Fatalf("CreateIntegration B: %v", err)
		}
		resA, err := repo.CreateResource(wsA, integrations.Resource{IntegrationID: intA.ID, ResourceType: integrations.ResourceTypeRepo, ExternalKey: "repo-a"})
		if err != nil {
			t.Fatalf("CreateResource A: %v", err)
		}
		resB, err := repo.CreateResource(wsB, integrations.Resource{IntegrationID: intB.ID, ResourceType: integrations.ResourceTypeRepo, ExternalKey: "repo-b"})
		if err != nil {
			t.Fatalf("CreateResource B: %v", err)
		}

		if got, err := repo.ListResources(wsA, intB.ID); err != nil || len(got) != 0 {
			t.Fatalf("cross-workspace ListResources = %#v (err %v)", got, err)
		}
		if _, err := repo.GetResource(wsA, intB.ID, resB.ID); !errors.Is(err, integrations.ErrNotFound) {
			t.Fatalf("cross-workspace GetResource error = %v", err)
		}
		if _, _, err := repo.FindResourceByProvider(wsA, integrations.ProviderGitHub, integrations.ResourceTypeRepo, "", "repo-b"); !errors.Is(err, integrations.ErrNotFound) {
			t.Fatalf("cross-workspace FindResourceByProvider error = %v", err)
		}
		if _, err := repo.CreateResource(wsA, integrations.Resource{IntegrationID: intB.ID, ResourceType: integrations.ResourceTypeRepo, ExternalKey: "repo-cross"}); !errors.Is(err, integrations.ErrNotFound) {
			t.Fatalf("cross-workspace CreateResource error = %v", err)
		}
		if err := repo.UpdateResourceConfigJSON(wsA, intB.ID, resB.ID, `{"blocked":true}`); !errors.Is(err, integrations.ErrNotFound) {
			t.Fatalf("cross-workspace UpdateResource error = %v", err)
		}
		if err := repo.DeleteResource(wsA, intB.ID, resB.ID); !errors.Is(err, integrations.ErrNotFound) {
			t.Fatalf("cross-workspace DeleteResource error = %v", err)
		}

		_, eventA, err := repo.RecordWebhookEvent(wsA, integrations.WebhookEvent{IntegrationID: intA.ID, ResourceID: resA.ID, Provider: integrations.ProviderGitHub, EventType: integrations.WebhookEventMergeRequest, ExternalEventID: "evt-a", PayloadJSON: `{}`, Status: integrations.WebhookStatusPending})
		if err != nil {
			t.Fatalf("RecordWebhookEvent A: %v", err)
		}
		_, eventB, err := repo.RecordWebhookEvent(wsB, integrations.WebhookEvent{IntegrationID: intB.ID, ResourceID: resB.ID, Provider: integrations.ProviderGitHub, EventType: integrations.WebhookEventMergeRequest, ExternalEventID: "evt-b", PayloadJSON: `{}`, Status: integrations.WebhookStatusPending})
		if err != nil {
			t.Fatalf("RecordWebhookEvent B: %v", err)
		}
		if _, _, err := repo.RecordWebhookEvent(wsA, integrations.WebhookEvent{IntegrationID: intB.ID, ResourceID: resB.ID, Provider: integrations.ProviderGitHub, EventType: integrations.WebhookEventMergeRequest, ExternalEventID: "evt-cross", PayloadJSON: `{}`, Status: integrations.WebhookStatusPending}); !errors.Is(err, integrations.ErrNotFound) {
			t.Fatalf("cross-workspace RecordWebhookEvent error = %v", err)
		}
		if _, _, err := repo.RecordWebhookEvent(wsA, integrations.WebhookEvent{IntegrationID: intA.ID, ResourceID: resB.ID, Provider: integrations.ProviderGitHub, EventType: integrations.WebhookEventMergeRequest, ExternalEventID: "evt-cross-resource", PayloadJSON: `{}`, Status: integrations.WebhookStatusPending}); !errors.Is(err, integrations.ErrNotFound) {
			t.Fatalf("cross-parent RecordWebhookEvent error = %v", err)
		}
		if got, err := repo.ListWebhookEvents(wsA, integrations.WebhookEventFilter{IntegrationID: intB.ID}); err != nil || len(got) != 0 {
			t.Fatalf("cross-workspace ListWebhookEvents = %#v (err %v)", got, err)
		}
		if _, err := repo.UpdateWebhookEventStatus(wsA, eventB.ID, integrations.WebhookStatusProcessed, ""); !errors.Is(err, integrations.ErrNotFound) {
			t.Fatalf("cross-workspace UpdateWebhookEventStatus error = %v", err)
		}
		if _, err := repo.UpdateWebhookEventStatus(wsA, eventA.ID, integrations.WebhookStatusProcessed, ""); err != nil {
			t.Fatalf("same-workspace UpdateWebhookEventStatus: %v", err)
		}

		stateA, err := repo.CreateOAuthState(wsA, integrations.OAuthState{State: "state-a", IntegrationID: intA.ID, Provider: integrations.ProviderGitHub, HostKey: "github.github_com", ExpiresAt: time.Now().UTC().Add(time.Hour)})
		if err != nil {
			t.Fatalf("CreateOAuthState A: %v", err)
		}
		_, err = repo.CreateOAuthState(wsB, integrations.OAuthState{State: "state-b", IntegrationID: intB.ID, Provider: integrations.ProviderGitHub, HostKey: "github.github_com", ExpiresAt: time.Now().UTC().Add(time.Hour)})
		if err != nil {
			t.Fatalf("CreateOAuthState B: %v", err)
		}
		if _, err := repo.GetOAuthState(wsA, "state-b"); !errors.Is(err, integrations.ErrNotFound) {
			t.Fatalf("cross-workspace GetOAuthState error = %v", err)
		}
		if _, err := repo.ConsumeOAuthState(wsA, "state-b"); !errors.Is(err, integrations.ErrNotFound) {
			t.Fatalf("cross-workspace ConsumeOAuthState error = %v", err)
		}
		if _, err := repo.GetOAuthState(wsA, stateA.State); err != nil {
			t.Fatalf("same-workspace GetOAuthState: %v", err)
		}

		if _, err := repo.UpsertTrackerLink(wsA, integrations.TrackerLink{IntegrationID: intA.ID, ResourceID: resA.ID, ChangeRequestID: "cr-shared", ExternalKey: "A-1"}); err != nil {
			t.Fatalf("UpsertTrackerLink A: %v", err)
		}
		if _, err := repo.UpsertTrackerLink(wsB, integrations.TrackerLink{IntegrationID: intB.ID, ResourceID: resB.ID, ChangeRequestID: "cr-shared-b", ExternalKey: "B-1"}); err != nil {
			t.Fatalf("UpsertTrackerLink B: %v", err)
		}
		if got, err := repo.TrackerLinkByExternal(wsA, intB.ID, "", "B-1"); err != nil || got != nil {
			t.Fatalf("cross-workspace TrackerLinkByExternal = %#v (err %v)", got, err)
		}
		if got, err := repo.ListTrackerLinksByChangeRequest(wsA, "cr-shared"); err != nil || len(got) != 1 || got[0].IntegrationID != intA.ID {
			t.Fatalf("workspace TrackerLink list = %#v (err %v)", got, err)
		}

		if _, err := repo.UpsertDeliveryLink(wsA, integrations.DeliveryLink{IntegrationID: intA.ID, ResourceID: resB.ID, ExternalType: integrations.ExternalTypeMergeRequest, ExternalIID: "cross"}); !errors.Is(err, integrations.ErrNotFound) {
			t.Fatalf("cross-workspace UpsertDeliveryLink error = %v", err)
		}
		if _, err := repo.CreateGovernanceFeedbackEvent(wsA, integrations.GovernanceFeedbackEvent{IntegrationID: intA.ID, ResourceID: resB.ID, EventType: integrations.FeedbackEventPRMerged, Status: integrations.FeedbackStatusReceived}); !errors.Is(err, integrations.ErrNotFound) {
			t.Fatalf("cross-parent feedback create error = %v", err)
		}

		if _, err := repo.CreateGovernanceFeedbackEvent(wsA, integrations.GovernanceFeedbackEvent{IntegrationID: intA.ID, EventType: integrations.FeedbackEventPRMerged, Status: integrations.FeedbackStatusReceived}); err != nil {
			t.Fatalf("CreateGovernanceFeedbackEvent A: %v", err)
		}
		feedbackB, err := repo.CreateGovernanceFeedbackEvent(wsB, integrations.GovernanceFeedbackEvent{IntegrationID: intB.ID, EventType: integrations.FeedbackEventPRMerged, Status: integrations.FeedbackStatusReceived})
		if err != nil {
			t.Fatalf("CreateGovernanceFeedbackEvent B: %v", err)
		}
		if got, err := repo.ListGovernanceFeedbackEvents(wsA, integrations.GovernanceFeedbackFilter{Limit: 10}); err != nil || len(got) != 1 || got[0].IntegrationID != intA.ID {
			t.Fatalf("workspace feedback list = %#v (err %v)", got, err)
		}
		if _, err := repo.UpdateGovernanceFeedbackEventStatus(wsA, feedbackB.ID, integrations.FeedbackStatusAccepted, ""); !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("cross-workspace feedback update error = %v", err)
		}
	})
}

func TestIntegrationRepository_ScopesAgentFeedbackWithoutIntegration(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewIntegrationRepository(gdb)
		workspaceA := integrations.WithWorkspace(context.Background(), "ws-agent-a")
		workspaceB := integrations.WithWorkspace(context.Background(), "ws-agent-b")

		created, err := repo.CreateGovernanceFeedbackEvent(workspaceA, integrations.GovernanceFeedbackEvent{
			ChangeRequestID: "cr-agent-a",
			EventType:       integrations.FeedbackEventCodingAgentCompleted,
			Status:          integrations.FeedbackStatusReceived,
		})
		if err != nil {
			t.Fatalf("CreateGovernanceFeedbackEvent agent row: %v", err)
		}

		inA, err := repo.ListGovernanceFeedbackEvents(workspaceA, integrations.GovernanceFeedbackFilter{Limit: 10})
		if err != nil {
			t.Fatalf("ListGovernanceFeedbackEvents workspace A: %v", err)
		}
		if len(inA) != 1 || inA[0].ID != created.ID {
			t.Fatalf("workspace A feedback = %#v, want agent event %q", inA, created.ID)
		}

		inB, err := repo.ListGovernanceFeedbackEvents(workspaceB, integrations.GovernanceFeedbackFilter{Limit: 10})
		if err != nil {
			t.Fatalf("ListGovernanceFeedbackEvents workspace B: %v", err)
		}
		if len(inB) != 0 {
			t.Fatalf("workspace B feedback = %#v, want none", inB)
		}
		if _, err := repo.UpdateGovernanceFeedbackEventStatus(workspaceB, created.ID, integrations.FeedbackStatusAccepted, "wrong workspace"); !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("cross-workspace feedback update error = %v", err)
		}
	})
}

func TestIntegrationRepository_RoundTripResourcesAndWebhookEvents(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		ctx := integrations.WithWorkspace(context.Background(), "ws-test")
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

// A Linear API token round-trips through the encrypted column and surfaces as
// the derived has_api_token presence flag without ever exposing plaintext.
func TestIntegrationService_SetApiToken_EncryptsAndFlags(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		t.Setenv(integrations.SecretKeyEnvVar, "0000000000000000000000000000000000000000000000000000000000000001")
		ctx := integrations.WithWorkspace(context.Background(), "ws-test")
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
		ctx := integrations.WithWorkspace(context.Background(), "ws-test")
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
		ctx := integrations.WithWorkspace(context.Background(), "ws-test")
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
		ctx := integrations.WithWorkspace(context.Background(), "ws-test")
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
		ctx := integrations.WithWorkspace(context.Background(), "ws-test")
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
