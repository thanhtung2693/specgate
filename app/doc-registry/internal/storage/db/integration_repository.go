package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/specgate/doc-registry/internal/integrations"
	"gorm.io/gorm"
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
