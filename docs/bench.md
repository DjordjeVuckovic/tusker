# bench — IR Benchmark CLI

`bench` evaluates full-text, vector, and hybrid search queries against multiple engines (PostgreSQL variants, Elasticsearch, the news-hunter API), computes IR quality metrics and latency statistics, and writes self-attesting JSON and HTML reports.

## Track convention

Everything lives in a self-contained **track folder**:

```
tracks/<name>/
  spec.yaml                         # engines, jobs, metrics config, defaults
  suite.yaml                        # queries and per-engine SQL/JSON templates
  trec/
    pool.yaml                       # candidate docs (bench pool output)
    annotations.<strategy>.yaml     # relevance grades (bench judge output)
    qrels.<strategy>.tsv            # TREC qrels (bench export --format qrels)
  reports/
    <run_id>.json                   # one per bench run
    latest.json                     # pointer to most recent report
    <run_id>.html                   # optional HTML (bench export --format html)
    <run_id>.md                     # optional Markdown (bench export --format markdown)
```

One track, multiple judgment strategies living side by side.  
Switch strategies with `--judgments <name>` on `bench run` — no YAML editing required.

### Track kind

`spec.yaml` may declare the IR paradigm via `kind: fts | structured | fuzzy | semantic | hybrid`.
It is primarily a taxonomy/provenance label (one per track); requirements are *derived*
from it rather than declared separately. `kind` is **optional**, but omitting it emits a
load-time warning.

`semantic` and `hybrid` derive `RequiresEmbedder = true`: their queries carry the reserved
`{{precomputed}}` vector placeholder, so `validate`/`pool`/`run` need a live query embedder
(`EMBEDDING_BASE_URL` + a postgres engine). Without one, **`bench validate` fails up front**
for these kinds instead of stubbing a fake vector and reporting a misleading OK.

## Pipeline

```
bench init    <name>               1. scaffold tracks/<name>/
bench validate [<name>]            2. dry-run all queries through each engine
bench pool     [<name>] [--depth N]
                                   3. gather candidate docs → trec/pool.yaml
bench judge    [<name>] --strategy <S>
                                   4. grade pool → trec/annotations.<S>.yaml
bench run      [<name>]            5. execute suite + compute metrics → reports/
bench export   [<name>] --format <F>
                                   6. export HTML / Markdown / TREC qrels
```

Inspect at any point:

```
bench status [<name>]              one-glance pipeline state
bench show   report|pool|judgments|spec [<name>]
bench diff   [<name>]              compare latest two runs
bench clean  [<name>]              remove old report files
```

Every command accepts a track name as a positional arg (`bench run fts_quality`), a `--track` flag, or resolves from the current directory when you `cd tracks/<name>`.

## Strategy taxonomy

| Strategy     | Class     | Status   | Description                                              |
|--------------|-----------|----------|----------------------------------------------------------|
| `lexical`    | Heuristic | ✅        | Token-overlap baseline — fast, deterministic, no network |
| `bm25`       | Heuristic | ✅        | Pool-local Okapi BM25, normalised → grade (no network)   |
| `vector`     | Heuristic | ✅        | Cosine similarity; doc vectors from the store → grade    |
| `hybrid`     | Heuristic | ✅        | Weighted BM25 + vector fusion → grade                    |
| `claude-cli` | LLM       | ✅        | `claude -p` subprocess per batch                         |
| `claude-api` | LLM       | ✅        | Anthropic Messages API per batch                         |
| `manual`     | Human     | ✅        | Emits `grade: -1` placeholders for hand-grading          |

`vector`/`hybrid` are storage-agnostic: document vectors are read from a
`storage.VectorStore` (Postgres `article_embeddings` today, ES later — PG takes
precedence) and only the **query** is embedded at runtime via local Ollama. They
do not re-embed documents. Configure with `--pg`/`PG_CONNECTION_STRING` and
`--embedding-base`/`EMBEDDING_BASE_URL` (+ optional `EMBEDDING_MODEL`). The same
`VectorStore` powers `pool`/`run`, which embed the query and inject it into
vector queries via the reserved `{{precomputed}}` placeholder. `bm25` computes
term statistics over each query's candidate pool, so it runs with no external
services.

