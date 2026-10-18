"""Draw 50 random term disagreements for manual review of the class assigned to each.

25 from the ES chain closest to PG and 25 from ParadeDB's unicode_words chain, both against
PG's english config, all fields. Prints each term with its side, its automatic class and a
window of the source text around the first case-insensitive match of its first four
characters. Writes out/review_sample.json.
"""

import json
import random
import re

from agreement import term_class
from common import OUT, read_dump, read_sample

SEED = 20261008
COMPARISONS = ["es.standard+apos+pgstop+snowball", "pdb.unicode_words+stem+stop"]


def main() -> None:
    texts = {(row["line"], field): row[field] for row in read_sample("a") for field in ("title", "description", "content")}
    reference = read_dump("pg.english")
    rng = random.Random(SEED)
    picks = []
    for chain in COMPARISONS:
        candidate = read_dump(chain)
        instances = []
        for key, ref_row in reference.items():
            ref_terms, cand_terms = set(ref_row["terms"]), set(candidate[key]["terms"])
            instances += [(key, term, "only_pg") for term in sorted(ref_terms - cand_terms)]
            instances += [(key, term, f"only_{chain}") for term in sorted(cand_terms - ref_terms)]
        for key, term, side in rng.sample(instances, 25):
            text = texts[key]
            match = re.search(re.escape(term[:4]), text, re.IGNORECASE)
            start = max(0, (match.start() if match else 0) - 50)
            picks.append({
                "chain": chain,
                "line": key[0],
                "field": key[1],
                "term": term,
                "side": side,
                "class": term_class(term),
                "context": text[start : start + 120].replace("\n", " "),
            })
    (OUT / "review_sample.json").write_text(json.dumps(picks, indent=1, ensure_ascii=False))
    for i, p in enumerate(picks, 1):
        print(f"{i:2d} {p['side'][:12]:12s} {p['class']:20s} {p['term']!r:28s} …{p['context']}…")


if __name__ == "__main__":
    main()
