# REST Conventions & API Design Decisions

## TL;DR - Recommended API Structure

```
# Simple Search (GET - cacheable, bookmarkable)
GET  /v1/articles/search?q=climate change

# Structured Search (POST - complex queries)
POST /v1/articles/_search
{
  "query": {"match": {...}}
}
```

---

## GET vs POST for Search: Industry Analysis

### Elasticsearch Approach
```
GET  /articles/_search?q=...        # Simple
POST /articles/_search              # Complex (body)
```
✅ Uses `_search` with underscore
✅ Supports both GET and POST
✅ GET for cacheability, POST for complexity

### Google Cloud Search
```
GET  /v1/search?query=...          # Simple
POST /v1/search                     # Complex
```
✅ Uses `q` or `query` parameter
✅ Clean naming without underscore

### Algolia
```
POST /indexes/{index}/query        # All searches
```
✅ POST-only approach
✅ Simpler (one endpoint)
❌ Not cacheable

### GitHub Search API
```
GET /search/repositories?q=...     # Simple
```
✅ GET-only
✅ Cacheable
❌ Limited to URL length

---

## Recommendation for Tusker

### Option A: Elasticsearch Style (RECOMMENDED)

```
GET  /v1/articles/search?q=...          # Simple (cacheable)
POST /v1/articles/_search               # Structured (complex)
```

**Pros:**
- ✅ Follows ES convention (good for thesis comparison!)
- ✅ `_search` clearly indicates special endpoint
- ✅ GET for simple, POST for complex
- ✅ Industry-standard pattern

**Cons:**
- Underscore in URL (some consider non-RESTful)

### Option B: Google Style

```
GET  /v1/articles/search?q=...          # Simple
POST /v1/articles/search                # Structured
```

**Pros:**
- ✅ Clean URLs
- ✅ Same path, different methods
- ✅ RESTful purist approach

**Cons:**
- GET and POST on same path (some frameworks handle poorly)

### Option C: Separate Paths

```
GET  /v1/articles/search?q=...          # Simple
POST /v1/articles/query                 # Structured
```

**Pros:**
- ✅ Clear distinction
- ✅ No method conflicts

**Cons:**
- Two different naming conventions
- Less intuitive

---

## Final Decision: **Option A (Elasticsearch Style)**

### Rationale:
1. **Thesis Alignment**: You're comparing PG vs ES, so following ES conventions aids clarity
2. **Industry Standard**: Most search APIs use this pattern
3. **Clear Semantics**: `_search` indicates special search endpoint
4. **Best of Both**: GET for caching, POST for complexity

### Endpoints:

```go
// Simple Query String Search
GET /v1/articles/search?q={query}&size={size}&cursor={cursor}&lang={lang}

// Structured Search
POST /v1/articles/_search
{
  "size": 10,
  "cursor": "...",
  "query": {
    "match": {...},
    "multi_match": {...},
    "bool": {...},
    "phrase": {...}
  }
}
```

---

## Query Parameter Naming

### Simple Search Parameters

| Parameter | Type | Required | Description | Standard |
|-----------|------|----------|-------------|----------|
| `q` | string | Yes | Search query | Google, Bing, GitHub |
| `size` | int | No | Results per page | ES uses `size` |
| `cursor` | string | No | Pagination cursor | Custom (better than offset) |
| `lang` | string | No | Search language | Common abbreviation |

**Why `q` instead of `query`?**
- ✅ Universal standard (Google, Bing, GitHub, Twitter)
- ✅ Shorter URLs
- ✅ Familiar to all developers

**Why NOT `from` + `size`?**
- ❌ Inconsistent results during pagination
- ❌ Poor performance for deep pagination
- ✅ Cursor-based is superior

---

## Naming Convention for Query Types

### Current (Implemented):
```json
{
  "query": {
    "match": {...},           // Single-field
    "multi_match": {...}      // Multi-field
  }
}
```

