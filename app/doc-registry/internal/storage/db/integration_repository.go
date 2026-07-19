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
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/specgate/doc-registry/internal/governanceprofile"
	"github.com/specgate/doc-registry/internal/integrations"
	"github.com/specgate/doc-registry/internal/workboard"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	_ integrations.IntegrationCRUDStore       = (*IntegrationRepository)(nil)
	_ integrations.ResourceStore              = (*IntegrationRepository)(nil)
	_ integrations.OAuthStore                 = (*IntegrationRepository)(nil)
	_ integrations.WebhookEventStore          = (*IntegrationRepository)(nil)
	_ integrations.TrackerLinkStore           = (*IntegrationRepository)(nil)
	_ integrations.FeedbackEventStore         = (*IntegrationRepository)(nil)
	_ integrations.Store                      = (*IntegrationRepository)(nil)
	_ integrations.ChangeRequestHandoffLocker = (*IntegrationRepository)(nil)
)

// IntegrationRepository persists native workflow integrations and webhook inbox rows.
type IntegrationRepository struct {
	db *gorm.DB
}

func NewIntegrationRepository(db *gorm.DB) *IntegrationRepository {
	return &IntegrationRepository{db: db}
}

// scopeIntegrationRows applies the trusted workspace boundary to rows that
// carry an integration_id. Empty workspace context intentionally leaves the
// query unscoped for provider callbacks and other internal workers.
func scopeIntegrationRows(db *gorm.DB, ctx context.Context, column string) *gorm.DB {
	workspaceID := integrations.WorkspaceID(ctx)
	if workspaceID == "" {
		return db
	}
	return db.Where("EXISTS (SELECT 1 FROM integrations AS scope_integration WHERE scope_integration.id = "+column+" AND scope_integration.workspace_id = ?)", workspaceID)
}

// WithTx runs fn against a tx-scoped repository so the caller can compose
// multi-write flows (record + upsert + feedback + status update) atomically.
// All writes inside fn share one DB transaction; returning an error rolls back.
func (r *IntegrationRepository) WithTx(ctx context.Context, fn func(integrations.Store) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(&IntegrationRepository{db: tx})
	})
}

// WithChangeRequestHandoffLock holds a transaction-scoped PostgreSQL advisory
// lock while the callback resolves/reuses a deterministic Linear issue and
// persists its one primary tracker link. The lock disappears on commit/rollback.
func (r *IntegrationRepository) WithChangeRequestHandoffLock(ctx context.Context, changeRequestID string, fn func(integrations.TrackerLinkStore) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended('linear-handoff:' || ?, 0))", changeRequestID).Error; err != nil {
			return fmt.Errorf("acquire linear handoff lock: %w", err)
		}
		return fn(&IntegrationRepository{db: tx})
	})
}

// isUniqueViolation maps typed database errors so callers can return 409
// Conflict without interpreting driver error text.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.Code == "23505"
}

func (r *IntegrationRepository) ListIntegrations(ctx context.Context) ([]integrations.Integration, error) {
	var rows []integrations.Integration
	db := r.db.WithContext(ctx)
	if workspaceID := integrations.WorkspaceID(ctx); workspaceID != "" {
		db = db.Where("workspace_id = ?", workspaceID)
	}
	if err := db.Order("provider ASC, name ASC, created_at ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list integrations: %w", err)
	}
	for i := range rows {
		rows[i].HasAPIToken = rows[i].APITokenEncrypted != ""
		rows[i].HasOAuthToken = rows[i].OAuthAccessTokenEncrypted != ""
	}
	return rows, nil
}

func (r *IntegrationRepository) GetIntegration(ctx context.Context, id string) (*integrations.Integration, error) {
	var row integrations.Integration
	db := r.db.WithContext(ctx).Where("id = ?", id)
	if workspaceID := integrations.WorkspaceID(ctx); workspaceID != "" {
		db = db.Where("workspace_id = ?", workspaceID)
	}
	if err := db.First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, integrations.ErrNotFound
		}
		return nil, fmt.Errorf("get integration %s: %w", id, err)
	}
	row.HasAPIToken = row.APITokenEncrypted != ""
	row.HasOAuthToken = row.OAuthAccessTokenEncrypted != ""
	return &row, nil
}

