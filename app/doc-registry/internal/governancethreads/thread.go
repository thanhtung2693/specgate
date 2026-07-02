package governancethreads

import (
	"context"
	"errors"
	"time"
)

var ErrNotFound = errors.New("governance-chat thread not found")

// Thread is the lightweight, Doc Registry-backed index used by the UI sidebar.
// Full transcript/checkpoint state remains owned by LangGraph.
type Thread struct {
	ThreadID  string    `gorm:"column:thread_id;primaryKey"`
	Title     string    `gorm:"column:title;not null"`
	Preview   string    `gorm:"column:preview;not null"`
	Archived  bool      `gorm:"column:archived;not null;default:false;index:idx_governance_threads_archived_updated"`
	CreatedAt time.Time `gorm:"column:created_at;not null"`
	UpdatedAt time.Time `gorm:"column:updated_at;not null;index:idx_governance_threads_archived_updated"`
}

func (Thread) TableName() string { return "governance_threads" }

type ListFilter struct {
	Limit           int
	Offset          int
	IncludeArchived bool
}

type Store interface {
	Upsert(ctx context.Context, thread Thread) (*Thread, error)
	List(ctx context.Context, filter ListFilter) ([]Thread, int64, error)
	Delete(ctx context.Context, threadID string, now time.Time) error
}
