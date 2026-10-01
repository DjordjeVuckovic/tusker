# Search Router API Documentation

## Overview

The Search Router provides two complementary search APIs following Elasticsearch conventions:

1. **Simple Search (GET)** - For quick, cacheable queries
2. **Structured Search (POST)** - For complex, fine-tuned queries

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
| `lang`    | string | No       | Language: english only (default); any other value returns 400 | `english` |

**Example Request:**
```bash
GET /v1/articles/search?q=climate%20change&size=10&lang=english
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
        "url": "https://example.com/article",
        "language": "english",
        "created_at": "2024-01-15T10:30:00Z",
        "metadata": {
          "source_name": "BBC News",
          "source_id": "bbc-news",
          "published_at": "2024-01-15T09:00:00Z",
          "category": "Environment",
          "imported_at": "2024-01-15T10:00:00Z"
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
  "total_matches": 1523
}
```

**Behavior:**
- Application automatically searches across: `title`, `description`, `content`
- Default field weights: equal (1.0)
- Default operator: OR
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
- Need typo tolerance (fuzziness)
- Require all terms (AND operator)

**Parameters:**

| Field | Type | Required | Description | Example |
|-------|------|----------|-------------|---------|
| `field` | string | Yes | Field to search: title, description, content | `"title"` |
| `query` | string | Yes | Search text | `"climate change"` |
| `operator` | string | No | "and" or "or" (default: "or") | `"and"` |
| `fuzziness` | string | No | Typo tolerance: AUTO, 0, 1, 2 (ES only) | `"AUTO"` |
| `language` | string | No | english only (default); any other value returns 400 | `"english"` |

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
      "language": "english"
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

// Allow typos (Elasticsearch only)
{"field": "title", "query": "climte changge", "fuzziness": "AUTO"}
```

---

### 2.2 Multi-Match Query

**Description:** Multi-field search with custom weights

**When to use:**
- Search across multiple fields
- Fine-tune relevance with field weights
- Boost important fields (e.g., title 3x)

**Parameters:**

| Field           | Type   | Required | Description                            | Example                |
|-----------------|--------|----------|----------------------------------------|------------------------|
| `query`         | string | Yes      | Search text                            | `"renewable energy"`   |
| `fields`        | array  | Yes      | Fields to search                       | `["title", "content"]` |
| `field_weights` | object | No       | Field boost multipliers (default: 1.0) | `{"title": 3.0}`       |
| `operator`      | string | No       | "and" or "or" (default: "or")          | `"or"`                 |
| `language`      | string | No       | english only (default); any other value returns 400 | `"english"` |

**Example Request:**
```json
POST /v1/articles/_search

{
  "size": 10,
  "query": {
    "multi_match": {
      "query": "renewable energy",
      "fields": ["title", "description", "content"],
      "field_weights": {
        "title": 3.0,
        "description": 2.0,
        "content": 1.0
      },
      "operator": "or",
      "language": "english"
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
  "fields": ["title", "content"],
  "field_weights": {
    "title": 5.0,
    "content": 1.0
  }
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
  "total_matches": int64
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

**ArticleSearchResult Structure:**
```json
{
  "article": {
    "id": "uuid",
    "title": "string",
    "subtitle": "string",
    "description": "string",
    "content": "string",
    "author": "string",
    "url": "string",
    "language": "string",
    "created_at": "timestamp",
    "metadata": {
      "source_id": "string",
      "source_name": "string",
      "published_at": "timestamp",
      "category": "string",
      "imported_at": "timestamp"
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

**Common causes:**
- Missing required parameter (`q` or `query` field)
- Invalid operator value
- Unsupported language (anything but `english`)
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
- Query type not implemented by active storage (e.g., fuzziness on PostgreSQL)

---

## Storage Backend Support

| Feature | PostgreSQL | Elasticsearch |
|---------|------------|---------------|
| Simple Search (GET) | ✅ Full | ✅ Full |
| Match Query | ✅ Full | ✅ Full |
| MultiMatch Query | ✅ Full | ✅ Full |
| Fuzziness | ❌ Ignored | ✅ Full |
| Language Analysis | ✅ Full | ✅ Full |
| Cursor Pagination | ✅ Full | ✅ Full |

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
- ✅ Use field weights to improve relevance

### 3. Relevance Tuning

```json
// Default: Equal weights
{"fields": ["title", "content"]}

// Better: Boost important fields
{
  "fields": ["title", "content"],
  "field_weights": {
    "title": 3.0,
    "content": 1.0
  }
}

// Best: Test and tune based on user feedback
{
  "field_weights": {
    "title": 3.0,
    "description": 2.0,
    "content": 1.0
  }
}
```

### 4. Operator Selection

**OR (default):**
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