func (r *IntegrationRepository) CreateIntegration(ctx context.Context, in integrations.Integration) (*integrations.Integration, error) {
	if workspaceID := integrations.WorkspaceID(ctx); workspaceID != "" {
		if in.WorkspaceID != "" && in.WorkspaceID != workspaceID {
			return nil, fmt.Errorf("%w: workspace_id", integrations.ErrValidation)
		}
		in.WorkspaceID = workspaceID
	}
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
	db := r.db.WithContext(ctx).Model(&integrations.Integration{}).Where("id = ?", in.ID)
	if workspaceID := integrations.WorkspaceID(ctx); workspaceID != "" {
		db = db.Where("workspace_id = ?", workspaceID)
	}
	res := db.Updates(updates)
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
	db := r.db.WithContext(ctx).Where("id = ?", id)
	if workspaceID := integrations.WorkspaceID(ctx); workspaceID != "" {
		db = db.Where("workspace_id = ?", workspaceID)
	}
	res := db.Delete(&integrations.Integration{})
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
	db := r.db.WithContext(ctx).Model(&integrations.Integration{}).Where("id = ?", id)
	if workspaceID := integrations.WorkspaceID(ctx); workspaceID != "" {
		db = db.Where("workspace_id = ?", workspaceID)
	}
	res := db.Updates(map[string]any{
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

func (r *IntegrationRepository) UpdateOAuthGrant(ctx context.Context, in integrations.Integration) error {
	now := time.Now().UTC()
	db := r.db.WithContext(ctx).Model(&integrations.Integration{}).Where("id = ?", in.ID)
	if workspaceID := integrations.WorkspaceID(ctx); workspaceID != "" {
		db = db.Where("workspace_id = ?", workspaceID)
	}
	res := db.Updates(map[string]any{
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
	db := r.db.WithContext(ctx).Model(&integrations.Integration{}).Where("id = ?", id)
	if workspaceID := integrations.WorkspaceID(ctx); workspaceID != "" {
		db = db.Where("workspace_id = ?", workspaceID)
	}
	res := db.Updates(map[string]any{
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
	if workspaceID := integrations.WorkspaceID(ctx); workspaceID != "" {
		if in.WorkspaceID != "" && in.WorkspaceID != workspaceID {
			return nil, fmt.Errorf("%w: workspace_id", integrations.ErrValidation)
		}
		in.WorkspaceID = workspaceID
		if in.IntegrationID != "" {
			if _, err := r.GetIntegration(ctx, in.IntegrationID); err != nil {
				return nil, err
			}
		}
	}
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
	q := r.db.WithContext(ctx).Where("state = ?", state)
	if workspaceID := integrations.WorkspaceID(ctx); workspaceID != "" {
		q = q.Where("workspace_id = ?", workspaceID)
	}
	if err := q.First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, integrations.ErrNotFound
		}
		return nil, fmt.Errorf("get oauth state %s: %w", state, err)
	}
	return &row, nil
}

func (r *IntegrationRepository) ConsumeOAuthState(ctx context.Context, state string) (*integrations.OAuthState, error) {
	now := time.Now().UTC()
	q := r.db.WithContext(ctx).Model(&integrations.OAuthState{}).
		Where("state = ? AND consumed_at IS NULL", state)
	if workspaceID := integrations.WorkspaceID(ctx); workspaceID != "" {
		q = q.Where("workspace_id = ?", workspaceID)
	}
	res := q.
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
	q := r.db.WithContext(ctx).Where("integration_id = ?", integrationID)
	q = scopeIntegrationRows(q, ctx, "integration_resources.integration_id")
	if err := q.
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
	q := r.db.WithContext(ctx).Where("integration_id = ? AND id = ?", integrationID, resourceID)
	q = scopeIntegrationRows(q, ctx, "integration_resources.integration_id")
	if err := q.
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
	if integrations.WorkspaceID(ctx) != "" {
		if _, err := r.GetIntegration(ctx, in.IntegrationID); err != nil {
			return nil, err
		}
	}
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
	db := r.db.WithContext(ctx).Model(&integrations.Resource{}).
		Where("integration_id = ? AND id = ?", integrationID, resourceID)
	db = scopeIntegrationRows(db, ctx, "integration_resources.integration_id")
	res := db.
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
	db := r.db.WithContext(ctx).Model(&integrations.Resource{}).
		Where("integration_id = ? AND id = ?", integrationID, resourceID)
	db = scopeIntegrationRows(db, ctx, "integration_resources.integration_id")
	res := db.
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
	db := r.db.WithContext(ctx).
		Where("integration_id = ? AND id = ?", integrationID, resourceID)
	db = scopeIntegrationRows(db, ctx, "integration_resources.integration_id")
	res := db.
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
		q := r.db.WithContext(ctx).
			Table("integration_resources AS r").
			Select("r.*").
			Joins("JOIN integrations AS i ON i.id = r.integration_id").
			Where("i.provider = ? AND r.resource_type = ?", provider, resourceType)
		if workspaceID := integrations.WorkspaceID(ctx); workspaceID != "" {
			q = q.Where("i.workspace_id = ?", workspaceID)
		}
		return q
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
	q := r.db.WithContext(ctx).Where("integration_id = ? AND external_event_id = ?", integrationID, externalEventID)
	q = scopeIntegrationRows(q, ctx, "integration_webhook_events.integration_id")
	if err := q.
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
	if integrations.WorkspaceID(ctx) != "" {
		if _, err := r.GetIntegration(ctx, in.IntegrationID); err != nil {
			return false, nil, err
		}
		if in.ResourceID != "" {
			if _, err := r.GetResource(ctx, in.IntegrationID, in.ResourceID); err != nil {
				return false, nil, err
			}
		}
	}
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
	// keeping the transaction usable. Empty external_event_id has no
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

// ClaimFailedWebhookEvent atomically returns a failed delivery to pending so a
// queue retry may process it. The status predicate is the concurrency guard:
// only one redelivery can claim a failed row, while processed or already
// pending duplicates remain no-ops.
func (r *IntegrationRepository) ClaimFailedWebhookEvent(ctx context.Context, id string) (bool, *integrations.WebhookEvent, error) {
	now := time.Now().UTC()
	db := r.db.WithContext(ctx).Model(&integrations.WebhookEvent{}).
		Where("id = ? AND status = ?", id, integrations.WebhookStatusFailed)
	if workspaceID := integrations.WorkspaceID(ctx); workspaceID != "" {
		db = db.Where("EXISTS (SELECT 1 FROM integrations AS scope_integration WHERE scope_integration.id = integration_webhook_events.integration_id AND scope_integration.workspace_id = ?)", workspaceID)
	}
	res := db.Updates(map[string]any{
		"status":       integrations.WebhookStatusPending,
		"error":        "",
		"processed_at": nil,
		"updated_at":   now,
	})
	if res.Error != nil {
		return false, nil, fmt.Errorf("claim failed webhook event %s: %w", id, res.Error)
	}

	var event integrations.WebhookEvent
	q := r.db.WithContext(ctx).Where("id = ?", id)
	q = scopeIntegrationRows(q, ctx, "integration_webhook_events.integration_id")
	if err := q.First(&event).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil, integrations.ErrNotFound
		}
		return false, nil, fmt.Errorf("get claimed webhook event %s: %w", id, err)
	}
	return res.RowsAffected == 1, &event, nil
}

func (r *IntegrationRepository) UpdateWebhookEventStatus(ctx context.Context, id string, status string, errorMessage string) (*integrations.WebhookEvent, error) {
	now := time.Now().UTC()
	processedAt := &now
	db := r.db.WithContext(ctx).Model(&integrations.WebhookEvent{}).Where("id = ?", id)
	if workspaceID := integrations.WorkspaceID(ctx); workspaceID != "" {
		db = db.Where("EXISTS (SELECT 1 FROM integrations AS scope_integration WHERE scope_integration.id = integration_webhook_events.integration_id AND scope_integration.workspace_id = ?)", workspaceID)
	}
	res := db.Updates(map[string]any{
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
	q := r.db.WithContext(ctx).Where("id = ?", id)
	q = scopeIntegrationRows(q, ctx, "integration_webhook_events.integration_id")
	if err := q.First(&event).Error; err != nil {
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
	q = scopeIntegrationRows(q, ctx, "integration_webhook_events.integration_id")
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
	if integrations.WorkspaceID(ctx) != "" {
		if _, err := r.GetIntegration(ctx, in.IntegrationID); err != nil {
			return nil, err
		}
		if in.ResourceID != "" {
			if _, err := r.GetResource(ctx, in.IntegrationID, in.ResourceID); err != nil {
				return nil, err
			}
		}
	}
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
			"head_sha",
			"merge_commit_sha",
			"last_event_id",
			"updated_at",
		}),
	}).Create(&in).Error; err != nil {
		return nil, fmt.Errorf("upsert delivery link: %w", err)
	}
	var out integrations.DeliveryLink
	q := r.db.WithContext(ctx)
	q = scopeIntegrationRows(q, ctx, "integration_delivery_links.integration_id")
	if err := q.
		Where("resource_id = ? AND external_type = ? AND external_iid = ?", in.ResourceID, in.ExternalType, in.ExternalIID).
		First(&out).Error; err != nil {
		return nil, fmt.Errorf("get delivery link after upsert: %w", err)
	}
	return &out, nil
}

// ListDeliveryLinksByChangeRequest returns one work item's delivery links,
// constrained by the integration-owned workspace boundary, newest first.
func (r *IntegrationRepository) ListDeliveryLinksByChangeRequest(ctx context.Context, changeRequestID string) ([]integrations.DeliveryLink, error) {
	changeRequestID = strings.TrimSpace(changeRequestID)
	if changeRequestID == "" {
		return nil, nil
	}
	var out []integrations.DeliveryLink
	q := r.db.WithContext(ctx).Where("change_request_id = ?", changeRequestID)
	q = scopeIntegrationRows(q, ctx, "integration_delivery_links.integration_id")
	if err := q.Order("updated_at DESC").Find(&out).Error; err != nil {
		return nil, fmt.Errorf("list delivery links by change request %s: %w", changeRequestID, err)
	}
	return out, nil
}

// UpsertTrackerLink persists a work item's one primary Linear handoff link.
func (r *IntegrationRepository) UpsertTrackerLink(ctx context.Context, in integrations.TrackerLink) (*integrations.TrackerLink, error) {
	if integrations.WorkspaceID(ctx) != "" {
		if _, err := r.GetIntegration(ctx, in.IntegrationID); err != nil {
			return nil, err
		}
	}
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
	if strings.TrimSpace(in.ChangeRequestID) == "" || strings.TrimSpace(in.ResourceID) == "" {
		return nil, fmt.Errorf("%w: change_request_id and resource_id are required", integrations.ErrValidation)
	}
	resource, err := r.GetResource(ctx, in.IntegrationID, in.ResourceID)
	if err != nil {
		return nil, err
	}
	if resource.IntegrationID != in.IntegrationID {
		return nil, fmt.Errorf("%w: tracker resource does not belong to integration", integrations.ErrValidation)
	}
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "change_request_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"integration_id", "resource_id", "feature_id", "external_id", "external_key", "url", "title", "state", "tracker_state", "updated_at",
		}),
		Where: clause.Where{Exprs: []clause.Expression{gorm.Expr("tracker_links.integration_id = EXCLUDED.integration_id AND tracker_links.resource_id = EXCLUDED.resource_id")}},
	}).Create(&in).Error; err != nil {
		return nil, fmt.Errorf("upsert tracker link: %w", err)
	}
	var out integrations.TrackerLink
	q := r.db.WithContext(ctx)
	q = scopeIntegrationRows(q, ctx, "tracker_links.integration_id")
	if err := q.
		Where("change_request_id = ?", in.ChangeRequestID).
		First(&out).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			var existing integrations.TrackerLink
			if lookupErr := r.db.WithContext(ctx).Where("change_request_id = ?", in.ChangeRequestID).First(&existing).Error; lookupErr == nil {
				return nil, integrations.ErrConflict
			}
		}
		return nil, fmt.Errorf("get tracker link after upsert: %w", err)
	}
	if out.IntegrationID != in.IntegrationID || out.ResourceID != in.ResourceID {
		return nil, integrations.ErrConflict
	}
	return &out, nil
}

