package db

import (
	"context"
	"errors"
	"time"

	"github.com/specgate/doc-registry/internal/artifactedit"
	"gorm.io/gorm"
)

const (
	fileRoleBase    = "base"
	fileRoleWorking = "working"
)

// ArtifactEditRepository is the durable, DB-backed implementation of
// artifactedit.Store. It persists sessions, base/working file snapshots,
// append-only hunk decisions, and saved revisions.
type ArtifactEditRepository struct {
	db *gorm.DB
}

func NewArtifactEditRepository(db *gorm.DB) *ArtifactEditRepository {
	return &ArtifactEditRepository{db: db}
}

var _ artifactedit.Store = (*ArtifactEditRepository)(nil)

func (r *ArtifactEditRepository) CreateSession(
	ctx context.Context,
	s artifactedit.Session,
	baseFiles, workingFiles map[string]string,
) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&s).Error; err != nil {
			return err
		}
		rows := make([]artifactedit.SessionFile, 0, len(baseFiles)*2)
		for key, content := range baseFiles {
			working := content
			if w, ok := workingFiles[key]; ok {
				working = w
			}
			rows = append(rows,
				artifactedit.SessionFile{SessionID: s.ID, FileKey: key, Role: fileRoleBase, Content: content},
				artifactedit.SessionFile{SessionID: s.ID, FileKey: key, Role: fileRoleWorking, Content: working},
			)
		}
		if len(rows) == 0 {
			return nil
		}
		return tx.Create(&rows).Error
	})
}

func (r *ArtifactEditRepository) LoadSession(
	ctx context.Context,
	id string,
) (*artifactedit.SessionState, error) {
	var session artifactedit.Session
	if err := r.db.WithContext(ctx).First(&session, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, artifactedit.ErrNotFound
		}
		return nil, err
	}
	var files []artifactedit.SessionFile
	if err := r.db.WithContext(ctx).
		Where("session_id = ?", id).
		Find(&files).Error; err != nil {
		return nil, err
	}
	state := &artifactedit.SessionState{
		Session:   session,
		Base:      map[string]string{},
		Working:   map[string]string{},
		Decisions: map[string]artifactedit.HunkDecision{},
	}
	for _, f := range files {
		switch f.Role {
		case fileRoleBase:
			state.Base[f.FileKey] = f.Content
		case fileRoleWorking:
			state.Working[f.FileKey] = f.Content
		}
	}
	// Append-only log: ascending order means the last write for a hunk wins.
	var decisions []artifactedit.HunkDecision
	if err := r.db.WithContext(ctx).
		Where("session_id = ?", id).
		Order("decided_at ASC, id ASC").
		Find(&decisions).Error; err != nil {
		return nil, err
	}
	for _, d := range decisions {
		state.Decisions[d.HunkID] = d
	}
	return state, nil
}

func (r *ArtifactEditRepository) UpdateWorkingFile(
	ctx context.Context,
	sessionID, fileKey, content string,
	updatedAt time.Time,
) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&artifactedit.SessionFile{}).
			Where("session_id = ? AND file_key = ? AND role = ?", sessionID, fileKey, fileRoleWorking).
			Update("content", content)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			if err := tx.Create(&artifactedit.SessionFile{
				SessionID: sessionID, FileKey: fileKey, Role: fileRoleWorking, Content: content,
			}).Error; err != nil {
				return err
			}
		}
		return tx.Model(&artifactedit.Session{}).
			Where("id = ?", sessionID).
			Update("updated_at", updatedAt).Error
	})
}

func (r *ArtifactEditRepository) SetSessionMeta(
	ctx context.Context,
	sessionID string,
	meta artifactedit.SessionMeta,
) error {
	res := r.db.WithContext(ctx).
		Model(&artifactedit.Session{}).
		Where("id = ?", sessionID).
		Updates(map[string]any{
			"state":             meta.State,
			"saved_revision_id": meta.SavedRevisionID,
			"last_diff_summary": meta.LastDiffSummary,
			"updated_at":        meta.UpdatedAt,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return artifactedit.ErrNotFound
	}
	return nil
}

func (r *ArtifactEditRepository) AppendHunkDecision(
	ctx context.Context,
	d artifactedit.HunkDecision,
) error {
	// Decision row + session touch are written atomically, mirroring the
	// status+event discipline used elsewhere (per spec §14).
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&d).Error; err != nil {
			return err
		}
		return tx.Model(&artifactedit.Session{}).
			Where("id = ?", d.SessionID).
			Update("updated_at", d.DecidedAt).Error
	})
}

func (r *ArtifactEditRepository) CreateRevision(
	ctx context.Context,
	rev artifactedit.Revision,
) error {
	return r.db.WithContext(ctx).Create(&rev).Error
}

func (r *ArtifactEditRepository) GetRevision(
	ctx context.Context,
	revisionID string,
) (*artifactedit.Revision, error) {
	var rev artifactedit.Revision
	if err := r.db.WithContext(ctx).First(&rev, "revision_id = ?", revisionID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, artifactedit.ErrNotFound
		}
		return nil, err
	}
	return &rev, nil
}

func (r *ArtifactEditRepository) ListRevisions(
	ctx context.Context,
	baseArtifactID string,
) ([]artifactedit.Revision, error) {
	var rows []artifactedit.Revision
	if err := r.db.WithContext(ctx).
		Where("base_artifact_id = ?", baseArtifactID).
		Order("created_at ASC, revision_id ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *ArtifactEditRepository) ListProposals(ctx context.Context) ([]artifactedit.Session, error) {
	var rows []artifactedit.Session
	if err := r.db.WithContext(ctx).
		Where("source_kind <> '' AND state = ?", "active").
		Order("created_at DESC, id ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}
