package db

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/specgate/doc-registry/internal/knowledge"
)

type KnowledgeRepository struct {
	db *gorm.DB
}

func NewKnowledgeRepository(db *gorm.DB) *KnowledgeRepository {
	return &KnowledgeRepository{db: db}
}

func scopedKnowledgeDocuments(ctx context.Context, q *gorm.DB) *gorm.DB {
	if workspaceID, ok := knowledge.WorkspaceFromContext(ctx); ok {
		return q.Where("workspace_id = ?", workspaceID)
	}
	return q
}

func (r *KnowledgeRepository) CreateVersion(ctx context.Context, doc *knowledge.Document, links []knowledge.Link) error {
	if workspaceID, ok := knowledge.WorkspaceFromContext(ctx); ok && strings.TrimSpace(doc.WorkspaceID) != workspaceID {
		return fmt.Errorf("%w: document workspace does not match request workspace", knowledge.ErrValidation)
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		latest := scopedKnowledgeDocuments(ctx, tx.Model(&knowledge.Document{}))
		if err := latest.
			Where("document_id = ?", doc.DocumentID).
			Update("is_latest", false).Error; err != nil {
			return err
		}
		if err := tx.Create(doc).Error; err != nil {
			return err
		}
		if len(links) > 0 {
			if err := tx.Create(&links).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *KnowledgeRepository) Get(ctx context.Context, documentID, version string) (*knowledge.Document, error) {
	var doc knowledge.Document
	err := scopedKnowledgeDocuments(ctx, r.db.WithContext(ctx)).
		Preload(clause.Associations).
		First(&doc, "document_id = ? AND version = ?", documentID, version).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, knowledge.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

func (r *KnowledgeRepository) List(ctx context.Context, f knowledge.ListFilter) ([]knowledge.Document, error) {
	q := r.listQuery(ctx, f).Preload(clause.Associations)
	if f.Limit > 0 {
		q = q.Limit(f.Limit)
	}
	if f.Offset > 0 {
		q = q.Offset(f.Offset)
	}
	var out []knowledge.Document
	return out, q.Order("updated_at DESC").Find(&out).Error
}

func (r *KnowledgeRepository) Count(ctx context.Context, f knowledge.ListFilter) (int, error) {
	var total int64
	if err := r.listQuery(ctx, f).Count(&total).Error; err != nil {
		return 0, err
	}
	return int(total), nil
}

func (r *KnowledgeRepository) listQuery(ctx context.Context, f knowledge.ListFilter) *gorm.DB {
	q := r.db.WithContext(ctx).Model(&knowledge.Document{})
	if workspaceID, ok := knowledge.WorkspaceFromContext(ctx); ok {
		if f.WorkspaceID != "" && f.WorkspaceID != workspaceID {
			return q.Where("1 = 0")
		}
		f.WorkspaceID = workspaceID
	}
	if f.WorkspaceID != "" {
		q = q.Where("workspace_id = ?", f.WorkspaceID)
	} else {
		q = q.Where("workspace_id IS NOT NULL AND workspace_id <> ''")
	}
	if !f.IncludeHistory {
		q = q.Where("is_latest = ?", true)
	}
	if f.LinkedFeatureID != "" {
		q = q.Where("linked_feature_id = ?", f.LinkedFeatureID)
	}
	if f.LinkedRequestID != "" {
		q = q.Where("linked_request_id = ?", f.LinkedRequestID)
	}
	if f.DocumentType != "" {
		q = q.Where("document_type = ?", f.DocumentType)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	return q
}

// ListByFeatureOrRequest returns all document versions (including non-latest)
// in the given workspace linked to the given feature or change request via
// linked_feature_id / linked_request_id. Workspace scope is required: an empty
// workspaceID returns an empty slice without a DB call so provenance can never
// cross workspaces. The WHERE clause is built dynamically: when featureRefs is
// empty the feature condition is omitted; when requestID is empty the request
// condition is omitted. Both empty returns an empty slice without a DB call.
// Results are ordered by authority priority then title (per spec §3).
func (r *KnowledgeRepository) ListByFeatureOrRequest(ctx context.Context, workspaceID string, featureRefs []string, requestID string) ([]knowledge.Document, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return []knowledge.Document{}, nil
	}
	if scopedWorkspace, ok := knowledge.WorkspaceFromContext(ctx); ok && scopedWorkspace != workspaceID {
		return []knowledge.Document{}, nil
	}
	featureRefs = normalizeKnowledgeFeatureRefs(featureRefs)
	requestID = strings.TrimSpace(requestID)
	if len(featureRefs) == 0 && requestID == "" {
		return []knowledge.Document{}, nil
	}
	q := r.db.WithContext(ctx).Model(&knowledge.Document{}).Where("workspace_id = ?", workspaceID)
	switch {
	case len(featureRefs) > 0 && requestID != "":
		q = q.Where("linked_feature_id IN ? OR linked_request_id = ?", featureRefs, requestID)
	case len(featureRefs) > 0:
		q = q.Where("linked_feature_id IN ?", featureRefs)
	default:
		q = q.Where("linked_request_id = ?", requestID)
	}
	q = q.Order("CASE authority_level" +
		" WHEN 'source_of_truth' THEN 1" +
		" WHEN 'high'            THEN 2" +
		" WHEN 'reference'       THEN 3" +
		" WHEN 'low'             THEN 4" +
		" ELSE 5 END, title")
	var out []knowledge.Document
	return out, q.Find(&out).Error
}

func normalizeKnowledgeFeatureRefs(raw []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, rawRef := range raw {
		ref := strings.TrimSpace(rawRef)
		if ref == "" || seen[ref] {
			continue
		}
		seen[ref] = true
		out = append(out, ref)
	}
	return out
}

func (r *KnowledgeRepository) History(ctx context.Context, documentID string) ([]knowledge.Document, error) {
	var out []knowledge.Document
	err := scopedKnowledgeDocuments(ctx, r.db.WithContext(ctx)).
		Where("document_id = ?", documentID).
		Order("created_at ASC").
		Find(&out).Error
	return out, err
}

func (r *KnowledgeRepository) UpdateStatus(ctx context.Context, documentID, version string, status knowledge.Status, summary, errorMessage string) error {
	updates := map[string]any{
		"status":     status,
		"updated_at": time.Now().UTC(),
		// error_message is always written so a state transition clears a stale
		// error (e.g. retrying a failed version), and a failed transition records
		// it. Summary stays sticky (only overwritten when non-empty).
		"error_message": errorMessage,
	}
	if summary != "" {
		updates["summary"] = summary
	}
	res := scopedKnowledgeDocuments(ctx, r.db.WithContext(ctx)).
		Model(&knowledge.Document{}).
		Where("document_id = ? AND version = ?", documentID, version).
		Updates(updates)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return knowledge.ErrNotFound
	}
	return nil
}

func (r *KnowledgeRepository) ReplaceChunks(ctx context.Context, documentID, version string, chunks []knowledge.Chunk) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := r.getTx(ctx, tx, documentID, version); err != nil {
			return err
		}
		if err := tx.Delete(&knowledge.Chunk{}, "document_id = ? AND version = ?", documentID, version).Error; err != nil {
			return err
		}
		if len(chunks) == 0 {
			return nil
		}
		return tx.Create(&chunks).Error
	})
}

