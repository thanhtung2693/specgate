package governancefiles

import (
	"errors"
	"time"
)

// Status of a governance file row.
type Status string

const (
	StatusPending Status = "pending"
	StatusReady   Status = "ready"
)

// File is a row in governance_files. Mirrors the DB columns 1:1 (per spec §5.1).
type File struct {
	ID         string    `gorm:"column:id;primaryKey"`
	Name       string    `gorm:"column:name"`
	Mime       string    `gorm:"column:mime"`
	SizeBytes  int64     `gorm:"column:size_bytes"`
	ObjectKey  string    `gorm:"column:object_key;uniqueIndex"`
	Status     Status    `gorm:"column:status;index"`
	CreatedAt  time.Time `gorm:"column:created_at"`
	LastUsedAt time.Time `gorm:"column:last_used_at;index"`
}

// TableName binds the GORM model to the migration table.
func (File) TableName() string { return "governance_files" }

// ErrNotFound is returned by Store when a file ID does not exist.
var ErrNotFound = errors.New("governance file not found")

// ListFilter scopes a List call (per spec §5.2 GET /governance/files).
type ListFilter struct {
	Q      string // case-insensitive substring on name
	Limit  int    // clamped 1..200 by handler
	Offset int
}
