"""Lemmatize sample A in context with spaCy and record, per lowercase word, its lemma counts.

spaCy's lemmatizer uses the part-of-speech tag, so one surface word can receive different
lemmas in different sentences ("saw" as a noun or a verb). Each word's lemma for the stemmer
comparison is its most frequent one; the ambiguity is kept so it can be reported.
Only lowercase ASCII-alphabetic tokens are recorded, matching the stem vocabulary.

Writes out/spacy_lemmas.json: word -> {lemma: count}, plus the model and spaCy versions.
"""

import importlib.metadata
import json
from collections import Counter, defaultdict

import spacy

from common import FIELDS, OUT, read_sample

MAX_CHARS = 100_000


def main() -> None:
    nlp = spacy.load("en_core_web_sm", disable=["parser", "ner"])
    texts = [row[field][:MAX_CHARS] for row in read_sample("a") for field in FIELDS if row[field]]
    lemmas: dict[str, Counter] = defaultdict(Counter)
    for doc in nlp.pipe(texts, batch_size=256, n_process=6):
        for token in doc:
            word = token.text.lower()
            if word.isascii() and word.isalpha():
                lemmas[word][token.lemma_.lower()] += 1
    result = {
        "spacy": importlib.metadata.version("spacy"),
        "model": importlib.metadata.version("en_core_web_sm"),
        "texts": len(texts),
        "max_chars": MAX_CHARS,
        "lemmas": {word: dict(counts) for word, counts in sorted(lemmas.items())},
    }
    (OUT / "spacy_lemmas.json").write_text(json.dumps(result))
    print("words", len(lemmas), "texts", len(texts))


if __name__ == "__main__":
    main()