func (r *KnowledgeRepository) ChunksForDocument(ctx context.Context, documentID, version string) ([]knowledge.Chunk, error) {
	var chunks []knowledge.Chunk
	err := r.scopedChunks(ctx, r.db.WithContext(ctx)).
		Where("document_id = ? AND version = ?", documentID, version).
		Order("chunk_index ASC").
		Find(&chunks).Error
	return chunks, err
}

func (r *KnowledgeRepository) ChunkCount(ctx context.Context, documentID, version string) (int, error) {
	var count int64
	err := r.scopedChunks(ctx, r.db.WithContext(ctx)).
		Model(&knowledge.Chunk{}).
		Where("document_id = ? AND version = ?", documentID, version).
		Count(&count).Error
	return int(count), err
}

func (r *KnowledgeRepository) DeleteVersion(ctx context.Context, documentID, version string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		doc, err := r.getTx(ctx, tx, documentID, version)
		if errors.Is(err, knowledge.ErrNotFound) {
			return knowledge.ErrNotFound
		}
		if err != nil {
			return err
		}
		if err := scopedKnowledgeDocuments(ctx, tx).Delete(&knowledge.Document{}, "document_id = ? AND version = ?", documentID, version).Error; err != nil {
			return err
		}
		if !doc.IsLatest {
			return nil
		}
		var latest knowledge.Document
		err = scopedKnowledgeDocuments(ctx, tx).Where("document_id = ?", documentID).Order("created_at DESC").First(&latest).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		return scopedKnowledgeDocuments(ctx, tx.Model(&knowledge.Document{})).
			Where("document_id = ? AND version = ?", latest.DocumentID, latest.Version).
			Update("is_latest", true).Error
	})
}

