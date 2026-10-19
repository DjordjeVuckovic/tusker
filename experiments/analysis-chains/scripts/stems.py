"""Stem every word of the sample A vocabulary one word at a time, on every engine and library.

The vocabulary is each distinct lowercase ASCII-alphabetic lexeme of PG's `simple` config over
sample A (PG's parser, no stemming, no stopwords). Its weight is document frequency over
(article, field) units. Feeding single words removes the tokenizer from the comparison, so any
difference left is the stemmer itself.

Writes out/vocab.json (word -> df) and out/stems_engines.json (stemmer -> word -> outputs).
"""

import json

import psycopg
import requests

from common import ES, OUT, PARADEDB, PG, read_dump

BATCH = 5000
ES_GAP = 101  # _analyze puts position_increment_gap (100) + 1 between array elements


def vocabulary() -> dict[str, int]:
    df: dict[str, int] = {}
    for row in read_dump("pg.simple").values():
        for term in row["terms"]:
            if term.isascii() and term.isalpha():
                df[term] = df.get(term, 0) + 1
    return dict(sorted(df.items()))


def pg_lexize(conn: psycopg.Connection, dictionary: str, words: list[str]) -> dict[str, list[str]]:
    rows = conn.execute(
        "SELECT w, ts_lexize(%s::regdictionary, w) FROM unnest(%s::text[]) AS w",
        (dictionary, words),
    ).fetchall()
    return {word: (list(lexemes) if lexemes is not None else None) for word, lexemes in rows}


def es_analyze(filters: list, words: list[str]) -> dict[str, list[str]]:
    out: dict[str, list[str]] = {word: [] for word in words}
    for start in range(0, len(words), BATCH):
        chunk = words[start : start + BATCH]
        body = {"tokenizer": "keyword", "filter": filters, "text": chunk}
        response = requests.post(f"{ES}/_analyze", json=body)
        response.raise_for_status()
        for token in response.json()["tokens"]:
            index, remainder = divmod(token["position"], ES_GAP)
            assert remainder == 0, token
            out[chunk[index]].append(token["token"])
    return out


def pdb_stem(conn: psycopg.Connection, words: list[str]) -> dict[str, list[str]]:
    rows = conn.execute(
        "SELECT w, w::pdb.simple('stemmer=english')::text[] FROM unnest(%s::text[]) AS w",
        (words,),
    ).fetchall()
    return {word: list(tokens) for word, tokens in rows}


def main() -> None:
    vocab = vocabulary()
    words = list(vocab)
    (OUT / "vocab.json").write_text(json.dumps(vocab))

    stems: dict[str, dict] = {}
    with psycopg.connect(**PG, autocommit=True) as conn:
        stems["pg.snowball_english"] = pg_lexize(conn, "english_stem_nostop", words)
        stems["pg.hunspell"] = pg_lexize(conn, "english_hunspell_nostop", words)
    with psycopg.connect(**PARADEDB, autocommit=True) as conn:
        stems["pdb.stemmer_english"] = pdb_stem(conn, words)

    es_filters = {
        "es.snowball_english": [{"type": "snowball", "language": "English"}],
        "es.stemmer_porter2": [{"type": "stemmer", "language": "porter2"}],
        "es.stemmer_english": [{"type": "stemmer", "language": "english"}],
        "es.porter_stem": ["porter_stem"],
        "es.kstem": ["kstem"],
        "es.stemmer_light_english": [{"type": "stemmer", "language": "light_english"}],
        "es.stemmer_minimal_english": [{"type": "stemmer", "language": "minimal_english"}],
        "es.stemmer_lovins": [{"type": "stemmer", "language": "lovins"}],
        "es.hunspell": [{"type": "hunspell", "locale": "en_US", "dedup": True}],
    }
    for name, filters in es_filters.items():
        stems[name] = es_analyze(filters, words)

    (OUT / "stems_engines.json").write_text(json.dumps(stems, ensure_ascii=False))
    print("words", len(words), "stemmers", list(stems))


if __name__ == "__main__":
    main()
