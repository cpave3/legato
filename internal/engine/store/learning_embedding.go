package store

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
)

func (s *Store) LearningEmbeddingCurrent(ctx context.Context, learningID string, revision int, digest, model string) (bool, error) {
	var count int
	err := s.db.GetContext(ctx, &count, `SELECT COUNT(*) FROM learning_embeddings WHERE learning_id = ? AND revision = ? AND content_digest = ? AND model = ? AND status = 'indexed'`, learningID, revision, digest, model)
	return count == 1, err
}

func (s *Store) UpsertLearningEmbedding(ctx context.Context, embedding LearningEmbedding) error {
	encoded, err := json.Marshal(embedding.Vector)
	if err != nil {
		return err
	}
	embedding.VectorJSON = string(encoded)
	_, err = s.db.NamedExecContext(ctx, `INSERT INTO learning_embeddings
		(learning_id, revision, content_digest, provider, model, dimensions, vector_json, status, error)
		VALUES (:learning_id, :revision, :content_digest, :provider, :model, :dimensions, :vector_json, :status, '')
		ON CONFLICT(learning_id) DO UPDATE SET revision=excluded.revision, content_digest=excluded.content_digest,
		provider=excluded.provider, model=excluded.model, dimensions=excluded.dimensions, vector_json=excluded.vector_json,
		status=excluded.status, error='', updated_at=datetime('now')`, embedding)
	return err
}

func (s *Store) MarkLearningEmbeddingFailed(ctx context.Context, learningID string, revision int, digest, model, message string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO learning_embeddings
		(learning_id, revision, content_digest, model, dimensions, vector_json, status, error)
		VALUES (?, ?, ?, ?, 0, '[]', 'failed', ?)
		ON CONFLICT(learning_id) DO UPDATE SET revision=excluded.revision, content_digest=excluded.content_digest,
		model=excluded.model, dimensions=0, vector_json='[]', status='failed', error=excluded.error, updated_at=datetime('now')`,
		learningID, revision, digest, model, message)
	return err
}

func (s *Store) SearchLearningEmbeddings(ctx context.Context, query []float32, model string, filter LearningQuery, limit int) ([]SemanticLearningResult, error) {
	where, args := learningFilters(filter, "l.")
	clause := " WHERE e.status = 'indexed' AND e.model = ? AND e.dimensions = ?"
	if where != "" {
		clause += " AND " + where[len(" WHERE "):]
	}
	args = append([]any{model, len(query)}, args...)
	var rows []struct {
		Learning
		VectorJSON string `db:"vector_json"`
	}
	if err := s.db.SelectContext(ctx, &rows, `SELECT l.*, e.vector_json FROM learning_embeddings e JOIN learnings l ON l.id=e.learning_id`+clause, args...); err != nil {
		return nil, fmt.Errorf("search learning embeddings: %w", err)
	}
	results := make([]SemanticLearningResult, 0, len(rows))
	for _, row := range rows {
		var vector []float32
		if err := json.Unmarshal([]byte(row.VectorJSON), &vector); err != nil {
			return nil, err
		}
		results = append(results, SemanticLearningResult{Learning: row.Learning, Score: cosine(query, vector)})
	}
	sort.SliceStable(results, func(i, j int) bool { return results[i].Score > results[j].Score })
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	learnings := make([]Learning, len(results))
	for i := range results {
		learnings[i] = results[i].Learning
	}
	if err := s.hydrateLearningTags(ctx, learnings); err != nil {
		return nil, err
	}
	for i := range results {
		results[i].Learning = learnings[i]
	}
	return results, nil
}

func cosine(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, aa, bb float64
	for i := range a {
		x, y := float64(a[i]), float64(b[i])
		dot += x * y
		aa += x * x
		bb += y * y
	}
	if aa == 0 || bb == 0 {
		return 0
	}
	return dot / (math.Sqrt(aa) * math.Sqrt(bb))
}
