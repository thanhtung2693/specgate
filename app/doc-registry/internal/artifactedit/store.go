// Package artifactedit owns durable artifact edit-session state: the base and
// working file snapshots, per-hunk apply/reject decisions, and saved revisions.
// Hunk decisions persist server-side and record an actor and timestamp.
// Decisions are append-only — the latest row for a hunk wins on read, earlier
// rows remain as the audit trail.
package artifactedit

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

// ErrNotFound is returned when a session or revision id does not exist.
var ErrNotFound = errors.New("artifact edit session not found")

type Session struct {
	ID              string `gorm:"column:id;primaryKey" json:"id"`
	BaseArtifactID  string `gorm:"column:base_artifact_id;not null;index:idx_aes_base" json:"base_artifact_id"`
	BaseVersion     string `gorm:"column:base_version" json:"base_version,omitempty"`
	BaseRevisionID  string `gorm:"column:base_revision_id" json:"base_revision_id,omitempty"`
	State           string `gorm:"column:state;not null" json:"state"`
	SavedRevisionID string `gorm:"column:saved_revision_id" json:"saved_revision_id,omitempty"`
	LastDiffSummary string `gorm:"column:last_diff_summary" json:"last_diff_summary,omitempty"`
	RequestedBy     string `gorm:"column:requested_by" json:"requested_by,omitempty"`
	// CompareToken is an opaque UUID generated at session creation. Clients may
	// echo it back on save; a mismatch causes a stale_base response so
	// concurrent modifications are detected before overwriting.
	CompareToken string `gorm:"column:compare_token" json:"compare_token,omitempty"`
	// SourceKind/SourceID mark a session as a reconciliation proposal and link
	// it to its origin (e.g. "feedback_event" + the event id). Empty for an
	// ordinary edit session. Sourced + active sessions are the review queue.
	SourceKind string    `gorm:"column:source_kind" json:"source_kind,omitempty"`
	SourceID   string    `gorm:"column:source_id" json:"source_id,omitempty"`
	CreatedAt  time.Time `gorm:"column:created_at;not null" json:"created_at"`
	UpdatedAt  time.Time `gorm:"column:updated_at;not null" json:"updated_at"`
}

func (Session) TableName() string { return "artifact_edit_sessions" }

type SessionFile struct {
	SessionID string `gorm:"column:session_id;primaryKey" json:"session_id"`
	FileKey   string `gorm:"column:file_key;primaryKey" json:"file_key"`
	Role      string `gorm:"column:role;primaryKey" json:"role"` // base | working
	Content   string `gorm:"column:content" json:"content"`
}

func (SessionFile) TableName() string { return "artifact_edit_session_files" }

// HunkDecision is one append-only audit row: who decided what, when, for which
// hunk. HunkID embeds the working content, so editing a file produces new ids
// and old decisions naturally stop matching the current diff.
type HunkDecision struct {
	ID        string    `gorm:"column:id;primaryKey" json:"id"`
	SessionID string    `gorm:"column:session_id;not null;index:idx_aehd_session" json:"session_id"`
	HunkID    string    `gorm:"column:hunk_id;not null" json:"hunk_id"`
	FileKey   string    `gorm:"column:file_key;not null" json:"file_key"`
	State     string    `gorm:"column:state;not null" json:"state"`
	Actor     string    `gorm:"column:actor" json:"actor,omitempty"`
	DecidedAt time.Time `gorm:"column:decided_at;not null;index:idx_aehd_session" json:"decided_at"`
}

func (HunkDecision) TableName() string { return "artifact_edit_hunk_decisions" }

type Revision struct {
	RevisionID            string    `gorm:"column:revision_id;primaryKey" json:"revision_id"`
	BaseArtifactID        string    `gorm:"column:base_artifact_id;not null;index:idx_aer_base" json:"base_artifact_id"`
	ArtifactID            string    `gorm:"column:artifact_id" json:"artifact_id,omitempty"`
	State                 string    `gorm:"column:state;not null" json:"state"`
	SessionID             string    `gorm:"column:session_id" json:"session_id,omitempty"`
	Summary               string    `gorm:"column:summary" json:"summary,omitempty"`
	DiffJSON              string    `gorm:"column:diff_json;not null;default:'{}'" json:"diff_json,omitempty"`
	ParentRevisionID      string    `gorm:"column:parent_revision_id" json:"parent_revision_id,omitempty"`
	LineageRootArtifactID string    `gorm:"column:lineage_root_artifact_id" json:"lineage_root_artifact_id,omitempty"`
	CreatedAt             time.Time `gorm:"column:created_at;not null;index:idx_aer_base" json:"created_at"`
}

func (Revision) TableName() string { return "artifact_edit_revisions" }

