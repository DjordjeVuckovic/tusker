# Tusker Search API Documentation

## Overview

The Tusker API provides comprehensive search capabilities across multiple paradigms:
- **Simple Search**: Google-like query string search
- **Structured Search**: Elasticsearch-style structured queries
- **Field-Level Control**: Match specific fields with custom weights
- **Boolean Logic**: Complex queries with AND/OR/NOT operators
- **Phrase Matching**: Exact phrase search

## API Endpoints

### 1. Simple Search (GET)

**Endpoint:** `GET /v1/articles/search`

**Use Case:** Quick searches, browser addressbar, caching, bookmarking

**Query Parameters:**
- `q` (required): Search query text
- `size` (optional): Results per page (default: 100, max: 10000)
- `cursor` (optional): Pagination cursor (base64-encoded)
- `lang` (optional): Search language (default: english)

**Examples:**

```bash
# Simple text search
GET /v1/articles/search?q=climate change&size=10

# With language
GET /v1/articles/search?q=promene klime&lang=serbian

# With pagination
GET /v1/articles/search?q=renewable energy&cursor=eyJzY29yZSI6...
```

**Response:**
```json
{
  "hits": [
    {
      "article": {
        "id": "uuid",
        "title": "Climate Change Impact",
        "description": "Article about climate change...",
        "content": "Full article content...",
        "url": "https://example.com/article",
        "language": "english",
        "created_at": "2024-01-15T10:30:00Z",
        "metadata": {
          "source_name": "BBC News",
          "published_at": "2024-01-15T09:00:00Z",
          "category": "Environment"
        }
      },
      "score": 0.95,
      "score_normalized": 0.95
    }
  ],
  "next_cursor": "eyJzY29yZSI6MC45...",
  "has_more": true,
  "max_score": 1.0,
  "page_max_score": 0.95,
  "total_matches": 1523
}
```

---

### 2. Structured Search (POST)

**Endpoint:** `POST /v1/articles/_search`

**Use Case:** Complex queries, programmatic access, fine-tuned control

**Request Body Structure:**
```json
{
  "size": 10,
  "cursor": "base64_encoded_cursor",
  "query": {
    "<query_type>": {
      // query type specific parameters
    }
  }
}
```

---

## Query Types

### 2.1. Match Query

**Description:** Single-field search with full-text analysis

**Example:**
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

**Parameters:**
- `field` (required): Field to search ("title", "description", "content")
- `query` (required): Search text
- `operator` (optional): "and" or "or" (default: "or")
- `fuzziness` (optional): Typo tolerance - "AUTO", "0", "1", "2" (ES only)
- `language` (optional): Language for text analysis (default: "english")

**Use Cases:**
- Search only in titles: `field: "title"`
- Search only in content: `field: "content"`
- Require all terms: `operator: "and"`
- Allow typos: `fuzziness: "AUTO"`

---

### 2.2. Multi-Match Query

**Description:** Multi-field search with custom field weights

**Example:**
```json
POST /v1/articles/_search
{
  "size": 10,
  "query": {
    "multi_match": {
      "query": "climate change",
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

**Parameters:**
- `query` (required): Search text
- `fields` (required): Array of fields to search
- `field_weights` (optional): Field boost multipliers (default: 1.0 for all)
- `operator` (optional): "and" or "or" (default: "or")
- `language` (optional): Language for text analysis (default: "english")

**Use Cases:**
- Boost title matches: `"title": 3.0`
- Search across all fields: `fields: ["title", "description", "content"]`
- Fine-tune relevance: Adjust weights based on importance

---

### 2.3. Boolean Query (Coming Soon)

**Description:** Complex queries with must/should/must_not clauses

**Example:**
```json
POST /v1/articles/_search
{
  "query": {
    "bool": {
      "must": [
        {"match": {"field": "title", "query": "climate"}}
      ],
      "should": [
        {"match": {"field": "content", "query": "change"}},
        {"match": {"field": "content", "query": "warming"}}
      ],
      "must_not": [
        {"match": {"field": "content", "query": "politics"}}
      ]
    }
  }
}
```

**Parameters:**
- `must` (optional): All queries must match (AND logic)
- `should` (optional): At least one query should match (OR logic)
- `must_not` (optional): Queries must not match (NOT logic)
- `filter` (optional): Queries that filter results (no scoring)

---

### 2.4. Phrase Query (Coming Soon)

**Description:** Exact phrase matching (words must appear in order)

**Example:**
```json
POST /v1/articles/_search
{
  "query": {
    "phrase": {
      "field": "title",
      "query": "climate change",
      "slop": 0
    }
  }
}
```

**Parameters:**
- `field` (required): Field to search
- `query` (required): Exact phrase
- `slop` (optional): Allowed word distance (default: 0)

---

### 2.5. Query String (Coming Soon)

**Description:** Application-optimized simple search (same as GET but in POST body)

**Example:**
```json
POST /v1/articles/_search
{
  "query": {
    "query_string": {
      "query": "climate change",
      "default_operator": "or",
      "language": "english"
    }
  }
}
```

---

## Pagination

All search endpoints support cursor-based pagination:

```bash
# First page
GET /v1/articles/search?q=climate&size=10

