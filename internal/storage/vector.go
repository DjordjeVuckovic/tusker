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

// HNSW build parameters, declared so both engines index at the same point
// rather than at their own defaults (pgvector 64, Elasticsearch 100). The
// CREATE INDEX in db/*migrations repeats them; the container tests check both
// engines against these. Changing either is a new run.
const (
	HNSWM              = 16
	HNSWEfConstruction = 64
)