// SessionState is the hydrated view a caller needs to build a diff: the session
// row plus base/working file maps and the current (latest-per-hunk) decisions.
type SessionState struct {
	Session   Session
	Base      map[string]string
	Working   map[string]string
	Decisions map[string]HunkDecision
}

// SessionMeta is the mutable subset of a session row updated as edits land.
type SessionMeta struct {
	State           string
	SavedRevisionID string
	LastDiffSummary string
	UpdatedAt       time.Time
}

type Store interface {
	// CreateSession writes the session and its file rows in one transaction.
	// workingFiles overrides the working side for the given keys (omitted keys
	// start equal to base); pass nil for a fresh base==working session.
	CreateSession(ctx context.Context, s Session, baseFiles, workingFiles map[string]string) error
	LoadSession(ctx context.Context, id string) (*SessionState, error)
	UpdateWorkingFile(ctx context.Context, sessionID, fileKey, content string, updatedAt time.Time) error
	SetSessionMeta(ctx context.Context, sessionID string, meta SessionMeta) error
	AppendHunkDecision(ctx context.Context, d HunkDecision) error
	CreateRevision(ctx context.Context, r Revision) error
	GetRevision(ctx context.Context, revisionID string) (*Revision, error)
	ListRevisions(ctx context.Context, baseArtifactID string) ([]Revision, error)
	// ListProposals returns sourced sessions still awaiting a verdict
	// (state=active) — the artifact-update proposal review queue.
	ListProposals(ctx context.Context) ([]Session, error)
}

// MemoryStore is an ephemeral Store used as a test double. Production wires the
// DB-backed repository; see internal/storage/db.
type MemoryStore struct {
	mu        sync.Mutex
	sessions  map[string]*memSession
	revisions map[string]Revision
}

type memSession struct {
	session   Session
	base      map[string]string
	working   map[string]string
	decisions map[string]HunkDecision
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		sessions:  map[string]*memSession{},
		revisions: map[string]Revision{},
	}
}

func (m *MemoryStore) CreateSession(_ context.Context, s Session, baseFiles, workingFiles map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	working := cloneMap(baseFiles)
	for key, content := range workingFiles {
		working[key] = content
	}
	m.sessions[s.ID] = &memSession{
		session:   s,
		base:      cloneMap(baseFiles),
		working:   working,
		decisions: map[string]HunkDecision{},
	}
	return nil
}

func (m *MemoryStore) LoadSession(_ context.Context, id string) (*SessionState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.sessions[id]
	if !ok {
		return nil, ErrNotFound
	}
	return &SessionState{
		Session:   rec.session,
		Base:      cloneMap(rec.base),
		Working:   cloneMap(rec.working),
		Decisions: cloneDecisions(rec.decisions),
	}, nil
}

func (m *MemoryStore) UpdateWorkingFile(_ context.Context, sessionID, fileKey, content string, updatedAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.sessions[sessionID]
	if !ok {
		return ErrNotFound
	}
	rec.working[fileKey] = content
	rec.session.UpdatedAt = updatedAt
	return nil
}

func (m *MemoryStore) SetSessionMeta(_ context.Context, sessionID string, meta SessionMeta) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.sessions[sessionID]
	if !ok {
		return ErrNotFound
	}
	rec.session.State = meta.State
	rec.session.SavedRevisionID = meta.SavedRevisionID
	rec.session.LastDiffSummary = meta.LastDiffSummary
	rec.session.UpdatedAt = meta.UpdatedAt
	return nil
}

func (m *MemoryStore) AppendHunkDecision(_ context.Context, d HunkDecision) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.sessions[d.SessionID]
	if !ok {
		return ErrNotFound
	}
	rec.decisions[d.HunkID] = d
	return nil
}

func (m *MemoryStore) CreateRevision(_ context.Context, r Revision) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.revisions[r.RevisionID] = r
	return nil
}

func (m *MemoryStore) GetRevision(_ context.Context, revisionID string) (*Revision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.revisions[revisionID]
	if !ok {
		return nil, ErrNotFound
	}
	return &r, nil
}

func (m *MemoryStore) ListRevisions(_ context.Context, baseArtifactID string) ([]Revision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Revision, 0)
	for _, r := range m.revisions {
		if r.BaseArtifactID == baseArtifactID {
			out = append(out, r)
		}
	}
	return out, nil
}

func (m *MemoryStore) ListProposals(_ context.Context) ([]Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Session, 0)
	for _, rec := range m.sessions {
		if rec.session.SourceKind != "" && rec.session.State == "active" {
			out = append(out, rec.session)
		}
	}
	// Match the DB repository ordering (created_at DESC, id ASC) so the
	// in-memory double does not diverge from production behavior.
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func cloneMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneDecisions(in map[string]HunkDecision) map[string]HunkDecision {
	out := make(map[string]HunkDecision, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
