# news_semantic — Semantic / Vector Search Track

Evaluates concept-level retrieval accuracy across:

- **pgvector-cosine**: PostgreSQL `pgvector` extension with HNSW index, cosine distance (`<=>`)
- **pgvector-exact**: Same query with the index turned off, so it returns the true nearest
  neighbours. Ground truth for pgvector-cosine's recall.
- **elasticsearch**: ES `knn` query on `dense_vector` field (cosine similarity)

Cosine is the only distance measured. The corpus vectors are L2-normalised, and on unit
vectors `<->` and `<#>` rank identically to `<=>`, so a second operator would cost an index
without producing a different ranking.

Queries are designed to require semantic understanding beyond keyword overlap — relevant documents may not contain the exact query terms.

## Prerequisites

```sql
-- PostgreSQL — owned by db/migrations, repeated here because the build point is
-- part of what this track measures.
CREATE EXTENSION IF NOT EXISTS vector;
CREATE INDEX idx_article_embedding ON article_embeddings
  USING hnsw (embedding vector_cosine_ops)
  WITH (m = 16, ef_construction = 100);

-- Elasticsearch — built by internal/storage/es at the same point.
-- "embedding": {
--   "type": "dense_vector", "dims": 1024, "index": true, "similarity": "cosine",
--   "index_options": { "type": "hnsw", "m": 16, "ef_construction": 100 }
-- }
```

Both graphs are built at the same `m` and `ef_construction`. Left to the products they would
not be — pgvector builds at `ef_construction = 64` and Elasticsearch at `100` — and part of any
recall gap between the two engines would be that parameter rather than the engine. Changing
either value rebuilds the graph, so it is a new run and earlier numbers no longer compare.

## Embedding generation

Query embeddings must be pre-computed before running `bench pool`:

```bash
# Generate embeddings for all suite queries using OpenAI text-embedding-3-small
python scripts/embed_queries.py \
  --suite tracks/global-news-dataset/news_semantic/suite.yaml \
  --model text-embedding-3-small \
  --output tracks/global-news-dataset/news_semantic/trec/query_embeddings.json

# Embed the article corpus (one-time)
python scripts/embed_corpus.py \
  --pg "postgresql://news_user:news_password@localhost:54320/news_db"
```

The `{{precomputed}}` placeholder in SQL/JSON queries is replaced by the float array from `query_embeddings.json` at run time by the bench executor.

## Pipeline

```bash
bench pool     news_semantic
bench judge    news_semantic --strategy claude-api   # lexical alone is inadequate for semantic
bench run      news_semantic
bench export   news_semantic --format html
```

## Why LLM judgments for semantic search?

Lexical overlap is a poor judge of semantic relevance — an article about "Fed raises rates to fight inflation" is semantically relevant to "economic crisis consequences" but shares no key terms. Use `--strategy claude-api` to get accurate relevance grades.
