package integrations

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/specgate/doc-registry/internal/integrations/coretypes"
)

type webhookRollbackStore struct {
	Store
	integration        *Integration
	resource           *Resource
	resources          []Resource
	secretErr          error
	configErr          error
	deleted            []string
	integrationDeleted bool
}

func (s *webhookRollbackStore) GetIntegration(context.Context, string) (*Integration, error) {
	return s.integration, nil
}

func (s *webhookRollbackStore) CreateResource(_ context.Context, in Resource) (*Resource, error) {
	in.ID = "resource-1"
	s.resource = &in
	return &in, nil
}

func (s *webhookRollbackStore) GetResource(context.Context, string, string) (*Resource, error) {
	return s.resource, nil
}

func (s *webhookRollbackStore) UpdateResourceWebhookSecretEncrypted(context.Context, string, string, string) error {
	return s.secretErr
}

func (s *webhookRollbackStore) UpdateResourceConfigJSON(context.Context, string, string, string) error {
	return s.configErr
}

func (s *webhookRollbackStore) DeleteResource(_ context.Context, integrationID, resourceID string) error {
	s.deleted = append(s.deleted, integrationID+"/"+resourceID)
	return nil
}

func (s *webhookRollbackStore) ListResources(context.Context, string) ([]Resource, error) {
	return s.resources, nil
}

func (s *webhookRollbackStore) DeleteIntegration(context.Context, string) error {
	s.integrationDeleted = true
	return nil
}

type webhookRollbackDriver struct {
	provisionErr error
	deleteErr    error
	onProvision  func()
	onDelete     func()
	deleted      []struct {
		hookID string
		target ProviderTarget
	}
}

func (*webhookRollbackDriver) VerifyDelivery(string, InboundWebhook) error { return nil }
func (*webhookRollbackDriver) Normalize(InboundWebhook) (*coretypes.NormalizedDelivery, *coretypes.NormalizedComment, string, error) {
	return nil, nil, "", nil
}
func (*webhookRollbackDriver) SupportsManagedWebhook() bool { return true }
func (d *webhookRollbackDriver) ProvisionWebhook(context.Context, ProvisionInput) (ProvisionResult, error) {
	if d.onProvision != nil {
		d.onProvision()
	}
	if d.provisionErr != nil {
		return ProvisionResult{}, d.provisionErr
	}
	return ProvisionResult{ProviderHookID: "hook-1", Secret: "webhook-secret"}, nil
}
func (d *webhookRollbackDriver) DeleteWebhook(_ context.Context, hookID string, target ProviderTarget) error {
	if d.onDelete != nil {
		d.onDelete()
	}
	d.deleted = append(d.deleted, struct {
		hookID string
		target ProviderTarget
	}{hookID: hookID, target: target})
	return d.deleteErr
}

