# Embeddings

Document embeddings power the semantic and hybrid search tracks. There are two
ways to produce them, selected by `EMBEDDING_SOURCE`:

| `EMBEDDING_SOURCE` | How | Where |
|--------------------|-----|-------|
| `online` (default) | Generated inline during ingestion via Ollama | `datapipe load articles` (gated by `EMBEDDING_ENABLED=true`) |
| `file`             | Precomputed offline, loaded from an object store | `datapipe load embeddings` |
| `none`             | No embeddings | — |

The `file` path exists because embedding generation is a one-time, GPU-bound job
best delegated to Colab. See `scripts/embed_qwen3.ipynb`.

## Offline workflow (`file`)

```
Colab (Qwen3-Embedding-0.6B, last-token pool, L2-norm)
  → gl_news_embeddings.parquet
  → upload to S3-compatible store
  → datapipe load embeddings  → article_embeddings
```

**Order matters:** articles must already be ingested first —
`article_embeddings.article_id` is a foreign key to `articles.id`. Embeddings
whose `id` has no matching article are skipped (and logged), not fatal.

### Artifact format

Parquet with two columns:

| Column | Type | Description |
|--------|------|-------------|
| `id` | `string` | article UUID |
| `embedding` | `list<float32>` | 1024-dim, L2-normalised |

Plus file-level metadata read by the loader:

| Key | Purpose |
|-----|---------|
| `model` | stored as `article_embeddings.model_name`; must match the query-time model |
| `hf_model_id`, `dim`, `pooling`, `normalized`, `row_count`, `created_at` | provenance / validation |

The loader uses `model` from the file so document-side and query-side embeddings
always agree on `model_name`. Override with `EMBEDDING_MODEL` if needed.

### Running

```bash
cp cmd/datapipe/embeddings.env.example cmd/datapipe/embeddings.env   # then edit
go run ./cmd/datapipe load embeddings
```

Re-runnable: each batch is COPYed into a temp staging table, then upserted
(`ON CONFLICT (article_id, model_name) DO UPDATE`), so partial runs can be
repeated safely.

When loading from S3 the file is first downloaded to a temp file (via
`os.CreateTemp`, cleaned up on exit). The corpus embeddings file can be several
GB — ensure the system temp dir (`$TMPDIR`) has enough space, or set
`EMBEDDING_FILE_PATH` to point at a pre-downloaded local copy.

### Configuration

| Env | Description |
|-----|-------------|
| `EMBEDDING_SOURCE` | must be `file` for this command |
| `EMBEDDING_FILE_PATH` | local Parquet path (skips S3) |
| `EMBEDDING_S3_ENDPOINT` | S3-compatible endpoint (omit for AWS S3) |
| `EMBEDDING_S3_REGION` / `_BUCKET` / `_KEY` | object location |
| `EMBEDDING_S3_ACCESS_KEY` / `_SECRET_KEY` | credentials (falls back to default AWS chain if unset) |
| `EMBEDDING_S3_USE_PATH_STYLE` | `true` for MinIO, `false` for AWS S3 |
| `EMBEDDING_MODEL` | optional override of the file's `model` metadata |
| `EMBEDDING_BATCH_SIZE` | rows per bulk upsert (default 5000) |
