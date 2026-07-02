package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/specgate/doc-registry/internal/integrations"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	_ integrations.IntegrationCRUDStore = (*IntegrationRepository)(nil)
	_ integrations.ResourceStore        = (*IntegrationRepository)(nil)
	_ integrations.OAuthStore           = (*IntegrationRepository)(nil)
	_ integrations.WebhookEventStore    = (*IntegrationRepository)(nil)
	_ integrations.TrackerLinkStore     = (*IntegrationRepository)(nil)
	_ integrations.FeedbackEventStore   = (*IntegrationRepository)(nil)
	_ integrations.Store                = (*IntegrationRepository)(nil)
)

// IntegrationRepository persists native workflow integrations and webhook inbox rows.
type IntegrationRepository struct {
	db *gorm.DB
}

func NewIntegrationRepository(db *gorm.DB) *IntegrationRepository {
	return &IntegrationRepository{db: db}
}

// WithTx runs fn against a tx-scoped repository so the caller can compose
// multi-write flows (record + upsert + feedback + status update) atomically.
// All writes inside fn share one DB transaction; returning an error rolls back.
func (r *IntegrationRepository) WithTx(ctx context.Context, fn func(integrations.Store) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(&IntegrationRepository{db: tx})
	})
}

// isUniqueViolation maps the cross-driver "unique constraint failed" signal so
// callers can return 409 Conflict instead of opaque 500s.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint") ||
		strings.Contains(msg, "duplicate key") ||
		strings.Contains(msg, "unique violation")
}

func (r *IntegrationRepository) ListIntegrations(ctx context.Context) ([]integrations.Integration, error) {
	var rows []integrations.Integration
	if err := r.db.WithContext(ctx).Order("provider ASC, name ASC, created_at ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list integrations: %w", err)
	}
	for i := range rows {
		rows[i].HasAPIToken = rows[i].APITokenEncrypted != ""
		rows[i].HasOAuthToken = rows[i].OAuthAccessTokenEncrypted != ""
		rows[i].HasWebhookSecret = rows[i].WebhookSecretEncrypted != ""
	}
	return rows, nil
}

func (r *IntegrationRepository) GetIntegration(ctx context.Context, id string) (*integrations.Integration, error) {
	var row integrations.Integration
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, integrations.ErrNotFound
		}
		return nil, fmt.Errorf("get integration %s: %w", id, err)
	}
	row.HasAPIToken = row.APITokenEncrypted != ""
	row.HasOAuthToken = row.OAuthAccessTokenEncrypted != ""
	row.HasWebhookSecret = row.WebhookSecretEncrypted != ""
	return &row, nil
}

func (r *IntegrationRepository) CreateIntegration(ctx context.Context, in integrations.Integration) (*integrations.Integration, error) {
	now := time.Now().UTC()
	if in.ID == "" {
		in.ID = uuid.NewString()
	}
	if in.CreatedAt.IsZero() {
		in.CreatedAt = now
	}
	in.UpdatedAt = now
	if err := r.db.WithContext(ctx).Create(&in).Error; err != nil {
		if isUniqueViolation(err) {
			return nil, integrations.ErrConflict
		}
		return nil, fmt.Errorf("create integration: %w", err)
	}
	return &in, nil
}

