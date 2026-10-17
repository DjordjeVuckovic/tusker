# Search Router API Documentation

## Overview

The Search Router provides two complementary search APIs following Elasticsearch conventions:

1. **Simple Search (GET)** - For quick, cacheable queries
2. **Structured Search (POST)** - For complex, fine-tuned queries

---

## Search Contract

Every engine answers the same request in the same way, or says it cannot:

- **A stated parameter that changes which articles match** (`operator`, `fields`/`field`, `fuzziness`, `language`) is honoured by the engine, or the request fails with 400 naming the parameter and the engine.
- **An omitted operator** is filled by the engine's own default. The defaults differ on purpose: Postgres favours precision, Elasticsearch recall.
- **Boosts** (`title^3`) only change ranking. They are best effort, and one the engine drops is listed in `ignored`.
- **A parameter the request type does not define** is a 400 naming it: an unknown JSON key (`"fuzziness"` on `multi_match`, a misspelt `"operater"`) or a search box query parameter other than `q`, `size`, `cursor` and `lang`.
- **Lenient handling** is opt-in with the RFC 7240 header `Prefer: handling=lenient`. A parameter that would have been a 400 is removed from the request before it runs and listed in `ignored`, and the response carries `Preference-Applied: handling=lenient`. A field is removed with its boost. When removing fields would leave none to search, the request is still a 400. An unrecognised `handling` value is ignored, so the request stays strict.
- **Every search response** carries `effective`, the contract the search ran under with every default filled in and every ignored parameter removed, and `ignored`, the stated parameters the engine did not honour.

| | PostgreSQL | Elasticsearch |
|---|---|---|
| Operator when none is given | AND (every term) | OR (any term) |
| Fields it can match alone | title, description, content | title, description, content |
| `fuzziness` | 400, or ignored when lenient | honoured |
| Phrase `slop` | honoured | honoured |
| Boosts (between 1.4e-45 and 3.4e38) | applied to title, description, content | applied |
| `language` | `eng` stems in English | `eng` accepted; analysis is fixed by the index mapping |

Any other field is a 400 on both engines, lenient or not. `GET /v1/capabilities` reports the running engine's row of this table.

---

## Endpoints

### 1. Simple Search - `GET /v1/articles/search`

**Purpose:** Quick text search with automatic optimization

**Use Cases:**
- User-facing search boxes
- Quick lookups
- Browser addressbar searches
- Cacheable/bookmarkable results

**Parameters:**

| Parameter | Type   | Required | Description                                   | Example           |
|-----------|--------|----------|-----------------------------------------------|-------------------|
| `q`       | string | Yes      | Search query text                             | `climate change`  |
| `size`    | int    | No       | Results per page (default: 100, max: 10000)   | `10`              |
| `cursor`  | string | No       | Pagination cursor from previous response      | `eyJzY29yZSI6...` |
| `lang`    | string | No       | ISO 639-3 code; only eng today (default: eng); any other value returns 400 | `eng` |

**Example Request:**
```bash
GET /v1/articles/search?q=climate%20change&size=10&lang=eng
```

**Example Response:**
```json
{
  "hits": [
    {
      "article": {
        "id": "550e8400-e29b-41d4-a716-446655440000",
        "title": "Climate Change Impact on Ecosystems",
        "description": "Study reveals significant impact...",
        "content": "Full article content...",
        "url": "https://www.bbc.co.uk/news/article",
        "publishedAt": "2024-01-15T09:00:00Z",
        "language": "eng",
        "publisherDomain": "bbc.co.uk",
        "metadata": {
          "publisherName": "BBC News",
          "section": "Environment"
        }
      },
      "score": 2.456,
      "score_normalized": 0.95
    }
  ],
  "next_cursor": "eyJzY29yZSI6Mi40NTYsInV1aWQiOiI1NTBlODQwMC1lMjliLTQxZDQtYTcxNi00NDY2NTU0NDAwMDAifQ==",
  "has_more": true,
  "max_score": 2.589,
  "page_max_score": 2.456,
  "total_matches": 1523,
  "effective": {"operator": "OR", "fields": ["title", "description", "content"], "language": "eng"},
  "ignored": []
}
```

**Behavior:**
- The query is free text: operators, quotes and `-` are read as words, not syntax
- Searches `title`, `description` and `content`, each term in any of them
- Terms combine under the engine's default operator (see [Search Contract](#search-contract))
- Elasticsearch ranks with title boosted 3 and description 2; Postgres ranks with its default weight bands
- Results sorted by relevance score (descending)

---

### 2. Structured Search - `POST /v1/articles/_search`

**Purpose:** Complex queries with explicit control

