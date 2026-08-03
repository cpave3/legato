package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

// EmbeddingProvider is the provider-neutral document/query embedding boundary.
type EmbeddingProvider interface {
	Embed(context.Context, string) ([]float32, error)
	Model() string
}

// SemanticIndex isolates vector persistence and retrieval from learning logic.
// A PostgreSQL implementation can map this contract to pgvector.
type SemanticIndex interface {
	LearningEmbeddingCurrent(context.Context, string, int, string, string) (bool, error)
	UpsertLearningEmbedding(context.Context, store.LearningEmbedding) error
	MarkLearningEmbeddingFailed(context.Context, string, int, string, string, string) error
	SearchLearningEmbeddings(context.Context, []float32, string, store.LearningQuery, int) ([]store.SemanticLearningResult, error)
}

type LearningSearchFilter struct {
	LearningFilter
	MinScore float64 `json:"min_score,omitempty"`
}

type LearningSearchItem struct {
	store.Learning
	LexicalRank    int     `json:"lexical_rank,omitempty"`
	SemanticRank   int     `json:"semantic_rank,omitempty"`
	SemanticScore  float64 `json:"semantic_score,omitempty"`
	RelevanceScore float64 `json:"relevance_score"`
}

type LearningSearchResult struct {
	Items          []LearningSearchItem `json:"items"`
	Mode           string               `json:"mode"`
	SemanticStatus string               `json:"semantic_status"`
	Fallback       string               `json:"fallback,omitempty"`
}

func NewLearningServiceWithEmbeddings(s *store.Store, bus *events.Bus, provider EmbeddingProvider, index SemanticIndex) *LearningService {
	svc := NewLearningService(s, bus)
	svc.embedder = provider
	svc.semantic = index
	return svc
}

func (s *LearningService) ConfigureEmbeddings(provider EmbeddingProvider, index SemanticIndex) {
	s.embedder, s.semantic = provider, index
}

func learningEmbeddingText(learning store.Learning) string {
	return strings.TrimSpace(strings.Join([]string{learning.Claim, learning.Rationale, learning.Category, strings.Join(learning.Tags, " ")}, "\n"))
}

func (s *LearningService) indexLearning(ctx context.Context, learning store.Learning) error {
	if s.embedder == nil || s.semantic == nil {
		return nil
	}
	text := learningEmbeddingText(learning)
	digest := sha256.Sum256([]byte(text))
	digestText := "sha256:" + hex.EncodeToString(digest[:])
	current, err := s.semantic.LearningEmbeddingCurrent(ctx, learning.ID, learning.Revision, digestText, s.embedder.Model())
	if err != nil {
		return err
	}
	if current {
		return nil
	}
	vector, err := s.embedder.Embed(ctx, text)
	if err != nil {
		_ = s.semantic.MarkLearningEmbeddingFailed(ctx, learning.ID, learning.Revision, digestText, s.embedder.Model(), err.Error())
		return err
	}
	if len(vector) == 0 {
		return fmt.Errorf("embedding provider returned an empty vector")
	}
	return s.semantic.UpsertLearningEmbedding(ctx, store.LearningEmbedding{LearningID: learning.ID, Revision: learning.Revision, ContentDigest: digestText, Model: s.embedder.Model(), Dimensions: len(vector), Vector: vector, Status: "indexed"})
}

func (s *LearningService) SearchKnowledge(ctx context.Context, text string, filter LearningSearchFilter) (*LearningSearchResult, error) {
	lexical, err := s.Search(ctx, text, filter.LearningFilter)
	if err != nil {
		return nil, err
	}
	result := &LearningSearchResult{Items: lexicalSearchItems(lexical), Mode: "lexical", SemanticStatus: "disabled"}
	if s.embedder == nil || s.semantic == nil {
		return result, nil
	}
	// Lazily backfill a bounded filtered set. Upserts are revision/digest aware,
	// so this is safe on repeated searches and after model changes.
	backfillFilter := filter.LearningFilter
	backfillFilter.Limit = max(filter.Limit, 50)
	candidates, listErr := s.List(ctx, backfillFilter)
	if listErr == nil {
		for _, candidate := range candidates {
			_ = s.indexLearning(ctx, candidate)
		}
	}
	queryVector, err := s.embedder.Embed(ctx, text)
	if err != nil {
		result.SemanticStatus = "failed"
		result.Fallback = err.Error()
		return result, nil
	}
	semantic, err := s.semantic.SearchLearningEmbeddings(ctx, queryVector, s.embedder.Model(), storeLearningQuery(filter.LearningFilter), max(filter.Limit, 50))
	if err != nil {
		result.SemanticStatus = "failed"
		result.Fallback = err.Error()
		return result, nil
	}
	result.Items = reciprocalRankFusion(lexical, semantic, filter.MinScore, learningLimit(filter.Limit))
	result.Mode, result.SemanticStatus = "hybrid", "available"
	return result, nil
}

func learningLimit(limit int) int {
	if limit <= 0 {
		return 50
	}
	if limit > 200 {
		return 200
	}
	return limit
}

func lexicalSearchItems(learnings []store.Learning) []LearningSearchItem {
	items := make([]LearningSearchItem, len(learnings))
	for i, learning := range learnings {
		items[i] = LearningSearchItem{Learning: learning, LexicalRank: i + 1, RelevanceScore: 1 / (60 + float64(i+1))}
	}
	return items
}

func reciprocalRankFusion(lexical []store.Learning, semantic []store.SemanticLearningResult, minScore float64, limit int) []LearningSearchItem {
	const k = 60.0
	type ranked struct {
		item          store.Learning
		score         float64
		order         int
		lexicalRank   int
		semanticRank  int
		semanticScore float64
	}
	byID := map[string]*ranked{}
	for i, item := range lexical {
		byID[item.ID] = &ranked{item: item, score: 1 / (k + float64(i+1)), order: i, lexicalRank: i + 1}
	}
	semanticRank := 0
	for _, item := range semantic {
		if item.Score < minScore {
			continue
		}
		semanticRank++
		r, ok := byID[item.Learning.ID]
		if !ok {
			r = &ranked{item: item.Learning, order: len(lexical) + semanticRank}
			byID[item.Learning.ID] = r
		}
		r.semanticRank, r.semanticScore = semanticRank, item.Score
		r.score += 1 / (k + float64(semanticRank))
	}
	all := make([]ranked, 0, len(byID))
	for _, item := range byID {
		all = append(all, *item)
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].score == all[j].score {
			return all[i].order < all[j].order
		}
		return all[i].score > all[j].score
	})
	if len(all) > limit {
		all = all[:limit]
	}
	out := make([]LearningSearchItem, len(all))
	for i := range all {
		out[i] = LearningSearchItem{Learning: all[i].item, LexicalRank: all[i].lexicalRank, SemanticRank: all[i].semanticRank, SemanticScore: all[i].semanticScore, RelevanceScore: all[i].score}
	}
	return out
}
