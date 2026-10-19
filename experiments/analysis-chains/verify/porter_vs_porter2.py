"""Count vocabulary words that Porter and Porter2 stem differently, df-weighted.

Usage: porter_vs_porter2.py [nltk.json sbs.json]

Without arguments it compares the live engine stems (ES `porter_stem` against ES `snowball`).
With the outputs of `full_ref.py nltk` and `full_ref.py sbs` it repeats the count on the isolated
reference stemmers (NLTK MARTIN_EXTENSIONS against snowballstemmer 2.2.0).
"""

import json
import sys
from pathlib import Path

OUT = Path(__file__).resolve().parent.parent / "out"
# Lucene EnglishAnalyzer.ENGLISH_STOP_WORDS_SET: the built-in english analyzer drops these before stemming.
LUCENE_STOPWORDS = set(
    "a an and are as at be but by for if in into is it no not of on or such that the their then "
    "there these they this to was will with".split()
)


def first(stem):
    return stem[0] if isinstance(stem, list) and stem else stem


def compare(porter, porter2, df):
    differ = [w for w in df if first(porter[w]) != first(porter2[w])]
    differ.sort(key=lambda w: df[w], reverse=True)
    share = sum(df[w] for w in differ) / sum(df.values())
    print(f"{len(differ)} of {len(df)} words differ, {share:.3%} df-weighted")
    kept = [w for w in df if w not in LUCENE_STOPWORDS]
    kept_differ = [w for w in differ if w not in LUCENE_STOPWORDS]
    kept_share = sum(df[w] for w in kept_differ) / sum(df[w] for w in kept)
    print(f"without Lucene's 33 stopwords: {len(kept_differ)} words, {kept_share:.3%} df-weighted")
    for w in kept_differ[:12] + ["use", "used", "us"]:
        print(f"  {w}: {first(porter[w])} / {first(porter2[w])}")


df = json.load(open(OUT / "vocab.json"))
if len(sys.argv) == 3:
    porter = json.load(open(sys.argv[1]))["res"]["MARTIN_EXTENSIONS"]
    porter2 = json.load(open(sys.argv[2]))["res"]
else:
    engines = json.load(open(OUT / "stems_engines.json"))
    porter, porter2 = engines["es.porter_stem"], engines["es.snowball_english"]
compare(porter, porter2, df)
