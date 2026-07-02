package identity

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
)

var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{2,39}$`)

type User struct {
	ID          string    `json:"id" gorm:"column:id;primaryKey"`
	Username    string    `json:"username" gorm:"column:username"`
	DisplayName string    `json:"display_name" gorm:"column:display_name"`
	Email       string    `json:"email,omitempty" gorm:"column:email"`
	CreatedAt   time.Time `json:"created_at" gorm:"column:created_at"`
	UpdatedAt   time.Time `json:"updated_at" gorm:"column:updated_at"`
}

func (User) TableName() string { return "users" }

type Workspace struct {
	ID        string    `json:"id" gorm:"column:id;primaryKey"`
	Slug      string    `json:"slug" gorm:"column:slug"`
	Name      string    `json:"name" gorm:"column:name"`
	CreatedAt time.Time `json:"created_at" gorm:"column:created_at"`
	UpdatedAt time.Time `json:"updated_at" gorm:"column:updated_at"`
}

func (Workspace) TableName() string { return "workspaces" }

type WorkspaceMember struct {
	WorkspaceID string    `json:"workspace_id" gorm:"column:workspace_id;primaryKey"`
	UserID      string    `json:"user_id" gorm:"column:user_id;primaryKey"`
	Role        string    `json:"role" gorm:"column:role"`
	CreatedAt   time.Time `json:"created_at" gorm:"column:created_at"`
}

func (WorkspaceMember) TableName() string { return "workspace_members" }

type Selection struct {
	User      User      `json:"user"`
	Workspace Workspace `json:"workspace"`
}

type BootstrapInput struct {
	WorkspaceName string
	DisplayName   string
	Username      string
	Email         string
}

type Store interface {
	Bootstrap(ctx context.Context, in BootstrapInput) (*Selection, error)
	ListUsers(ctx context.Context) ([]User, error)
	ListWorkspaces(ctx context.Context) ([]Workspace, error)
	GetUser(ctx context.Context, id string) (*User, error)
	GetWorkspace(ctx context.Context, idOrSlug string) (*Workspace, error)
}

func NormalizeUsername(raw string) (string, error) {
	username := strings.ToLower(strings.TrimSpace(raw))
	if !usernamePattern.MatchString(username) {
		return "", errors.New("username must be 3-40 chars: lowercase letters, numbers, underscores, or hyphens; start with a letter or number")
	}
	return username, nil
}

func WorkspaceSlug(name string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastDash = false
		case !lastDash:
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}
