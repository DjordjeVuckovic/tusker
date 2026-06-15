# Implementation Goal: Multi-Paradigm Search Architecture

> **Purpose**: Reference for how Tusker supports multiple search paradigms (full-text, boolean, phrase, semantic, hybrid) across storage backends (PostgreSQL, Elasticsearch).

## Architecture Overview

The architecture lets the project:
- Support **multiple search paradigms** as defined in `SEARCH_TERMINOLOGY.md`.
- Let **each storage backend implement only what it supports** via segregated interfaces.
- Keep **type safety** while exposing capability discovery at runtime.
- Enable **clear benchmarking** of PostgreSQL vs Elasticsearch on comparable paradigms.

### Core Principle: Interface Segregation

Rather than one fat `Search` method, each paradigm gets its own interface. A backend that
does not support a paradigm simply does not implement that interface; the router wires only
the searchers that are available and reports them through `/v1/capabilities`.

---

## Storage Interfaces

Located in `internal/storage/` (`searcher.go`, `reader.go`, `indexer.go`, `vector.go`).

```go
// FtsSearcher — lexical full-text search (searcher.go)
type FtsSearcher interface {
    SearchStringQuery(ctx, *query.String, *query.BaseOptions) (*SearchResult, error)
    SearchField(ctx, *query.Match, *query.BaseOptions) (*SearchResult, error)
    SearchFields(ctx, *query.MultiMatch, *query.BaseOptions) (*SearchResult, error)
    SearchPhrase(ctx, *query.Phrase, *query.BaseOptions) (*SearchResult, error)
    SearchBoolean(ctx, *query.Boolean, *query.BaseOptions) (*SearchResult, error)
}

// SemanticSearcher — vector similarity search
type SemanticSearcher interface {
    SearchSemantic(ctx, *query.Semantic, *query.BaseOptions) (*VectorSearchResult, error)
}

// HybridSearcher — lexical FTS fused with vector similarity via RRF
type HybridSearcher interface {
    SearchHybrid(ctx, *query.Hybrid, *query.BaseOptions) (*SearchResult, error)
}

// VectorStore — engine-agnostic vector access (powers semantic/hybrid + bench)
type VectorStore interface {
    QueryVector(ctx, text string) ([]float32, error)
    DocVectors(ctx, ids []uuid.UUID) (map[uuid.UUID][]float32, error)
}

// Indexer / EmbedIndexer — document & embedding ingestion
// Reader — point lookup: GetByIDs(ctx, ids) ([]document.Article, error)
```

There is no monolithic `Reader.Search`, no `VectorSearcher`/`BooleanSearcher`/`FuzzySearcher`/
`FieldLevelSearcher`, and no `SearchQuery`/`QueryType` discriminated union — paradigms are
expressed as distinct query types and distinct searcher interfaces.

### Capability Matrix

| Paradigm   | PostgreSQL | Elasticsearch |
|------------|------------|---------------|
| FTS (string/match/multi_match) | done (tsvector + ts_rank) | done (multi_match + BM25) |
| Boolean    | done (`to_tsquery`)       | done (bool query) |
| Phrase     | done (`phraseto_tsquery`/`<N>`) | done (match_phrase + slop) |
| Fuzzy      | bench-only (pg_trgm SQL)  | bench-only (fuzzy DSL) |
| Semantic   | done (pgvector)           | done (kNN dense_vector) |
| Hybrid     | done (RRF in SQL)         | in progress |

Fuzzy is exercised only through the bench harness (`tracks/news_fuzzy`, raw SQL/DSL templates);
there is no fuzzy HTTP endpoint. (Note: `query.Match` carries a `Fuzziness` field that ES honors
and PG currently ignores.)

---

## Query Types

Located in `internal/types/query/query.go`. Each paradigm is a dedicated struct, built with
option constructors. `Base` is the wrapper used by the structured-search DTO (exactly one
field non-nil, selected by `Kind`):

```go
type Kind string
const (
    StringType     Kind = "query_string"
    MatchType      Kind = "match"
    MultiMatchType Kind = "multi_match"
    BooleanType    Kind = "boolean"
    PhraseType     Kind = "phrase"
    HybridType     Kind = "hybrid"
)

type Base struct {
    Kind        Kind
    QueryString *String
    Match       *Match
    MultiMatch  *MultiMatch
    Boolean     *Boolean
    Phrase      *Phrase
    Hybrid      *Hybrid
}
```

Concrete types: `String` (query + language + default operator), `Match` (single field,
operator, fuzziness), `MultiMatch` (weighted fields, best_fields strategy), `Phrase`
(fields + slop ≤ 3), `Boolean` (expression with AND/OR/NOT), `Semantic` (query + threshold),
`Hybrid` (query + RRF constant `K`, default 60).

---

## API Design

Endpoints in `internal/api/router/search.go`:

| Method & Path | Paradigm |
|---------------|----------|
| `GET /v1/articles/search?q=...` | simple query_string (`String`) |
| `POST /v1/articles/_search`     | structured: `match`, `multi_match`, `phrase`, `boolean`, `hybrid` |
| `GET /v1/articles/semantic_search?q=...` | semantic (wired only if a `SemanticSearcher` is provided) |
| `GET /v1/capabilities`          | reports supported paradigms |

There is no single `POST /v1/articles/search` discriminated-union endpoint. The structured
endpoint dispatches on the `query.Base.Kind` wrapper, e.g.:

```json
POST /v1/articles/_search
{
  "size": 10,
  "query": {
    "match": { "field": "title", "query": "climate change", "operator": "and" }
  }
}
```

Swap `match` for `multi_match`, `phrase`, `boolean`, or `hybrid`. Hybrid returns 400 if no
`HybridSearcher` is wired.

### Capability Discovery

`GET /v1/capabilities` returns `query.Capabilities` — booleans for `string_query`, `match`,
`multi_match`, `phrase`, `boolean`, `semantic`. The FTS searcher is always wired, so the first
five are always `true`; `semantic` is `true` only when a semantic searcher is configured.

```json
{ "string_query": true, "match": true, "multi_match": true,
  "phrase": true, "boolean": true, "semantic": true }
```

---

## Testing & Benchmarking

- Unit tests live alongside sources; PG storage tests use testcontainers (require Docker).
- Cross-engine relevance comparison is done with the **bench CLI** (`cmd/bench/`, `internal/bench/`)
  over TREC-style tracks in `tracks/` (`fts_quality`, `news_fuzzy`, `news_semantic`,
  `news_hybrid`) — NDCG/MAP/MRR/Bpref/P/R/F1. See `docs/bench.md`.

---

## Status Summary

- [x] FTS: string / match / multi_match / phrase / boolean — PG and ES, HTTP + bench.
- [x] Semantic search — PG and ES (pgvector / kNN), HTTP endpoint.
- [x] Hybrid (RRF) — PG done (SQL); ES in progress.
- [x] Fuzzy — bench-only (`tracks/news_fuzzy`); no HTTP endpoint.
- [x] Capability discovery endpoint.

---

*Master Thesis: "PostgreSQL as a Search Engine"*
