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
	ID          string `json:"id" gorm:"column:id;primaryKey"`
	Username    string `json:"username" gorm:"column:username"`
	DisplayName string `json:"display_name" gorm:"column:display_name"`
	Email       string `json:"email,omitempty" gorm:"column:email"`
	// Credential is the bcrypt hash the gateway verifier checks. It never leaves
	// the service: no DTO, no API response, and no log line carries it.
	Credential string    `json:"-" gorm:"column:credential"`
	CreatedAt  time.Time `json:"created_at" gorm:"column:created_at"`
	UpdatedAt  time.Time `json:"updated_at" gorm:"column:updated_at"`
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

type WorkspaceMemberDetail struct {
	WorkspaceID string    `json:"workspace_id" gorm:"column:workspace_id"`
	UserID      string    `json:"user_id" gorm:"column:user_id"`
	Username    string    `json:"username" gorm:"column:username"`
	DisplayName string    `json:"display_name" gorm:"column:display_name"`
	Email       string    `json:"email,omitempty" gorm:"column:email"`
	Role        string    `json:"role" gorm:"column:role"`
	CreatedAt   time.Time `json:"created_at" gorm:"column:created_at"`
	Current     bool      `json:"current,omitempty" gorm:"-"`
	// CredentialSet reports whether this member can authenticate to the gateway.
	// The hash itself never leaves the service.
	CredentialSet bool `json:"credential_set" gorm:"-"`
	// CredentialChangedBy and CredentialChangedAt attribute the member's most
	// recent access change, so an operator reading the member list can see who
	// granted or revoked it rather than only the current state.
	CredentialChangedBy string     `json:"credential_changed_by,omitempty" gorm:"-"`
	CredentialChangedAt *time.Time `json:"credential_changed_at,omitempty" gorm:"-"`
}

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
	ListWorkspaceMembers(ctx context.Context, workspaceID string) ([]WorkspaceMemberDetail, error)
	// CredentialsConfigured reports whether any user can authenticate. False keeps
	// the gateway open, the posture a default loopback appliance expects.
	CredentialsConfigured(ctx context.Context) (bool, error)
	// UserCredential returns the stored bcrypt hash for a username, empty when the
	// user is unknown or has none, so a missing user is indistinguishable from a
	// wrong password.
	UserCredential(ctx context.Context, username string) (string, error)
	// SetUserCredential stores a bcrypt hash for a member, or removes the
	// credential when hash is empty. It reports ErrUserNotFound for an unknown
	// member rather than creating one: membership is granted elsewhere.
	SetUserCredential(ctx context.Context, username, hash string) error
	// RecordCredentialEvent appends one access-change record, resolving the member
	// by username so callers never handle internal ids. A failure to record must
	// fail the operation that caused it: an unlogged credential change is exactly
	// the gap this trail exists to close.
	RecordCredentialEvent(ctx context.Context, in CredentialEventInput) error
	// LatestCredentialEvent returns the most recent access change for a username,
	// or nil when that member has none.
	LatestCredentialEvent(ctx context.Context, username string) (*IdentityEvent, error)
}

// IdentityEvent records a change to who can authenticate. Granting or revoking a
// gateway credential is a governance act, so it leaves a row rather than only
// mutating state.
//
// The trail is append-only by convention and carries no hash chain, so it shows
// what the service recorded, not that the database was never edited afterwards.
// Documentation must not describe it as tamper-evident.
type IdentityEvent struct {
	ID string `json:"id" gorm:"column:id;primaryKey"`
	// WorkspaceID is a pointer because the column is a nullable UUID foreign key:
	// an empty string is not valid UUID input, and an access change made before a
	// workspace is selected still has to be recorded.
	WorkspaceID *string   `json:"workspace_id,omitempty" gorm:"column:workspace_id"`
	SubjectID   string    `json:"subject_id" gorm:"column:subject_id"`
	EventType   string    `json:"event_type" gorm:"column:event_type"`
	Actor       string    `json:"actor,omitempty" gorm:"column:actor"`
	Detail      string    `json:"detail,omitempty" gorm:"column:detail"`
	CreatedAt   time.Time `json:"created_at" gorm:"column:created_at"`
}

func (IdentityEvent) TableName() string { return "identity_events" }

// CredentialEventInput describes one access change to record.
type CredentialEventInput struct {
	Username    string
	EventType   string
	Actor       string
	Detail      string
	WorkspaceID string
}

// Identity event types.
const (
	EventCredentialIssued  = "identity.credential_issued"
	EventCredentialRevoked = "identity.credential_revoked"
)

// ErrUserNotFound reports a credential operation against a member this appliance
// does not have.
var ErrUserNotFound = errors.New("user not found")

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