func (r *IntegrationRepository) UpdateIntegration(ctx context.Context, in integrations.Integration) (*integrations.Integration, error) {
	in.UpdatedAt = time.Now().UTC()
	// Sparse update: only persist fields the caller populated. webhook_secret
	// is intentionally NOT updated through this path — callers must use
	// UpdateWebhookSecret so rotations stay auditable and a forgotten body
	// field cannot wipe the stored secret.
	updates := map[string]any{"updated_at": in.UpdatedAt}
	if in.Provider != "" {
		updates["provider"] = in.Provider
	}
	if in.Name != "" {
		updates["name"] = in.Name
	}
	if in.Status != "" {
		updates["status"] = in.Status
	}
	if in.BaseURL != "" {
		updates["base_url"] = in.BaseURL
	}
	if in.ConfigJSON != "" {
		updates["config_json"] = in.ConfigJSON
	}
	if in.LastHealthCheckAt != nil {
		updates["last_health_check_at"] = in.LastHealthCheckAt
	}
	if in.LastError != "" || (in.Status == integrations.StatusConnected && in.LastError == "") {
		updates["last_error"] = in.LastError
	}
	res := r.db.WithContext(ctx).Model(&integrations.Integration{}).Where("id = ?", in.ID).Updates(updates)
	if res.Error != nil {
		if isUniqueViolation(res.Error) {
			return nil, integrations.ErrConflict
		}
		return nil, fmt.Errorf("update integration %s: %w", in.ID, res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, integrations.ErrNotFound
	}
	return r.GetIntegration(ctx, in.ID)
}

// DeleteIntegration removes one integration row. Foreign keys with ON DELETE
// CASCADE in the migrations (credentials, resources, webhook events, delivery
// links, governance feedback events) ensure the dependent rows go with it.
// Returns ErrNotFound when the id is unknown so the HTTP layer can map to 404.
func (r *IntegrationRepository) DeleteIntegration(ctx context.Context, id string) error {
	res := r.db.WithContext(ctx).Where("id = ?", id).Delete(&integrations.Integration{})
	if res.Error != nil {
		return fmt.Errorf("delete integration %s: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return integrations.ErrNotFound
	}
	return nil
}

// UpdateApiTokenEncrypted stores the recoverable (AES-256-GCM) ciphertext of a
// provider API token (Linear). The service layer computes the ciphertext; an
// empty value clears the token (e.g. when no secret key is configured).
func (r *IntegrationRepository) UpdateApiTokenEncrypted(ctx context.Context, id string, encrypted string) error {
	now := time.Now().UTC()
	res := r.db.WithContext(ctx).Model(&integrations.Integration{}).Where("id = ?", id).Updates(map[string]any{
		"api_token_encrypted":           encrypted,
		"auth_method":                   integrations.AuthMethodPAT,
		"oauth_access_token_encrypted":  "",
		"oauth_refresh_token_encrypted": "",
		"oauth_expires_at":              nil,
		"oauth_token_type":              "",
		"oauth_scope":                   "",
		"oauth_account_id":              "",
		"oauth_account_name":            "",
		"oauth_account_email":           "",
		"oauth_host_key":                "",
		"updated_at":                    now,
	})
	if res.Error != nil {
		return fmt.Errorf("update api token encrypted %s: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return integrations.ErrNotFound
	}
	return nil
}

// UpdateWebhookSecretEncrypted stores the recoverable (AES-256-GCM) ciphertext
// of a per-integration inbound-webhook secret (GitLab/GitHub). The service layer
// computes the ciphertext; this is the only write path for the column so a
// rotation cannot be clobbered by a sparse UpdateIntegration.
func (r *IntegrationRepository) UpdateWebhookSecretEncrypted(ctx context.Context, id string, encrypted string) error {
	res := r.db.WithContext(ctx).Model(&integrations.Integration{}).Where("id = ?", id).Updates(map[string]any{
		"webhook_secret_encrypted": encrypted,
		"updated_at":               time.Now().UTC(),
	})
	if res.Error != nil {
		return fmt.Errorf("update webhook secret encrypted %s: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return integrations.ErrNotFound
	}
	return nil
}

func (r *IntegrationRepository) UpdateOAuthGrant(ctx context.Context, in integrations.Integration) error {
	now := time.Now().UTC()
	res := r.db.WithContext(ctx).Model(&integrations.Integration{}).Where("id = ?", in.ID).Updates(map[string]any{
		"auth_method":                   in.AuthMethod,
		"api_token_encrypted":           "",
		"oauth_access_token_encrypted":  in.OAuthAccessTokenEncrypted,
		"oauth_refresh_token_encrypted": in.OAuthRefreshTokenEncrypted,
		"oauth_expires_at":              in.OAuthExpiresAt,
		"oauth_token_type":              in.OAuthTokenType,
		"oauth_scope":                   in.OAuthScope,
		"oauth_account_id":              in.OAuthAccountID,
		"oauth_account_name":            in.OAuthAccountName,
		"oauth_account_email":           in.OAuthAccountEmail,
		"oauth_host_key":                in.OAuthHostKey,
		"updated_at":                    now,
	})
	if res.Error != nil {
		return fmt.Errorf("update oauth grant %s: %w", in.ID, res.Error)
	}
	if res.RowsAffected == 0 {
		return integrations.ErrNotFound
	}
	return nil
}

func (r *IntegrationRepository) ClearOAuthGrant(ctx context.Context, id string) error {
	now := time.Now().UTC()
	res := r.db.WithContext(ctx).Model(&integrations.Integration{}).Where("id = ?", id).Updates(map[string]any{
		"auth_method":                   "",
		"oauth_access_token_encrypted":  "",
		"oauth_refresh_token_encrypted": "",
		"oauth_expires_at":              nil,
		"oauth_token_type":              "",
		"oauth_scope":                   "",
		"oauth_account_id":              "",
		"oauth_account_name":            "",
		"oauth_account_email":           "",
		"oauth_host_key":                "",
		// Disconnecting removes the only credential, so the integration is no
		// longer active — drop it out of "connected" (the status CHECK allows
		// connected/disabled/error; a fresh OAuth connect resets it to connected).
		"status":     integrations.StatusDisabled,
		"last_error": "",
		"updated_at": now,
	})
	if res.Error != nil {
		return fmt.Errorf("clear oauth grant %s: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return integrations.ErrNotFound
	}
	return nil
}

func (r *IntegrationRepository) CreateOAuthState(ctx context.Context, in integrations.OAuthState) (*integrations.OAuthState, error) {
	now := time.Now().UTC()
	if in.ID == "" {
		in.ID = uuid.NewString()
	}
	if in.CreatedAt.IsZero() {
		in.CreatedAt = now
	}
	in.UpdatedAt = now
	if err := r.db.WithContext(ctx).Create(&in).Error; err != nil {
		if isUniqueViolation(err) {
			return nil, integrations.ErrConflict
		}
		return nil, fmt.Errorf("create oauth state: %w", err)
	}
	return &in, nil
}

func (r *IntegrationRepository) GetOAuthState(ctx context.Context, state string) (*integrations.OAuthState, error) {
	var row integrations.OAuthState
	if err := r.db.WithContext(ctx).Where("state = ?", state).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, integrations.ErrNotFound
		}
		return nil, fmt.Errorf("get oauth state %s: %w", state, err)
	}
	return &row, nil
}

func (r *IntegrationRepository) ConsumeOAuthState(ctx context.Context, state string) (*integrations.OAuthState, error) {
	now := time.Now().UTC()
	res := r.db.WithContext(ctx).Model(&integrations.OAuthState{}).
		Where("state = ? AND consumed_at IS NULL", state).
		Updates(map[string]any{"consumed_at": &now, "updated_at": now})
	if res.Error != nil {
		return nil, fmt.Errorf("consume oauth state %s: %w", state, res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, integrations.ErrNotFound
	}
	return r.GetOAuthState(ctx, state)
}

func (r *IntegrationRepository) ListResources(ctx context.Context, integrationID string) ([]integrations.Resource, error) {
	var rows []integrations.Resource
	if err := r.db.WithContext(ctx).
		Where("integration_id = ?", integrationID).
		Order("resource_type ASC, external_key ASC, created_at ASC").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list resources %s: %w", integrationID, err)
	}
	for i := range rows {
		rows[i].HasWebhookSecret = rows[i].WebhookSecretEncrypted != ""
	}
	return rows, nil
}

func (r *IntegrationRepository) GetResource(ctx context.Context, integrationID string, resourceID string) (*integrations.Resource, error) {
	var row integrations.Resource
	if err := r.db.WithContext(ctx).
		Where("integration_id = ? AND id = ?", integrationID, resourceID).
		First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, integrations.ErrNotFound
		}
		return nil, fmt.Errorf("get resource %s: %w", resourceID, err)
	}
	row.HasWebhookSecret = row.WebhookSecretEncrypted != ""
	return &row, nil
}

func (r *IntegrationRepository) CreateResource(ctx context.Context, in integrations.Resource) (*integrations.Resource, error) {
	now := time.Now().UTC()
	if in.ID == "" {
		in.ID = uuid.NewString()
	}
	if in.CreatedAt.IsZero() {
		in.CreatedAt = now
	}
	in.UpdatedAt = now
	if err := r.db.WithContext(ctx).Create(&in).Error; err != nil {
		if isUniqueViolation(err) {
			return nil, integrations.ErrConflict
		}
		return nil, fmt.Errorf("create resource: %w", err)
	}
	in.HasWebhookSecret = in.WebhookSecretEncrypted != ""
	return &in, nil
}

func (r *IntegrationRepository) UpdateResourceWebhookSecretEncrypted(ctx context.Context, integrationID string, resourceID string, encrypted string) error {
	res := r.db.WithContext(ctx).Model(&integrations.Resource{}).
		Where("integration_id = ? AND id = ?", integrationID, resourceID).
		Updates(map[string]any{
			"webhook_secret_encrypted": encrypted,
			"updated_at":               time.Now().UTC(),
		})
	if res.Error != nil {
		return fmt.Errorf("update resource webhook secret encrypted %s: %w", resourceID, res.Error)
	}
	if res.RowsAffected == 0 {
		return integrations.ErrNotFound
	}
	return nil
}

func (r *IntegrationRepository) UpdateResourceConfigJSON(ctx context.Context, integrationID string, resourceID string, configJSON string) error {
	res := r.db.WithContext(ctx).Model(&integrations.Resource{}).
		Where("integration_id = ? AND id = ?", integrationID, resourceID).
		Updates(map[string]any{
			"config_json": configJSON,
			"updated_at":  time.Now().UTC(),
		})
	if res.Error != nil {
		return fmt.Errorf("update resource config json %s: %w", resourceID, res.Error)
	}
	if res.RowsAffected == 0 {
		return integrations.ErrNotFound
	}
	return nil
}

func (r *IntegrationRepository) DeleteResource(ctx context.Context, integrationID string, resourceID string) error {
	res := r.db.WithContext(ctx).
		Where("integration_id = ? AND id = ?", integrationID, resourceID).
		Delete(&integrations.Resource{})
	if res.Error != nil {
		return fmt.Errorf("delete resource %s: %w", resourceID, res.Error)
	}
	if res.RowsAffected == 0 {
		return integrations.ErrNotFound
	}
	return nil
}

func (r *IntegrationRepository) FindResourceByProvider(ctx context.Context, provider string, resourceType string, externalID string, externalKey string) (*integrations.Integration, *integrations.Resource, error) {
	base := func() *gorm.DB {
		return r.db.WithContext(ctx).
			Table("integration_resources AS r").
			Select("r.*").
			Joins("JOIN integrations AS i ON i.id = r.integration_id").
			Where("i.provider = ? AND r.resource_type = ?", provider, resourceType)
	}
	loadIntegration := func(resource integrations.Resource) (*integrations.Integration, *integrations.Resource, error) {
		integration, getErr := r.GetIntegration(ctx, resource.IntegrationID)
		return integration, &resource, getErr
	}
	if externalID != "" {
		var resource integrations.Resource
		err := base().Where("r.external_id = ?", externalID).Order("r.created_at ASC").First(&resource).Error
		if err == nil {
			return loadIntegration(resource)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, fmt.Errorf("find resource by provider external id %s: %w", externalID, err)
		}
	}
	if externalKey != "" {
		var resource integrations.Resource
		err := base().Where("r.external_key = ?", externalKey).Order("r.created_at ASC").First(&resource).Error
		if err == nil {
			return loadIntegration(resource)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, fmt.Errorf("find resource by provider external key %s: %w", externalKey, err)
		}
	}
	return nil, nil, integrations.ErrNotFound
}

func (r *IntegrationRepository) findWebhookEventByExternalID(ctx context.Context, integrationID string, externalEventID string) (*integrations.WebhookEvent, error) {
	var event integrations.WebhookEvent
	if err := r.db.WithContext(ctx).
		Where("integration_id = ? AND external_event_id = ?", integrationID, externalEventID).
		First(&event).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, integrations.ErrNotFound
		}
		return nil, fmt.Errorf("find webhook event by external id %s: %w", externalEventID, err)
	}
	return &event, nil
}