func TestCreateResourceAndProvisionWebhook_CleansUpRemoteHookAfterPostProvisionFailure(t *testing.T) {
	for _, tc := range []struct {
		name        string
		configure   func(*webhookRollbackStore, *webhookRollbackDriver)
		wantCleanup bool
	}{
		{
			name: "secret encryption",
			configure: func(_ *webhookRollbackStore, d *webhookRollbackDriver) {
				d.onProvision = func() { t.Setenv(SecretKeyEnvVar, "") }
			},
			wantCleanup: true,
		},
		{
			name: "secret persistence",
			configure: func(s *webhookRollbackStore, _ *webhookRollbackDriver) {
				s.secretErr = errors.New("secret write failed")
			},
			wantCleanup: true,
		},
		{
			name: "config persistence",
			configure: func(s *webhookRollbackStore, _ *webhookRollbackDriver) {
				s.configErr = errors.New("config write failed")
			},
			wantCleanup: true,
		},
		{
			name: "provision failure",
			configure: func(_ *webhookRollbackStore, d *webhookRollbackDriver) {
				d.provisionErr = errors.New("provider rejected webhook")
			},
		},
		{
			name: "pre provision failure",
			configure: func(s *webhookRollbackStore, _ *webhookRollbackDriver) {
				s.integration.APITokenEncrypted = "not a ciphertext"
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(SecretKeyEnvVar, "0000000000000000000000000000000000000000000000000000000000000001")
			store := &webhookRollbackStore{integration: &Integration{
				ID: "integration-1", Provider: ProviderGitHub, Status: StatusConnected, HasAPIToken: true,
				APITokenEncrypted: encryptedSecretForTest(t, "provider-token"), BaseURL: "https://github.example",
			}}
			driver := &webhookRollbackDriver{}
			tc.configure(store, driver)
			previous, ok := coretypes.LookupWebhookDriver(ProviderGitHub)
			if !ok {
				t.Fatal("missing GitHub webhook driver")
			}
			coretypes.RegisterWebhookDriver(ProviderGitHub, driver)
			t.Cleanup(func() { coretypes.RegisterWebhookDriver(ProviderGitHub, previous) })

			_, err := NewService(store).CreateResourceAndProvisionWebhook(context.Background(), "integration-1", Resource{
				ResourceType: ResourceTypeRepo, ExternalID: "repo-id", ExternalKey: "owner/repo",
			}, "https://registry.example")
			if err == nil {
				t.Fatal("want create failure")
			}
			if got := store.deleted; len(got) != 1 || got[0] != "integration-1/resource-1" {
				t.Fatalf("local deletes = %#v, want resource rollback", got)
			}
			if len(driver.deleted) != 0 && !tc.wantCleanup {
				t.Fatalf("remote deletes = %#v, want none before/at provision failure", driver.deleted)
			}
			if len(driver.deleted) != 1 && tc.wantCleanup {
				t.Fatalf("remote deletes = %#v, want one", driver.deleted)
			}
			if tc.wantCleanup {
				got := driver.deleted[0]
				if got.hookID != "hook-1" || got.target != (ProviderTarget{BaseURL: "https://github.example", Token: "provider-token", ExternalID: "repo-id", ExternalKey: "owner/repo"}) {
					t.Fatalf("remote delete = %#v, want exact hook and target", got)
				}
			}
		})
	}
}

func TestWebhookableResourceTypeRequiresProviderContractPair(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		provider     string
		resourceType string
		want         bool
	}{
		{provider: ProviderGitHub, resourceType: ResourceTypeRepo, want: true},
		{provider: ProviderGitHub, resourceType: ResourceTypeProject, want: false},
		{provider: ProviderGitHub, resourceType: ResourceTypeTeam, want: false},
		{provider: ProviderGitLab, resourceType: ResourceTypeProject, want: true},
		{provider: ProviderGitLab, resourceType: ResourceTypeRepo, want: false},
		{provider: ProviderGitLab, resourceType: ResourceTypeTeam, want: false},
		{provider: ProviderLinear, resourceType: ResourceTypeTeam, want: true},
		{provider: ProviderLinear, resourceType: ResourceTypeRepo, want: false},
		{provider: ProviderLinear, resourceType: ResourceTypeProject, want: false},
		{provider: "unknown", resourceType: ResourceTypeRepo, want: false},
	} {
		if got := webhookableResourceType(tc.provider, tc.resourceType); got != tc.want {
			t.Errorf("webhookableResourceType(%q, %q) = %t, want %t", tc.provider, tc.resourceType, got, tc.want)
		}
	}
}

func TestCreateResourceAndProvisionWebhookRejectsMismatchedResourceType(t *testing.T) {
	t.Parallel()
	store := &webhookRollbackStore{integration: &Integration{
		ID: "integration-1", Provider: ProviderGitHub, Status: StatusConnected,
	}}

	_, err := NewService(store).CreateResourceAndProvisionWebhook(context.Background(), "integration-1", Resource{
		ResourceType: ResourceTypeProject, ExternalID: "repo-id", ExternalKey: "owner/repo",
	}, "https://registry.example")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want ErrValidation", err)
	}
	if store.resource != nil {
		t.Fatalf("resource = %#v, want no persisted mismatched resource", store.resource)
	}
}

