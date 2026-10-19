"""Check that the ParadeDB cast output collected by collect_pdb.py is what a bm25 index stores.

Builds one bm25 index over sample A with one alias per (field, chain), since a table can hold
only one bm25 index. For 500 sampled terms per (field, chain), 200 most frequent plus 300
random, the number of rows `paradedb.term(alias, term)` matches is compared with the number
of rows whose cast dump holds the term. paradedb.term matches the stored term without
analysing it. Writes out/pdb_index_check.json.
"""

import json
import random

import psycopg

from collect_pdb import CHAINS
from common import FIELDS, OUT, PARADEDB, read_dump, read_sample

SEED = 20261008


def alias(field: str, chain: str) -> str:
    return f"{field}_" + chain.removeprefix("pdb.").replace("+", "_")


def cast_with_alias(cast: str, name: str) -> str:
    if cast.endswith(")"):
        return cast[:-1] + f", 'alias={name}')"
    return f"{cast}('alias={name}')"


def main() -> None:
    sample = read_sample("a")
    report = {}
    with psycopg.connect(**PARADEDB, autocommit=True) as conn:
        conn.execute("DROP TABLE IF EXISTS pdb_a")
        conn.execute("CREATE TABLE pdb_a (line int PRIMARY KEY, title text, description text, content text)")
        with conn.cursor().copy("COPY pdb_a (line, title, description, content) FROM STDIN") as copy:
            for row in sample:
                copy.write_row((row["line"], row["title"], row["description"], row["content"]))
        columns = ", ".join(
            f"({field}::{cast_with_alias(cast, alias(field, chain))})"
            for chain, cast in CHAINS.items()
            for field in FIELDS
        )
        conn.execute(f"CREATE INDEX pdb_a_bm25 ON pdb_a USING bm25 (line, {columns}) WITH (key_field='line')")

        rng = random.Random(SEED)
        for chain in CHAINS:
            dump = read_dump(chain)
            for field in FIELDS:
                df: dict[str, int] = {}
                for (_, f), row in dump.items():
                    if f == field:
                        for term in row["terms"]:
                            df[term] = df.get(term, 0) + 1
                frequent = sorted(df, key=lambda t: (-df[t], t))[:200]
                rest = sorted(set(df) - set(frequent))
                terms = frequent + rng.sample(rest, min(300, len(rest)))
                mismatches = []
                for term in terms:
                    (count,) = conn.execute(
                        "SELECT count(*) FROM pdb_a WHERE line @@@ paradedb.term(%s, %s)",
                        (alias(field, chain), term),
                    ).fetchone()
                    if count != df[term]:
                        mismatches.append({"term": term, "index": count, "cast": df[term]})
                report[f"{chain}/{field}"] = {"terms_checked": len(terms), "mismatches": mismatches}
                print(chain, field, len(terms), "mismatches", len(mismatches), mismatches[:3])
    (OUT / "pdb_index_check.json").write_text(json.dumps(report, indent=1, ensure_ascii=False))


if __name__ == "__main__":
    main()