// RecordWebhookEvent performs an INSERT and returns whether the row is fresh.
// When ExternalEventID is set the partial unique index
// `uq_integration_webhook_events_external` enforces dedup at the DB level.
// On conflict we re-read the existing row and return created=false so the
// caller can short-circuit any side effects — this is the only TOCTOU-safe
// shape; the prior SELECT-then-INSERT pattern raced under concurrent
// deliveries from GitLab's redrive system.
func (r *IntegrationRepository) RecordWebhookEvent(ctx context.Context, in integrations.WebhookEvent) (bool, *integrations.WebhookEvent, error) {
	now := time.Now().UTC()
	if in.ID == "" {
		in.ID = uuid.NewString()
	}
	// Single source of truth for the body hash: every recorded signal — webhook
	// pipeline or public inbox — flows through here, so compute it once.
	if in.PayloadHash == "" {
		sum := sha256.Sum256([]byte(in.PayloadJSON))
		in.PayloadHash = hex.EncodeToString(sum[:])
	}
	if in.ReceivedAt.IsZero() {
		in.ReceivedAt = now
	}
	if in.CreatedAt.IsZero() {
		in.CreatedAt = now
	}
	in.UpdatedAt = now
	// ON CONFLICT DO NOTHING rather than letting the unique-index violation
	// raise: on Postgres a raised 23505 poisons the surrounding transaction
	// (this method runs inside WithTx in the webhook pipelines), so the
	// dedup re-read below would then fail with "current transaction is
	// aborted". DoNothing returns no error and RowsAffected=0 on a duplicate,
	// keeping the tx usable on both drivers. Empty external_event_id has no
	// unique index, so it always inserts (RowsAffected=1, created=true).
	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&in)
	if res.Error != nil {
		return false, nil, fmt.Errorf("record webhook event: %w", res.Error)
	}
	if res.RowsAffected == 1 {
		copy := in
		return true, &copy, nil
	}
	// RowsAffected == 0 → external_event_id already present; re-read the
	// canonical row so the caller has both id and status for the dedup branch.
	existing, findErr := r.findWebhookEventByExternalID(ctx, in.IntegrationID, in.ExternalEventID)
	if findErr != nil {
		return false, nil, findErr
	}
	return false, existing, nil
}