// TrackerLinkByExternal resolves a handoff-created issue link by its immutable
// external id or human key (newest wins). Returns (nil, nil) on no match so the
// caller can fall back to the exact SpecGate work-reference marker.
func (r *IntegrationRepository) TrackerLinkByExternal(ctx context.Context, integrationID, externalID, externalKey string) (*integrations.TrackerLink, error) {
	externalID = strings.TrimSpace(externalID)
	externalKey = strings.TrimSpace(externalKey)
	if integrationID == "" || (externalID == "" && externalKey == "") {
		return nil, nil
	}
	q := r.db.WithContext(ctx).Where("integration_id = ?", integrationID)
	q = scopeIntegrationRows(q, ctx, "tracker_links.integration_id")
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

// ListTrackerLinksByChangeRequest returns a work item's primary tracker issue.
func (r *IntegrationRepository) ListTrackerLinksByChangeRequest(ctx context.Context, changeRequestID string) ([]integrations.TrackerLink, error) {
	changeRequestID = strings.TrimSpace(changeRequestID)
	if changeRequestID == "" {
		return nil, nil
	}
	var out []integrations.TrackerLink
	q := r.db.WithContext(ctx).Where("change_request_id = ?", changeRequestID)
	q = scopeIntegrationRows(q, ctx, "tracker_links.integration_id")
	if err := q.
		Order("updated_at DESC").
		Find(&out).Error; err != nil {
		return nil, fmt.Errorf("list tracker links by change request %s: %w", changeRequestID, err)
	}
	return out, nil
}

func (r *IntegrationRepository) CreateGovernanceFeedbackEvent(ctx context.Context, in integrations.GovernanceFeedbackEvent) (*integrations.GovernanceFeedbackEvent, error) {
	now := time.Now().UTC()
	in.WorkspaceID = strings.TrimSpace(in.WorkspaceID)
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
	if workspaceID := integrations.WorkspaceID(ctx); workspaceID != "" {
		if in.WorkspaceID != "" && in.WorkspaceID != workspaceID {
			return nil, fmt.Errorf("%w: workspace_id", integrations.ErrValidation)
		}
		in.WorkspaceID = workspaceID
		if in.IntegrationID == "" {
			if in.WorkspaceID == "" {
				return nil, fmt.Errorf("%w: workspace_id is required", integrations.ErrValidation)
			}
		} else if _, err := r.GetIntegration(ctx, in.IntegrationID); err != nil {
			return nil, err
		}
		if in.ResourceID != "" {
			if in.IntegrationID == "" {
				return nil, fmt.Errorf("%w: integration_id is required when resource_id is set", integrations.ErrValidation)
			}
			if _, err := r.GetResource(ctx, in.IntegrationID, in.ResourceID); err != nil {
				return nil, err
			}
		}
	} else if in.IntegrationID != "" {
		integration, err := r.GetIntegration(ctx, in.IntegrationID)
		if err != nil {
			return nil, err
		}
		in.WorkspaceID = strings.TrimSpace(integration.WorkspaceID)
	}
	if in.WorkspaceID == "" {
		return nil, fmt.Errorf("%w: workspace_id is required", integrations.ErrValidation)
	}
	if in.Status == "" {
		in.Status = integrations.FeedbackStatusReceived
	}
	if in.ID == "" {
		in.ID = uuid.NewString()
	}
	if in.CreatedAt.IsZero() {
		in.CreatedAt = now
	}
	in.UpdatedAt = now
	create := func(tx *gorm.DB) error {
		if in.EventType == integrations.FeedbackEventCodingAgentCompleted &&
			in.ChangeRequestID != "" {
			var cr workboard.ChangeRequest
			q := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("id = ? AND workspace_id = ?", in.ChangeRequestID, in.WorkspaceID)
			err := q.First(&cr).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if err == nil {
				var accepted int64
				if err := tx.Model(&workboard.GateRun{}).
					Where(
						"workspace_id = ? AND subject_kind = ? AND subject_id = ? AND gate = ? AND executor = ? AND state = ?",
						in.WorkspaceID,
						workboard.GateRunSubjectChangeRequest,
						in.ChangeRequestID,
						governanceprofile.DeliveryReviewGateKey,
						workboard.GateRunExecutorHuman,
						workboard.NextActionStatePass,
					).
					Count(&accepted).Error; err != nil {
					return err
				}
				if cr.Archived || accepted > 0 {
					return fmt.Errorf("%w: delivery is already accepted; create a new work item", integrations.ErrValidation)
				}
			}
		}
		return tx.Create(&in).Error
	}
	if err := r.db.WithContext(ctx).Transaction(create); err != nil {
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
	if filter.EventType != "" {
		q = q.Where("event_type = ?", strings.TrimSpace(filter.EventType))
	}
	if workspaceID := integrations.WorkspaceID(ctx); workspaceID != "" {
		q = q.Where("workspace_id = ?", workspaceID)
	}
	var rows []integrations.GovernanceFeedbackEvent
	if err := q.Order("created_at DESC, id DESC").Limit(limit).Find(&rows).Error; err != nil {
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
	db := r.db.WithContext(ctx).
		Model(&integrations.GovernanceFeedbackEvent{}).
		Where("id = ?", id)
	if workspaceID := integrations.WorkspaceID(ctx); workspaceID != "" {
		db = db.Where("workspace_id = ?", workspaceID)
	}
	res := db.
		Updates(map[string]any{"status": status, "reason": reason, "updated_at": now})
	if res.Error != nil {
		return nil, fmt.Errorf("update governance feedback event status %s: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	var row integrations.GovernanceFeedbackEvent
	q := r.db.WithContext(ctx).Where("id = ?", id)
	if workspaceID := integrations.WorkspaceID(ctx); workspaceID != "" {
		q = q.Where("workspace_id = ?", workspaceID)
	}
	if err := q.First(&row).Error; err != nil {
		return nil, fmt.Errorf("get governance feedback event %s: %w", id, err)
	}
	return &row, nil
}
