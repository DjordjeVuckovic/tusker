package factory

import (
	"context"
	"fmt"

	"github.com/DjordjeVuckovic/tusker/internal/embedding"
	"github.com/DjordjeVuckovic/tusker/internal/storage"
	"github.com/DjordjeVuckovic/tusker/internal/storage/es"
	"github.com/DjordjeVuckovic/tusker/internal/storage/in_mem"
	"github.com/DjordjeVuckovic/tusker/internal/storage/pg"
	"github.com/DjordjeVuckovic/tusker/internal/storage/pg/native"
)

// TODO: accept pool as param and reuse it across indexer and searcher when using PG, to avoid creating multiple pools

// NewIndexer creates a new storage.Indexer based on the storage type
func NewIndexer(ctx context.Context, cfg StorageConfig) (storage.Indexer, error) {
	switch cfg.Type {
	case storage.PG:
		pgConfig := *cfg.Pg

		pool, err := pg.NewConnectionPool(ctx, pgConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create PostgreSQL connection pool: %w", err)
		}

		return pg.NewIndexer(pool)

	case storage.ES:
		esConfig := *cfg.Es

		return es.NewIndexer(ctx, esConfig)

	case storage.Solr:
		return nil, fmt.Errorf("solr storer not yet implemented")

	case storage.InMem:
		return in_mem.NewInMemIndexer(), nil

	default:
		return nil, fmt.Errorf(string(storage.ErrUnsupportedStorer), cfg.Type)
	}
}

func NewEmbedderIndexer(ctx context.Context, cfg StorageConfig) (storage.EmbedIndexer, error) {
	switch cfg.Type {
	case storage.PG:
		pgConfig := pg.PoolConfig{
			ConnStr:          cfg.Pg.ConnStr,
			RegisterVecTypes: true,
		}

		pool, err := pg.NewConnectionPool(ctx, pgConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create PostgreSQL connection pool: %w", err)
		}

		return pg.NewEmbedder(pool), nil

	case storage.ES:
		if cfg.Es == nil {
			return nil, fmt.Errorf("elasticsearch config is not set")
		}
		return es.NewEmbedder(ctx, *cfg.Es)
	}
	return nil, fmt.Errorf(string(storage.ErrUnsupportedStorer), cfg.Type)
}

// NewSearcher creates a new storage.FtsSearcher based on the storage type
func NewSearcher(ctx context.Context, cfg StorageConfig) (storage.FtsSearcher, error) {
	switch cfg.Type {
	case storage.PG:
		pgConfig := *cfg.Pg

		pool, err := pg.NewConnectionPool(ctx, pgConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create PostgreSQL connection pool: %w", err)
		}

		return native.NewReader(pool)

	case storage.ES:
		esConfig := *cfg.Es

		return es.NewSearcher(esConfig)

	case storage.Solr:
		return nil, fmt.Errorf("solr reader not yet implemented")

	case storage.InMem:
		// TODO: Implement InMem when needed
		return nil, fmt.Errorf("inmem reader not yet implemented")

	default:
		return nil, fmt.Errorf(string(storage.ErrUnsupportedStorer), cfg.Type)
	}
}

func NewReader(ctx context.Context, cfg StorageConfig) (storage.Reader, error) {
	if cfg.Type != storage.PG {
		return nil, fmt.Errorf("reader not supported for storage type %s", cfg.Type)
	}

	pool, err := pg.NewConnectionPool(ctx, *cfg.Pg)
	if err != nil {
		return nil, fmt.Errorf("failed to create PostgreSQL connection pool: %w", err)
	}

	return pg.NewArticleReader(pool), nil
}

func NewSemanticSearcher(ctx context.Context, cfg StorageConfig, client embedding.Client) (storage.SemanticSearcher, error) {
	switch cfg.Type {
	case storage.PG:
		pgConfig := pg.PoolConfig{
			ConnStr:          cfg.Pg.ConnStr,
			RegisterVecTypes: true,
		}

		pool, err := pg.NewConnectionPool(ctx, pgConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create PostgreSQL connection pool: %w", err)
		}

		embedder := embedding.NewEmbedder(client, embedding.WithExecutorMaxLength(1024))

		return pg.NewSemanticSearcher(embedder, pool), nil

	case storage.ES:
		if cfg.Es == nil {
			return nil, fmt.Errorf("elasticsearch config is not set")
		}

		model := embedding.DefaultModel
		embedder := embedding.NewEmbedder(client,
			embedding.WithExecutorMaxLength(1024),
			embedding.WithExecutorModel(model),
		)

		return es.NewSemanticSearcher(*cfg.Es, embedder, model)

	case storage.Solr:
		return nil, fmt.Errorf("solr semantic searcher not yet implemented")

	case storage.InMem:
		return nil, fmt.Errorf("inmem semantic searcher not yet implemented")

	default:
		return nil, fmt.Errorf(string(storage.ErrUnsupportedStorer), cfg.Type)
	}
}

func NewHybridSearcher(ctx context.Context, cfg StorageConfig, client embedding.Client) (storage.HybridSearcher, error) {
	switch cfg.Type {
	case storage.PG:
		pgConfig := pg.PoolConfig{
			ConnStr:          cfg.Pg.ConnStr,
			RegisterVecTypes: true,
		}

		pool, err := pg.NewConnectionPool(ctx, pgConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create PostgreSQL connection pool: %w", err)
		}

		embedder := embedding.NewEmbedder(client, embedding.WithExecutorMaxLength(1024))

		return pg.NewHybridSearcher(embedder, pool), nil

	case storage.ES:
		if cfg.Es == nil {
			return nil, fmt.Errorf("elasticsearch config is not set")
		}

		model := embedding.DefaultModel
		embedder := embedding.NewEmbedder(client,
			embedding.WithExecutorMaxLength(1024),
			embedding.WithExecutorModel(model),
		)

		return es.NewHybridSearcher(*cfg.Es, embedder, model)

	case storage.Solr:
		return nil, fmt.Errorf("solr hybrid searcher not yet implemented")

	case storage.InMem:
		return nil, fmt.Errorf("inmem hybrid searcher not yet implemented")

	default:
		return nil, fmt.Errorf(string(storage.ErrUnsupportedStorer), cfg.Type)
	}
}