func (r *IntegrationRepository) UpdateWebhookEventStatus(ctx context.Context, id string, status string, errorMessage string) (*integrations.WebhookEvent, error) {
	now := time.Now().UTC()
	processedAt := &now
	res := r.db.WithContext(ctx).Model(&integrations.WebhookEvent{}).Where("id = ?", id).Updates(map[string]any{
		"status":       status,
		"error":        errorMessage,
		"processed_at": processedAt,
		"updated_at":   now,
	})
	if res.Error != nil {
		return nil, fmt.Errorf("update webhook event status %s: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, integrations.ErrNotFound
	}
	var event integrations.WebhookEvent
	if err := r.db.WithContext(ctx).First(&event, "id = ?", id).Error; err != nil {
		return nil, fmt.Errorf("get webhook event %s: %w", id, err)
	}
	return &event, nil
}

func (r *IntegrationRepository) ListWebhookEvents(ctx context.Context, filter integrations.WebhookEventFilter) ([]integrations.WebhookEvent, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	q := r.db.WithContext(ctx).Model(&integrations.WebhookEvent{})
	if filter.IntegrationID != "" {
		q = q.Where("integration_id = ?", filter.IntegrationID)
	}
	if filter.ResourceID != "" {
		q = q.Where("resource_id = ?", filter.ResourceID)
	}
	if filter.Status != "" {
		q = q.Where("status = ?", filter.Status)
	}
	var rows []integrations.WebhookEvent
	if err := q.Order("received_at DESC, created_at DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list webhook events: %w", err)
	}
	return rows, nil
}

