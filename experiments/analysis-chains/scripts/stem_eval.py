"""M1 and M4: stem identity across implementations, and the character of each stemmer.

M1 compares implementations that claim the same algorithm, word by word over the whole
sample A vocabulary.

M4 measures each stemmer on the words with document frequency >= 3: vocabulary reduction,
and Paice's (1994) under-stemming index UI, over-stemming index OI and stemming weight
SW = OI / UI, each against two lemma references (spaCy in context, WordNet morphy). A
reference is a tool's output, not ground truth.

Writes out/stem_eval.json.
"""

import json
from collections import Counter, defaultdict

import nltk

from common import OUT, ROOT

MIN_DF = 3


def load_stems() -> dict[str, dict[str, object]]:
    stems = json.loads((OUT / "stems_engines.json").read_text())
    for name in (
        "ref_pystemmer.json",
        "ref_pystemmer_2.2.0.3.json",
        "ref_snowballstemmer_2.2.0.json",
        "ref_snowballstemmer_3.0.1.json",
        "ref_nltk.json",
    ):
        stems.update(json.loads((OUT / name).read_text()))
    return stems


def single(output: object, word: str) -> str:
    """One stem per word. A list output with several entries (hunspell) keeps the word itself
    when it is among them, else the first; an empty list (a dictionary that does not know the
    word) or a missing output keeps the word. An empty string is a real stem: the 1980 Porter
    algorithm reduces `s` to it."""
    if isinstance(output, list):
        if not output:
            return word
        if len(output) > 1 and word in output:
            return word
        return output[0]
    return word if output is None else output


def paice(words: list[str], concept: dict[str, str], stem: dict[str, str]) -> dict:
    """Paice's understemming and overstemming indices over a word list."""
    total = len(words)
    groups: dict[str, list[str]] = defaultdict(list)
    for word in words:
        groups[concept[word]].append(word)
    gdmt = gumt = gdnt = 0.0
    for members in groups.values():
        n = len(members)
        gdmt += 0.5 * n * (n - 1)
        gdnt += 0.5 * n * (total - n)
        sizes = Counter(stem[w] for w in members).values()
        gumt += 0.5 * sum(u * (n - u) for u in sizes)
    stem_groups: dict[str, list[str]] = defaultdict(list)
    for word in words:
        stem_groups[stem[word]].append(word)
    gwmt = 0.0
    wrong = []
    for stem_value, members in stem_groups.items():
        size = len(members)
        sizes = Counter(concept[w] for w in members)
        wmt = 0.5 * sum(v * (size - v) for v in sizes.values())
        gwmt += wmt
        if wmt:
            wrong.append((wmt, stem_value, sorted(members)[:8], len(sizes)))
    ui = gumt / gdmt if gdmt else 0.0
    oi = gwmt / gdnt if gdnt else 0.0
    wrong.sort(key=lambda item: -item[0])
    return {
        "UI": ui,
        "OI": oi,
        "SW": oi / ui if ui else None,
        "worst_merges": [{"stem": s, "wmt": w, "concepts": c, "words": m} for w, s, m, c in wrong[:12]],
    }


def references(words: list[str]) -> dict[str, dict[str, str]]:
    spacy_data = json.loads((OUT / "spacy_lemmas.json").read_text())["lemmas"]
    nltk.data.path.insert(0, str(ROOT / "deps" / "nltk_data"))
    from nltk.corpus import wordnet

    spacy_ref = {w: max(spacy_data[w].items(), key=lambda kv: (kv[1], kv[0]))[0] for w in words}
    wordnet_ref = {w: (wordnet.morphy(w) or w) for w in words}
    return {"spacy": spacy_ref, "wordnet": wordnet_ref}


def identity(stems: dict, vocab: dict[str, int], pairs: list[tuple[str, str]]) -> list[dict]:
    total_df = sum(vocab.values())
    rows = []
    for left, right in pairs:
        differ = [w for w in vocab if stems[left][w] != stems[right][w]]
        rows.append({
            "left": left,
            "right": right,
            "words": len(vocab),
            "differ": len(differ),
            "differ_df_share": sum(vocab[w] for w in differ) / total_df,
            "examples": [(w, stems[left][w], stems[right][w], vocab[w]) for w in sorted(differ, key=lambda w: -vocab[w])[:15]],
        })
    return rows