# Response includes next_cursor
{
  "next_cursor": "eyJzY29yZSI6MC45NSwidXVpZCI6IjEyMyJ9",
  "has_more": true,
  ...
}

# Next page
GET /v1/articles/search?q=climate&size=10&cursor=eyJzY29yZSI6...
```

**Benefits:**
- Consistent results during pagination
- Efficient for deep pagination
- No offset/limit inconsistencies

---

## Response Format

All endpoints return the same response structure:

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

**Fields:**
- `hits`: Array of search results with articles and scores
- `next_cursor`: Cursor for next page (null if no more results)
- `has_more`: Whether more results exist
- `max_score`: Highest score across all results
- `page_max_score`: Highest score in current page
- `total_matches`: Total number of matching documents

---

## Error Responses

### 400 Bad Request
```json
{
  "error": "query is required"
}
```

### 404 Not Found
```json
{
  "error": "article not found"
}
```

### 500 Internal Server Error
```json
{
  "error": "internal server error"
}
```

### 501 Not Implemented
```json
{
  "error": "match search is not supported by the current storage backend"
}
```

---

## Storage Backend Support

| Query Type | PostgreSQL | Elasticsearch |
|------------|------------|---------------|
| Simple (GET) | ✅ Full | ✅ Full |
| Match | ✅ Full | ✅ Full + Fuzziness |
| Multi-Match | ✅ Full | ✅ Full |
| Boolean | 🚧 Planned | 🚧 Planned |
| Phrase | 🚧 Planned | 🚧 Planned |

---

## Best Practices

### When to use GET vs POST

**Use GET (`/search?q=...`) when:**
- ✅ Simple text queries
- ✅ Need caching
- ✅ Want bookmarkable URLs
- ✅ Building user-facing search boxes

**Use POST (`/_search`) when:**
- ✅ Complex structured queries
- ✅ Need field-level control
- ✅ Customizing weights/operators
- ✅ Building programmatic integrations

### Performance Tips

1. **Use smaller page sizes** for faster responses
2. **Use cursor pagination** for deep pagination
3. **Specify fields** in multi_match to search only what you need
4. **Use caching** with GET requests
5. **Boost important fields** in multi_match queries

### Language Support

Supported languages:
- `english` (default)
- `serbian`

More languages can be added via configuration.

---

## Examples by Use Case

### Search in titles only
```json
POST /v1/articles/_search
{
  "query": {
    "match": {
      "field": "title",
      "query": "climate change"
    }
  }
}
```

### Fuzzy search (typo tolerance)
```json
POST /v1/articles/_search
{
  "query": {
    "match": {
      "field": "content",
      "query": "climte changge",
      "fuzziness": "AUTO"
    }
  }
}
```

### Boost title matches 3x
```json
POST /v1/articles/_search
{
  "query": {
    "multi_match": {
      "query": "renewable energy",
      "fields": ["title", "content"],
      "field_weights": {
        "title": 3.0,
        "content": 1.0
      }
    }
  }
}
```

### Require all terms (AND)
```json
POST /v1/articles/_search
{
  "query": {
    "match": {
      "field": "content",
      "query": "climate change impact",
      "operator": "and"
    }
  }
}
```