### Planned Extensions:
```json
{
  "query": {
    "query_string": {...},    // Simple with app defaults
    "phrase": {...},          // Exact phrase
    "bool": {...},            // Boolean logic
    "fuzzy": {...},           // Fuzzy matching (optional)
    "prefix": {...}           // Prefix search (optional)
  }
}
```

### Why These Names?

**`query_string`** (not `simple` or `text`):
- ✅ Matches ES terminology exactly
- ✅ Clear: it's a string that gets parsed
- ✅ Distinguishes from structured queries

**`phrase`** (not `match_phrase`):
- ✅ Shorter, clearer
- ✅ Standalone concept
- ⚠️ Consider: `match_phrase` for ES consistency

**`bool`** (not `boolean`):
- ✅ Matches ES exactly
- ✅ Shorter
- ✅ Industry standard

**`fuzzy`**:
- ✅ Clear purpose
- ✅ Matches ES
- ⚠️ May be redundant with `match + fuzziness` param

---

## Response Structure

All endpoints return identical structure:

```json
{
  "hits": [
    {
      "article": {...},
      "score": 0.95,
      "score_normalized": 0.95
    }
  ],
  "next_cursor": "eyJ...",
  "has_more": true,
  "max_score": 1.0,
  "page_max_score": 0.95,
  "total_matches": 1523
}
```

---

## HTTP Status Codes

| Code | Meaning | Use Case |
|------|---------|----------|
| 200 | OK | Successful search (even if 0 results) |
| 400 | Bad Request | Invalid query, missing required params |
| 404 | Not Found | Resource doesn't exist (not used for 0 results) |
| 500 | Internal Server Error | Database errors, crashes |
| 501 | Not Implemented | Query type not supported by storage backend |
| 503 | Service Unavailable | Database connection lost |

**Note:** Empty results return 200, not 404!

---

## Caching Strategy

### GET `/search?q=...`
```
Cache-Control: public, max-age=300  # 5 minutes
Vary: Accept-Language
```
✅ Cacheable by browsers and CDNs
✅ Significantly faster for repeated queries

### POST `/_search`
```
Cache-Control: no-cache
```
❌ Not cacheable (body can vary)
✅ Appropriate for complex, dynamic queries

---

## Migration Path

### Phase 1: Current ✅
```
GET  /v1/articles/search?query=...
POST /v1/articles/search            # Structured
```

### Phase 2: ES Convention (Recommended)
```
GET  /v1/articles/search?q=...
POST /v1/articles/_search           # Structured
```

### Phase 3: Deprecate Old
```
GET  /v1/articles/search?query=... [DEPRECATED]
GET  /v1/articles/search?q=...     [PREFERRED]
POST /v1/articles/_search
```

---

## Implementation Checklist

- [x] Simple GET endpoint
- [x] Structured POST endpoint
- [x] Match query type
- [x] Multi-match query type
- [ ] Rename GET param `query` → `q`
- [ ] Move POST to `/_search` path
- [ ] Add `query_string` query type
- [ ] Add `phrase` query type
- [ ] Add `bool` query type
- [ ] Add comprehensive Swagger docs
- [ ] Add API examples in `/api/examples/`
- [ ] Add caching headers for GET
- [ ] Add rate limiting

---

## Recommendation Summary

**DO:**
- ✅ Use `GET /search?q=...` for simple queries
- ✅ Use `POST /_search` for structured queries
- ✅ Follow ES naming conventions (thesis alignment!)
- ✅ Use cursor-based pagination
- ✅ Return 200 for empty results
- ✅ Cache GET responses

**DON'T:**
- ❌ Use offset/limit pagination
- ❌ Return 404 for empty results
- ❌ Mix naming conventions
- ❌ Put complex queries in GET parameters
- ❌ Forget to document with Swagger

---

## Next Steps

1. **Rename endpoints** to follow ES convention
2. **Update Swagger docs** with examples
3. **Create API examples** directory with curl commands
4. **Add integration tests** for all query types
5. **Document in CLAUDE.md** for future AI assistance
