"""Check that tiger (pg_textsearch) indexes sample A the way PG's english config does.

pg_textsearch exposes no per-document tokens and its index dump lists only 20 terms, so the
check runs on what it does expose:

- corpus statistics per field (documents, total length, distinct terms) from
  bm25_summarize_index, against the same numbers computed from PG's to_tsvector;
- for 500 sampled surface words per field, the number of rows tiger scores non-zero for the
  word, against the number of rows where PG's tsvector matches plainto_tsquery of the word.

Writes out/tiger_check.json.
"""

import json
import random
import re

import psycopg

from common import FIELDS, OUT, TIGER, read_dump, read_sample

SEED = 20261008
WORDS_PER_FIELD = 500
STAT = re.compile(r"^\s*(total_docs|total_len): (\d+)$", re.MULTILINE)
SEGMENT_TERMS = re.compile(r"Total: \d+ segments, \d+ pages \([^)]*\), (\d+) terms, (\d+) docs")


def sample_words(field: str) -> list[str]:
    simple = read_dump("pg.simple")
    counts: dict[str, int] = {}
    for (_, f), row in simple.items():
        if f != field:
            continue
        for term in row["terms"]:
            if term.isascii() and term.isalpha():
                counts[term] = counts.get(term, 0) + 1
    frequent = sorted(counts, key=lambda w: (-counts[w], w))[:200]
    rest = sorted(set(counts) - set(frequent))
    return frequent + random.Random(SEED).sample(rest, WORDS_PER_FIELD - len(frequent))


def main() -> None:
    sample = read_sample("a")
    english = read_dump("pg.english")
    report = {}
    with psycopg.connect(**TIGER, autocommit=True) as conn:
        conn.execute("CREATE EXTENSION IF NOT EXISTS pg_textsearch")
        for field in FIELDS:
            table, index = f"tiger_{field}", f"tiger_{field}_bm25"
            conn.execute(f"DROP TABLE IF EXISTS {table}")
            conn.execute(f"CREATE TABLE {table} (line int PRIMARY KEY, body text NOT NULL)")
            with conn.cursor().copy(f"COPY {table} (line, body) FROM STDIN") as copy:
                for row in sample:
                    copy.write_row((row["line"], row[field]))
            conn.execute(f"CREATE INDEX {index} ON {table} USING bm25 (body) WITH (text_config='english')")
            conn.execute("SELECT bm25_force_merge(%s)", (index,))
            summary = conn.execute("SELECT bm25_summarize_index(%s)", (index,)).fetchone()[0]
            stats = {key: int(value) for key, value in STAT.findall(summary)}
            segment = SEGMENT_TERMS.search(summary)

            rows = [row for (_, f), row in english.items() if f == field]
            pg_stats = {
                "docs_with_terms": sum(1 for row in rows if row["n"] > 0),
                "total_len": sum(row["n"] for row in rows),
                "terms": len({term for row in rows for term in row["terms"]}),
            }

            mismatches = []
            words = sample_words(field)
            for word in words:
                tiger_rows, pg_rows = conn.execute(
                    f"SELECT count(*) FILTER (WHERE (body <@> to_bm25query(%s, %s)) <> 0), "
                    f"count(*) FILTER (WHERE to_tsvector('english', body) @@ plainto_tsquery('english', %s)) "
                    f"FROM {table}",
                    (word, index, word),
                ).fetchone()
                if tiger_rows != pg_rows:
                    mismatches.append({"word": word, "tiger": tiger_rows, "pg": pg_rows})

            report[field] = {
                "tiger": {**stats, "terms": int(segment.group(1)) if segment else None},
                "pg": pg_stats,
                "words_checked": len(words),
                "word_mismatches": mismatches,
            }
            print(field, json.dumps(report[field])[:300])
    (OUT / "tiger_check.json").write_text(json.dumps(report, indent=2, ensure_ascii=False) + "\n")


if __name__ == "__main__":
    main()
