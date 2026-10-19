"""M5 and M6: how much the analysis chain changes top-10 results, and what each chain costs.

Sample B (50,000 articles) is indexed on ES, one sub-field per chain on title, description
and content, and on PG, one weighted tsvector (A/B/C) per text search configuration.

Queries are a proxy: the first 2 to 4 word noun chunk (spaCy) of titles drawn, with a fixed
seed, from canonical lines outside sample B. Every query requires all its terms:
ES multi_match cross_fields with operator AND over the chain's three sub-fields, PG
plainto_tsquery ranked by ts_rank. Ties break on the article line so the order is total.
The queries hold no stopwords by construction, so they cannot show a query-side stopword
effect.

For each pair of chains on one engine: mean top-10 Jaccard over queries where both chains
return at least one hit, and extrapolated RBO (p = 0.9, Webber et al. 2010, eq. 32) over
queries where both return a full top 10, plus zero-hit counts. Divergence, not quality: nothing here says
which ranking is better.

Writes out/divergence.json and out/queries.json.
"""

import itertools
import json
import random

import psycopg
import requests
import spacy

from collect_es import CHAINS as ES_CHAINS
from collect_es import index_body as es_index_body
from collect_es import subfield
from common import ES, FIELDS, OUT, PG, ROOT, read_sample

SEED = 20261008
QUERY_COUNT = 300
INDEX = "ana_b"
ES_SELECTED = [
    "es.standard",
    "es.english",
    "es.standard+pgstop+snowball",
    "es.standard+apos+pgstop+snowball",
    "es.standard+lucenestop+porter2",
    "es.standard+lucenestop+porter",
    "es.standard+lucenestop+kstem",
    "es.standard+lucenestop+minimal",
    "es.standard+lucenestop+hunspell",
]
PG_CONFIGS = {"pg.english": "english", "pg.english_nostop": "english_nostop", "pg.simple": "simple", "pg.hunspell": "english_hunspell"}


def proxy_queries(excluded: set[int]) -> list[str]:
    source = ROOT.parent.parent / "datasets" / "cc-news" / "dataset-canonical.jsonl"
    titles = []
    with source.open(encoding="utf-8") as handle:
        for line_no, line in enumerate(handle):
            if line_no not in excluded:
                titles.append(json.loads(line)["title"])
    rng = random.Random(SEED)
    rng.shuffle(titles)
    nlp = spacy.load("en_core_web_sm", disable=["ner"])
    queries: list[str] = []
    seen: set[str] = set()
    for doc in nlp.pipe(titles[:5000], batch_size=256):
        for chunk in doc.noun_chunks:
            words = [t.text.lower() for t in chunk if t.is_alpha and not t.is_stop]
            if 2 <= len(words) <= 4:
                query = " ".join(words)
                if query not in seen:
                    seen.add(query)
                    queries.append(query)
                break
        if len(queries) == QUERY_COUNT:
            break
    return queries


def es_load(sample: list[dict]) -> None:
    existing = requests.get(f"{ES}/{INDEX}/_count")
    if existing.ok and existing.json()["count"] == len(sample):
        mapping = requests.get(f"{ES}/{INDEX}/_mapping").json()[INDEX]["mappings"]["properties"]
        if "line" in mapping:
            return
    requests.delete(f"{ES}/{INDEX}")
    body = es_index_body()
    body["mappings"]["properties"]["line"] = {"type": "integer"}
    requests.put(f"{ES}/{INDEX}", json=body).raise_for_status()
    for start in range(0, len(sample), 1000):
        lines = []
        for row in sample[start : start + 1000]:
            lines.append({"index": {"_index": INDEX, "_id": str(row["line"])}})
            lines.append({"line": row["line"], **{f: row[f] for f in FIELDS}})
        body = "\n".join(json.dumps(line) for line in lines) + "\n"
        result = requests.post(f"{ES}/_bulk", data=body, headers={"content-type": "application/x-ndjson"}).json()
        assert not result["errors"]
    requests.post(f"{ES}/{INDEX}/_refresh").raise_for_status()
    requests.post(f"{ES}/{INDEX}/_forcemerge?max_num_segments=1").raise_for_status()
    assert requests.get(f"{ES}/{INDEX}/_count").json()["count"] == len(sample)


def es_search(chain: str, query: str) -> list[int]:
    fields = [f"{f}.{subfield(chain)}" for f in FIELDS]
    body = {
        "size": 10,
        "_source": False,
        "query": {"multi_match": {"query": query, "type": "cross_fields", "operator": "and", "fields": fields}},
        "sort": [{"_score": "desc"}, {"line": "asc"}],
    }
    response = requests.post(f"{ES}/{INDEX}/_search?request_cache=false", json=body)
    response.raise_for_status()
    return [int(hit["_id"]) for hit in response.json()["hits"]["hits"]]


