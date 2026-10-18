"""Compare ES and ParadeDB chains directly, and the stopword sets each drops.

Equal agreement with PG does not make two chains agree with each other, so the UAX#29
tokenizers and the closest full chains are compared pairwise. The stopword check runs PG's
127 words through Lucene's `_english_` filter and ParadeDB's `stopwords_language=english` and
compares what each drops. Writes out/es_vs_pdb.json.
"""

import json

import psycopg
import requests

from agreement import compare
from common import ES, FIELDS, OUT, PARADEDB, ROOT, read_dump

PAIRS = [
    ("es.standard", "pdb.unicode_words"),
    ("es.icu", "pdb.icu"),
    ("es.standard", "es.icu"),
    ("es.standard+lucenestop+porter2", "pdb.unicode_words+stem+stop"),
]


def main() -> None:
    out = {}
    for left, right in PAIRS:
        report = compare(read_dump(left), read_dump(right))
        out[f"{left} vs {right}"] = {
            field: {
                "jaccard_mean": report[field]["jaccard_mean"],
                "exact_share": report[field]["exact_share"],
                "vocab_jaccard": report[field]["vocab_jaccard"],
                "only_left": {c: v["units"] for c, v in report[field]["only_in_reference"].items()},
                "only_right": {c: v["units"] for c, v in report[field]["only_in_candidate"].items()},
                "top_only_left": [t for c in report[field]["only_in_reference"].values() for t in c["top"][:3]][:8],
                "top_only_right": [t for c in report[field]["only_in_candidate"].values() for t in c["top"][:3]][:8],
            }
            for field in FIELDS
        }
    pg_words = [w.strip() for w in (ROOT / "deps" / "pg_english.stop").read_text().splitlines() if w.strip()]
    body = {"tokenizer": "whitespace", "filter": [{"type": "stop", "stopwords": "_english_"}], "text": " ".join(pg_words)}
    lucene_kept = [t["token"] for t in requests.post(f"{ES}/_analyze", json=body).json()["tokens"]]
    with psycopg.connect(**PARADEDB) as conn:
        query = "SELECT (%s::text::pdb.whitespace('stopwords_language=english'))::text[]"
        pdb_kept = list(conn.execute(query, (" ".join(pg_words),)).fetchone()[0])
    lucene_drop = sorted(set(pg_words) - set(lucene_kept))
    pdb_drop = sorted(set(pg_words) - set(pdb_kept))
    out["stopwords"] = {
        "lucene_drops_of_pg127": lucene_drop,
        "paradedb_drops_of_pg127": pdb_drop,
        "identical": lucene_drop == pdb_drop,
    }
    (OUT / "es_vs_pdb.json").write_text(json.dumps(out, indent=1, ensure_ascii=False))
    for pair, fields in out.items():
        if pair != "stopwords":
            print(pair, {f: round(v["jaccard_mean"], 6) for f, v in fields.items()})
    print("stopword sets identical:", out["stopwords"]["identical"])


if __name__ == "__main__":
    main()
