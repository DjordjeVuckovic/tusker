"""M2 and M3: how far each engine chain's indexed terms agree with PG's, per field.

Two reference comparisons:
- tokenizer only: each lowercase-only chain against PG's `simple` config (PG parser, no
  stemming, no stopwords), so stemming and stopwords are out of the picture;
- full chain: each Snowball-English chain against PG's `english` config.

Per (article, field) unit: Jaccard similarity of the two term sets. Units where both sets are
empty are counted separately and left out of the Jaccard statistics. Corpus level: vocabulary
Jaccard over distinct terms. Disagreement: every term in one set and not the other, by class.

Writes out/agreement.json.
"""

import json
import re
import statistics
from collections import Counter

from common import FIELDS, OUT, ROOT, read_dump

TOKENIZER_ONLY = [
    "es.standard", "es.classic", "es.letter", "es.whitespace", "es.icu", "es.standard+apos",
    "pdb.unicode_words", "pdb.simple", "pdb.whitespace", "pdb.icu",
]
FULL_CHAIN = [
    "es.english", "es.standard+pgstop+snowball", "es.standard+apos+pgstop+snowball",
    "es.icu+pgstop+snowball", "es.standard+lucenestop+porter2",
    "pdb.unicode_words+stem+stop", "pdb.simple+stem+stop", "pdb.icu+stem+stop",
]

DOTTED_ABBREVIATION = re.compile(r"^(?:[a-z]\.)+[a-z]?\.?$")
HOST = re.compile(r"^[a-z0-9-]+(?:\.[a-z0-9-]+)+$")


def stopword_forms() -> set[str]:
    """PG's 127 stopwords and their Snowball stems on PG and ES, so a stopword that one engine
    keeps and stems (because -> becaus) is still recognised as one."""
    words = [w.strip() for w in (ROOT / "deps" / "pg_english.stop").read_text().splitlines() if w.strip()]
    stems = json.loads((OUT / "stems_engines.json").read_text())
    forms = set(words)
    for name in ("pg.snowball_english", "es.snowball_english"):
        for word in words:
            output = stems[name].get(word)
            forms.update(output if isinstance(output, list) else [output] if output else [])
    return forms


STOPWORD_FORMS = stopword_forms()


def term_class(term: str) -> str:
    if "@" in term:
        return "email"
    if "/" in term or term.startswith(("http", "www")):
        return "slash_or_url"
    if "'" in term or "’" in term:
        return "apostrophe"
    if not term.isascii():
        if any(ch.isalpha() and ord(ch) < 0x250 for ch in term):
            return "latin_diacritic"
        if any(ch.isalpha() for ch in term):
            return "non_latin_script"
        return "symbol_or_emoji"
    if DOTTED_ABBREVIATION.match(term):
        return "dotted_abbreviation"
    if any(ch.isdigit() for ch in term):
        return "contains_digit"
    if HOST.match(term):
        return "dotted_host"
    if "-" in term:
        return "hyphen"
    if "_" in term:
        return "underscore"
    if term.isalpha():
        return "stopword" if term in STOPWORD_FORMS else "alpha"
    return "other_punctuation"


def percentile(values: list[float], share: float) -> float:
    ordered = sorted(values)
    return ordered[max(0, int(share * len(ordered)) - 1)] if ordered else float("nan")


def compare(reference: dict, candidate: dict) -> dict:
    report = {}
    for field in FIELDS:
        jaccards, both_empty, exact = [], 0, 0
        vocab_ref, vocab_cand = Counter(), Counter()
        only_ref, only_cand = Counter(), Counter()
        for key, ref_row in reference.items():
            if key[1] != field:
                continue
            ref_terms, cand_terms = set(ref_row["terms"]), set(candidate[key]["terms"])
            vocab_ref.update(ref_terms)
            vocab_cand.update(cand_terms)
            if not ref_terms and not cand_terms:
                both_empty += 1
                continue
            union = ref_terms | cand_terms
            score = len(ref_terms & cand_terms) / len(union)
            jaccards.append(score)
            exact += score == 1.0
            only_ref.update(ref_terms - cand_terms)
            only_cand.update(cand_terms - ref_terms)
        shared = set(vocab_ref) & set(vocab_cand)
        everything = set(vocab_ref) | set(vocab_cand)

        def by_class(counter: Counter) -> dict:
            classes: dict[str, dict] = {}
            for term, units in counter.items():
                entry = classes.setdefault(term_class(term), {"terms": 0, "units": 0, "top": Counter()})
                entry["terms"] += 1
                entry["units"] += units
                entry["top"][term] = units
            return {
                name: {"terms": e["terms"], "units": e["units"], "top": e["top"].most_common(15)}
                for name, e in sorted(classes.items(), key=lambda item: -item[1]["units"])
            }

        report[field] = {
            "units": len(jaccards) + both_empty,
            "units_both_empty": both_empty,
            "jaccard_mean": statistics.fmean(jaccards) if jaccards else None,
            "jaccard_median": statistics.median(jaccards) if jaccards else None,
            "jaccard_p5": percentile(jaccards, 0.05),
            "exact_share": exact / len(jaccards) if jaccards else None,
            "vocab_ref": len(vocab_ref),
            "vocab_candidate": len(vocab_cand),
            "vocab_jaccard": len(shared) / len(everything) if everything else None,
            "only_in_reference": by_class(only_ref),
            "only_in_candidate": by_class(only_cand),
        }
    return report


def main() -> None:
    results = {"tokenizer_only": {}, "full_chain": {}}
    simple, english = read_dump("pg.simple"), read_dump("pg.english")
    for chain in TOKENIZER_ONLY:
        results["tokenizer_only"][chain] = compare(simple, read_dump(chain))
    for chain in FULL_CHAIN:
        results["full_chain"][chain] = compare(english, read_dump(chain))
    (OUT / "agreement.json").write_text(json.dumps(results, ensure_ascii=False, indent=1))
    for group, chains in results.items():
        print(f"== {group}")
        for chain, fields in chains.items():
            cells = "  ".join(
                f"{f[:5]} J={v['jaccard_mean']:.4f} exact={v['exact_share']:.3f} vocabJ={v['vocab_jaccard']:.4f}"
                for f, v in fields.items()
            )
            print(f"{chain:36s} {cells}")


if __name__ == "__main__":
    main()