func (r *IntegrationRepository) UpsertDeliveryLink(ctx context.Context, in integrations.DeliveryLink) (*integrations.DeliveryLink, error) {
	now := time.Now().UTC()
	if in.ID == "" {
		in.ID = uuid.NewString()
	}
	if in.CreatedAt.IsZero() {
		in.CreatedAt = now
	}
	in.UpdatedAt = now
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "resource_id"},
			{Name: "external_type"},
			{Name: "external_iid"},
		},
		DoUpdates: clause.AssignmentColumns([]string{
			"integration_id",
			"feature_id",
			"change_request_id",
			"external_id",
			"external_key",
			"url",
			"title",
			"state",
			"source_branch",
			"target_branch",
			"merge_commit_sha",
			"last_event_id",
			"updated_at",
		}),
	}).Create(&in).Error; err != nil {
		return nil, fmt.Errorf("upsert delivery link: %w", err)
	}
	var out integrations.DeliveryLink
	if err := r.db.WithContext(ctx).
		Where("resource_id = ? AND external_type = ? AND external_iid = ?", in.ResourceID, in.ExternalType, in.ExternalIID).
		First(&out).Error; err != nil {
		return nil, fmt.Errorf("get delivery link after upsert: %w", err)
	}
	return &out, nil
}

// UpsertTrackerLink persists a handoff's issue↔work-item link, keyed by
// (integration_id, external_key) so FE/BE lanes (distinct identifiers) stay
// distinct rows and a re-emit of the same issue updates in place.
func (r *IntegrationRepository) UpsertTrackerLink(ctx context.Context, in integrations.TrackerLink) (*integrations.TrackerLink, error) {
	now := time.Now().UTC()
	if in.ID == "" {
		in.ID = uuid.NewString()
	}
	if in.CreatedAt.IsZero() {
		in.CreatedAt = now
	}
	in.UpdatedAt = now
	if in.State == "" {
		in.State = integrations.TrackerStateOpened
	}
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "integration_id"}, {Name: "external_key"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"feature_id", "change_request_id", "lane", "external_id", "url", "title", "state", "tracker_state", "updated_at",
		}),
	}).Create(&in).Error; err != nil {
		return nil, fmt.Errorf("upsert tracker link: %w", err)
	}
	var out integrations.TrackerLink
	if err := r.db.WithContext(ctx).
		Where("integration_id = ? AND external_key = ?", in.IntegrationID, in.ExternalKey).
		First(&out).Error; err != nil {
		return nil, fmt.Errorf("get tracker link after upsert: %w", err)
	}
	return &out, nil
}

