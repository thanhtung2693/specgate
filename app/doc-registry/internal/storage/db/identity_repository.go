package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/specgate/doc-registry/internal/identity"
)

type IdentityRepository struct {
	db *gorm.DB
}

func NewIdentityRepository(db *gorm.DB) *IdentityRepository {
	return &IdentityRepository{db: db}
}

func (r *IdentityRepository) Bootstrap(ctx context.Context, in identity.BootstrapInput) (*identity.Selection, error) {
	username, err := identity.NormalizeUsername(in.Username)
	if err != nil {
		return nil, err
	}
	displayName := strings.TrimSpace(in.DisplayName)
	if displayName == "" {
		return nil, errors.New("display name is required")
	}
	workspaceName := strings.TrimSpace(in.WorkspaceName)
	if workspaceName == "" {
		return nil, errors.New("workspace name is required")
	}
	slug := identity.WorkspaceSlug(workspaceName)
	if slug == "" {
		return nil, errors.New("workspace name must contain a letter or number")
	}
	email := strings.TrimSpace(in.Email)

	var out identity.Selection
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		user := identity.User{
			ID:          uuid.NewString(),
			Username:    username,
			DisplayName: displayName,
			Email:       email,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&user).Error; err != nil {
			return fmt.Errorf("create user: %w", err)
		}
		var storedUser identity.User
		if err := tx.Where("username = ?", username).First(&storedUser).Error; err != nil {
			return fmt.Errorf("load user: %w", err)
		}

		workspace := identity.Workspace{
			ID:        uuid.NewString(),
			Slug:      slug,
			Name:      workspaceName,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&workspace).Error; err != nil {
			return fmt.Errorf("create workspace: %w", err)
		}
		var storedWorkspace identity.Workspace
		if err := tx.Where("slug = ?", slug).First(&storedWorkspace).Error; err != nil {
			return fmt.Errorf("load workspace: %w", err)
		}

		member := identity.WorkspaceMember{
			WorkspaceID: storedWorkspace.ID,
			UserID:      storedUser.ID,
			Role:        "owner",
			CreatedAt:   now,
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&member).Error; err != nil {
			return fmt.Errorf("create workspace member: %w", err)
		}
		out = identity.Selection{User: storedUser, Workspace: storedWorkspace}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (r *IdentityRepository) ListUsers(ctx context.Context) ([]identity.User, error) {
	var users []identity.User
	err := r.db.WithContext(ctx).Order("username ASC").Find(&users).Error
	return users, err
}

func (r *IdentityRepository) ListWorkspaces(ctx context.Context) ([]identity.Workspace, error) {
	var workspaces []identity.Workspace
	err := r.db.WithContext(ctx).Order("name ASC").Find(&workspaces).Error
	return workspaces, err
}

func (r *IdentityRepository) GetUser(ctx context.Context, id string) (*identity.User, error) {
	var user identity.User
	query := r.db.WithContext(ctx)
	if _, err := uuid.Parse(id); err == nil {
		query = query.Where("id = ?", id)
	} else {
		query = query.Where("username = ?", id)
	}
	err := query.First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &user, err
}

func (r *IdentityRepository) GetWorkspace(ctx context.Context, idOrSlug string) (*identity.Workspace, error) {
	var workspace identity.Workspace
	query := r.db.WithContext(ctx)
	if _, err := uuid.Parse(idOrSlug); err == nil {
		query = query.Where("id = ?", idOrSlug)
	} else {
		query = query.Where("slug = ?", idOrSlug)
	}
	err := query.First(&workspace).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &workspace, err
}

func (r *IdentityRepository) ListWorkspaceMembers(ctx context.Context, workspaceID string) ([]identity.WorkspaceMemberDetail, error) {
	// Reports whether each member can authenticate, and who last changed that,
	// without exposing the hash: an operator asking "who has access here" should
	// not need a second command, and the access-change trail is only useful if
	// something reads it. The lateral join takes each member's newest event.
	type memberRow struct {
		identity.WorkspaceMemberDetail
		Credential string
		EventActor string
		EventAt    *time.Time
		EventType  string
	}
	var rows []memberRow
	err := r.db.WithContext(ctx).
		Table("workspace_members AS wm").
		Select(`wm.workspace_id, wm.user_id, wm.role, wm.created_at,
			u.username, u.display_name, u.email, u.credential,
			ev.actor AS event_actor, ev.created_at AS event_at, ev.event_type AS event_type`).
		Joins("JOIN users AS u ON u.id = wm.user_id").
		Joins(`LEFT JOIN LATERAL (
			SELECT actor, created_at, event_type FROM identity_events
			WHERE subject_id = u.id ORDER BY created_at DESC LIMIT 1
		) AS ev ON TRUE`).
		Where("wm.workspace_id = ?", workspaceID).
		Order("u.username ASC").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	members := make([]identity.WorkspaceMemberDetail, 0, len(rows))
	for _, row := range rows {
		member := row.WorkspaceMemberDetail
		member.CredentialSet = strings.TrimSpace(row.Credential) != ""
		if row.EventType != "" {
			member.CredentialChangedBy = row.EventActor
			member.CredentialChangedAt = row.EventAt
		}
		members = append(members, member)
	}
	return members, nil
}

// CredentialsConfigured reports whether any user has a gateway credential.
func (r *IdentityRepository) CredentialsConfigured(ctx context.Context) (bool, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&identity.User{}).
		Where("credential <> ''").Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// UserCredential returns one user's bcrypt hash, or an empty string when the
// user does not exist or has no credential. Both cases look identical to the
// caller on purpose: an unknown username must not be distinguishable from a
// wrong password.
func (r *IdentityRepository) UserCredential(ctx context.Context, username string) (string, error) {
	normalized, err := identity.NormalizeUsername(username)
	if err != nil {
		return "", nil
	}
	var user identity.User
	err = r.db.WithContext(ctx).Where("username = ?", normalized).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return user.Credential, nil
}

// SetUserCredential stores or clears one member's bcrypt credential.
func (r *IdentityRepository) SetUserCredential(ctx context.Context, username, hash string) error {
	normalized, err := identity.NormalizeUsername(username)
	if err != nil {
		return identity.ErrUserNotFound
	}
	result := r.db.WithContext(ctx).Model(&identity.User{}).
		Where("username = ?", normalized).
		Updates(map[string]any{"credential": hash, "updated_at": time.Now().UTC()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return identity.ErrUserNotFound
	}
	return nil
}

// RecordCredentialEvent appends one access-change record for a member.
func (r *IdentityRepository) RecordCredentialEvent(ctx context.Context, in identity.CredentialEventInput) error {
	subjectID, err := r.userIDForUsername(ctx, in.Username)
	if err != nil {
		return err
	}
	row := identity.IdentityEvent{
		// The column defaults to gen_random_uuid(), but GORM sends the zero value,
		// and "" is not valid UUID input. Generate it here, as the sibling stores do.
		ID:        uuid.NewString(),
		SubjectID: subjectID,
		EventType: strings.TrimSpace(in.EventType),
		Actor:     strings.TrimSpace(in.Actor),
		Detail:    strings.TrimSpace(in.Detail),
		CreatedAt: time.Now().UTC(),
	}
	if workspaceID := strings.TrimSpace(in.WorkspaceID); workspaceID != "" {
		row.WorkspaceID = &workspaceID
	}
	return r.db.WithContext(ctx).Create(&row).Error
}

// LatestCredentialEvent returns a member's most recent access change.
func (r *IdentityRepository) LatestCredentialEvent(ctx context.Context, username string) (*identity.IdentityEvent, error) {
	normalized, err := identity.NormalizeUsername(username)
	if err != nil {
		return nil, nil
	}
	var user identity.User
	if err := r.db.WithContext(ctx).Where("username = ?", normalized).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	var event identity.IdentityEvent
	err = r.db.WithContext(ctx).
		Where("subject_id = ?", user.ID).
		Order("created_at DESC").
		First(&event).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &event, nil
}

// userIDForUsername resolves a username to its user id for event subjects.
func (r *IdentityRepository) userIDForUsername(ctx context.Context, username string) (string, error) {
	normalized, err := identity.NormalizeUsername(username)
	if err != nil {
		return "", identity.ErrUserNotFound
	}
	var user identity.User
	if err := r.db.WithContext(ctx).Where("username = ?", normalized).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", identity.ErrUserNotFound
		}
		return "", err
	}
	return user.ID, nil
}
