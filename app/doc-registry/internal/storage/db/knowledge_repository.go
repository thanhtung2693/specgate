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

func (r *KnowledgeRepository) CreateVersion(ctx context.Context, doc *knowledge.Document, links []knowledge.Link) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&knowledge.Document{}).
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
	err := r.db.WithContext(ctx).
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
	q := r.db.WithContext(ctx).Model(&knowledge.Document{}).Preload(clause.Associations)
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
	if f.Limit > 0 {
		q = q.Limit(f.Limit)
	}
	if f.Offset > 0 {
		q = q.Offset(f.Offset)
	}
	var out []knowledge.Document
	return out, q.Order("updated_at DESC").Find(&out).Error
}

// ListByFeatureOrRequest returns all document versions (including non-latest)
// linked to the given feature or change request via linked_feature_id /
// linked_request_id. The WHERE clause is built dynamically: when featureID is
// empty the feature condition is omitted; when requestID is empty the request
// condition is omitted. Both empty returns an empty slice without a DB call.
// Results are ordered by authority priority then title (per spec §3).
func (r *KnowledgeRepository) ListByFeatureOrRequest(ctx context.Context, featureID, requestID string) ([]knowledge.Document, error) {
	featureID = strings.TrimSpace(featureID)
	requestID = strings.TrimSpace(requestID)
	if featureID == "" && requestID == "" {
		return []knowledge.Document{}, nil
	}
	q := r.db.WithContext(ctx).Model(&knowledge.Document{})
	switch {
	case featureID != "" && requestID != "":
		q = q.Where("linked_feature_id = ? OR linked_request_id = ?", featureID, requestID)
	case featureID != "":
		q = q.Where("linked_feature_id = ?", featureID)
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

func (r *KnowledgeRepository) History(ctx context.Context, documentID string) ([]knowledge.Document, error) {
	var out []knowledge.Document
	err := r.db.WithContext(ctx).
		Where("document_id = ?", documentID).
		Order("created_at ASC").
		Find(&out).Error
	return out, err
}

func (r *KnowledgeRepository) UpdateStatus(ctx context.Context, documentID, version string, status knowledge.Status, summary, errorMessage string) error {
	updates := map[string]any{
		"status":     status,
		"updated_at": time.Now().UTC(),
	}
	if summary != "" {
		updates["summary"] = summary
	}
	if errorMessage != "" {
		updates["error_message"] = errorMessage
	}
	res := r.db.WithContext(ctx).
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
		if err := tx.Delete(&knowledge.Chunk{}, "document_id = ? AND version = ?", documentID, version).Error; err != nil {
			return err
		}
		if len(chunks) == 0 {
			return nil
		}
		return tx.Create(&chunks).Error
	})
}

func (r *KnowledgeRepository) ChunkCount(ctx context.Context, documentID, version string) (int, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&knowledge.Chunk{}).
		Where("document_id = ? AND version = ?", documentID, version).
		Count(&count).Error
	return int(count), err
}

func (r *KnowledgeRepository) DeleteVersion(ctx context.Context, documentID, version string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var doc knowledge.Document
		err := tx.First(&doc, "document_id = ? AND version = ?", documentID, version).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return knowledge.ErrNotFound
		}
		if err != nil {
			return err
		}
		if err := tx.Delete(&knowledge.Document{}, "document_id = ? AND version = ?", documentID, version).Error; err != nil {
			return err
		}
		if !doc.IsLatest {
			return nil
		}
		var latest knowledge.Document
		err = tx.Where("document_id = ?", documentID).Order("created_at DESC").First(&latest).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		return tx.Model(&knowledge.Document{}).
			Where("document_id = ? AND version = ?", latest.DocumentID, latest.Version).
			Update("is_latest", true).Error
	})
}

func (r *KnowledgeRepository) LatestVersion(ctx context.Context, documentID string) (string, error) {
	var doc knowledge.Document
	err := r.db.WithContext(ctx).First(&doc, "document_id = ? AND is_latest = ?", documentID, true).Error
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
	if err := r.db.WithContext(ctx).
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
