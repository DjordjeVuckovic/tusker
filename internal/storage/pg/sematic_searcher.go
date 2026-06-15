package pg

import (
	"context"
	"log/slog"

	"github.com/DjordjeVuckovic/tusker/internal/api/dto"
	"github.com/DjordjeVuckovic/tusker/internal/embedding"
	"github.com/DjordjeVuckovic/tusker/internal/storage"
	"github.com/DjordjeVuckovic/tusker/internal/types/query"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
)

const defaultThreshold = 0.7

type SemanticSearcher struct {
	embedder *embedding.Embedder
	db       *pgxpool.Pool
}

func NewSemanticSearcher(embedder *embedding.Embedder, pool *ConnectionPool) *SemanticSearcher {
	return &SemanticSearcher{
		embedder: embedder,
		db:       pool.GetConn(),
	}
}

func (s *SemanticSearcher) SearchSemantic(ctx context.Context, query *query.Semantic, baseOpts *query.BaseOptions) (*storage.VectorSearchResult, error) {
	vec, err := s.embedder.EmbedQuery(ctx, query.Query)
	if err != nil {
		return nil, err
	}
	vecEncoded := pgvector.NewVector(vec.Embedding)

	cmd := `
		SELECT a.id,
			   a.title,
			   a.subtitle,
			   a.content,
			   a.description,
			   a.author,
			   a.url,
			   a.language,
			   a.created_at,
			   a.metadata,
			   e.distance
		FROM (
			SELECT article_id,
				   embedding <=> $1 AS distance
			FROM article_embeddings
			WHERE embedding <=> $1 < $2
			ORDER BY embedding <=> $1
			LIMIT $3
		) e
		INNER JOIN articles a ON a.id = e.article_id
		ORDER BY e.distance;
	`

	threshold := query.Threshold
	if threshold == 0 {
		threshold = defaultThreshold
	}

	rows, err := s.db.Query(
		ctx,
		cmd,
		vecEncoded,
		threshold,
		baseOpts.Size,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hits []dto.Article
	for rows.Next() {
		article, dist, err := MapToArticle(rows)
		if err != nil {
			return nil, err
		}
		slog.Debug("Semantic search hit", "article_id", article.ID, "distance", dist)
		hits = append(hits, *article)
	}

	return &storage.VectorSearchResult{
		Hits:       hits,
		NextCursor: nil,
		HasMore:    false,
	}, nil
}
