// Package artifactattachment is the domain model for feature-scoped reference
// attachments — links, files, and screenshots a product team pins to a feature
// to support quality-gate review and the coding-agent handoff.
//
// Attachments are scoped to feature_id (not a single artifact version) so a bug
// screenshot or reference link survives across spec re-publishes. Each row
// carries an audience flag that controls who downstream consumes it: the
// quality-gate judge sees gate/both; the coding-agent context-pack sees
// coding_agent/both. The default is gate-only — reaching the coding agent is a
// deliberate opt-in, never the default.
package artifactattachment

import (
	"context"
	"errors"
	"time"
)

// Kind is the attachment's storage shape.
type Kind string

const (
	// KindLink is an external URL (no S3 object).
	KindLink Kind = "link"
	// KindFile is an uploaded non-image file, stored as a governance_files object.
	KindFile Kind = "file"
	// KindImage is an uploaded image (screenshot), stored as a governance_files object.
	KindImage Kind = "image"
)

// Audience controls which downstream consumer sees the attachment.
type Audience string

const (
	// AudienceGate surfaces the attachment to the quality-gate reviewer only.
	AudienceGate Audience = "gate"
	// AudienceCodingAgent surfaces it in the coding-agent handoff context only.
	AudienceCodingAgent Audience = "coding_agent"
	// AudienceBoth surfaces it to both consumers.
	AudienceBoth Audience = "both"
)

// ValidKind reports whether k is a known attachment kind.
func ValidKind(k Kind) bool {
	switch k {
	case KindLink, KindFile, KindImage:
		return true
	default:
		return false
	}
}

// ValidAudience reports whether a is a known audience.
func ValidAudience(a Audience) bool {
	switch a {
	case AudienceGate, AudienceCodingAgent, AudienceBoth:
		return true
	default:
		return false
	}
}

// Attachment is a row in artifact_attachments. Mirrors the DB columns 1:1.
type Attachment struct {
	ID               string    `gorm:"column:id;primaryKey"`
	WorkspaceID      string    `gorm:"column:workspace_id;index"`
	FeatureID        string    `gorm:"column:feature_id;index"`
	Kind             Kind      `gorm:"column:kind"`
	URL              string    `gorm:"column:url"`
	GovernanceFileID string    `gorm:"column:governance_file_id"`
	Title            string    `gorm:"column:title"`
	Note             string    `gorm:"column:note"`
	Audience         Audience  `gorm:"column:audience"`
	CreatedBy        string    `gorm:"column:created_by"`
	CreatedAt        time.Time `gorm:"column:created_at"`
}

// TableName binds the GORM model to the migration table.
func (Attachment) TableName() string { return "artifact_attachments" }

// ErrNotFound is returned when an attachment id does not exist.
var ErrNotFound = errors.New("artifact attachment not found")

// Store is the persistence boundary for artifact_attachments. Implementations
// live under internal/storage/db; tests validate via forEachDriver + testcontainers-go
// (per doc-registry/AGENTS.md).
type Store interface {
	// Create inserts a new attachment row.
	Create(ctx context.Context, a Attachment) (*Attachment, error)
	// ListByFeature returns a feature's attachments, newest first.
	ListByFeature(ctx context.Context, workspaceID, featureID string) ([]Attachment, error)
	// Get returns a single attachment; ErrNotFound otherwise.
	Get(ctx context.Context, workspaceID, id string) (*Attachment, error)
	// Delete removes the row. Returns ErrNotFound if it does not exist.
	Delete(ctx context.Context, workspaceID, id string) error
}
