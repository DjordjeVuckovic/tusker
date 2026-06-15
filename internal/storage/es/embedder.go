package es

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/DjordjeVuckovic/tusker/internal/embedding"
	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/esutil"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types"
	"github.com/google/uuid"
)

// EmbeddingDims is the vector width produced by the embedding model and declared
// on the article index mapping. Must match the loaded embeddings file.
const EmbeddingDims = 1024

// Embedder is the Elasticsearch implementation of storage.EmbedIndexer. Unlike
// Postgres (a separate article_embeddings table), it stores the vector as a
// dense_vector field on the existing article document, keyed by _id == article
// UUID. Writes are partial updates, so they are idempotent and re-runnable;
// updates targeting a non-existent article (orphan) are skipped, not fatal.
type Embedder struct {
	client    *elasticsearch.TypedClient
	indexName string
}

func NewEmbedder(ctx context.Context, config ClientConfig) (*Embedder, error) {
	client, err := newClient(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Elasticsearch client: %w", err)
	}

	e := &Embedder{client: client, indexName: config.IndexName}
	if err := e.ensureEmbeddingField(ctx); err != nil {
		return nil, err
	}
	return e, nil
}

// embeddingUpdate is the partial-update body: {"doc": {"embedding": [...], ...}}.
type embeddingUpdate struct {
	Doc embeddingDoc `json:"doc"`
}

type embeddingDoc struct {
	Embedding []float32 `json:"embedding"`
	Model     string    `json:"embedding_model"`
}

func (e *Embedder) Save(ctx context.Context, vec *embedding.Vec) (uuid.UUID, error) {
	if err := e.SaveBulk(ctx, []*embedding.Vec{vec}); err != nil {
		return uuid.Nil, err
	}
	return vec.ID, nil
}

func (e *Embedder) SaveBulk(ctx context.Context, vecs []*embedding.Vec) error {
	if len(vecs) == 0 {
		return nil
	}

	bi, err := esutil.NewBulkIndexer(esutil.BulkIndexerConfig{
		Index:         e.indexName,
		Client:        e.client,
		NumWorkers:    4,
		FlushBytes:    5e+6,
		FlushInterval: 30 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("failed to create bulk indexer: %w", err)
	}

	var orphans, otherFailures int64
	for _, vec := range vecs {
		body, err := json.Marshal(embeddingUpdate{Doc: embeddingDoc{
			Embedding: vec.Embedding,
			Model:     vec.Model,
		}})
		if err != nil {
			slog.Error("failed to marshal embedding update", "error", err, "id", vec.ID)
			continue
		}

		err = bi.Add(ctx, esutil.BulkIndexerItem{
			Action:     "update",
			DocumentID: vec.ID.String(),
			Body:       bytes.NewReader(body),
			OnFailure: func(_ context.Context, item esutil.BulkIndexerItem, res esutil.BulkIndexerResponseItem, err error) {
				// 404 == no such article (orphan): expected, skip like Postgres.
				if err == nil && res.Status == 404 {
					atomic.AddInt64(&orphans, 1)
					return
				}
				atomic.AddInt64(&otherFailures, 1)
				if err != nil {
					slog.Error("bulk embedding update error", "error", err, "id", item.DocumentID)
				} else {
					slog.Error("bulk embedding update error", "status", res.Status, "type", res.Error.Type, "reason", res.Error.Reason, "id", item.DocumentID)
				}
			},
		})
		if err != nil {
			slog.Error("failed to add embedding to bulk indexer", "error", err, "id", vec.ID)
		}
	}

	if err := bi.Close(ctx); err != nil {
		return fmt.Errorf("failed to close bulk indexer: %w", err)
	}

	stats := bi.Stats()
	if orphans > 0 {
		slog.Warn("skipped embeddings with no matching article", "skipped", orphans, "updated", stats.NumUpdated)
	}
	if otherFailures > 0 {
		return fmt.Errorf("failed to update %d embeddings", otherFailures)
	}
	return nil
}

// ensureEmbeddingField adds the dense_vector field to an existing index when it
// is missing (PUT mapping is a no-op when the field already matches). New
// indices already get it via Indexer.EnsureIndex.
func (e *Embedder) ensureEmbeddingField(ctx context.Context) error {
	builder := NewIndexBuilder()
	props := map[string]types.Property{
		"embedding":       builder.denseVectorProperty(),
		"embedding_model": types.NewKeywordProperty(),
	}
	if _, err := e.client.Indices.PutMapping(e.indexName).Properties(props).Do(ctx); err != nil {
		return fmt.Errorf("failed to add embedding field to index %q: %w", e.indexName, err)
	}
	return nil
}
