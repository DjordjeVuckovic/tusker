"""Collect PG text-search lexemes for sample A under each PG configuration.

Each row holds the distinct lexemes of one field and the number of distinct token positions,
so a hunspell word that yields two lemmas at one position still counts as one token.
"""

import sys

import psycopg

from common import FIELDS, PG, read_sample, write_dump

CONFIGS = {
    "pg.english": "english",
    "pg.english_nostop": "english_nostop",
    "pg.simple": "simple",
    "pg.hunspell": "english_hunspell",
}

QUERY = """
SELECT t.ord,
       COALESCE((SELECT array_agg(lexeme ORDER BY lexeme) FROM unnest(v.vec)), '{}'),
       (SELECT count(DISTINCT p) FROM unnest(v.vec) u, unnest(u.positions) p)
FROM unnest(%s::text[]) WITH ORDINALITY AS t(body, ord)
CROSS JOIN LATERAL (SELECT to_tsvector(%s::regconfig, t.body) AS vec) v
ORDER BY t.ord
"""


def collect(conn: psycopg.Connection, config: str, sample: list[dict]):
    batch = 200
    for start in range(0, len(sample), batch):
        chunk = sample[start : start + batch]
        for field in FIELDS:
            texts = [row[field] for row in chunk]
            result = conn.execute(QUERY, (texts, config)).fetchall()
            assert len(result) == len(chunk)
            for row, (_, terms, positions) in zip(chunk, result, strict=True):
                yield {"line": row["line"], "field": field, "terms": list(terms), "n": positions}


def main(chains: list[str]) -> None:
    sample = read_sample("a")
    with psycopg.connect(**PG, autocommit=True) as conn:
        for chain in chains or CONFIGS:
            path = write_dump(chain, collect(conn, CONFIGS[chain], sample))
            print(chain, path)


if __name__ == "__main__":
    main(sys.argv[1:])