// TrackerLinkByExternal resolves a handoff-created issue link by its immutable
// external id or human key (newest wins). Returns (nil, nil) on no match so the
// caller can fall back to the `fixes SPECGATE-{key}` footer.
func (r *IntegrationRepository) TrackerLinkByExternal(ctx context.Context, integrationID, externalID, externalKey string) (*integrations.TrackerLink, error) {
	externalID = strings.TrimSpace(externalID)
	externalKey = strings.TrimSpace(externalKey)
	if integrationID == "" || (externalID == "" && externalKey == "") {
		return nil, nil
	}
	q := r.db.WithContext(ctx).Where("integration_id = ?", integrationID)
	switch {
	case externalID != "" && externalKey != "":
		q = q.Where("external_id = ? OR external_key = ?", externalID, externalKey)
	case externalID != "":
		q = q.Where("external_id = ?", externalID)
	default:
		q = q.Where("external_key = ?", externalKey)
	}
	var out integrations.TrackerLink
	if err := q.Order("updated_at DESC").First(&out).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("tracker link by external: %w", err)
	}
	return &out, nil
}

// ListTrackerLinksByChangeRequest returns a work item's tracker issue links
// across all lanes, newest first.
func (r *IntegrationRepository) ListTrackerLinksByChangeRequest(ctx context.Context, changeRequestID string) ([]integrations.TrackerLink, error) {
	changeRequestID = strings.TrimSpace(changeRequestID)
	if changeRequestID == "" {
		return nil, nil
	}
	var out []integrations.TrackerLink
	if err := r.db.WithContext(ctx).
		Where("change_request_id = ?", changeRequestID).
		Order("updated_at DESC").
		Find(&out).Error; err != nil {
		return nil, fmt.Errorf("list tracker links by change request %s: %w", changeRequestID, err)
	}
	return out, nil
}