def main() -> None:
    vocab = json.loads((OUT / "vocab.json").read_text())
    raw = load_stems()
    stems = {name: {w: single(out.get(w), w) for w in vocab} for name, out in raw.items()}

    porter2 = "pg.snowball_english"
    m1 = identity(stems, vocab, [
        (porter2, "es.snowball_english"),
        (porter2, "es.stemmer_porter2"),
        (porter2, "pdb.stemmer_english"),
        (porter2, "pystemmer-2.2.0.3.english"),
        (porter2, "snowballstemmer-2.2.0.english"),
        (porter2, "pystemmer-3.0.0.english"),
        (porter2, "snowballstemmer-3.0.1.english"),
        ("es.snowball_english", "es.stemmer_porter2"),
        ("es.snowball_english", "pdb.stemmer_english"),
        ("es.snowball_english", "snowballstemmer-2.2.0.english"),
        ("pystemmer-3.0.0.english", "snowballstemmer-3.0.1.english"),
        ("es.porter_stem", "es.stemmer_english"),
        ("es.porter_stem", "pystemmer-2.2.0.3.porter"),
        ("es.porter_stem", "nltk-3.9.1.porter.ORIGINAL_ALGORITHM"),
        ("es.porter_stem", "nltk-3.9.1.porter.MARTIN_EXTENSIONS"),
        ("es.porter_stem", "nltk-3.9.1.porter.NLTK_EXTENSIONS"),
        ("es.kstem", "es.stemmer_light_english"),
    ])
    hunspell_sets = [
        w for w in vocab
        if sorted(raw["pg.hunspell"].get(w) or [w]) != sorted(raw["es.hunspell"].get(w) or [w])
    ]
    ambiguous = {name: sum(1 for w in vocab if isinstance(raw[name].get(w), list) and len(raw[name][w]) > 1)
                 for name in ("pg.hunspell", "es.hunspell")}

    spacy_words = json.loads((OUT / "spacy_lemmas.json").read_text())["lemmas"]
    words = sorted(w for w, df in vocab.items() if df >= MIN_DF and w in spacy_words)
    refs = references(words)
    spacy_ambiguous = sum(1 for w in words if len(spacy_words[w]) > 1)

    candidates = {
        "none": {w: w for w in words},
        "S-stemmer (es minimal_english)": stems["es.stemmer_minimal_english"],
        "Krovetz (es kstem)": stems["es.kstem"],
        "Porter (es porter_stem)": stems["es.porter_stem"],
        "Porter2, Snowball 2.2 (es snowball)": stems["es.snowball_english"],
        "Porter2, PG 18.1 snapshot": stems["pg.snowball_english"],
        "Porter2, Snowball 3.0 (pystemmer 3.0.0)": stems["pystemmer-3.0.0.english"],
        "Lovins (es lovins)": stems["es.stemmer_lovins"],
        "hunspell en_US (pg)": stems["pg.hunspell"],
        "lemma: spaCy": refs["spacy"],
        "lemma: WordNet morphy": refs["wordnet"],
    }
    m4 = {}
    for name, mapping in candidates.items():
        mapped = {w: mapping[w] for w in words}
        entry = {
            "words": len(words),
            "classes": len(set(mapped.values())),
            "reduction": 1 - len(set(mapped.values())) / len(words),
        }
        for ref_name, ref in refs.items():
            entry[f"vs_{ref_name}"] = paice(words, ref, mapped)
        m4[name] = entry

    result = {
        "m1_identity": m1,
        "m1_hunspell_pg_vs_es_differ": len(hunspell_sets),
        "m1_hunspell_pg_vs_es_examples": [(w, raw["pg.hunspell"].get(w), raw["es.hunspell"].get(w)) for w in hunspell_sets[:20]],
        "m1_hunspell_multi_output_words": ambiguous,
        "m4_min_df": MIN_DF,
        "m4_words": len(words),
        "m4_spacy_words_with_several_lemmas": spacy_ambiguous,
        "m4": m4,
    }
    (OUT / "stem_eval.json").write_text(json.dumps(result, ensure_ascii=False, indent=1))

    for row in m1:
        print(f"{row['left']:28s} vs {row['right']:38s} differ {row['differ']:6d}/{row['words']}  df-share {100*row['differ_df_share']:.3f}%")
    print("hunspell pg vs es differ:", len(hunspell_sets), "multi-output:", ambiguous)
    print(f"\nM4 words={len(words)} (df>={MIN_DF}), spaCy words with >1 lemma: {spacy_ambiguous}")
    print(f"{'stemmer':42s} {'classes':>8s} {'reduct':>7s} | {'UI_sp':>6s} {'OI_sp':>9s} | {'UI_wn':>6s} {'OI_wn':>9s}")
    for name, e in m4.items():
        sp, wn = e["vs_spacy"], e["vs_wordnet"]
        print(f"{name:42s} {e['classes']:8d} {e['reduction']:7.3f} | {sp['UI']:6.3f} {sp['OI']:9.2e} | {wn['UI']:6.3f} {wn['OI']:9.2e}")


if __name__ == "__main__":
    main()
