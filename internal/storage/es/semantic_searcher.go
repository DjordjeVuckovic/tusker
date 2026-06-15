package es

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/DjordjeVuckovic/tusker/internal/api/dto"
	"github.com/DjordjeVuckovic/tusker/internal/embedding"
	"github.com/DjordjeVuckovic/tusker/internal/storage"
	dquery "github.com/DjordjeVuckovic/tusker/internal/types/query"
	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types"
	"github.com/google/uuid"
)

const minNumCandidates = 100

// defaultThreshold is the distance ceiling applied when no threshold is set,
// matching the PG semantic searcher.
const defaultThreshold = 0.7

type SemanticSearcher struct {
	client    *elasticsearch.TypedClient
	indexName string
	embedder  *embedding.Embedder
	model     string
}

func NewSemanticSearcher(config ClientConfig, embedder *embedding.Embedder, model string) (*SemanticSearcher, error) {
	client, err := newClient(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Elasticsearch client: %w", err)
	}
	return &SemanticSearcher{
		client:    client,
		indexName: config.IndexName,
		embedder:  embedder,
		model:     model,
	}, nil
}

func (s *SemanticSearcher) SearchSemantic(ctx context.Context, query *dquery.Semantic, baseOpts *dquery.BaseOptions) (*storage.VectorSearchResult, error) {
	vec, err := s.embedder.EmbedQuery(ctx, query.Query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}

	size := baseOpts.Size
	k := size
	numCandidates := k * 10
	if numCandidates < minNumCandidates {
		numCandidates = minNumCandidates
	}

	threshold := query.Threshold
	if threshold == 0 {
		threshold = defaultThreshold
	}

	// ES kNN filters by minimum cosine similarity (1 - distance), so map the
	// distance threshold to a similarity floor.
	sim := float32(1 - threshold)
	knn := types.KnnSearch{
		Field:         "embedding",
		QueryVector:   vec.Embedding,
		K:             &k,
		NumCandidates: &numCandidates,
		Similarity:    &sim,
	}

	slog.Info("Executing es semantic kNN search",
		"query", query.Query,
		"model", s.model,
		"k", k,
		"num_candidates", numCandidates,
		"size", size)

	res, err := s.client.Search().
		Index(s.indexName).
		Knn(knn).
		SourceIncludes_(
			"id", "title", "subtitle", "content", "description",
			"author", "url", "language", "created_at",
			"source_id", "source_name", "published_at", "category", "imported_at",
		).
		Size(size).
		Do(ctx)
	if err != nil {
		slog.Error("Elasticsearch kNN query failed", "error", err, "query", query.Query)
		return nil, fmt.Errorf("failed to execute semantic search: %w", err)
	}

	hits, err := s.mapToArticles(res.Hits.Hits)
	if err != nil {
		return nil, fmt.Errorf("failed to map semantic search results: %w", err)
	}

	slog.Info("ES semantic search results fetched",
		"total_matches", res.Hits.Total.Value,
		"returned_count", len(hits))

	// kNN returns a single page (parity with PG): no cursor follow-up.
	return &storage.VectorSearchResult{
		Hits:       hits,
		NextCursor: nil,
		HasMore:    false,
	}, nil
}

func (s *SemanticSearcher) mapToArticles(hits []types.Hit) ([]dto.Article, error) {
	articles := make([]dto.Article, 0, len(hits))
	for _, hit := range hits {
		var doc ArticleDocument
		if err := json.Unmarshal(hit.Source_, &doc); err != nil {
			return nil, fmt.Errorf("failed to unmarshal document: %w", err)
		}

		id, err := uuid.Parse(doc.ID)
		if err != nil {
			return nil, fmt.Errorf("parse document id %q: %w", doc.ID, err)
		}

		articles = append(articles, dto.Article{
			ID:          id,
			Title:       doc.Title,
			Subtitle:    doc.Subtitle,
			Content:     doc.Content,
			Author:      doc.Author,
			Description: doc.Description,
			URL:         doc.URL,
			Language:    doc.Language,
			CreatedAt:   doc.CreatedAt,
			Metadata: dto.ArticleMetadata{
				SourceId:    doc.SourceId,
				SourceName:  doc.SourceName,
				PublishedAt: doc.PublishedAt,
				Category:    doc.Category,
				ImportedAt:  doc.ImportedAt,
			},
		})
	}
	return articles, nil
}

var _ storage.SemanticSearcher = (*SemanticSearcher)(nil)