func (r *IntegrationRepository) CreateGovernanceFeedbackEvent(ctx context.Context, in integrations.GovernanceFeedbackEvent) (*integrations.GovernanceFeedbackEvent, error) {
	now := time.Now().UTC()
	in.IntegrationID = strings.TrimSpace(in.IntegrationID)
	in.ResourceID = strings.TrimSpace(in.ResourceID)
	in.WebhookEventID = strings.TrimSpace(in.WebhookEventID)
	in.DeliveryLinkID = strings.TrimSpace(in.DeliveryLinkID)
	in.FeatureID = strings.TrimSpace(in.FeatureID)
	in.ChangeRequestID = strings.TrimSpace(in.ChangeRequestID)
	in.ArtifactID = strings.TrimSpace(in.ArtifactID)
	in.EventType = strings.TrimSpace(in.EventType)
	in.PayloadJSON = strings.TrimSpace(in.PayloadJSON)
	in.Reason = strings.TrimSpace(in.Reason)
	in.Status = strings.TrimSpace(in.Status)
	if in.Status == "" {
		in.Status = integrations.FeedbackStatusPending
	}
	if in.ID == "" {
		in.ID = uuid.NewString()
	}
	if in.CreatedAt.IsZero() {
		in.CreatedAt = now
	}
	in.UpdatedAt = now
	if err := r.db.WithContext(ctx).Create(&in).Error; err != nil {
		return nil, fmt.Errorf("create governance feedback event: %w", err)
	}
	return &in, nil
}

func (r *IntegrationRepository) ListGovernanceFeedbackEvents(ctx context.Context, filter integrations.GovernanceFeedbackFilter) ([]integrations.GovernanceFeedbackEvent, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	q := r.db.WithContext(ctx).Model(&integrations.GovernanceFeedbackEvent{})
	if filter.Status != "" {
		q = q.Where("status = ?", filter.Status)
	}
	if filter.ChangeRequestID != "" {
		q = q.Where("change_request_id = ?", strings.TrimSpace(filter.ChangeRequestID))
	}
	if filter.ArtifactID != "" {
		q = q.Where("artifact_id = ?", strings.TrimSpace(filter.ArtifactID))
	}
	var rows []integrations.GovernanceFeedbackEvent
	if err := q.Order("created_at DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list governance feedback events: %w", err)
	}
	return rows, nil
}

func (r *IntegrationRepository) UpdateGovernanceFeedbackEventStatus(
	ctx context.Context,
	id string,
	status string,
	reason string,
) (*integrations.GovernanceFeedbackEvent, error) {
	now := time.Now().UTC()
	res := r.db.WithContext(ctx).
		Model(&integrations.GovernanceFeedbackEvent{}).
		Where("id = ?", id).
		Updates(map[string]any{"status": status, "reason": reason, "updated_at": now})
	if res.Error != nil {
		return nil, fmt.Errorf("update governance feedback event status %s: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	var row integrations.GovernanceFeedbackEvent
	if err := r.db.WithContext(ctx).First(&row, "id = ?", id).Error; err != nil {
		return nil, fmt.Errorf("get governance feedback event %s: %w", id, err)
	}
	return &row, nil
}
