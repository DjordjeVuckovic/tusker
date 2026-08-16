---
name: bench-annotate
description: Annotate / grade news articles for the IR benchmark when a human-in-the-loop pass is needed. Use this when asked to grade, annotate, label, or evaluate relevance for the bench pool — typically reviewing or overriding entries in a judgments YAML produced by `bench judge`.
---

# Bench Annotate

Grade news articles for relevance against IR benchmark queries.

## When to use this skill

- The user asks you to **review** an `annotations.<strategy>.yaml` and adjust grades you disagree with
- The user asks you to **fill in** a `manual` judgments file (every doc has `grade: -1`)
- The user explicitly wants a Claude pass instead of `bench judge --strategy claude-cli/claude-api`

For fully automated grading without human review, prefer the `bench judge` CLI — it batches docs, handles resume, and writes incrementally.

## Grading Scale

- **3** — Highly relevant: article is centrally about the query topic
- **2** — Relevant: article clearly covers the topic
- **1** — Marginal: topic is mentioned but not the focus
- **0** — Not relevant: article is about something else

When in doubt between two grades, pick the lower one. Grade what the article **is about**, not whether query terms appear in the text.

## Workflow (per-query, never load the whole pool)

The pool file can be large. Work one query at a time:

1. **Read the pool file header** to get the suite name and confirm structure.
2. **For each query in the pool:**
   1. Find the query block: `grep -n "query_id: <id>" tracks/<name>/trec/pool.yaml`
   2. Extract the `doc_id`s for that query
   3. For each `doc_id`, fetch the article from Postgres if you need content:
      ```sql
      SELECT id, title, description, content FROM articles WHERE id = '<uuid>';
      ```
   4. Grade every doc using the scale above.
3. **Write the output** to `tracks/<name>/trec/annotations.<strategy>.yaml`.

## Output schema

Match `internal/bench/judgment/types.go` exactly. Every artifact must carry `schema_version: 1` and a `meta:` block:

```yaml
schema_version: 1
meta:
  run_id: 2026-05-21T14-08-55-judge-7c91a3
  tool: bench/1.0.0 (sha:abc1234)
  generated_at: 2026-05-21T14:08:55Z
  strategy: manual
  judge_model: ""
  judge_prompt_version: v1
  pool_ref: tracks/global-news-dataset/fts_quality/trec/pool.yaml
  graded_count: 42
  relevance_scale: [0, 1, 2, 3]
strategy: manual
queries:
  - query_id: qs-climate
    docs:
      - doc_id: 493384af-c5fc-4677-ad11-2081e73f0588
        grade: 2
      - doc_id: 413b1526-e74b-4ee1-95f4-4a30580fcf09
        grade: 3
```

Rules:
- Every `doc_id` from the input pool must appear in the output
- Grades are `0`, `1`, `2`, or `3` — never `-1` in the final output
- `strategy` should reflect who/what produced the grades (e.g. `manual`, `claude-api`)

## Track layout

```
tracks/<name>/
  spec.yaml
  suite.yaml
  trec/
    pool.yaml                         ← input
    annotations.<strategy>.yaml       ← output (this skill writes here)
  reports/
    <run_id>.json
    latest.json
```

## After writing

Confirm: count of queries written, total docs graded, output path. Suggest running:
```
bench run <name> --judgments <strategy>
```
to score the run against these judgments.