File convention: `trec/annotations.<strategy>.yaml`, `trec/qrels.<strategy>.tsv`.

## Schema v1

Every produced artifact carries `schema_version: 1` and a `meta:` block. The meta block records `run_id`, `tool` (with git sha), `generated_at`, and artifact-specific provenance (spec_id, strategy, judge_model, judge_prompt_version, sources).

Loading any artifact without `schema_version: 1` is a hard error — there is no silent tolerance.

## Command reference

### `bench init <name>`

Scaffolds `tracks/<name>/` with `spec.yaml`, `suite.yaml`, `trec/`, `reports/`, and `README.md`.

### `bench validate [<name>]`

Dry-runs every query through every engine using the engine's native validation endpoint (PostgreSQL `EXPLAIN`, Elasticsearch `_validate/query`). Reports per-query pass/fail with colored status — no data is stored.

For `semantic`/`hybrid` kinds it first requires an embedder (fails fast if `EMBEDDING_BASE_URL` is unset) and embeds each query for real, so dimension mismatches surface here. It also warns when the declared `kind` and actual `{{precomputed}}` usage disagree.

### `bench pool [<name>] [--depth N]`

Runs all queries in parallel, gathers the top-N results per engine, deduplicates by doc ID, and writes `trec/pool.yaml`. Default depth is from `spec.defaults.pool_depth`.

### `bench judge [<name>] --strategy <S>`

Grades every `(query, doc)` pair in the pool using the chosen strategy. Output: `trec/annotations.<S>.yaml`.

Key flags:
- `--resume` — skip docs already graded (errors if model or prompt version changed)
- `--batch N` — override LLM batch size
- `--concurrency N` — parallel Grade calls (per-doc mode)

### `bench run [<name>] [--judgments <S|path>] [--jobs <name,...>]`

Executes the suite against all engines, computes IR metrics and latency, prints a styled table with per-engine NDCG/MAP/MRR/Bpref + latency percentiles + statistical significance, then writes `reports/<run_id>.json` and updates `reports/latest.json`.

Judgments resolution order:
1. `--judgments <strategy|path>` (CLI flag)
2. `spec.defaults.judgments` (per-track default)
3. None → latency-only report, warning printed

Flags:
- `--jobs pg,es` — run only the named job(s) from the spec (useful during development)
- `--k 3,5,10` — NDCG/P cut-off values
- `--warmup N`, `--iterations N` — override spec settings
- `--max-k N` — docs retrieved per query

Elapsed time is printed after the results table.

### `bench export [<name>] --format <F>`

| Format               | Output                  | Description                                                                          |
|----------------------|-------------------------|--------------------------------------------------------------------------------------|
| `qrels` (or `tsv`)   | `trec/qrels.<S>.tsv`    | TREC qrels TSV for `trec_eval`, R, pytrec_eval                                       |
| `html`               | `reports/<run_id>.html` | Self-contained HTML with sortable tables, SVG charts, significance table, provenance |
| `markdown` (or `md`) | `reports/<run_id>.md`   | GitHub-Flavored Markdown tables for thesis writing and PRs                           |

Examples:
```bash
bench export fts_quality --format qrels
bench export fts_quality --format qrels --strategy claude-api
bench export fts_quality --format html
bench export fts_quality --format markdown
bench export fts_quality --format markdown --output /tmp/results.md
```

### `bench status [<name>]`

Prints a one-glance dashboard showing which artifacts exist, when they were last generated, and what the natural next step is — like `git status` for the pipeline.

### `bench diff [<name>]`

Loads the two most-recent reports and shows per-engine metric deltas (NDCG, MAP, MRR, latency) and per-query NDCG regressions sorted by magnitude. Pass `--a` / `--b` to compare specific run IDs.

### `bench show <subcommand> [<name>|path]`

Pretty-prints a one-page summary of any artifact:

