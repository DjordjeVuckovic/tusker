# news_fuzzy — Fuzzy / Approximate Search Track

Evaluates typo-tolerance and approximate matching across:

- **pg-trgm**: PostgreSQL `pg_trgm` extension — trigram similarity (`similarity()` > threshold)
- **pg-levenshtein**: PostgreSQL `fuzzystrmatch` — Levenshtein edit distance per token
- **elasticsearch**: ES `multi_match` with `fuzziness: "AUTO"`

## Prerequisites

```sql
-- On the PG instance
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS fuzzystrmatch;

-- Optional: GIN indexes for faster trigram search
CREATE INDEX articles_title_trgm_idx ON articles USING gin(title gin_trgm_ops);
CREATE INDEX articles_desc_trgm_idx  ON articles USING gin(description gin_trgm_ops);
```

## Query design

Each query contains deliberate misspellings (1-2 character edits) to test:
1. **Recall**: does the engine find relevant docs despite the typo?
2. **Ranking**: do truly relevant docs rank above unrelated "trigram-similar" documents?

## Pipeline

```bash
bench pool     news_fuzzy
bench judge    news_fuzzy --strategy lexical
bench run      news_fuzzy
bench export   news_fuzzy --format html
```