func (r *KnowledgeRepository) LatestVersion(ctx context.Context, documentID string) (string, error) {
	var doc knowledge.Document
	err := scopedKnowledgeDocuments(ctx, r.db.WithContext(ctx)).First(&doc, "document_id = ? AND is_latest = ?", documentID, true).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", knowledge.ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return doc.Version, nil
}

func (r *KnowledgeRepository) NextMinorVersion(ctx context.Context, documentID string) (string, error) {
	var versions []string
	if err := scopedKnowledgeDocuments(ctx, r.db.WithContext(ctx)).
		Model(&knowledge.Document{}).
		Where("document_id = ?", documentID).
		Pluck("version", &versions).Error; err != nil {
		return "", err
	}
	if len(versions) == 0 {
		return "", knowledge.ErrNotFound
	}
	sort.Slice(versions, func(i, j int) bool {
		return versionParts(versions[i]) < versionParts(versions[j])
	})
	latest := versions[len(versions)-1]
	major, minor := parseVersion(latest)
	if major <= 0 {
		return "", fmt.Errorf("cannot increment malformed version %q", latest)
	}
	return fmt.Sprintf("v%d.%d", major, minor+1), nil
}

func (r *KnowledgeRepository) getTx(ctx context.Context, tx *gorm.DB, documentID, version string) (*knowledge.Document, error) {
	var doc knowledge.Document
	err := scopedKnowledgeDocuments(ctx, tx).First(&doc, "document_id = ? AND version = ?", documentID, version).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, knowledge.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

func (r *KnowledgeRepository) scopedChunks(ctx context.Context, q *gorm.DB) *gorm.DB {
	if workspaceID, ok := knowledge.WorkspaceFromContext(ctx); ok {
		return q.Where("EXISTS (SELECT 1 FROM documents d WHERE d.document_id = document_chunks.document_id AND d.version = document_chunks.version AND d.workspace_id = ?)", workspaceID)
	}
	return q
}

var versionNumRE = regexp.MustCompile(`^v(\d+)(?:\.(\d+))?$`)

func parseVersion(v string) (int, int) {
	m := versionNumRE.FindStringSubmatch(strings.TrimSpace(v))
	if len(m) == 0 {
		return 0, 0
	}
	major, _ := strconv.Atoi(m[1])
	minor := 0
	if len(m) > 2 && m[2] != "" {
		minor, _ = strconv.Atoi(m[2])
	}
	return major, minor
}

func versionParts(v string) int {
	major, minor := parseVersion(v)
	return major*100000 + minor
}