| Subcommand | Reads |
|-----------|-------|
| `show spec` | `spec.yaml` |
| `show pool` | `trec/pool.yaml` |
| `show judgments [--strategy S]` | `trec/annotations.<S>.yaml` |
| `show report` | `reports/latest.json` → actual report |

`bench report [<name>]` is a top-level shorthand for `bench show report`.

### `bench clean [<name>] [--keep N]`

Removes old JSON, HTML, and Markdown files from `reports/`, keeping the `--keep` most-recent (default 5). `latest.json` is never deleted.

```bash
bench clean fts_quality            # keep 5 most recent
bench clean fts_quality --keep 2
bench clean fts_quality --dry-run  # show what would be deleted
```

## Metrics

All metrics are computed per-query then averaged across judged queries:

| Metric   | Description                                                    |
|----------|----------------------------------------------------------------|
| `NDCG@k` | Normalized Discounted Cumulative Gain — primary quality signal |
| `P@k`    | Precision at k                                                 |
| `R@k`    | Recall at k                                                    |
| `F1@k`   | Harmonic mean of P@k and R@k                                   |
| `MAP`    | Mean Average Precision                                         |
| `MRR`    | Mean Reciprocal Rank                                           |
| `Bpref`  | Binary preference — robust to incomplete judgments             |

Statistical significance is computed pairwise (Wilcoxon signed-rank, two-tailed) for NDCG@K, MAP, and MRR. Requires ≥4 non-tied paired observations; `*` = p<0.05, `**` = p<0.01.

Latency: per-engine min/p50/p75/p90/p95/p99/max/mean/stddev across all queries.

## Artifacts

All artifacts are self-attesting. A report's `provenance.sources` block records the exact paths of the spec, suite, pool, and judgments files used — you can reconstruct any run from the report alone.

For per-track documentation, see `tracks/<name>/README.md`.

## Track naming convention

One track per (dataset × IR paradigm). Two layouts are supported side by side:

**Flat** — `tracks/<dataset>_<paradigm>/`, addressed by its name:

| Track           | Paradigm            | Engines                                 |
|-----------------|---------------------|-----------------------------------------|
| `news_fts`      | Full-text search    | pg-seq, pg-gin, paradedb, elasticsearch |
| `news_fuzzy`    | Fuzzy / approximate | pg_trgm, ES fuzziness                   |
| `news_semantic` | Semantic / vector   | pgvector, ES dense_vector kNN           |
| `news_hybrid`   | Hybrid (RRF fusion) | pgvector+BM25, ES hybrid                |

**Nested** — `tracks/<dataset>/<paradigm>/`, addressed by a slash path
(`news/fts`), grouping a dataset's paradigms under one directory:

```
tracks/news/fts/        bench run news/fts     # one paradigm
tracks/news/fuzzy/      bench run 'news/*'     # every paradigm of the dataset
tracks/news/semantic/
tracks/news/hybrid/
```

This decomposition ensures that pools and judgments are paradigm-specific (different query types, different relevance criteria) and that statistical comparisons are between equivalent systems.

### Track resolution & grouping

A track arg is interpreted as:

1. **Verbatim path** — absolute, `./`-/`../`-prefixed, or a `*.yaml` etc. — used as-is (escape hatch for tracks outside `./tracks`).
2. **Name** — mapped under `tracks/`. May be nested with `/` (`news/fts` → `tracks/news/fts`).
3. **Glob** — `news/*` fans out across every track-shaped match; `validate`, `pool`, `judge`, `run`, and `status` run once per matched track.

Grouping is **explicit**: only a glob expands. A bare name always means exactly one track — there is no implicit "directory becomes a group" behaviour, so `bench run news` (when `news` is a directory of tracks, not a track) is an error. Glob mode forbids the single-track path overrides (`--spec`/`--suite`/`--pool`/`--output`). A per-track failure is logged and the run continues; the command exits non-zero listing the tracks that failed. Quote a glob so the shell doesn't expand it first: `bench run 'news/*'`.
