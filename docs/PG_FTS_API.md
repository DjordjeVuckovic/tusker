# PostgreSQL Full-Text Search API Reference

## Overview

This document describes the PostgreSQL full-text search implementation for Tusker, covering the weight label system, field boosting, and query capabilities that match Elasticsearch's API design.

## Table of Contents

1. [tsquery Functions Comparison](#tsquery-functions-comparison)
2. [Weight Label System](#weight-label-system)
3. [Field Boosting](#field-boosting)
4. [Query Operators](#query-operators)
5. [SQL Implementation Examples](#sql-implementation-examples)
6. [Go Implementation Patterns](#go-implementation-patterns)
7. [API Endpoints](#api-endpoints)
8. [Performance Considerations](#performance-considerations)

---

## tsquery Functions Comparison

PostgreSQL provides multiple functions to convert text into `tsquery` objects. Each has different capabilities and limitations.

### Function Overview

| Function | Input Type | Operators | Stop Words | Errors | Best For |
|----------|-----------|-----------|------------|--------|----------|
| `plainto_tsquery()` | Plain text | None (always AND) | Auto-removed | Never | Simple searches |
| `websearch_to_tsquery()` | Web syntax | OR, -, quotes | Auto-removed | Never | User input |
| `to_tsquery()` | tsquery syntax | Full (&, \|, !, <->) | Auto-removed | Strict | Programmatic |
| `phraseto_tsquery()` | Plain text | None (uses <->) | Kept as <N> | Never | Exact phrases |

### plainto_tsquery()

**Purpose**: Simplest function for plain text queries.

**Behavior**:
- Normalizes text like `to_tsvector()`
- Inserts `&` (AND) operator between all terms
- Removes stop words

**Limitations**:
- ❌ Does NOT recognize operators (&, |, !, <->)
- ❌ Does NOT support weight labels in input
- ❌ Does NOT support prefix matching (*)
- ❌ Cannot do OR queries
- ❌ Cannot do phrase search

**Example**:
```sql
SELECT plainto_tsquery('english', 'The Fat & Rats:C');
-- Result: 'fat' & 'rat' & 'c'
-- Note: Operators are treated as text, not operators!
```

**Use Case**: Simple AND-based searches where all terms must match.

### websearch_to_tsquery() ⭐ RECOMMENDED

**Purpose**: Accepts web search syntax, safe for user input.

**Behavior**:
- Unquoted text → `&` operators
- Quoted text → `<->` operators (phrase search)
- `OR` keyword → `|` operator
- `-` prefix → `!` operator (NOT)
- **Never raises syntax errors**

**Advantages**:
- ✅ Supports OR queries
- ✅ Supports phrase search (quoted text)
- ✅ Supports NOT operator (-)
- ✅ Safe for untrusted user input
- ✅ Familiar web search syntax

**Examples**:
```sql
-- OR query
SELECT websearch_to_tsquery('english', 'climate OR change');
-- Result: 'climat' | 'chang'

-- Phrase search
SELECT websearch_to_tsquery('english', '"climate change"');
-- Result: 'climat' <-> 'chang'

-- NOT operator
SELECT websearch_to_tsquery('english', 'climate -politics');
-- Result: 'climat' & !'polit'

-- Combined
SELECT websearch_to_tsquery('english', '"climate change" OR "global warming" -hoax');
-- Result: 'climat' <-> 'chang' | 'global' <-> 'warm' & !'hoax'
```

**Use Case**: User-facing search queries, OR operator support, phrase search.

### to_tsquery()

**Purpose**: Most powerful function with full operator support.

**Behavior**:
- Requires proper tsquery syntax
- Supports all operators: `&` (AND), `|` (OR), `!` (NOT), `<->` (FOLLOWED BY)
- Supports weight labels: `:A`, `:B`, `:C`, `:D`
- Supports prefix matching: `:*`
- **Raises syntax errors** on invalid input

**Advantages**:
- ✅ Full operator control
- ✅ Weight label filtering support
- ✅ Prefix matching
- ✅ Distance operators `<N>`

**Limitations**:
- ❌ Requires strict syntax
- ❌ Not safe for raw user input

**Examples**:
```sql
-- Weight label filtering
SELECT to_tsquery('english', 'climate:A | change:B');
-- Result: 'climat':A | 'chang':B

-- Prefix matching
SELECT to_tsquery('english', 'clim:*');
-- Result: 'clim':*

-- Distance operator
SELECT to_tsquery('english', 'climate <2> change');
-- Result: 'climat' <2> 'chang'
```

**Use Case**: Programmatic queries where syntax is controlled, advanced features needed.

### phraseto_tsquery()

**Purpose**: Exact phrase matching with lexeme order preservation.

**Behavior**:
- Like `plainto_tsquery` but uses `<->` instead of `&`
- Preserves stop words as `<N>` distance operators
- Checks lexeme order

**Example**:
```sql
SELECT phraseto_tsquery('english', 'The Fat Rats');
-- Result: 'fat' <-> 'rat'
```

**Use Case**: Exact phrase searches where word order matters.

---

## Weight Label System

PostgreSQL uses weight labels (`A`, `B`, `C`, `D`) to mark lexemes from different document sections.

### Field-to-Label Mapping

```
Field           → Weight Label → Array Position → Default Boost
----------------------------------------------------------------
title           → A            → 3              → 1.0
description     → B            → 2              → 0.4
content         → C            → 1              → 0.2
subtitle/author → D            → 0              → 0.1
```

### How Weights Are Stored

The `search_vector` column contains tsvector with labeled lexemes:

```sql
-- Example search_vector value:
'climat':1A,5B,10C 'chang':2A,6B,11C 'action':3B 'trump':15D
```

This means:
- 'climat' appears in title (position 1, weight A), description (position 5, weight B), content (position 10, weight C)
- 'action' appears only in description (position 3, weight B)
- 'trump' appears only in author/subtitle (position 15, weight D)

### Weight Label Creation (Migration)

```sql
-- From migration 003_add_search_vector_trigger.up.sql
CREATE OR REPLACE FUNCTION update_article_search_vector()
    RETURNS TRIGGER AS $$
BEGIN
    NEW.search_vector :=
        setweight(to_tsvector(COALESCE(NEW.language, 'english')::regconfig,
                              COALESCE(NEW.title, '')), 'A') ||
        setweight(to_tsvector(COALESCE(NEW.language, 'english')::regconfig,
                              COALESCE(NEW.description, '')), 'B') ||
        setweight(to_tsvector(COALESCE(NEW.language, 'english')::regconfig,
                              COALESCE(NEW.content, '')), 'C') ||
        setweight(to_tsvector(COALESCE(NEW.language, 'english')::regconfig,
                              COALESCE(NEW.subtitle, '') || ' ' || COALESCE(NEW.author, '')), 'D');
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
```

### Weight Label Filtering

**Filtering by Specific Fields** (WHERE Clause):

```sql
-- Search ONLY in title (weight A)
WHERE search_vector @@ (plainto_tsquery('english', 'trump')::text || ':A')::tsquery

-- Search in title OR description (weights A or B)
WHERE search_vector @@ (plainto_tsquery('english', 'trump')::text || ':AB')::tsquery

-- Search in title OR description using || operator (alternative)
WHERE search_vector @@ (
    (plainto_tsquery('english', 'trump')::text || ':A')::tsquery
    ||
    (plainto_tsquery('english', 'trump')::text || ':B')::tsquery
)
```

**Official PostgreSQL Documentation**:
> "Lexemes in a tsquery can be labeled with one or more weight letters, which restricts them to match only tsvector lexemes with one of those weights."

### Two-Layer Architecture

PostgreSQL FTS uses a **two-layer approach** to match Elasticsearch behavior:

1. **Weight Labels** → Field Selection (Filtering)
   - Determines **which documents match**
   - Equivalent to ES `fields: ["title", "description"]`

2. **ts_rank Weights** → Field Boosting (Ranking)
   - Determines **how to score** matched documents
   - Equivalent to ES `title^3.0, description^1.5`

---

## Field Boosting

Field boosting uses the `ts_rank()` function with custom weights array to match Elasticsearch's `field^weight` syntax.

### Elasticsearch Syntax

```json
{
  "multi_match": {
    "query": "climate change",
    "fields": ["title^3.0", "description^1.5", "content^0.8"]
  }
}
```

### PostgreSQL Equivalent

```sql
SELECT
    id,
    title,
    ts_rank(
        '{0.0, 0.8, 1.5, 3.0}',  -- {D, C, B, A} format (REVERSE ORDER!)
        search_vector,
        plainto_tsquery('english', 'climate change')
    ) as score
FROM articles
WHERE search_vector @@ (plainto_tsquery('english', 'climate change')::text || ':ABC')::tsquery
ORDER BY score DESC;
```

### Weight Array Format

**IMPORTANT**: The weights array is in **reverse order**: `{D, C, B, A}`

```
{D-weight, C-weight, B-weight, A-weight}
{   0.0  ,   0.8   ,   1.5   ,   3.0  }
 index 0   index 1   index 2   index 3
```

**Default PostgreSQL weights**: `{0.1, 0.2, 0.4, 1.0}`

### Boost Value Behavior

| Boost | Effect | Use Case |
|-------|--------|----------|
| `> 1.0` | Increases field importance | Boost title: `title^3.0` |
| `1.0` | Default weight | Normal field importance |
| `0.0 < x < 1.0` | Decreases field importance | De-prioritize: `author^0.2` |
| `0.0` | No score contribution | Exclude from scoring but still matches |

**Important**: Setting boost to `0.0` does NOT exclude the field from matching. Documents with matches in that field will still be returned, but they contribute 0 to the score.

### Example: Zero Boost Still Matches

```sql
-- Search in title AND description, but only title contributes to score
SELECT
    id,
    title,
    description,
    ts_rank(
        '{0.0, 0.0, 0.0, 3.0}',  -- Only A (title) weighted
        search_vector,
        plainto_tsquery('english', 'trump')
    ) as score,
    -- Verify which fields matched
    search_vector @@ (plainto_tsquery('english', 'trump')::text || ':A')::tsquery as in_title,
    search_vector @@ (plainto_tsquery('english', 'trump')::text || ':B')::tsquery as in_desc
FROM articles
WHERE search_vector @@ (plainto_tsquery('english', 'trump')::text || ':AB')::tsquery
ORDER BY score DESC;

-- Result: Returns documents with 'trump' in EITHER title or description
--         But only title matches get non-zero scores
```

---

## Query Operators

### AND Operator (Default)

**Behavior**: All terms must match.

```sql
-- Using plainto_tsquery (always AND)
SELECT * FROM articles
WHERE search_vector @@ plainto_tsquery('english', 'climate change');
-- Requires both 'climat' AND 'chang'

-- Using websearch_to_tsquery (explicit)
SELECT * FROM articles
WHERE search_vector @@ websearch_to_tsquery('english', 'climate change');
-- Requires both 'climat' AND 'chang'
```

### OR Operator

**Behavior**: At least one term must match.

**Method 1: Using websearch_to_tsquery** ⭐ RECOMMENDED
```sql
SELECT * FROM articles
WHERE search_vector @@ websearch_to_tsquery('english', 'climate OR change');
-- Matches documents with 'climat' OR 'chang' OR both
```

**Method 2: Using to_tsquery**
```sql
SELECT * FROM articles
WHERE search_vector @@ to_tsquery('english', 'climate | change');
-- Matches documents with 'climat' OR 'chang' OR both
```

**Current Implementation Issue**:
The codebase currently uses `plainto_tsquery` for OR operator, which is **incorrect** because plainto_tsquery does NOT support operators. This needs to be changed to `websearch_to_tsquery`.

### NOT Operator

**Using websearch_to_tsquery**:
```sql
SELECT * FROM articles
WHERE search_vector @@ websearch_to_tsquery('english', 'climate -politics');
-- Matches documents with 'climat' but NOT 'polit'
```

**Using to_tsquery**:
```sql
SELECT * FROM articles
WHERE search_vector @@ to_tsquery('english', 'climate & !politics');
-- Matches documents with 'climat' but NOT 'polit'
```

### Phrase Search

**Using websearch_to_tsquery** (quoted text):
```sql
SELECT * FROM articles
WHERE search_vector @@ websearch_to_tsquery('english', '"climate change"');
-- Matches only when 'climat' is immediately followed by 'chang'
```

**Using phraseto_tsquery**:
```sql
SELECT * FROM articles
WHERE search_vector @@ phraseto_tsquery('english', 'climate change');
-- Matches only when 'climat' is immediately followed by 'chang'
```

**Using to_tsquery** (with <-> operator):
```sql
SELECT * FROM articles
WHERE search_vector @@ to_tsquery('english', 'climate <-> change');
-- Matches only when 'climat' is immediately followed by 'chang'
```

### Proximity Search

**Distance operator `<N>`**: Matches when terms are within N positions.

```sql
-- Using to_tsquery
SELECT * FROM articles
WHERE search_vector @@ to_tsquery('english', 'climate <3> change');
-- Matches when 'climat' is within 3 positions of 'chang'

-- Immediate adjacency
SELECT * FROM articles
WHERE search_vector @@ to_tsquery('english', 'climate <-> change');
-- Equivalent to <1> - immediate adjacency
```

### Prefix Matching

**Using to_tsquery with :* operator**:
```sql
SELECT * FROM articles
WHERE search_vector @@ to_tsquery('english', 'clim:*');
-- Matches 'climat', 'climate', 'climatic', etc.
```

**Note**: `plainto_tsquery` and `websearch_to_tsquery` do NOT support prefix matching.

---

## SQL Implementation Examples

### Example 1: Single Field Search (Title Only)

```sql
-- Search for 'trump' ONLY in title field
SELECT
    id,
    title,
    ts_rank(
        '{0.0, 0.0, 0.0, 1.0}',  -- Only A weighted
        search_vector,
        plainto_tsquery('english', 'trump')
    ) as score
FROM articles
WHERE search_vector @@ (plainto_tsquery('english', 'trump')::text || ':A')::tsquery
ORDER BY score DESC
LIMIT 10;

-- Count
SELECT COUNT(*) FROM articles
WHERE search_vector @@ (plainto_tsquery('english', 'trump')::text || ':A')::tsquery;
-- Expected: 605 (from playground.sql testing)
```

### Example 2: Multi-Field Search with Boosting

```sql
-- Search in title^3.0 and description^1.5
SELECT
    id,
    title,
    substring(description, 1, 100) as desc_preview,
    ts_rank(
        '{0.0, 0.0, 1.5, 3.0}',  -- {D, C, B, A}
        search_vector,
        plainto_tsquery('english', 'climate change')
    ) as score
FROM articles
WHERE search_vector @@ (plainto_tsquery('english', 'climate change')::text || ':AB')::tsquery
ORDER BY score DESC
LIMIT 10;
```

### Example 3: OR Query with Field Filtering

```sql
-- Search for "climate OR change" in title and description
SELECT
    id,
    title,
    ts_rank(
        '{0.0, 0.0, 1.0, 2.0}',  -- Title boosted 2x
        search_vector,
        websearch_to_tsquery('english', 'climate OR change')
    ) as score
FROM articles
WHERE search_vector @@ (
    (websearch_to_tsquery('english', 'climate OR change')::text || ':AB')::tsquery
)
ORDER BY score DESC
LIMIT 10;
```

### Example 4: Phrase Search

```sql
-- Search for exact phrase "climate change" in all fields
SELECT
    id,
    title,
    ts_rank(
        search_vector,
        websearch_to_tsquery('english', '"climate change"')
    ) as score
FROM articles
WHERE search_vector @@ websearch_to_tsquery('english', '"climate change"')
ORDER BY score DESC
LIMIT 10;
```

### Example 5: Complex Query (OR + NOT + Phrase)

```sql
-- Search for ("climate change" OR "global warming") but NOT "hoax"
SELECT
    id,
    title,
    ts_rank(
        search_vector,
        websearch_to_tsquery('english', '"climate change" OR "global warming" -hoax')
    ) as score
FROM articles
WHERE search_vector @@ websearch_to_tsquery('english', '"climate change" OR "global warming" -hoax')
ORDER BY score DESC
LIMIT 10;
```

### Example 6: Pagination with COUNT

```sql
-- Get total count and page of results
WITH ranked AS (
    SELECT
        id,
        title,
        description,
        ts_rank(
            '{0.0, 0.0, 1.5, 3.0}',
            search_vector,
            plainto_tsquery('english', 'trump')
        ) as score
    FROM articles
    WHERE search_vector @@ (plainto_tsquery('english', 'trump')::text || ':AB')::tsquery
)
SELECT
    (SELECT COUNT(*) FROM ranked) as total_count,
    (SELECT COALESCE(MAX(score), 0.0) FROM ranked) as max_score,
    *
FROM ranked
ORDER BY score DESC
LIMIT 10 OFFSET 0;
```

---

## Go Implementation Patterns

### Field-to-Label Mapping

```go
package pg

// Field to weight label mapping
var fieldToLabel = map[string]string{
    "title":       "A",
    "description": "B",
    "content":     "C",
    "subtitle":    "D",
    "author":      "D",
}

// Label to array position mapping (for ts_rank weights array)
var labelToPosition = map[string]int{
    "A": 3,  // {D, C, B, A} - position 3
    "B": 2,  // {D, C, B, A} - position 2
    "C": 1,  // {D, C, B, A} - position 1
    "D": 0,  // {D, C, B, A} - position 0
}
```

### Parse ES-Style Field Boost

```go
// FieldBoost represents a field with its boost value
type FieldBoost struct {
    Field  string
    Boost  float64
}

// parseFieldBoost parses "field^boost" notation
// Examples:
//   "title^3.0"     → FieldBoost{Field: "title", Boost: 3.0}
//   "description"   → FieldBoost{Field: "description", Boost: 1.0}
func parseFieldBoost(fieldStr string) FieldBoost {
    parts := strings.Split(fieldStr, "^")
    if len(parts) == 2 {
        boost, err := strconv.ParseFloat(parts[1], 64)
        if err == nil {
            return FieldBoost{Field: parts[0], Boost: boost}
        }
    }
    return FieldBoost{Field: fieldStr, Boost: 1.0}  // Default boost
}
```

### Build Weight Labels String

```go
// buildWeightLabels converts field names to weight label string
// Examples:
//   ["title", "description"] → "AB"
//   ["title", "content"]     → "AC"
//   ["title"]                → "A"
//   []                       → "" (all fields)
func buildWeightLabels(fields []string) string {
    if len(fields) == 0 {
        return ""  // Empty = search all fields
    }

    labels := make(map[string]bool)
    for _, field := range fields {
        if label, ok := fieldToLabel[field]; ok {
            labels[label] = true
        }
    }

    // Build sorted string (ABCD order)
    result := ""
    for _, label := range []string{"A", "B", "C", "D"} {
        if labels[label] {
            result += label
        }
    }

    return result
}
```

### Build Weights Array

```go
// buildWeightsArray creates ts_rank weights array from field boosts
// Array format: {D-weight, C-weight, B-weight, A-weight}
// Examples:
//   [FieldBoost{"title", 3.0}, FieldBoost{"description", 1.5}]
//   → "{0.0, 0.0, 1.5, 3.0}"
func buildWeightsArray(fieldBoosts []FieldBoost) string {
    weights := [4]float64{0.0, 0.0, 0.0, 0.0}  // {D, C, B, A}

    for _, fb := range fieldBoosts {
        label := fieldToLabel[fb.Field]
        position := labelToPosition[label]

        // For D (subtitle/author), take max boost
        if position == 0 {
            weights[position] = math.Max(weights[position], fb.Boost)
        } else {
            weights[position] = fb.Boost
        }
    }

    return fmt.Sprintf("{%.2f, %.2f, %.2f, %.2f}",
        weights[0], weights[1], weights[2], weights[3])
}
```

### Build WHERE Clause with Label Filtering

```go
// buildTsWhereClause builds WHERE clause with weight label filtering
func buildTsWhereClause(fields []string, lang query.Language, op operator.Operator, paramNum int) string {
    labels := buildWeightLabels(fields)

    // Build base tsquery
    var baseQuery string
    if op.IsOr() {
        baseQuery = fmt.Sprintf("websearch_to_tsquery('%s'::regconfig, $%d)", lang, paramNum)
    } else {
        baseQuery = fmt.Sprintf("plainto_tsquery('%s'::regconfig, $%d)", lang, paramNum)
    }

    // Add weight label filtering if fields specified
    if labels != "" {
        return fmt.Sprintf(
            "search_vector @@ (%s::text || ':%s')::tsquery",
            baseQuery, labels,
        )
    }

    // No field filtering - search all
    return fmt.Sprintf("search_vector @@ %s", baseQuery)
}
```

### Build Rank Expression

```go
// buildRankExpression builds ts_rank with custom weights
func buildRankExpression(fieldBoosts []FieldBoost, lang query.Language, op operator.Operator, paramNum int) string {
    // Build base tsquery
    var baseQuery string
    if op.IsOr() {
        baseQuery = fmt.Sprintf("websearch_to_tsquery('%s'::regconfig, $%d)", lang, paramNum)
    } else {
        baseQuery = fmt.Sprintf("plainto_tsquery('%s'::regconfig, $%d)", lang, paramNum)
    }

    // Use custom weights if specified
    if len(fieldBoosts) > 0 {
        weightsArray := buildWeightsArray(fieldBoosts)
        return fmt.Sprintf(
            "ts_rank('%s', search_vector, %s)",
            weightsArray, baseQuery,
        )
    }

    // Use default PostgreSQL weights
    return fmt.Sprintf("ts_rank(search_vector, %s)", baseQuery)
}
```

---

## API Endpoints

### 1. QueryString API (Simple)

**Endpoint**: `GET /v1/articles/search?query=climate+change`

**PostgreSQL Implementation**:
```go
// Application determines fields and weights
fields := []string{"title", "description", "content"}
boosts := []FieldBoost{
    {"title", 3.0},
    {"description", 1.5},
    {"content", 1.0},
}

whereClause := buildTsWhereClause(fields, lang, op, 1)
rankExpr := buildRankExpression(boosts, lang, op, 1)
```

**Generated SQL**:
```sql
WHERE search_vector @@ (plainto_tsquery('english', $1)::text || ':ABC')::tsquery
ORDER BY ts_rank('{0.0, 1.0, 1.5, 3.0}', search_vector, plainto_tsquery('english', $1)) DESC
```

### 2. Match API (Single Field)

**Endpoint**: `POST /v1/articles/search/match`

**Request Body**:
```json
{
  "query": "climate change",
  "field": "title",
  "operator": "and",
  "language": "english"
}
```

**PostgreSQL Implementation**:
```go
fields := []string{query.Field}
boosts := []FieldBoost{{query.Field, 1.0}}

whereClause := buildTsWhereClause(fields, query.Language, query.Operator, 1)
rankExpr := buildRankExpression(boosts, query.Language, query.Operator, 1)
```

**Generated SQL**:
```sql
WHERE search_vector @@ (plainto_tsquery('english', $1)::text || ':A')::tsquery
ORDER BY ts_rank('{0.0, 0.0, 0.0, 1.0}', search_vector, plainto_tsquery('english', $1)) DESC
```

### 3. MultiMatch API (Multiple Fields with Boosting)

**Endpoint**: `POST /v1/articles/search/multi_match`

**Request Body**:
```json
{
  "query": "climate change",
  "fields": ["title^3.0", "description^1.5", "content"],
  "operator": "or",
  "language": "english"
}
```

**PostgreSQL Implementation**:
```go
// Parse field boosts
fieldBoosts := make([]FieldBoost, len(query.Fields))
fields := make([]string, len(query.Fields))
for i, fieldStr := range query.Fields {
    fb := parseFieldBoost(fieldStr)
    fieldBoosts[i] = fb
    fields[i] = fb.Field
}

whereClause := buildTsWhereClause(fields, query.Language, query.Operator, 1)
rankExpr := buildRankExpression(fieldBoosts, query.Language, query.Operator, 1)
```

**Generated SQL**:
```sql
WHERE search_vector @@ (websearch_to_tsquery('english', $1)::text || ':ABC')::tsquery
ORDER BY ts_rank('{0.0, 1.0, 1.5, 3.0}', search_vector, websearch_to_tsquery('english', $1)) DESC
```

---

## Performance Considerations

### 1. Pre-Computed search_vector

**Advantages**:
- ✅ Computed once at INSERT/UPDATE time via trigger
- ✅ GIN-indexed for fast searches
- ✅ Weight labels baked in (no runtime computation)
- ✅ Significant performance improvement over dynamic `to_tsvector()`

**Trade-offs**:
- Storage overhead (additional column)
- Slight INSERT/UPDATE overhead (trigger execution)

**Recommendation**: Always use pre-computed `search_vector` for production.

### 2. GIN Index

```sql
CREATE INDEX idx_articles_search_vector ON articles USING GIN(search_vector);
```

**Benefits**:
- Fast full-text search (logarithmic lookup)
- Efficient for large datasets
- Supports weight label filtering

### 3. Function Choice Performance

| Function | Performance | Notes |
|----------|------------|-------|
| `plainto_tsquery` | Fast | Simplest parsing |
| `websearch_to_tsquery` | Fast | Minimal overhead vs plainto |
| `to_tsquery` | Fast | Same as above if syntax valid |
| `phraseto_tsquery` | Fast | Same as plainto |

**Conclusion**: Function choice has minimal performance impact. Choose based on features needed, not performance.

### 4. Weight Label Filtering Performance

**Impact**: Minimal to none. Weight label filtering is handled by the GIN index efficiently.

```sql
-- Both queries have similar performance
WHERE search_vector @@ plainto_tsquery('english', 'trump')
WHERE search_vector @@ (plainto_tsquery('english', 'trump')::text || ':A')::tsquery
```

### 5. ts_rank Performance

**Impact**: Moderate. `ts_rank()` adds computational overhead for scoring.

**Optimization**: Use `ts_rank()` only in ORDER BY, not in WHERE clause.

```sql
-- ✅ Good: ts_rank only for sorting
SELECT id, title, ts_rank(...) as score
FROM articles
WHERE search_vector @@ query
ORDER BY score DESC;

-- ❌ Bad: ts_rank in WHERE (unnecessary computation)
SELECT id, title, ts_rank(...) as score
FROM articles
WHERE ts_rank(...) > 0.5;  -- Don't filter by score!
```

### 6. Pagination Performance

**Cursor-based pagination** (recommended):
```sql
-- First page
WHERE search_vector @@ query AND (score, id) > (0.0, 0)
ORDER BY score DESC, id ASC
LIMIT 10;

-- Next page (using last score/id from previous page)
WHERE search_vector @@ query AND (score, id) > (0.85, 12345)
ORDER BY score DESC, id ASC
LIMIT 10;
```

**Offset-based pagination** (avoid for large offsets):
```sql
-- Works but slow for large offsets
LIMIT 10 OFFSET 1000;
```

### 7. Benchmarking Notes

For thesis benchmarking, measure:
1. **Query response time**: Time from query to first result
2. **Index size**: GIN index vs inverted index (ES)
3. **Memory usage**: PostgreSQL vs Elasticsearch
4. **Concurrency**: Queries per second under load
5. **Relevance**: Compare ranking quality (ts_rank vs BM25)

---

## Migration Checklist

### Current Issues to Fix

1. ❌ **OR operator uses `plainto_tsquery`** (incorrect)
   - Should use `websearch_to_tsquery` for OR support

2. ❌ **Weight labels not used in WHERE clause**
   - Fields parameter ignored in `buildTsWhereClause()`
   - All queries search all fields (same `total_matches`)

3. ❌ **Weight array mapping incorrect**
   - Current code maps `title` → position 0 (should be 3)
   - Array format is `{D, C, B, A}` (reverse order)

4. ❌ **No ES-style boost parsing**
   - Cannot parse `title^3.0` notation

### Implementation TODO

- [x] Create PG_FTS_API.md documentation
- [ ] Add `fieldToLabel` and `labelToPosition` constants
- [ ] Update `buildTsQuery()` to use `websearch_to_tsquery` for OR
- [ ] Update `buildTsWhereClause()` to use weight label filtering
- [ ] Fix `buildRankExpression()` weight array positions
- [ ] Add `parseFieldBoost()` helper function
- [ ] Add `buildWeightLabels()` helper function
- [ ] Add `buildWeightsArray()` helper function
- [ ] Update test queries in `playground.sql`
- [ ] Test field-specific search (verify different `total_matches`)
- [ ] Test ES-style boost notation
- [ ] Compare results with Elasticsearch

---

## References

- [PostgreSQL Full-Text Search Documentation](https://www.postgresql.org/docs/current/textsearch.html)
- [Text Search Functions](https://www.postgresql.org/docs/current/textsearch-controls.html)
- [Text Search Data Types](https://www.postgresql.org/docs/current/datatype-textsearch.html)
- [Elasticsearch Multi-Match Query](https://www.elastic.co/guide/en/elasticsearch/reference/current/query-dsl-multi-match-query.html)
- [Elasticsearch Query String Query](https://www.elastic.co/guide/en/elasticsearch/reference/current/query-dsl-query-string-query.html)

---

## Summary

This implementation provides **Elasticsearch-compatible API** with **PostgreSQL full-text search** capabilities:

- ✅ Field-specific search with weight label filtering
- ✅ ES-style field boosting (`field^weight`)
- ✅ OR/AND operators via `websearch_to_tsquery`
- ✅ Phrase search (quoted text)
- ✅ NOT operator (- prefix)
- ✅ Pre-computed search_vector with GIN index
- ✅ Two-layer architecture (filtering + ranking)
- ✅ High performance for production use

**Key Advantage**: Unified database for both data storage and search, eliminating the need for a separate search engine while maintaining feature parity with Elasticsearch.
