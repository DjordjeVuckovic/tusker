# news_hybrid — Hybrid Search Track

Evaluates Reciprocal Rank Fusion (RRF) of BM25 + vector signals across:

- **pg-rrf**: PostgreSQL CTE-based RRF (FTS via GIN + vector via pgvector HNSW)
- **paradedb-hybrid**: ParadeDB hybrid query (Tantivy BM25 + pgvector)
- **elasticsearch**: ES hybrid query (`bool.should` + `knn`)

## RRF formula

```
score(d) = Σ_i  1 / (k + rank_i(d))     k = 60 (standard constant)
```

Both sub-rankers produce a ranked list; RRF combines them without needing score calibration.

## Prerequisites

See `news_semantic/README.md` for pgvector and embedding setup — identical requirements apply here.

```sql
-- ParadeDB instance also needs vector extension
CREATE EXTENSION IF NOT EXISTS vector;
ALTER TABLE articles ADD COLUMN IF NOT EXISTS embedding vector(1536);
```

## Query design

Each hybrid query has:
1. A keyword anchor (term that FTS can match precisely)
2. A semantic intent (concept that benefits from embedding similarity)

This tests whether hybrid fusion gives better results than either signal alone.

## Pipeline

```bash
# Pre-compute embeddings first (same as news_semantic)
python scripts/embed_queries.py --suite tracks/news_hybrid/suite.yaml \
  --output tracks/news_hybrid/trec/query_embeddings.json

bench pool     news_hybrid
bench judge    news_hybrid --strategy claude-api
bench run      news_hybrid
bench export   news_hybrid --format html

# Compare against pure FTS
bench diff news_hybrid --a <fts_run_id> --b <hybrid_run_id>
```
