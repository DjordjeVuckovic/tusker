"""Collect ParadeDB (tantivy) tokens for sample A under each tokenizer chain.

A cast `text::pdb.<tokenizer>(options)::text[]` returns the tokens the bm25 index would store,
in order and with repeats; n is their count.
"""

import sys

import psycopg

from common import FIELDS, PARADEDB, read_sample, write_dump

STEM_STOP = "'stemmer=english', 'stopwords_language=english'"
CHAINS = {
    "pdb.unicode_words": "pdb.unicode_words",
    "pdb.simple": "pdb.simple",
    "pdb.whitespace": "pdb.whitespace",
    "pdb.icu": "pdb.icu",
    "pdb.unicode_words+stem+stop": f"pdb.unicode_words({STEM_STOP})",
    "pdb.simple+stem+stop": f"pdb.simple({STEM_STOP})",
    "pdb.icu+stem+stop": f"pdb.icu({STEM_STOP})",
}


def collect(conn: psycopg.Connection, cast: str, sample: list[dict]):
    query = (
        f"SELECT COALESCE(body::{cast}::text[], '{{}}') "
        "FROM unnest(%s::text[]) WITH ORDINALITY AS t(body, ord) ORDER BY ord"
    )
    batch = 200
    for start in range(0, len(sample), batch):
        chunk = sample[start : start + batch]
        for field in FIELDS:
            result = conn.execute(query, ([row[field] for row in chunk],)).fetchall()
            assert len(result) == len(chunk)
            for row, (tokens,) in zip(chunk, result, strict=True):
                yield {"line": row["line"], "field": field, "terms": sorted(set(tokens)), "n": len(tokens)}


def main(chains: list[str]) -> None:
    sample = read_sample("a")
    with psycopg.connect(**PARADEDB, autocommit=True) as conn:
        conn.execute("CREATE EXTENSION IF NOT EXISTS pg_search")
        for chain in chains or CHAINS:
            print(chain, write_dump(chain, collect(conn, CHAINS[chain], sample)))


if __name__ == "__main__":
    main(sys.argv[1:])