func TestDeleteIntegrationDeletesManagedWebhooksBeforeLocalData(t *testing.T) {
	t.Setenv(SecretKeyEnvVar, "0000000000000000000000000000000000000000000000000000000000000001")
	store := &webhookRollbackStore{
		integration: &Integration{
			ID: "integration-1", Provider: ProviderGitHub, Status: StatusConnected, HasAPIToken: true,
			APITokenEncrypted: encryptedSecretForTest(t, "provider-token"), BaseURL: "https://github.example",
		},
		resources: []Resource{
			{ID: "resource-1", IntegrationID: "integration-1", ResourceType: ResourceTypeRepo, ExternalID: "1", ExternalKey: "owner/one", ConfigJSON: `{"provider_webhook_id":"hook-1"}`},
			{ID: "resource-2", IntegrationID: "integration-1", ResourceType: ResourceTypeRepo, ExternalID: "2", ExternalKey: "owner/two", ConfigJSON: `{"provider_webhook_id":"hook-2"}`},
		},
	}
	driver := &webhookRollbackDriver{onDelete: func() {
		if store.integrationDeleted {
			t.Error("local integration was deleted before managed provider hooks")
		}
	}}
	previous, ok := coretypes.LookupWebhookDriver(ProviderGitHub)
	if !ok {
		t.Fatal("missing GitHub webhook driver")
	}
	coretypes.RegisterWebhookDriver(ProviderGitHub, driver)
	t.Cleanup(func() { coretypes.RegisterWebhookDriver(ProviderGitHub, previous) })

	if err := NewService(store).Delete(context.Background(), "integration-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(driver.deleted) != 2 || driver.deleted[0].hookID != "hook-1" || driver.deleted[1].hookID != "hook-2" {
		t.Fatalf("remote deletes = %#v, want both managed hooks", driver.deleted)
	}
	if !store.integrationDeleted {
		t.Fatal("local integration was not deleted after managed hooks")
	}
}

func TestDeleteIntegrationKeepsLocalDataWhenManagedWebhookDeleteFails(t *testing.T) {
	t.Setenv(SecretKeyEnvVar, "0000000000000000000000000000000000000000000000000000000000000001")
	store := &webhookRollbackStore{
		integration: &Integration{
			ID: "integration-1", Provider: ProviderGitHub, Status: StatusConnected, HasAPIToken: true,
			APITokenEncrypted: encryptedSecretForTest(t, "provider-token"), BaseURL: "https://github.example",
		},
		resources: []Resource{{
			ID: "resource-1", IntegrationID: "integration-1", ResourceType: ResourceTypeRepo,
			ExternalID: "1", ExternalKey: "owner/one", ConfigJSON: `{"provider_webhook_id":"hook-1"}`,
		}},
	}
	driver := &webhookRollbackDriver{deleteErr: errors.New("provider unavailable")}
	previous, ok := coretypes.LookupWebhookDriver(ProviderGitHub)
	if !ok {
		t.Fatal("missing GitHub webhook driver")
	}
	coretypes.RegisterWebhookDriver(ProviderGitHub, driver)
	t.Cleanup(func() { coretypes.RegisterWebhookDriver(ProviderGitHub, previous) })

	err := NewService(store).Delete(context.Background(), "integration-1")
	if !errors.Is(err, ErrUpstream) {
		t.Fatalf("Delete error = %v, want ErrUpstream", err)
	}
	if store.integrationDeleted {
		t.Fatal("local integration deleted despite provider cleanup failure")
	}
}

