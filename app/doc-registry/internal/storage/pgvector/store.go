package pgvector

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/specgate/doc-registry/internal/knowledge"
)

// Store is a pgvector-backed VectorStore. The knowledge_chunks table is created
// inline by EnsureCollection (idempotent DDL, no migration file required).
type Store struct {
	pool *pgxpool.Pool
	dim  int
}

// New creates a Store connected to dsn. dim is the vector dimension; defaults
// to 1024 when <= 0. Call EnsureCollection before Upsert/Search.
func New(ctx context.Context, dsn string, dim int) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("pgvector: connect: %w", err)
	}
	if dim <= 0 {
		dim = 1024
	}
	return &Store{pool: pool, dim: dim}, nil
}

// Close releases the connection pool. Call on shutdown to avoid leaking
// Postgres connections across restarts.
func (s *Store) Close() {
	if s.pool != nil {
		s.pool.Close()
	}
}

// EnsureCollection creates the pgvector extension and the knowledge_chunks table
// if they do not already exist. Safe to call multiple times (idempotent).
func (s *Store) EnsureCollection(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS vector`)
	if err != nil {
		return fmt.Errorf("pgvector: ensure extension: %w", err)
	}
	_, err = s.pool.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS knowledge_chunks (
			id      TEXT PRIMARY KEY,
			embedding vector(%d),
			payload JSONB NOT NULL DEFAULT '{}'
		)`, s.dim))
	if err != nil {
		return fmt.Errorf("pgvector: ensure table: %w", err)
	}
	return nil
}

// Upsert inserts or replaces the given vector points. per spec §knowledge/vector-store.
func (s *Store) Upsert(ctx context.Context, points []knowledge.VectorPoint) error {
	if len(points) == 0 {
		return nil
	}
	for _, p := range points {
		payload, err := json.Marshal(p.Payload)
		if err != nil {
			return fmt.Errorf("pgvector: marshal payload: %w", err)
		}
		_, err = s.pool.Exec(ctx,
			`INSERT INTO knowledge_chunks (id, embedding, payload)
             VALUES ($1, $2::vector, $3::jsonb)
             ON CONFLICT (id) DO UPDATE SET embedding = EXCLUDED.embedding, payload = EXCLUDED.payload`,
			p.ID, vecLiteral(p.Vector), payload)
		if err != nil {
			return fmt.Errorf("pgvector: upsert %s: %w", p.ID, err)
		}
	}
	return nil
}

// DeleteVersion removes all chunks whose payload carries the given document_id and version.
func (s *Store) DeleteVersion(ctx context.Context, documentID, version string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM knowledge_chunks
         WHERE payload->>'document_id' = $1 AND payload->>'version' = $2`,
		documentID, version)
	return err
}

// Search performs a cosine-distance nearest-neighbour search, applying optional
// payload filters (linked_feature_id, linked_request_id, document_type,
// authority_level). LatestOnly is accepted but not applied at the DB layer
// (callers filter post-query). Returns up to
// limit*4 rows ordered by cosine distance; score = 1 - distance.
func (s *Store) Search(ctx context.Context, in knowledge.VectorSearch) ([]knowledge.VectorResult, error) {
	limit := in.Limit
	if limit <= 0 {
		limit = 8
	}

	where := []string{}
	args := []any{vecLiteral(in.Vector), limit * 4}
	idx := 3

	if in.LinkedFeature != "" {
		where = append(where, fmt.Sprintf("payload->>'linked_feature_id' = $%d", idx))
		args = append(args, in.LinkedFeature)
		idx++
	}
	if in.LinkedRequest != "" {
		where = append(where, fmt.Sprintf("payload->>'linked_request_id' = $%d", idx))
		args = append(args, in.LinkedRequest)
		idx++
	}
	if len(in.DocumentTypes) > 0 {
		where = append(where, fmt.Sprintf("payload->>'document_type' = ANY($%d)", idx))
		args = append(args, in.DocumentTypes)
		idx++
	}
	if len(in.Authorities) > 0 {
		where = append(where, fmt.Sprintf("payload->>'authority_level' = ANY($%d)", idx))
		args = append(args, in.Authorities)
		idx++
	}
	_ = idx // suppress "declared and not used" if no filters added

	q := `SELECT id, 1 - (embedding <=> $1::vector) AS score, payload
          FROM knowledge_chunks`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY embedding <=> $1::vector LIMIT $2"

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("pgvector: search: %w", err)
	}
	defer rows.Close()

	var results []knowledge.VectorResult
	for rows.Next() {
		var r knowledge.VectorResult
		var payload []byte
		if err := rows.Scan(&r.ID, &r.Score, &payload); err != nil {
			return nil, fmt.Errorf("pgvector: scan: %w", err)
		}
		if err := json.Unmarshal(payload, &r.Payload); err != nil {
			return nil, fmt.Errorf("pgvector: unmarshal payload: %w", err)
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// vecLiteral encodes a float32 slice as a pgvector literal, e.g. "[1,0,0,0]".
func vecLiteral(v []float32) string {
	b := make([]string, len(v))
	for i, f := range v {
		b[i] = strconv.FormatFloat(float64(f), 'f', -1, 32)
	}
	return "[" + strings.Join(b, ",") + "]"
}
