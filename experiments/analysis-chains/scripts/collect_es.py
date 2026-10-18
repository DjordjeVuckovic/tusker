"""Collect Elasticsearch terms for sample A by indexing it and reading term vectors back.

Each chain is a sub-field of title, description and content with its own analyzer, so the
terms come from the same analysis path a real index uses. n is the number of distinct token
positions, so hunspell's several lemmas at one position count once.
"""

import sys

import requests

from common import ES, FIELDS, ROOT, read_sample, write_dump

INDEX = "ana_a"
PG_STOPWORDS = [w.strip() for w in (ROOT / "deps" / "pg_english.stop").read_text().splitlines() if w.strip()]

CHAINS = {
    "es.standard": {"tokenizer": "standard", "filter": ["lowercase"]},
    "es.classic": {"tokenizer": "classic", "filter": ["lowercase"]},
    "es.letter": {"tokenizer": "letter", "filter": ["lowercase"]},
    "es.whitespace": {"tokenizer": "whitespace", "filter": ["lowercase"]},
    "es.icu": {"tokenizer": "icu_tokenizer", "filter": ["lowercase"]},
    "es.standard+apos": {"char_filter": ["apos"], "tokenizer": "standard", "filter": ["lowercase"]},
    "es.english": None,
    "es.standard+pgstop+snowball": {"tokenizer": "standard", "filter": ["lowercase", "pg_stop", "snowball_en"]},
    "es.standard+apos+pgstop+snowball": {
        "char_filter": ["apos"],
        "tokenizer": "standard",
        "filter": ["lowercase", "pg_stop", "snowball_en"],
    },
    "es.icu+pgstop+snowball": {"tokenizer": "icu_tokenizer", "filter": ["lowercase", "pg_stop", "snowball_en"]},
    "es.standard+lucenestop+porter2": {"tokenizer": "standard", "filter": ["lowercase", "lucene_stop", "porter2"]},
    "es.standard+lucenestop+porter": {"tokenizer": "standard", "filter": ["lowercase", "lucene_stop", "porter_stem"]},
    "es.standard+lucenestop+kstem": {"tokenizer": "standard", "filter": ["lowercase", "lucene_stop", "kstem"]},
    "es.standard+lucenestop+minimal": {"tokenizer": "standard", "filter": ["lowercase", "lucene_stop", "minimal_en"]},
    "es.standard+lucenestop+lovins": {"tokenizer": "standard", "filter": ["lowercase", "lucene_stop", "lovins"]},
    "es.standard+lucenestop+hunspell": {"tokenizer": "standard", "filter": ["lowercase", "lucene_stop", "hunspell_en"]},
}


def subfield(chain: str) -> str:
    return "c_" + chain.removeprefix("es.").replace("+", "_")


def index_body() -> dict:
    analyzers = {
        subfield(chain): {"type": "custom", **definition}
        for chain, definition in CHAINS.items()
        if definition is not None
    }
    analysis = {
        "char_filter": {"apos": {"type": "mapping", "mappings": ["' => \\u0020", "’ => \\u0020"]}},
        "filter": {
            "pg_stop": {"type": "stop", "stopwords": PG_STOPWORDS},
            "lucene_stop": {"type": "stop", "stopwords": "_english_"},
            "snowball_en": {"type": "snowball", "language": "English"},
            "porter2": {"type": "stemmer", "language": "porter2"},
            "minimal_en": {"type": "stemmer", "language": "minimal_english"},
            "lovins": {"type": "stemmer", "language": "lovins"},
            "hunspell_en": {"type": "hunspell", "locale": "en_US", "dedup": True},
        },
        "analyzer": analyzers,
    }
    multi = {
        subfield(chain): {"type": "text", "analyzer": "english" if definition is None else subfield(chain)}
        for chain, definition in CHAINS.items()
    }
    field = {"type": "text", "fields": multi}
    return {
        "settings": {"number_of_shards": 1, "number_of_replicas": 0, "analysis": analysis},
        "mappings": {"properties": {f: field for f in FIELDS}},
    }


def load(sample: list[dict]) -> None:
    requests.delete(f"{ES}/{INDEX}")
    response = requests.put(f"{ES}/{INDEX}", json=index_body())
    response.raise_for_status()
    batch = 500
    for start in range(0, len(sample), batch):
        lines = []
        for row in sample[start : start + batch]:
            lines.append({"index": {"_index": INDEX, "_id": str(row["line"])}})
            lines.append({f: row[f] for f in FIELDS})
        body = "\n".join(requests.compat.json.dumps(line) for line in lines) + "\n"
        result = requests.post(f"{ES}/_bulk", data=body, headers={"content-type": "application/x-ndjson"}).json()
        assert not result["errors"], result
    requests.post(f"{ES}/{INDEX}/_refresh").raise_for_status()
    count = requests.get(f"{ES}/{INDEX}/_count").json()["count"]
    assert count == len(sample), count


def term_vectors(lines: list[int], fields: list[str]) -> dict:
    body = {
        "ids": [str(line) for line in lines],
        "parameters": {
            "fields": fields,
            "positions": True,
            "offsets": False,
            "payloads": False,
            "term_statistics": False,
            "field_statistics": False,
        },
    }
    response = requests.post(f"{ES}/{INDEX}/_mtermvectors", json=body)
    response.raise_for_status()
    return {int(doc["_id"]): doc.get("term_vectors", {}) for doc in response.json()["docs"]}


def collect(chain: str, sample: list[dict]):
    batch = 100
    fields = [f"{f}.{subfield(chain)}" for f in FIELDS]
    for start in range(0, len(sample), batch):
        chunk = sample[start : start + batch]
        vectors = term_vectors([row["line"] for row in chunk], fields)
        for row in chunk:
            for field in FIELDS:
                terms = vectors[row["line"]].get(f"{field}.{subfield(chain)}", {}).get("terms", {})
                positions = {token["position"] for term in terms.values() for token in term["tokens"]}
                yield {"line": row["line"], "field": field, "terms": sorted(terms), "n": len(positions)}


def main(chains: list[str]) -> None:
    sample = read_sample("a")
    load(sample)
    for chain in chains or CHAINS:
        print(chain, write_dump(chain, collect(chain, sample)))


if __name__ == "__main__":
    main(sys.argv[1:])