**Use Cases:**
- Programmatic/API access
- Field-specific searches
- Custom relevance tuning
- Complex boolean logic

**Request Body Structure:**
```json
{
  "size": 10,
  "cursor": "optional_base64_cursor",
  "query": {
    "<query_type>": {
      // Query type specific parameters
    }
  }
}
```

---

## Query Types

### 2.1 Match Query

**Description:** Single-field search with full-text analysis

**When to use:**
- Search in specific field only (e.g., titles only)
- Need typo tolerance (fuzziness, Elasticsearch only)
- Require all terms (AND operator)

**Parameters:**

| Field | Type | Required | Description | Example |
|-------|------|----------|-------------|---------|
| `field` | string | Yes | Field to search: title, description or content | `"title"` |
| `query` | string | Yes | Search text | `"climate change"` |
| `operator` | string | No | "and" or "or" (default: the engine's) | `"and"` |
| `fuzziness` | string | No | Typo tolerance: NONE, AUTO, 0, 1, 2. Elasticsearch only; Postgres returns 400 unless lenient | `"AUTO"` |
| `language` | string | No | ISO 639-3 code; only eng today (default: eng); any other value returns 400 | `"eng"` |

**Example Request:**
```json
POST /v1/articles/_search

{
  "size": 10,
  "query": {
    "match": {
      "field": "title",
      "query": "climate change",
      "operator": "and",
      "fuzziness": "AUTO",
      "language": "eng"
    }
  }
}
```

**Use Cases:**
```json
// Search only in titles
{"field": "title", "query": "renewable energy"}

// Require all terms (higher precision)
{"field": "content", "query": "climate change", "operator": "and"}

// Allow typos (Elasticsearch only; 400 on Postgres)
{"field": "title", "query": "climte changge", "fuzziness": "AUTO"}
```

---

### 2.2 Multi-Match Query

**Description:** Multi-field search; each term may match any of the fields

**When to use:**
- Search across multiple fields
- Boost important fields (e.g., `title^3`)

**Parameters:**

| Field           | Type   | Required | Description                            | Example                |
|-----------------|--------|----------|----------------------------------------|------------------------|
| `query`         | string | Yes      | Search text                            | `"renewable energy"`   |
| `fields`        | array  | Yes      | Fields to search, each optionally boosted as `field^boost` | `["title^3", "content"]` |
| `operator`      | string | No       | "and" or "or" (default: the engine's)  | `"or"`                 |
| `language`      | string | No       | ISO 639-3 code; only eng today (default: eng); any other value returns 400 | `"eng"` |

**Example Request:**
```json
POST /v1/articles/_search

{
  "size": 10,
  "query": {
    "multi_match": {
      "query": "renewable energy",
      "fields": ["title^3", "description^2", "content"],
      "operator": "or",
      "language": "eng"
    }
  }
}
```

**Use Cases:**
```json
// Equal weighting across all fields
{
  "query": "artificial intelligence",
  "fields": ["title", "description", "content"]
}

// Boost title matches heavily
{
  "query": "climate change",
  "fields": ["title^5", "content"]
}

// Require all terms across fields
{
  "query": "machine learning applications",
  "fields": ["title", "content"],
  "operator": "and"
}
```

---

## Response Format

All endpoints return the same structure:

```json
{
  "hits": [ArticleSearchResult],
  "next_cursor": "string | null",
  "has_more": boolean,
  "max_score": float64,
  "page_max_score": float64,
  "total_matches": int64,
  "effective": {"operator": "AND | OR", "fields": [string], "language": "eng"},
  "ignored": [{"param": string, "engine": string, "reason": string}]
}
```

**Field Descriptions:**

| Field | Type | Description |
|-------|------|-------------|
| `hits` | array | Search results with article data and scores |
| `next_cursor` | string/null | Cursor for next page (null if no more results) |
| `has_more` | boolean | Whether more results exist beyond current page |
| `max_score` | float64 | Highest relevance score across all matching documents |
| `page_max_score` | float64 | Highest score in current page |
| `total_matches` | int64 | Total number of documents matching the query |
| `effective` | object | Operator, fields and language the search ran under. A phrase has no operator; hybrid leaves the fields to the engine |
| `ignored` | array | Stated parameters the engine did not honour, each with the engine and a reason. Empty when everything was honoured |

**ArticleSearchResult Structure:**
```json
{
  "article": {
    "id": "uuid",
    "url": "string",
    "title": "string",
    "description": "string",
    "content": "string",
    "publishedAt": "timestamp",
    "language": "string",
    "publisherDomain": "string",
    "metadata": {
      "publisherName": "string",
      "authors": ["string"],
      "section": "string",
      "keywords": ["string"],
      "image": "string",
      "dateModified": "timestamp",
      "extra": {}
    }
  },
  "score": float64,              // Raw relevance score
  "score_normalized": float64     // Score normalized to 0.0-1.0 range
}
```

---

## Pagination

### Cursor-Based Pagination

All search endpoints use cursor-based pagination for consistency and performance.

**First Page:**
```bash
GET /v1/articles/search?q=climate&size=10
```

**Response includes cursor:**
```json
{
  "next_cursor": "eyJzY29yZSI6Mi40NTYsInV1aWQiOiIuLi4ifQ==",
  "has_more": true,
  ...
}
```

**Next Page:**
```bash
GET /v1/articles/search?q=climate&size=10&cursor=eyJzY29yZSI6...
```

**Benefits:**
- ✅ Consistent results during pagination
- ✅ Efficient for deep pagination
- ✅ No duplicate or missing results
- ✅ Better performance than offset/limit

---

## Error Responses

### 400 Bad Request
```json
{
  "error": "q parameter is required"
}
```

A parameter the engine cannot honour names itself and the engine:
```json
{
  "error": "fuzziness is not supported by postgres: terms match exactly",
  "title": "validation error"
}
```

**Common causes:**
- Missing required parameter (`q` or `query` field)
- A parameter the engine cannot honour in a strict request (see [Search Contract](#search-contract))
- A JSON key or query parameter the request type does not define
- Invalid operator value
- Unsupported language (anything but `eng`)
- Invalid size (exceeds max or negative)
- Malformed cursor

### 500 Internal Server Error
```json
{
  "error": "internal server error"
}
```

**Common causes:**
- Database connection failure
- Query execution error
- Unexpected server error

### 501 Not Implemented
```json
{
  "error": "match search is not supported by the current storage backend"
}
```

**Common causes:**
- Query type not implemented by active storage

---

## Storage Backend Support

| Feature | PostgreSQL | Elasticsearch |
|---------|------------|---------------|
| Simple Search (GET) | ✅ Full | ✅ Full |
| Match Query | ✅ title, description, content | ✅ Full |
| MultiMatch Query | ✅ title, description, content | ✅ Full |
| Fuzziness | ❌ 400, or ignored when lenient | ✅ Full |
| Language Analysis | ✅ English stemming | ⚠️ Fixed by the index mapping |
| Cursor Pagination | ✅ Full | ✅ Full |

`GET /v1/capabilities` returns the query types the server serves (`query_types`) and what the running engine honours (`full_text`: `engine`, `default_operator`, `fuzziness`, `phrase_slop`, `boosts`, `fields`).

---

## Best Practices

### 1. Choose the Right Endpoint

**Use GET `/search?q=...` when:**
- Building user-facing search
- Need cacheable results
- Want bookmarkable URLs
- Simple text queries

**Use POST `/_search` when:**
- Need field-level control
- Customizing weights/boosting
- Complex queries
- Programmatic access

### 2. Performance Tips

- ✅ Use smaller `size` for faster responses (10-50 is optimal)
- ✅ Use `cursor` for pagination (not multiple requests)
- ✅ Specify only needed `fields` in multi_match
- ✅ Cache GET responses (5-15 minutes)
- ✅ Use field boosts to improve relevance

### 3. Relevance Tuning

```json
// Default: Equal weights
{"fields": ["title", "content"]}

// Better: Boost important fields
{"fields": ["title^3", "content"]}

// Best: Test and tune based on user feedback
{"fields": ["title^3", "description^2", "content"]}
```

### 4. Operator Selection

Leaving `operator` out applies the engine's default: AND on Postgres, OR on Elasticsearch. State it when two engines must answer the same question.

**OR:**
- Higher recall (more results)
- "climate" OR "change" - documents with either term
- Good for discovery

**AND:**
- Higher precision (fewer, more relevant results)
- "climate" AND "change" - documents with both terms
- Good for specific searches

---

## Migration Notes

### From Legacy Endpoints

**Old:**
```bash
GET /v1/articles/search?query=text
POST /v1/articles/search/match
POST /v1/articles/search/multi_match
```

**New:**
```bash
GET /v1/articles/search?q=text
POST /v1/articles/_search
```

**Backward Compatibility:**
- `query` parameter still supported (use `q` for new code)
- Legacy POST endpoints still work but deprecated

---

## Examples

See `/scripts/curl_examples.sh` for 14 comprehensive examples covering:
- Simple searches
- Field-specific searches
- Fuzzy matching
- Multi-field searches
- Field weighting
- Pagination
- Error handling
- Relevance tuning

Run examples:
```bash
cd /home/tadjo/projects/news-hunter
./scripts/curl_examples.sh
```
