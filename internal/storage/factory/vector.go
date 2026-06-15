package factory

import (
	"context"
	"fmt"

	"github.com/DjordjeVuckovic/tusker/internal/embedding"
	"github.com/DjordjeVuckovic/tusker/internal/storage"
	"github.com/DjordjeVuckovic/tusker/internal/storage/es"
	"github.com/DjordjeVuckovic/tusker/internal/storage/pg"
)

// VectorStoreConfig selects and configures a storage.VectorStore. Postgres
// takes precedence: when a PG connection is available the PG store is used;
// Elasticsearch is the fallback when only an ES backend is configured.
type VectorStoreConfig struct {
	PgConnStr string
	Es        *es.ClientConfig

	// EmbeddingClient embeds query text (the document vectors come from the
	// store). Required for the PG store.
	EmbeddingClient embedding.Client
	// Model is the embedding model name; doc vectors are filtered by it so query
	// and document vectors stay comparable. Defaults to the embedder default.
	Model string
}

// NewVectorStore builds a VectorStore, preferring Postgres over Elasticsearch.
func NewVectorStore(ctx context.Context, cfg VectorStoreConfig) (storage.VectorStore, error) {
	switch {
	case cfg.PgConnStr != "":
		if cfg.EmbeddingClient == nil {
			return nil, fmt.Errorf("vector store: embedding client is required for query embedding")
		}
		pool, err := pg.NewConnectionPool(ctx, pg.PoolConfig{
			ConnStr:          cfg.PgConnStr,
			RegisterVecTypes: true,
		})
		if err != nil {
			return nil, fmt.Errorf("vector store: create PostgreSQL pool: %w", err)
		}
		// Query embedding and stored doc vectors must use the same model.
		model := cfg.Model
		if model == "" {
			model = embedding.DefaultModel
		}
		embedder := embedding.NewEmbedder(cfg.EmbeddingClient,
			embedding.WithExecutorMaxLength(1024),
			embedding.WithExecutorModel(model),
		)
		return pg.NewVectorStore(embedder, pool, model), nil

	case cfg.Es != nil:
		if cfg.EmbeddingClient == nil {
			return nil, fmt.Errorf("vector store: embedding client is required for query embedding")
		}
		model := cfg.Model
		if model == "" {
			model = embedding.DefaultModel
		}
		embedder := embedding.NewEmbedder(cfg.EmbeddingClient,
			embedding.WithExecutorMaxLength(1024),
			embedding.WithExecutorModel(model),
		)
		return es.NewVectorStore(*cfg.Es, embedder, model)

	default:
		return nil, fmt.Errorf("vector store: no Postgres or Elasticsearch backend configured")
	}
}
