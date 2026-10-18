"""Stem the vocabulary with one reference library, run in its own pinned environment.

Usage: ref_stem.py <library> <out.json>, where library is pystemmer, snowballstemmer or nltk.
Records the library version next to the outputs, so two Snowball releases can be compared.
"""

import importlib.metadata
import json
import sys
from pathlib import Path

OUT = Path(__file__).resolve().parent.parent / "out"


def main(library: str, out_path: str) -> None:
    words = list(json.loads((OUT / "vocab.json").read_text()))
    result: dict[str, dict] = {}
    if library == "pystemmer":
        import Stemmer

        version = importlib.metadata.version("PyStemmer")
        for algorithm in ("english", "porter"):
            stemmer = Stemmer.Stemmer(algorithm)
            result[f"pystemmer-{version}.{algorithm}"] = dict(zip(words, stemmer.stemWords(words), strict=True))
    elif library == "snowballstemmer":
        import snowballstemmer

        version = importlib.metadata.version("snowballstemmer")
        for algorithm in ("english", "porter"):
            stemmer = snowballstemmer.stemmer(algorithm)
            result[f"snowballstemmer-{version}.{algorithm}"] = dict(zip(words, stemmer.stemWords(words), strict=True))
    elif library == "nltk":
        from nltk.stem.porter import PorterStemmer

        version = importlib.metadata.version("nltk")
        for mode in (PorterStemmer.ORIGINAL_ALGORITHM, PorterStemmer.MARTIN_EXTENSIONS, PorterStemmer.NLTK_EXTENSIONS):
            stemmer = PorterStemmer(mode=mode)
            result[f"nltk-{version}.porter.{mode}"] = {word: stemmer.stem(word, to_lowercase=False) for word in words}
    else:
        raise SystemExit(f"unknown library {library}")
    Path(out_path).write_text(json.dumps(result))
    print(library, list(result))


if __name__ == "__main__":
    main(sys.argv[1], sys.argv[2])
