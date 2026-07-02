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
