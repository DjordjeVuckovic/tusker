package storage

import (
	"context"

	"github.com/google/uuid"
)

// VectorStore provides embedding vectors for benchmarking and semantic search.
// It is engine-agnostic, mirroring Reader / FtsSearcher: the query is embedded
// at runtime, while document vectors are read from whatever store already holds
// them (Postgres today, Elasticsearch later).
type VectorStore interface {
	// QueryVector embeds query text into a vector using the store's configured
	// model — the same model the stored document vectors were produced with.
	QueryVector(ctx context.Context, text string) ([]float32, error)

	// DocVectors returns the stored embedding for each of the given article ids.
	// Ids without a stored vector are simply absent from the map — that is not
	// an error (the caller decides how to treat un-embedded documents).
	DocVectors(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID][]float32, error)
}

// HNSW graph-construction parameters. Every engine builds its vector index with
// these, so a recall difference between two engines is a property of the engine
// and not of the graph it was handed. Left undeclared the two disagree: pgvector
// defaults ef_construction to 64, Elasticsearch to 100.
//
// The Postgres side is SQL, so it repeats the values in the CREATE INDEX of
// db/migrations, db/parade_migrations and db/tiger_migrations rather than
// importing them; the container tests on both sides assert against these
// constants to keep the copies honest.
//
// Changing either value is a new run — the graph is rebuilt, so no previously
// recorded number is comparable with one measured after the change.
const (
	HNSWM              = 16
	HNSWEfConstruction = 100
)