def pg_load(conn: psycopg.Connection, sample: list[dict]) -> None:
    """All tsvector columns are computed in one INSERT and indexed afterwards, so no index
    carries dead entries from a later column update and the GIN sizes are comparable."""
    conn.execute("DROP TABLE IF EXISTS ana_b_raw")
    conn.execute("DROP TABLE IF EXISTS ana_b")
    conn.execute("CREATE TABLE ana_b_raw (line int PRIMARY KEY, title text, description text, content text)")
    with conn.cursor().copy("COPY ana_b_raw (line, title, description, content) FROM STDIN") as copy:
        for row in sample:
            copy.write_row((row["line"], row["title"], row["description"], row["content"]))
    columns = [f"tsv_{name.split('.')[1]}" for name in PG_CONFIGS]
    expressions = [
        f"setweight(to_tsvector('{config}', title), 'A') || "
        f"setweight(to_tsvector('{config}', description), 'B') || "
        f"setweight(to_tsvector('{config}', content), 'C')"
        for config in PG_CONFIGS.values()
    ]
    conn.execute(
        f"CREATE TABLE ana_b AS SELECT line, title, description, content, "
        + ", ".join(f"{e} AS {c}" for e, c in zip(expressions, columns, strict=True))
        + " FROM ana_b_raw"
    )
    conn.execute("ALTER TABLE ana_b ADD PRIMARY KEY (line)")
    conn.execute("DROP TABLE ana_b_raw")
    for column in columns:
        conn.execute(f"CREATE INDEX ana_b_{column} ON ana_b USING gin ({column})")
    conn.execute("VACUUM ANALYZE ana_b")


def pg_search(conn: psycopg.Connection, name: str, query: str) -> list[int]:
    column, config = f"tsv_{name.split('.')[1]}", PG_CONFIGS[name]
    rows = conn.execute(
        f"SELECT line FROM ana_b, plainto_tsquery('{config}', %s) q WHERE {column} @@ q "
        f"ORDER BY ts_rank({column}, q) DESC, line ASC LIMIT 10",
        (query,),
    ).fetchall()
    return [row[0] for row in rows]


def rbo_extrapolated(left: list[int], right: list[int], p: float = 0.9) -> float:
    """Webber et al. (2010) eq. 32 for two lists of equal length k; identical lists score 1."""
    k = len(left)
    assert k == len(right) and k > 0
    total = 0.0
    for d in range(1, k + 1):
        total += len(set(left[:d]) & set(right[:d])) / d * p**d
    overlap_k = len(set(left) & set(right))
    return overlap_k / k * p**k + (1 - p) / p * total


def compare(results: dict[str, list[list[int]]]) -> list[dict]:
    rows = []
    for left, right in itertools.combinations(results, 2):
        jaccards, rbos = [], []
        for a, b in zip(results[left], results[right], strict=True):
            if a and b:
                jaccards.append(len(set(a) & set(b)) / len(set(a) | set(b)))
            if len(a) == 10 and len(b) == 10:
                rbos.append(rbo_extrapolated(a, b))
        rows.append({
            "left": left,
            "right": right,
            "queries_both_nonempty": len(jaccards),
            "top10_jaccard_mean": sum(jaccards) / len(jaccards) if jaccards else None,
            "queries_both_full": len(rbos),
            "rbo_ext_mean": sum(rbos) / len(rbos) if rbos else None,
            "identical_top10_share": sum(1 for j in jaccards if j == 1.0) / len(jaccards) if jaccards else None,
        })
    return rows


def costs(conn: psycopg.Connection) -> dict:
    es_usage = requests.post(f"{ES}/{INDEX}/_disk_usage?run_expensive_tasks=true").json()
    fields = es_usage[INDEX]["fields"]
    es_bytes = {
        chain: sum(fields.get(f"{f}.{subfield(chain)}", {}).get("total_in_bytes", 0) for f in FIELDS)
        for chain in ES_SELECTED
    }
    pg_bytes = {}
    for name in PG_CONFIGS:
        column = f"tsv_{name.split('.')[1]}"
        column_bytes = conn.execute(f"SELECT sum(pg_column_size({column})) FROM ana_b").fetchone()[0]
        index_bytes = conn.execute("SELECT pg_relation_size(%s)", (f"ana_b_{column}",)).fetchone()[0]
        pg_bytes[name] = {"tsvector_bytes": int(column_bytes), "gin_bytes": int(index_bytes)}
    return {"es_field_bytes": es_bytes, "pg": pg_bytes}


def main() -> None:
    sample = read_sample("b")
    queries = proxy_queries({row["line"] for row in sample})
    (OUT / "queries.json").write_text(json.dumps({"seed": SEED, "queries": queries}, indent=1))

    es_load(sample)
    es_results = {chain: [es_search(chain, q) for q in queries] for chain in ES_SELECTED}
    with psycopg.connect(**PG, autocommit=True) as conn:
        pg_load(conn, sample)
        pg_results = {name: [pg_search(conn, name, q) for q in queries] for name in PG_CONFIGS}
        cost = costs(conn)

    result = {
        "queries": len(queries),
        "zero_hits": {chain: sum(1 for r in rs if not r) for chain, rs in {**es_results, **pg_results}.items()},
        "es_pairs": compare(es_results),
        "pg_pairs": compare(pg_results),
        "cost": cost,
    }
    (OUT / "divergence.json").write_text(json.dumps(result, indent=1))
    (OUT / "divergence_results.json").write_text(json.dumps({"queries": queries, "results": {**es_results, **pg_results}}))
    print("queries", len(queries), "zero hits", result["zero_hits"])
    for row in result["es_pairs"] + result["pg_pairs"]:
        print(f"{row['left']:34s} {row['right']:34s} n={row['queries_both_nonempty']:3d} J={row['top10_jaccard_mean']:.3f} nfull={row['queries_both_full']:3d} RBOext={row['rbo_ext_mean']:.3f} same={row['identical_top10_share']:.3f}")
    print(json.dumps(cost, indent=1))


if __name__ == "__main__":
    main()
