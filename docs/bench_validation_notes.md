# New-track validation notes

Status of `bench validate` for the three new tracks, plus the remaining blockers
to an end-to-end `pool → judge → run`. Generated 2026-06-02 against the local
stack (pgvector @ 54320, paradedb @ 54321, ES @ 9200).

## Judging strategy (gap 3)

The full strategy taxonomy is now implemented — `bm25` (pool-local Okapi BM25),
`vector` (embedding cosine similarity), and `hybrid` (BM25 + vector fusion) join
the existing `lexical`, `claude-cli`, `claude-api`, and `manual` judges. `vector`
and `hybrid` need an embedding backend (`EMBEDDING_BASE_URL` / `--embedding-base`).

Track defaults:

| track          | `defaults.judgments` | rationale                                                            |
|----------------|----------------------|----------------------------------------------------------------------|
| `news_fuzzy`   | `lexical`            | query descriptions are correctly spelled → token-overlap is a fair baseline |
| `news_semantic`| `claude-cli`         | semantic relevance has no keyword overlap; needs an LLM judge        |
| `news_hybrid`  | `claude-cli`         | semantic half of the pool is mis-graded by token-overlap             |

The `vector`/`hybrid` judges are an alternative to `claude-cli` for the semantic
and hybrid tracks once embeddings are available, with no LLM cost.
`claude-cli` is a first-class strategy and is accepted by the spec validator.
Note: `bench judge` needs a populated `pool.yaml`, which `bench pool` only
produces by executing the engine queries — so the semantic/hybrid judge cannot
run until the embedding blockers below are resolved. Fuzzy can pool→judge today.

## Engine-name verification (gap 4)

All six new engine keys resolve to executors (no `UNSUPPORTED`):
`pg-trgm`, `pg-levenshtein`, `pgvector-cosine`, `pgvector-l2`, `pg-rrf`,
`paradedb-hybrid`. `engine.CreateFromSpec` switches on the `type` field, so the
keys are free-form labels — verified working.

### `bench validate` results

| track           | result        | notes |
|-----------------|---------------|-------|
| `news_fuzzy`    | **30/30 OK**  | green after adding the `fuzzystrmatch` migration (005) |
| `news_semantic` | 10/30 OK      | ES green; pgvector-* now reach EXPLAIN → `INVALID` (`column "embedding" does not exist`, blocker #1) |
| `news_hybrid`   | 10/30 OK      | ES OK; pg-rrf/paradedb reach EXPLAIN → schema `INVALID` (blocker #1) |
| `fts_quality`   | 120/120 OK    | regression check — unchanged |

## Fixed in this change

- **`fuzzystrmatch` extension** — `db/migrations/005_add_fuzzystrmatch.{up,down}.sql`.
  `pg-levenshtein`'s `levenshtein_less_equal` now resolves; whole fuzzy track green.
- **Missing `trec/` + `reports/` dirs** — the hand-scaffolded tracks lacked the
  `trec/` folder that `trackctx.Resolve` requires; added (with `.gitkeep`).
- **Judging strategy** — semantic/hybrid specs point at `claude-cli`.
- **ES top-level `knn` validation** — `EsExecutor.Validate` now routes bodies
  with a `knn` clause to a structural check (`validateKnnBody`), since
  `_validate/query` rejects `knn` outright. All 10 semantic ES rows now pass
  (were `INVALID`); hybrid ES still validates its `query` block too.
- **`{{precomputed}}` query-vector injection** — a storage-agnostic
  `storage.VectorStore` (PG impl now, ES stubbed) embeds the query at run time;
  `pool`/`run` inject it via the reserved `precomputed` param (renderer now
  resolves param-introduced placeholders). The `vector`/`hybrid` judges read
  document vectors from the same store instead of re-embedding. Semantic/hybrid
  PG rows now reach EXPLAIN, exposing the real schema blocker below.

## Remaining blockers (out of scope here)

1. **Embedding schema mismatch + empty table.** Suites query
   `articles.embedding vector(1536)`, but there is no `embedding` column on
   `articles`; embeddings live in `article_embeddings vector(1024)` (Qwen3) per
   `db/migrations/004_*`, currently **empty (0 rows)** while `articles` has
   105,375. The pgvector templates must JOIN `article_embeddings` at 1024 dims,
   and embeddings must be generated (`ingest embeddings`, `scripts/embed_qwen3.ipynb`)
   before pooling returns anything. The ES `articles` index also needs a
   `dense_vector` `embedding` field + reindex. (Ingestion handled separately.)

2. **ParadeDB (`paradedb-hybrid` @ 54321)** — better than expected: `pg_search`
   and `vector` extensions are installed and the `articles` table exists. Only
   blocked by #1 (schema + embeddings). The `pdb_hybrid` template's `@@@` /
   `paradedb.parse` / `pdb.score` usage will be exercised by EXPLAIN once the
   embeddings/schema land.