func TestReprovisionResourceWebhook_CleansUpRemoteHookAfterPostProvisionFailure(t *testing.T) {
	for _, tc := range []struct {
		name      string
		configure func(*webhookRollbackStore, *webhookRollbackDriver)
	}{
		{
			name: "secret encryption",
			configure: func(_ *webhookRollbackStore, d *webhookRollbackDriver) {
				d.onProvision = func() { t.Setenv(SecretKeyEnvVar, "") }
			},
		},
		{
			name: "secret persistence",
			configure: func(s *webhookRollbackStore, _ *webhookRollbackDriver) {
				s.secretErr = errors.New("secret write failed")
			},
		},
		{
			name: "config persistence",
			configure: func(s *webhookRollbackStore, _ *webhookRollbackDriver) {
				s.configErr = errors.New("config write failed")
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(SecretKeyEnvVar, "0000000000000000000000000000000000000000000000000000000000000001")
			store := &webhookRollbackStore{
				integration: &Integration{
					ID: "integration-1", Provider: ProviderGitHub, Status: StatusConnected, HasAPIToken: true,
					APITokenEncrypted: encryptedSecretForTest(t, "provider-token"), BaseURL: "https://github.example",
				},
				resource: &Resource{
					ID: "resource-1", IntegrationID: "integration-1", ResourceType: ResourceTypeRepo,
					ExternalID: "repo-id", ExternalKey: "owner/repo",
				},
			}
			driver := &webhookRollbackDriver{}
			tc.configure(store, driver)
			previous, ok := coretypes.LookupWebhookDriver(ProviderGitHub)
			if !ok {
				t.Fatal("missing GitHub webhook driver")
			}
			coretypes.RegisterWebhookDriver(ProviderGitHub, driver)
			t.Cleanup(func() { coretypes.RegisterWebhookDriver(ProviderGitHub, previous) })

			_, err := NewService(store).ReprovisionResourceWebhook(
				context.Background(), "integration-1", "resource-1", "https://registry.example",
			)
			if err == nil {
				t.Fatal("want reprovision failure")
			}
			if len(driver.deleted) != 1 {
				t.Fatalf("remote deletes = %#v, want one", driver.deleted)
			}
			got := driver.deleted[0]
			if got.hookID != "hook-1" || got.target != (ProviderTarget{BaseURL: "https://github.example", Token: "provider-token", ExternalID: "repo-id", ExternalKey: "owner/repo"}) {
				t.Fatalf("remote delete = %#v, want exact new hook and target", got)
			}
			if len(store.deleted) != 0 {
				t.Fatalf("local deletes = %#v, want existing resource preserved", store.deleted)
			}
		})
	}
}

func TestExistingProviderHookID(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		`{"provider_webhook_id":"42"}`:                             "42",
		`{"webhook_status":"connected","provider_webhook_id":"7"}`: "7",
		`{}`:                          "",
		``:                            "",
		`not json`:                    "",
		`{"provider_webhook_id":123}`: "", // non-string is ignored
	}
	for cfg, want := range cases {
		if got := existingProviderHookID(cfg); got != want {
			t.Fatalf("existingProviderHookID(%q) = %q, want %q", cfg, got, want)
		}
	}
}

func TestMergeResourceWebhookConfig_ClearsErrorOnConnected(t *testing.T) {
	t.Parallel()
	// A failed reprovision records the error.
	withErr := mergeResourceWebhookConfig(`{}`, managedWebhookConfig{
		Status: "error", LastError: "Url is blocked: localhost",
	})
	if !strings.Contains(withErr, "Url is blocked") || !strings.Contains(withErr, `"webhook_status":"error"`) {
		t.Fatalf("expected error recorded, got %s", withErr)
	}

	// A successful (re)provision clears the prior error and records the hook.
	connected := mergeResourceWebhookConfig(withErr, managedWebhookConfig{
		URL: "https://pub.example/webhook", ProviderHookID: "9", Status: "connected",
	})
	if strings.Contains(connected, "webhook_last_error") || strings.Contains(connected, "Url is blocked") {
		t.Fatalf("connected config still carries a stale error: %s", connected)
	}
	if !strings.Contains(connected, `"webhook_status":"connected"`) ||
		!strings.Contains(connected, `"provider_webhook_id":"9"`) ||
		!strings.Contains(connected, `"webhook_url":"https://pub.example/webhook"`) {
		t.Fatalf("connected config missing fields: %s", connected)
	}
}
